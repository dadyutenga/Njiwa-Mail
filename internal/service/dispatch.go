package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Claimed is a message leased to a worker. Payload is still encrypted.
type Claimed struct {
	ID        string
	Token     string
	Attempts  int
	Payload   []byte
	ExpiresAt time.Time
	Template  string
	Version   int
	Mode      string
}

// Claim atomically leases due messages (FOR UPDATE SKIP LOCKED) in a single
// committed statement, so no network I/O ever happens under a row lock.
// Messages whose deadline has passed are never claimed.
func (s *Service) Claim(ctx context.Context, limit int, lease time.Duration, modes []string) ([]Claimed, error) {
	rows, err := s.Pool.Query(ctx, `
WITH c AS (
  SELECT m.id FROM messages m JOIN environments e ON e.id = m.environment_id
  WHERE m.state IN ('queued','retry_wait') AND m.next_attempt_at <= now() AND m.expires_at > now() AND e.mode = ANY($1)
  ORDER BY m.next_attempt_at LIMIT $2
  FOR UPDATE OF m SKIP LOCKED),
u AS (
  UPDATE messages m SET state='submitting', attempts = m.attempts + 1, lease_token = gen_random_uuid(),
         lease_expires_at = now() + make_interval(secs => $3::float8), updated_at = now()
  FROM c WHERE m.id = c.id
  RETURNING m.id, m.lease_token, m.attempts, m.payload_enc, m.template, m.template_version, m.environment_id, m.expires_at),
ev AS (INSERT INTO message_events(message_id, event_type) SELECT id, 'submitting' FROM u)
SELECT u.id::text, u.lease_token::text, u.attempts, u.payload_enc, u.template, u.template_version, u.expires_at, e.mode
FROM u JOIN environments e ON e.id = u.environment_id`, modes, limit, lease.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Claimed
	for rows.Next() {
		var c Claimed
		if err := rows.Scan(&c.ID, &c.Token, &c.Attempts, &c.Payload, &c.Template, &c.Version, &c.ExpiresAt, &c.Mode); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// OpenPayload decrypts a claimed message's content.
func (s *Service) OpenPayload(c Claimed) (*Payload, error) {
	if len(c.Payload) == 0 {
		return nil, errors.New("payload missing")
	}
	pt, err := s.Box.Open(c.Payload, []byte(c.ID))
	if err != nil {
		return nil, err
	}
	var p Payload
	if err := json.Unmarshal(pt, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

type finishOpts struct {
	from       []string
	to         string
	event      string
	reason     string
	clear      bool // clear encrypted payload
	complete   bool
	reply      *string
	messageID  *string
	retryAfter *float64
	extra      func(ctx context.Context, tx pgx.Tx) error
}

// finish applies a lease-token-guarded transition. It reports false when the
// lease was lost (another actor moved the row), in which case nothing changes.
func (s *Service) finish(ctx context.Context, id, token string, o finishOpts) (bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
UPDATE messages SET state=$3::text,
  payload_enc = CASE WHEN $4::bool THEN NULL ELSE payload_enc END,
  completed_at = CASE WHEN $5::bool THEN now() ELSE completed_at END,
  submitted_at = CASE WHEN $3::text = 'submitted' THEN now() ELSE submitted_at END,
  smtp_reply = COALESCE($6::text, smtp_reply),
  message_id_header = COALESCE($7::text, message_id_header),
  lease_expires_at = CASE WHEN $3::text = 'submitting' THEN lease_expires_at ELSE NULL END,
  next_attempt_at = CASE WHEN $8::float8 IS NULL THEN next_attempt_at ELSE now() + make_interval(secs => $8::float8) END,
  updated_at = now()
WHERE id=$1 AND lease_token=$2::uuid AND state = ANY($9)`,
		id, token, o.to, o.clear, o.complete, o.reply, o.messageID, o.retryAfter, o.from)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO message_events(message_id, event_type, reason_code) VALUES ($1,$2,$3)`, id, o.event, o.reason); err != nil {
		return false, err
	}
	if o.extra != nil {
		if err := o.extra(ctx, tx); err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}

var submittingOnly = []string{"submitting"}

// MarkSubmitted records positive SMTP acknowledgement. It may also upgrade a
// row that a reaper already marked unknown after the lease expired, since a
// late definitive acknowledgement is strictly better evidence.
func (s *Service) MarkSubmitted(ctx context.Context, c Claimed, reason, reply, messageID string) (bool, error) {
	return s.finish(ctx, c.ID, c.Token, finishOpts{from: []string{"submitting", "unknown"}, to: "submitted", event: "submitted",
		reason: reason, clear: true, complete: true, reply: &reply, messageID: &messageID})
}

func (s *Service) MarkFailed(ctx context.Context, c Claimed, reason string) (bool, error) {
	return s.finish(ctx, c.ID, c.Token, finishOpts{from: submittingOnly, to: "failed", event: "failed", reason: reason, clear: true, complete: true})
}

func (s *Service) MarkUnknown(ctx context.Context, c Claimed, reason string) (bool, error) {
	return s.finish(ctx, c.ID, c.Token, finishOpts{from: submittingOnly, to: "unknown", event: "unknown", reason: reason, clear: true, complete: true})
}

func (s *Service) MarkExpired(ctx context.Context, c Claimed, reason string) (bool, error) {
	return s.finish(ctx, c.ID, c.Token, finishOpts{from: submittingOnly, to: "expired", event: "expired", reason: reason, clear: true, complete: true})
}

// MarkCaptured stores the sandbox capture (encrypted, auto-expiring) and ends
// the message. No external email is sent.
func (s *Service) MarkCaptured(ctx context.Context, c Claimed, raw []byte, messageID string) (bool, error) {
	enc, err := s.Box.Seal(raw, []byte("capture:"+c.ID))
	if err != nil {
		return false, err
	}
	return s.finish(ctx, c.ID, c.Token, finishOpts{from: submittingOnly, to: "captured", event: "captured", reason: "sandbox", clear: true, complete: true,
		messageID: &messageID,
		extra: func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO sandbox_captures(message_id, raw_enc, expires_at) VALUES ($1,$2, now() + make_interval(secs => $3::float8))`,
				c.ID, enc, s.Opts.CaptureRetention.Seconds())
			return err
		}})
}

// ScheduleRetry moves a known-safe temporary failure to retry_wait, or to
// failed when attempts are exhausted, or to expired when the next attempt
// would fall beyond the message deadline.
func (s *Service) ScheduleRetry(ctx context.Context, c Claimed, reason string, delay time.Duration) (string, error) {
	if c.Attempts >= s.Opts.MaxAttempts {
		_, err := s.finish(ctx, c.ID, c.Token, finishOpts{from: submittingOnly, to: "failed", event: "failed", reason: "retries_exhausted:" + reason, clear: true, complete: true})
		return "failed", err
	}
	if !time.Now().Add(delay).Before(c.ExpiresAt) {
		_, err := s.finish(ctx, c.ID, c.Token, finishOpts{from: submittingOnly, to: "expired", event: "expired", reason: "retry_beyond_ttl", clear: true, complete: true})
		return "expired", err
	}
	secs := delay.Seconds()
	_, err := s.finish(ctx, c.ID, c.Token, finishOpts{from: submittingOnly, to: "retry_wait", event: "retry_scheduled", reason: reason, retryAfter: &secs})
	return "retry_wait", err
}

// ReapLeases moves submissions whose lease expired to unknown. A worker that
// died after starting SMTP may have had the message accepted, so we never
// retry automatically.
func (s *Service) ReapLeases(ctx context.Context) (int, error) {
	tag, err := s.Pool.Exec(ctx, `
WITH u AS (UPDATE messages SET state='unknown', payload_enc=NULL, completed_at=now(), updated_at=now()
           WHERE state='submitting' AND lease_expires_at < now() RETURNING id)
INSERT INTO message_events(message_id, event_type, reason_code) SELECT id, 'unknown', 'lease_expired' FROM u`)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// ExpireDue expires queued/retry_wait messages whose deadline passed.
func (s *Service) ExpireDue(ctx context.Context) (int, error) {
	tag, err := s.Pool.Exec(ctx, `
WITH u AS (UPDATE messages SET state='expired', payload_enc=NULL, completed_at=now(), updated_at=now()
           WHERE state IN ('queued','retry_wait') AND expires_at <= now() RETURNING id)
INSERT INTO message_events(message_id, event_type, reason_code) SELECT id, 'expired', 'ttl_elapsed' FROM u`)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (s *Service) Heartbeat(ctx context.Context, workerID string) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO worker_heartbeats(worker_id, last_seen) VALUES ($1, now())
ON CONFLICT (worker_id) DO UPDATE SET last_seen = now()`, workerID)
	return err
}

// WorkerHealthy reports whether any worker has checked in within maxAge.
func (s *Service) WorkerHealthy(ctx context.Context, maxAge time.Duration) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM worker_heartbeats WHERE last_seen > now() - make_interval(secs => $1::float8))`, maxAge.Seconds()).Scan(&ok)
	return ok, err
}

type RetentionResult struct {
	Captures, Buckets, Messages, Payloads, Heartbeats int64
}

// RunRetention deletes expired captures, stale rate buckets, old terminal
// message metadata, and any payload left on a non-active row past the purge window.
func (s *Service) RunRetention(ctx context.Context) (RetentionResult, error) {
	var r RetentionResult
	run := func(dst *int64, q string, args ...any) error {
		tag, err := s.Pool.Exec(ctx, q, args...)
		if err != nil {
			return err
		}
		*dst = tag.RowsAffected()
		return nil
	}
	if err := run(&r.Captures, `DELETE FROM sandbox_captures WHERE expires_at < now()`); err != nil {
		return r, err
	}
	if err := run(&r.Buckets, `DELETE FROM rate_limit_buckets WHERE window_start < now() - interval '2 days'`); err != nil {
		return r, err
	}
	if err := run(&r.Payloads, `UPDATE messages SET payload_enc=NULL WHERE payload_enc IS NOT NULL
        AND state NOT IN ('queued','retry_wait','submitting') AND updated_at < now() - make_interval(secs => $1::float8)`, s.Opts.UnknownPurge.Seconds()); err != nil {
		return r, err
	}
	if err := run(&r.Messages, `DELETE FROM messages WHERE state NOT IN ('queued','retry_wait','submitting')
        AND created_at < now() - make_interval(secs => $1::float8)`, s.Opts.MetadataRetention.Seconds()); err != nil {
		return r, err
	}
	if err := run(&r.Heartbeats, `DELETE FROM worker_heartbeats WHERE last_seen < now() - interval '1 day'`); err != nil {
		return r, err
	}
	return r, nil
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/njiwa-tz/mail/internal/db"
	"time"

	"github.com/njiwa-tz/mail/internal/mailer"
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
  ORDER BY (m.priority = 'high') DESC, m.next_attempt_at LIMIT $2
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

// PromoteScheduled moves due scheduled messages onto the dispatch queue.
func (s *Service) PromoteScheduled(ctx context.Context) (int64, error) {
	tag, err := s.Pool.Exec(ctx, `UPDATE messages SET state='queued', updated_at=now()
WHERE state='scheduled' AND send_at <= now() AND expires_at > now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// LoadAttachments decrypts outbound files for a message that is about to be submitted.
func (s *Service) LoadAttachments(ctx context.Context, messageID string) ([]mailer.Attachment, error) {
	rows, err := s.Pool.Query(ctx, `SELECT filename, content_type, data_enc FROM message_attachments WHERE message_id=$1 ORDER BY filename`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mailer.Attachment
	for rows.Next() {
		var a mailer.Attachment
		var enc []byte
		if err := rows.Scan(&a.Filename, &a.ContentType, &enc); err != nil {
			return nil, err
		}
		a.Data, err = s.Box.Open(enc, []byte("attachment:"+messageID+":"+a.Filename))
		if err != nil {
			return nil, err
		}
		out = append(out, a)
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
	extra      func(ctx context.Context, tx *db.Tx) error
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
  transport_profile_id = CASE WHEN $3::text = 'submitted' THEN (SELECT id FROM transport_profiles WHERE is_active) ELSE transport_profile_id END,
  transport_revision = CASE WHEN $3::text = 'submitted' THEN (SELECT config_revision FROM transport_profiles WHERE is_active) ELSE transport_revision END,
  smtp_reply = COALESCE($6::text, smtp_reply),
  smtp_message_id = COALESCE($7::text, smtp_message_id),
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
	if o.clear {
		if _, err := tx.Exec(ctx, `DELETE FROM message_attachments WHERE message_id=$1`, id); err != nil {
			return false, err
		}
	}
	if o.extra != nil {
		if err := o.extra(ctx, tx); err != nil {
			return false, err
		}
	}
	// Webhook outbox: written in the same transaction as the state change.
	if webhookStates[o.to] {
		if err := emitMessageEvent(ctx, tx, id, "email."+o.to, o.reason); err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}

var submittingOnly = []string{"submitting"}

// Message states that produce a webhook event. Payloads carry ids, states and
// safe reason codes only.
var webhookStates = map[string]bool{"submitted": true, "failed": true, "unknown": true, "expired": true}

const messageEventData = `jsonb_build_object('message_id', m.id, 'state', m.state, 'reason', %s::text,
  'template', m.template, 'request_id', m.metadata->>'request_id')`

func emitMessageEvent(ctx context.Context, tx *db.Tx, id, eventType, reason string) error {
	_, err := tx.Exec(ctx, `SELECT dm_emit(m.environment_id, $2, `+fmt.Sprintf(messageEventData, "$3")+`) FROM messages m WHERE m.id=$1`,
		id, eventType, reason)
	return err
}

// sweep runs a bulk transition that records an event and a webhook per row,
// and returns how many messages moved.
func (s *Service) sweep(ctx context.Context, where, state, reason string) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `
WITH u AS (UPDATE messages SET state=$1, payload_enc=NULL, completed_at=now(), updated_at=now()
           WHERE `+where+` RETURNING id, environment_id, state, template, metadata),
ev AS (INSERT INTO message_events(message_id, event_type, reason_code) SELECT id, $1, $2 FROM u)
SELECT count(*), COALESCE(sum(dm_emit(m.environment_id, 'email.' || $1, `+fmt.Sprintf(messageEventData, "$2")+`)), 0) FROM u m`,
		state, reason).Scan(&n, new(int))
	return n, err
}

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
		extra: func(ctx context.Context, tx *db.Tx) error {
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
	return s.sweep(ctx, `state='submitting' AND lease_expires_at < now()`, "unknown", "lease_expired")
}

// ExpireDue expires queued/retry_wait messages whose deadline passed.
func (s *Service) ExpireDue(ctx context.Context) (int, error) {
	return s.sweep(ctx, `state IN ('queued','retry_wait') AND expires_at <= now()`, "expired", "ttl_elapsed")
}

// Heartbeat records liveness of the default dispatch worker role.
func (s *Service) Heartbeat(ctx context.Context, workerID string) error {
	return s.HeartbeatRole(ctx, "worker", workerID)
}

func (s *Service) HeartbeatRole(ctx context.Context, role, instanceID string) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO worker_heartbeats(worker_id, role, last_seen) VALUES ($1, $2, now())
ON CONFLICT (worker_id) DO UPDATE SET last_seen = now(), role = EXCLUDED.role`, role+":"+instanceID, role)
	return err
}

// WorkerHealthy reports whether any dispatch worker has checked in within maxAge.
func (s *Service) WorkerHealthy(ctx context.Context, maxAge time.Duration) (bool, error) {
	return s.RoleHealthy(ctx, "worker", maxAge)
}

func (s *Service) RoleHealthy(ctx context.Context, role string, maxAge time.Duration) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM worker_heartbeats WHERE role=$2 AND last_seen > now() - make_interval(secs => $1::float8))`,
		maxAge.Seconds(), role).Scan(&ok)
	return ok, err
}

type RetentionResult struct {
	Captures, Buckets, Messages, Payloads, Heartbeats int64
	Sessions, Deliveries, DNSChecks, Inbound, Spool   int64
}

// RunRetention deletes expired captures, stale rate buckets, old terminal
// message metadata, any payload left on a non-active row past the purge
// window, expired sessions, finished webhook deliveries, old DNS results and
// inbound mail past its retention period.
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
	meta := s.Opts.MetadataRetention.Seconds()
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
        AND created_at < now() - make_interval(secs => $1::float8)`, meta); err != nil {
		return r, err
	}
	if err := run(&r.Heartbeats, `DELETE FROM worker_heartbeats WHERE last_seen < now() - interval '1 day'`); err != nil {
		return r, err
	}
	if err := run(&r.Sessions, `DELETE FROM sessions WHERE expires_at < now()`); err != nil {
		return r, err
	}
	if err := run(&r.Deliveries, `DELETE FROM webhook_deliveries WHERE state IN ('succeeded','dead')
        AND created_at < now() - make_interval(secs => $1::float8)`, meta); err != nil {
		return r, err
	}
	if err := run(&r.DNSChecks, `DELETE FROM dns_checks WHERE checked_at < now() - interval '30 days'`); err != nil {
		return r, err
	}
	if err := run(new(int64), `DELETE FROM bounces WHERE received_at < now() - make_interval(secs => $1::float8)`, meta); err != nil {
		return r, err
	}
	var err error
	if r.Inbound, r.Spool, err = s.inboundRetention(ctx); err != nil {
		return r, err
	}
	return r, nil
}

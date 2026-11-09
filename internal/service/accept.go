package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/ditronics-tz/mail/internal/secure"
	"github.com/ditronics-tz/mail/internal/tmpl"
	"github.com/jackc/pgx/v5"
)

type SendRequest struct {
	From            string         `json:"from"`
	To              string         `json:"to"`
	Template        string         `json:"template"`
	TemplateVersion *int           `json:"template_version"`
	Variables       map[string]any `json:"variables"`
	TTLSeconds      *int           `json:"ttl_seconds"`
	Metadata        map[string]any `json:"metadata"`
}

// Payload is the sensitive content stored encrypted until a terminal outcome.
type Payload struct {
	From     string            `json:"from"`
	FromName string            `json:"from_name"`
	To       string            `json:"to"`
	Template string            `json:"template"`
	Version  int               `json:"version"`
	Vars     map[string]string `json:"vars"`
}

type Accepted struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

var (
	idemRe      = regexp.MustCompile(`^[A-Za-z0-9._:\-]{8,128}$`)
	requestIDRe = regexp.MustCompile(`^[A-Za-z0-9._:\-]{1,128}$`)
)

type validated struct {
	from, fromName, to string
	tpl                *tmpl.Template
	vars               map[string]string
	ttl                time.Duration
	requestID          string
	canonical          []byte
}

func invalid(code string) error { return newErr(422, code) }

func (s *Service) validate(ctx context.Context, p *Principal, body []byte) (*validated, error) {
	var req SendRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return nil, invalid("invalid_request")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, invalid("invalid_request")
	}
	v := &validated{}
	var ok bool
	if v.to, ok = NormalizeEmail(req.To); !ok {
		return nil, invalid("invalid_recipient")
	}
	if v.from, ok = NormalizeEmail(req.From); !ok {
		return nil, invalid("invalid_sender")
	}
	if req.TemplateVersion == nil {
		return nil, invalid("template_version_required")
	}
	if v.tpl = tmpl.Lookup(req.Template, *req.TemplateVersion); v.tpl == nil {
		return nil, invalid("unknown_template")
	}
	vars, verr := v.tpl.Validate(req.Variables, p.Origins)
	if verr != nil {
		return nil, invalid(verr.Code)
	}
	v.vars = vars

	v.ttl = v.tpl.DefaultTTL
	if req.TTLSeconds != nil {
		d := time.Duration(*req.TTLSeconds) * time.Second
		if d < 30*time.Second || d > v.tpl.MaxTTL {
			return nil, invalid("invalid_ttl")
		}
		v.ttl = d
	}
	for k, val := range req.Metadata {
		str, isStr := val.(string)
		if k != "request_id" || !isStr || !requestIDRe.MatchString(str) {
			return nil, invalid("invalid_metadata")
		}
		v.requestID = str
	}

	// Approved sender enforcement.
	var name string
	err := s.Pool.QueryRow(ctx, `SELECT display_name FROM sender_identities WHERE environment_id=$1 AND project_id=$2 AND email=$3`,
		p.EnvironmentID, p.ProjectID, v.from).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, newErr(403, "sender_not_approved")
	}
	if err != nil {
		return nil, unavailable("sender_lookup", err)
	}
	v.fromName = name

	// Keyed canonical fingerprint of the validated request (not a plain digest:
	// OTP-bearing payloads would otherwise be offline-guessable).
	canon, _ := json.Marshal(struct {
		From, To, Template string
		Version            int
		Vars               map[string]string
		TTL                int64
		RequestID          string
	}{v.from, v.to, v.tpl.Name, v.tpl.Version, v.vars, int64(v.ttl / time.Second), v.requestID})
	v.canonical = canon
	return v, nil
}

func allowlisted(list []string, email string) bool {
	at := strings.LastIndexByte(email, '@')
	for _, e := range list {
		if e == email || (strings.HasPrefix(e, "@") && e == email[at:]) {
			return true
		}
	}
	return false
}

// Accept validates, then atomically (quota + message + event) persists a
// message. It returns only after the transaction has committed.
func (s *Service) Accept(ctx context.Context, p *Principal, idemKey string, body []byte) (*Accepted, error) {
	if !p.HasScope(ScopeSend) {
		return nil, newErr(403, "missing_scope")
	}
	if !idemRe.MatchString(idemKey) {
		return nil, invalid("idempotency_key_invalid")
	}
	v, err := s.validate(ctx, p, body)
	if err != nil {
		return nil, err
	}
	reqMAC := s.Box.MAC("request", v.canonical)
	rcptHash := s.Box.MAC("recipient", []byte(v.to))

	for attempt := 0; attempt < 3; attempt++ {
		res, retry, err := s.acceptTx(ctx, p, v, idemKey, reqMAC, rcptHash)
		if err != nil {
			return nil, err
		}
		if !retry {
			return res, nil
		}
	}
	return nil, unavailable("accept_retry", errors.New("idempotency race did not settle"))
}

func (s *Service) acceptTx(ctx context.Context, p *Principal, v *validated, idemKey string, reqMAC, rcptHash []byte) (*Accepted, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, false, unavailable("begin", err)
	}
	defer tx.Rollback(ctx)

	// 1. Idempotent replay is resolved before any quota is charged.
	var exID, exState string
	var exMAC []byte
	err = tx.QueryRow(ctx, `SELECT id::text, state, request_hmac FROM messages WHERE project_id=$1 AND environment_id=$2 AND idempotency_key=$3`,
		p.ProjectID, p.EnvironmentID, idemKey).Scan(&exID, &exState, &exMAC)
	if err == nil {
		if hmac.Equal(exMAC, reqMAC) {
			return &Accepted{ID: exID, Status: exState}, false, nil
		}
		return nil, false, newErr(409, "idempotency_conflict")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, unavailable("idem_lookup", err)
	}

	// 2. Recipient policy and suppression.
	if p.Mode == "live" && p.RestrictRecipients && !allowlisted(p.Allowlist, v.to) {
		return nil, false, invalid("recipient_not_allowed")
	}
	var one int
	err = tx.QueryRow(ctx, `SELECT 1 FROM suppressions WHERE environment_id=$1 AND recipient_hash=$2`, p.EnvironmentID, rcptHash).Scan(&one)
	if err == nil {
		return nil, false, invalid("recipient_suppressed")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, unavailable("suppression_lookup", err)
	}

	// 3. Quotas: atomic increments in this transaction. Rolling back on any
	// later failure releases the reservation deterministically.
	scope := p.ProjectID + ":" + p.EnvironmentID
	rk := scope + ":" + string(hexOf(rcptHash))
	for _, b := range []struct {
		key    string
		window time.Duration
		limit  int
	}{
		{"p:" + scope + ":min", time.Minute, p.PerMinute},
		{"p:" + scope + ":day", 24 * time.Hour, p.PerDay},
		{"r:" + rk + ":min", time.Minute, p.RcptPerMinute},
		{"r:" + rk + ":hour", time.Hour, p.RcptPerHour},
	} {
		wait, ok, err := hit(ctx, tx, b.key, b.window, b.limit)
		if err != nil {
			return nil, false, unavailable("rate_limit", err)
		}
		if !ok {
			return nil, false, &Error{Status: 429, Code: "rate_limited", RetryAfter: wait}
		}
	}

	// 4. Durable insert.
	id := secure.NewUUID()
	pl, _ := json.Marshal(Payload{From: v.from, FromName: v.fromName, To: v.to, Template: v.tpl.Name, Version: v.tpl.Version, Vars: v.vars})
	sealed, err := s.Box.Seal(pl, []byte(id))
	if err != nil {
		return nil, false, unavailable("seal", err)
	}
	meta := map[string]string{}
	if v.requestID != "" {
		meta["request_id"] = v.requestID
	}
	mj, _ := json.Marshal(meta)
	tag, err := tx.Exec(ctx, `
INSERT INTO messages (id, project_id, environment_id, api_key_id, template, template_version, state, payload_enc,
                      recipient_hash, request_hmac, idempotency_key, metadata, expires_at, next_attempt_at)
VALUES ($1,$2,$3,$4,$5,$6,'queued',$7,$8,$9,$10,$11, now() + make_interval(secs => $12::float8), now())
ON CONFLICT (project_id, environment_id, idempotency_key) DO NOTHING`,
		id, p.ProjectID, p.EnvironmentID, p.KeyID, v.tpl.Name, v.tpl.Version, sealed, rcptHash, reqMAC, idemKey, mj, v.ttl.Seconds())
	if err != nil {
		return nil, false, unavailable("insert", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, true, nil // lost an idempotency race; re-resolve as a replay
	}
	if _, err := tx.Exec(ctx, `INSERT INTO message_events(message_id, event_type) VALUES ($1,'accepted')`, id); err != nil {
		return nil, false, unavailable("event", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, unavailable("commit", err)
	}
	return &Accepted{ID: id, Status: "queued"}, false, nil
}

func hexOf(b []byte) []byte {
	const d = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2], out[i*2+1] = d[c>>4], d[c&15]
	}
	return out
}

func hit(ctx context.Context, tx pgx.Tx, key string, window time.Duration, limit int) (time.Duration, bool, error) {
	now := time.Now()
	ws := now.Truncate(window)
	var n int
	err := tx.QueryRow(ctx, `
INSERT INTO rate_limit_buckets (scope_key, window_start, count) VALUES ($1,$2,1)
ON CONFLICT (scope_key, window_start) DO UPDATE SET count = rate_limit_buckets.count + 1
RETURNING count`, key, ws).Scan(&n)
	if err != nil {
		return 0, false, err
	}
	if n > limit {
		wait := ws.Add(window).Sub(now)
		if wait < time.Second {
			wait = time.Second
		}
		return wait, false, nil
	}
	return 0, true, nil
}

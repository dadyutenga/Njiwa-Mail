package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/njiwa-tz/mail/internal/db"
	"net"
	"strings"

	"github.com/njiwa-tz/mail/internal/secure"
)

// Cancel stops a queued or scheduled message before submission starts.
func (s *Service) Cancel(ctx context.Context, p *Principal, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE messages SET state='cancelled', payload_enc=NULL, completed_at=now(), updated_at=now()
WHERE id=$1 AND project_id=$2 AND environment_id=$3 AND state IN ('queued','scheduled')`, id, p.ProjectID, p.EnvironmentID)
	if err != nil {
		return unavailable("cancel", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(409, "not_cancellable")
	}
	_, _ = s.Pool.Exec(ctx, `DELETE FROM message_attachments WHERE message_id=$1`, id)
	_, _ = s.Pool.Exec(ctx, `INSERT INTO message_events(message_id, event_type, reason_code) VALUES ($1,'cancelled','operator_or_client')`, id)
	return nil
}

// BatchResult is one item of POST /v1/emails/batch.
type BatchResult struct {
	Index  int    `json:"index"`
	ID     string `json:"id,omitempty"`
	Status int    `json:"status"`
	State  string `json:"state,omitempty"`
	Code   string `json:"code,omitempty"`
}

// AcceptBatch queues up to 100 messages. Each item has its own idempotency key
// and its own result; one rejection does not roll back the others.
func (s *Service) AcceptBatch(ctx context.Context, p *Principal, messages []json.RawMessage) ([]BatchResult, error) {
	if len(messages) == 0 || len(messages) > 100 {
		return nil, invalid("invalid_batch")
	}
	out := make([]BatchResult, len(messages))
	for i, raw := range messages {
		out[i].Index = i
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			out[i].Status, out[i].Code = 422, "invalid_request"
			continue
		}
		var key string
		if k, ok := fields["idempotency_key"]; ok {
			_ = json.Unmarshal(k, &key)
			delete(fields, "idempotency_key")
		}
		body, _ := json.Marshal(fields)
		acc, err := s.Accept(ctx, p, key, body)
		if err != nil {
			e, _ := IsError(err)
			if e == nil {
				e = &Error{Status: 503, Code: "service_unavailable"}
			}
			out[i].Status, out[i].Code = e.Status, e.Code
			continue
		}
		out[i].Status, out[i].ID, out[i].State = 202, acc.ID, acc.Status
	}
	return out, nil
}

func (s *Service) CreateDomain(ctx context.Context, project, name, purpose string) (id, token string, err error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if purpose != "sending" && purpose != "inbound" {
		return "", "", badRequest("purpose must be sending or inbound")
	}
	if _, ok := NormalizeEmail("a@" + name); !ok {
		return "", "", badRequest("invalid domain")
	}
	var pid string
	if err := s.Pool.QueryRow(ctx, `SELECT id::text FROM projects WHERE name=$1`, project).Scan(&pid); err != nil {
		return "", "", notFound(err)
	}
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", "", unavailable("token", err)
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	err = s.Pool.QueryRow(ctx, `INSERT INTO domains(project_id, name, purpose, verify_token) VALUES ($1,$2,$3,$4)
ON CONFLICT (project_id, name, purpose) DO UPDATE SET name=EXCLUDED.name RETURNING id::text, verify_token`, pid, name, purpose, token).Scan(&id, &token)
	if err != nil {
		return "", "", unavailable("domain", err)
	}
	return id, token, nil
}

func (s *Service) ListDomains(ctx context.Context, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT d.id::text AS id, p.name AS project, d.name, d.purpose, d.verified_at, d.spf_ok, d.dkim_ok, d.dmarc_ok, d.created_at,
  'njiwa-mail-verify=' || d.verify_token AS txt_challenge
FROM domains d JOIN projects p ON p.id=d.project_id ORDER BY d.name`)
}

// VerifyDomain checks the TXT challenge and the mail records. Live sending stays
// blocked until the challenge, SPF, DKIM and DMARC all pass.
func (s *Service) VerifyDomain(ctx context.Context, id string, r Resolver) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	var name, token string
	if err := s.Pool.QueryRow(ctx, `SELECT name, verify_token FROM domains WHERE id=$1`, id).Scan(&name, &token); err != nil {
		return notFound(err)
	}
	if r == nil {
		r = net.DefaultResolver
	}
	txt, _ := r.LookupTXT(ctx, name)
	verified := false
	for _, rec := range txt {
		if strings.TrimSpace(rec) == "njiwa-mail-verify="+token {
			verified = true
		}
	}
	spf := strings.Contains(strings.ToLower(strings.Join(txt, " ")), "v=spf1")
	dmarcRec, _ := r.LookupTXT(ctx, "_dmarc."+name)
	dmarc := strings.Contains(strings.ToLower(strings.Join(dmarcRec, " ")), "v=dmarc1")
	dkimRec, _ := r.LookupTXT(ctx, s.Opts.DKIMSelector+"._domainkey."+name)
	dkim := strings.Contains(strings.ToLower(strings.Join(dkimRec, " ")), "v=dkim1")
	_, err := s.Pool.Exec(ctx, `UPDATE domains SET verified_at=CASE WHEN $2 THEN COALESCE(verified_at, now()) ELSE NULL END,
spf_ok=$3, dkim_ok=$4, dmarc_ok=$5 WHERE id=$1`, id, verified, spf, dkim, dmarc)
	if err != nil {
		return unavailable("domain_verify", err)
	}
	if !verified || !spf || !dkim || !dmarc {
		_ = s.Audit(ctx, Actor{Name: "scheduler"}, "domain.not_ready", "domain", id, map[string]any{"name": name})
	}
	return nil
}

func (s *Service) Analytics(ctx context.Context, project, mode, template, domain string) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT to_char(m.created_at AT TIME ZONE 'Africa/Dar_es_Salaam', 'YYYY-MM-DD') AS day,
  count(*) FILTER (WHERE m.state='submitted') AS submitted,
  count(*) FILTER (WHERE m.state='failed') AS failed,
  count(*) FILTER (WHERE m.state='unknown') AS unknown,
  count(*) FILTER (WHERE m.bounce_state='hard') AS hard,
  count(*) FILTER (WHERE m.bounce_state='soft') AS soft
FROM messages m JOIN projects p ON p.id=m.project_id JOIN environments e ON e.id=m.environment_id
WHERE ($1='' OR p.name=$1) AND ($2='' OR e.mode=$2) AND ($3='' OR m.template=$3)
  AND ($4='' OR EXISTS (SELECT 1 FROM domains d WHERE d.project_id=m.project_id AND d.name=$4))
GROUP BY 1 ORDER BY 1`, project, mode, template, domain)
}

func (s *Service) ExportRecipient(ctx context.Context, project, mode, email string) (map[string]any, error) {
	e, ok := NormalizeEmail(email)
	if !ok {
		return nil, badRequest("invalid email address")
	}
	_, eid, err := s.envID(ctx, project, mode)
	if err != nil {
		return nil, notFound(err)
	}
	hash := s.Box.MAC("recipient", []byte(e))
	msgs, err := s.maps(ctx, `SELECT id::text AS id, state, template, created_at FROM messages WHERE environment_id=$1 AND recipient_hash=$2`, eid, hash)
	if err != nil {
		return nil, err
	}
	inb, err := s.maps(ctx, `SELECT id::text AS id, subject, received_at FROM inbound_messages WHERE environment_id=$1 AND lower(envelope_from)=$2`, eid, e)
	if err != nil {
		return nil, err
	}
	return map[string]any{"email_masked": maskEmail(e), "messages": msgs, "inbound": inb}, nil
}

func (s *Service) DeleteRecipient(ctx context.Context, project, mode, email string) error {
	e, ok := NormalizeEmail(email)
	if !ok {
		return badRequest("invalid email address")
	}
	_, eid, err := s.envID(ctx, project, mode)
	if err != nil {
		return notFound(err)
	}
	hash := s.Box.MAC("recipient", []byte(e))
	if _, err := s.Pool.Exec(ctx, `DELETE FROM messages WHERE environment_id=$1 AND recipient_hash=$2`, eid, hash); err != nil {
		return unavailable("privacy_messages", err)
	}
	if _, err := s.Pool.Exec(ctx, `DELETE FROM inbound_messages WHERE environment_id=$1 AND lower(envelope_from)=$2`, eid, e); err != nil {
		return unavailable("privacy_inbound", err)
	}
	if _, err := s.Pool.Exec(ctx, `DELETE FROM suppressions WHERE environment_id=$1 AND recipient_hash=$2`, eid, hash); err != nil {
		return unavailable("privacy_suppressions", err)
	}
	return nil
}

func (s *Service) ListSessions(ctx context.Context, userID string) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT encode(token_hash,'hex') AS id, ip, user_agent, created_at, expires_at FROM sessions WHERE user_id=$1 AND expires_at>now() ORDER BY created_at DESC`, userID)
}

func (s *Service) RevokeSession(ctx context.Context, userID, id string) error {
	hash, err := hex.DecodeString(id)
	if err != nil {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1 AND token_hash=$2`, userID, hash)
	if err != nil {
		return unavailable("session_revoke", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	e, ok := NormalizeEmail(email)
	if !ok {
		return nil
	}
	var id string
	err := s.Pool.QueryRow(ctx, `SELECT id::text FROM users WHERE email=$1 AND NOT disabled`, e).Scan(&id)
	if errors.Is(err, db.ErrNoRows) {
		return nil
	}
	if err != nil {
		return unavailable("reset_lookup", err)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return unavailable("reset_token", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(token))
	if _, err := s.Pool.Exec(ctx, `INSERT INTO password_resets(token_hash, user_id, expires_at) VALUES ($1,$2, now() + interval '1 hour')`, sum[:], id); err != nil {
		return unavailable("reset_store", err)
	}
	link := strings.TrimRight(s.Opts.PublicURL, "/") + "/reset-password?token=" + token
	// Live only. Sandbox mail is captured and readable by every admin, so a
	// recovery token queued there is an account takeover. No live sender means
	// the token stays hashed and is not written into any readable body.
	var from, name, pid, eid string
	err = s.Pool.QueryRow(ctx, `SELECT si.email, si.display_name, e.project_id::text, e.id::text
FROM sender_identities si
JOIN environments e ON e.id=si.environment_id AND e.enabled
JOIN projects p ON p.id=e.project_id AND p.enabled
WHERE e.mode='live'
ORDER BY si.created_at
LIMIT 1`).Scan(&from, &name, &pid, &eid)
	if err != nil {
		return nil
	}
	mid := secure.NewUUID()
	body, _ := json.Marshal(Payload{From: from, FromName: name, To: e, Template: "raw", Raw: true, Confidential: true, Subject: "Password reset", Text: "Reset your Njiwa Mail password: " + link})
	sealed, err := s.Box.Seal(body, []byte(mid))
	if err != nil {
		return nil
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO messages (id, project_id, environment_id, template, template_version, state, payload_enc,
recipient_hash, request_hmac, idempotency_key, metadata, expires_at, locale, priority)
VALUES ($1,$2,$3,'raw',0,'queued',$4,$5,$6,$7,'{"purpose":"password_reset"}', now() + interval '1 hour', 'en', 'high')`,
		mid, pid, eid, sealed, s.Box.MAC("recipient", []byte(e)), s.Box.MAC("request", body), "reset-"+mid)
	return err
}

func (s *Service) ResetPassword(ctx context.Context, token, password string) error {
	if err := checkPassword(password); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(token))
	var userID string
	err := s.Pool.QueryRow(ctx, `DELETE FROM password_resets WHERE token_hash=$1 AND expires_at>now() RETURNING user_id::text`, sum[:]).Scan(&userID)
	if errors.Is(err, db.ErrNoRows) {
		return newErr(400, "invalid_request")
	}
	if err != nil {
		return unavailable("reset", err)
	}
	return s.UpdateUser(ctx, userID, nil, nil, &password)
}

func (s *Service) SaveAlertChannel(ctx context.Context, kind, target string) error {
	if kind != "email" && kind != "webhook" {
		return badRequest("kind must be email or webhook")
	}
	if kind == "email" {
		if _, ok := NormalizeEmail(target); !ok {
			return badRequest("invalid email address")
		}
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO alert_channels(kind, target) VALUES ($1,$2)`, kind, target)
	if err != nil {
		return unavailable("alert_channel", err)
	}
	return nil
}

func (s *Service) ListAlertChannels(ctx context.Context) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT id::text AS id, kind, target, enabled, created_at FROM alert_channels ORDER BY created_at`)
}

// DailySummary is the text of the owner summary. The caller sends it when SMTP is configured.
func (s *Service) DailySummary(ctx context.Context) (string, []string, error) {
	var submitted, failed, unknown, inbound int
	err := s.Pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM messages WHERE state='submitted' AND submitted_at > now() - interval '1 day'),
 (SELECT count(*) FROM messages WHERE state='failed' AND completed_at > now() - interval '1 day'),
 (SELECT count(*) FROM messages WHERE state='unknown'),
 (SELECT count(*) FROM inbound_messages WHERE received_at > now() - interval '1 day')`).Scan(&submitted, &failed, &unknown, &inbound)
	if err != nil {
		return "", nil, unavailable("summary", err)
	}
	body := fmt.Sprintf("Njiwa Mail daily summary\nsubmitted: %d\nfailed: %d\nunknown: %d\ninbound: %d\n", submitted, failed, unknown, inbound)
	rows, err := s.Pool.Query(ctx, `SELECT email FROM users WHERE role='owner' AND NOT disabled`)
	if err != nil {
		return "", nil, unavailable("owners", err)
	}
	defer rows.Close()
	var to []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return "", nil, err
		}
		to = append(to, e)
	}
	return body, to, rows.Err()
}

// Reencrypt rewrites every encrypted column with newKey. The caller swaps the
// process key only after this returns nil.
func (s *Service) Reencrypt(ctx context.Context, newKey []byte) error {
	next, err := s.Box.WithEncryptionKey(newKey)
	if err != nil {
		return err
	}
	rows, err := s.Pool.Query(ctx, `SELECT id::text, payload_enc FROM messages WHERE payload_enc IS NOT NULL`)
	if err != nil {
		return unavailable("reencrypt_list", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var enc []byte
		if err := rows.Scan(&id, &enc); err != nil {
			return err
		}
		pt, err := s.Box.Open(enc, []byte(id))
		if err != nil {
			return fmt.Errorf("message %s: %w", id, err)
		}
		sealed, err := next.Seal(pt, []byte(id))
		if err != nil {
			return err
		}
		if _, err := s.Pool.Exec(ctx, `UPDATE messages SET payload_enc=$2 WHERE id=$1`, id, sealed); err != nil {
			return err
		}
	}
	return rows.Err()
}

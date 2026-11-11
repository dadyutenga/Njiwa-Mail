package service

import (
	"context"
	"errors"
	"github.com/ditronics-tz/mail/internal/db"
	"strings"
	"time"
)

// Read models and writes for the admin API. Like the console views these are
// metadata only: never payloads, variables, key secrets or full recipients.
// (Sandbox captures and inbound bodies have their own role-gated accessors.)

type MessageFilter struct {
	Project, Mode, State, Template, Recipient, Bounce string
	From, To                                          *time.Time
}

const adminMessageCols = `m.id::text AS id, m.state, m.bounce_state, m.template, m.template_version, m.locale, p.name AS project, e.mode,
  m.priority, m.send_at, m.reply_to, m.attempts, m.created_at, m.updated_at, m.expires_at, m.submitted_at, m.completed_at, m.idempotency_key, m.smtp_reply,
  m.metadata, k.prefix AS api_key_prefix`

// AdminMessages lists messages with filters. A recipient filter is matched by
// keyed hash, so the address itself is never stored or compared in clear.
func (s *Service) AdminMessages(ctx context.Context, f MessageFilter, pg Page) ([]map[string]any, bool, error) {
	if !validConsoleState(f.State) {
		return nil, false, badRequest("unknown state")
	}
	var hash []byte
	if f.Recipient != "" {
		e, ok := NormalizeEmail(strings.ToLower(strings.TrimSpace(f.Recipient)))
		if !ok {
			return nil, false, badRequest("recipient filter must be a full email address")
		}
		hash = s.Box.MAC("recipient", []byte(e))
	}
	return s.page(ctx, pg, `SELECT `+adminMessageCols+`
FROM messages m JOIN projects p ON p.id=m.project_id JOIN environments e ON e.id=m.environment_id LEFT JOIN api_keys k ON k.id=m.api_key_id
WHERE ($1='' OR p.name=$1) AND ($2='' OR e.mode=$2) AND ($3='' OR m.state=$3) AND ($4='' OR m.template=$4)
  AND ($5::bytea IS NULL OR m.recipient_hash=$5) AND ($6='' OR m.bounce_state=$6)
  AND ($7::timestamptz IS NULL OR m.created_at >= $7) AND ($8::timestamptz IS NULL OR m.created_at < $8)
ORDER BY m.created_at DESC`, f.Project, f.Mode, f.State, f.Template, hash, f.Bounce, f.From, f.To)
}

// AdminMessage returns one message with its event timeline and bounces.
func (s *Service) AdminMessage(ctx context.Context, id string) (map[string]any, error) {
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	m, err := s.one(ctx, `SELECT `+adminMessageCols+`
FROM messages m JOIN projects p ON p.id=m.project_id JOIN environments e ON e.id=m.environment_id LEFT JOIN api_keys k ON k.id=m.api_key_id
WHERE m.id=$1`, id)
	if err != nil {
		return nil, err
	}
	if m["events"], err = s.maps(ctx, `SELECT event_type AS type, reason_code AS reason, created_at AS at FROM message_events WHERE message_id=$1 ORDER BY id`, id); err != nil {
		return nil, err
	}
	if m["bounces"], err = s.maps(ctx, `SELECT id::text AS id, type, status_code, diagnostic, recipient_masked AS recipient, received_at FROM bounces WHERE message_id=$1 ORDER BY received_at`, id); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Service) AdminProjects(ctx context.Context, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT p.id::text AS id, p.name, p.enabled, p.send_per_minute, p.send_per_day, p.recipient_per_minute, p.recipient_per_hour, p.created_at,
  (SELECT jsonb_agg(jsonb_build_object('mode', e.mode, 'enabled', e.enabled, 'approved_origins', to_jsonb(e.approved_origins),
     'restrict_recipients', e.restrict_recipients, 'recipient_allowlist', to_jsonb(e.recipient_allowlist)) ORDER BY e.mode)
   FROM environments e WHERE e.project_id=p.id) AS environments
FROM projects p ORDER BY p.name`)
}

// CreateProjectWithDefaults creates a project and applies the default quotas
// from settings.
func (s *Service) CreateProjectWithDefaults(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if !projectNameRe(name) {
		return badRequest("project name must be 1-64 letters, digits, dashes or underscores")
	}
	if err := s.CreateProject(ctx, name); err != nil {
		if strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate") {
			return newErr(409, "project_exists")
		}
		return unavailable("project_create", err)
	}
	return opErr(s.SetQuotas(ctx, name, s.settingInt(ctx, "default_send_per_minute"), s.settingInt(ctx, "default_send_per_day"), 0, 0))
}

func projectNameRe(n string) bool {
	if n == "" || len(n) > 64 {
		return false
	}
	for _, r := range n {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// opErr converts the plain errors returned by the CLI-era admin functions
// into API errors: their messages describe operator input, never secrets.
func opErr(err error) error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	msg := err.Error()
	if strings.Contains(msg, "not found") {
		return &Error{Status: 404, Code: "not_found", Message: msg}
	}
	if strings.Contains(msg, "SQLSTATE") || strings.Contains(msg, "connect") {
		return unavailable("admin_op", err)
	}
	return badRequest(msg)
}

// OpErr is opErr for callers outside the package (the admin API).
func OpErr(err error) error { return opErr(err) }

// SetEnvEnabled enables or disables one environment of a project.
func (s *Service) SetEnvEnabled(ctx context.Context, project, mode string, enabled bool) error {
	_, eid, err := s.envID(ctx, project, mode)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `UPDATE environments SET enabled=$2 WHERE id=$1`, eid, enabled)
	return err
}

// SetAllowlist replaces the approved recipient list of an environment.
func (s *Service) SetAllowlist(ctx context.Context, project, mode string, patterns []string) error {
	norm := []string{}
	for _, raw := range patterns {
		pat := strings.ToLower(strings.TrimSpace(raw))
		if strings.HasPrefix(pat, "@") {
			if _, ok := NormalizeEmail("x" + pat); !ok {
				return errors.New("invalid domain pattern")
			}
		} else if _, ok := NormalizeEmail(pat); !ok {
			return errors.New("invalid email address")
		}
		norm = append(norm, pat)
	}
	_, eid, err := s.envID(ctx, project, mode)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `UPDATE environments SET recipient_allowlist=$2 WHERE id=$1`, eid, norm)
	return err
}

func (s *Service) AdminKeys(ctx context.Context, project string, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT k.prefix, p.name AS project, e.mode, k.label, to_jsonb(k.scopes) AS scopes, k.created_at, k.expires_at, k.revoked_at, k.last_used_at,
  CASE WHEN k.revoked_at IS NOT NULL THEN 'revoked' WHEN k.expires_at IS NOT NULL AND k.expires_at <= now() THEN 'expired' ELSE 'active' END AS status
FROM api_keys k JOIN projects p ON p.id=k.project_id JOIN environments e ON e.id=k.environment_id
WHERE ($1='' OR p.name=$1) ORDER BY k.created_at DESC`, project)
}

func (s *Service) AdminSenders(ctx context.Context, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT si.id::text AS id, si.email, si.display_name, p.name AS project, e.mode, si.created_at
FROM sender_identities si JOIN projects p ON p.id=si.project_id JOIN environments e ON e.id=si.environment_id ORDER BY si.email, p.name, e.mode`)
}

func (s *Service) DeleteSender(ctx context.Context, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM sender_identities WHERE id=$1`, id)
	if err != nil {
		return unavailable("sender_delete", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

const suppressionCols = `s.id::text AS id, s.recipient_masked AS recipient, s.reason, s.source, s.bounce_id::text AS bounce_id, s.created_at`

func (s *Service) AdminSuppressions(ctx context.Context, project, mode, source string, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT `+suppressionCols+`, p.name AS project, e.mode
FROM suppressions s JOIN projects p ON p.id=s.project_id JOIN environments e ON e.id=s.environment_id
WHERE ($1='' OR p.name=$1) AND ($2='' OR e.mode=$2) AND ($3='' OR s.source=$3) ORDER BY s.created_at DESC`, project, mode, source)
}

// DeleteSuppression removes a suppression by id (envID "" = any environment).
func (s *Service) DeleteSuppression(ctx context.Context, envID, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM suppressions WHERE id=$1 AND ($2='' OR environment_id=NULLIF($2,'')::uuid)`, id, envID)
	if err != nil {
		return unavailable("suppression_delete", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

// ---- suppressions for API keys (scope suppressions:manage), always scoped
// to the key's project AND environment.

func (s *Service) KeySuppressions(ctx context.Context, p *Principal, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT `+suppressionCols+` FROM suppressions s WHERE s.project_id=$1 AND s.environment_id=$2 ORDER BY s.created_at DESC`,
		p.ProjectID, p.EnvironmentID)
}

func (s *Service) KeySuppress(ctx context.Context, p *Principal, email, reason string) (string, error) {
	e, ok := NormalizeEmail(strings.ToLower(strings.TrimSpace(email)))
	if !ok {
		return "", invalid("invalid_recipient")
	}
	reason = clean(strings.TrimSpace(reason), 200)
	if reason == "" {
		return "", invalid("reason_required")
	}
	var id string
	err := s.Pool.QueryRow(ctx, `INSERT INTO suppressions(project_id, environment_id, recipient_hash, recipient_masked, reason) VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (environment_id, recipient_hash) DO UPDATE SET reason=EXCLUDED.reason RETURNING id::text`,
		p.ProjectID, p.EnvironmentID, s.Box.MAC("recipient", []byte(e)), maskEmail(e), reason).Scan(&id)
	if err != nil {
		return "", unavailable("suppress", err)
	}
	return id, nil
}

// KeyUnsuppress removes by suppression id or by email address.
func (s *Service) KeyUnsuppress(ctx context.Context, p *Principal, idOrEmail string) error {
	if uuidRe.MatchString(idOrEmail) {
		return s.DeleteSuppression(ctx, p.EnvironmentID, idOrEmail)
	}
	e, ok := NormalizeEmail(strings.ToLower(strings.TrimSpace(idOrEmail)))
	if !ok {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM suppressions WHERE environment_id=$1 AND project_id=$2 AND recipient_hash=$3`,
		p.EnvironmentID, p.ProjectID, s.Box.MAC("recipient", []byte(e)))
	if err != nil {
		return unavailable("unsuppress", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

// ---- sandbox captures (admin and owner only; enforced by the API layer)

func (s *Service) AdminCaptures(ctx context.Context, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT m.id::text AS id, m.template, m.template_version, m.locale, p.name AS project, m.created_at, c.expires_at
FROM sandbox_captures c JOIN messages m ON m.id=c.message_id JOIN projects p ON p.id=m.project_id
WHERE c.expires_at > now() ORDER BY m.created_at DESC`)
}

// AdminCapture returns the decrypted raw captured message.
func (s *Service) AdminCapture(ctx context.Context, id string) ([]byte, error) {
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	var enc []byte
	err := s.Pool.QueryRow(ctx, `SELECT raw_enc FROM sandbox_captures WHERE message_id=$1 AND expires_at > now()`, id).Scan(&enc)
	if errors.Is(err, db.ErrNoRows) {
		return nil, newErr(404, "not_found")
	}
	if err != nil {
		return nil, unavailable("capture", err)
	}
	raw, err := s.Box.Open(enc, []byte("capture:"+id))
	if err != nil {
		return nil, unavailable("capture_open", err)
	}
	return raw, nil
}

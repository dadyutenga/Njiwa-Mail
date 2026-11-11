package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/njiwa-tz/mail/internal/db"
	"strings"
	"time"
	"unicode"

	"github.com/njiwa-tz/mail/internal/tmpl"
)

// Admin operations are CLI-only. There are deliberately no public
// administrative write endpoints in this release.

func (s *Service) envID(ctx context.Context, project, mode string) (pid, eid string, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT p.id::text, e.id::text FROM projects p JOIN environments e ON e.project_id=p.id WHERE p.name=$1 AND e.mode=$2`,
		project, mode).Scan(&pid, &eid)
	if errors.Is(err, db.ErrNoRows) {
		return "", "", fmt.Errorf("project %q environment %q not found", project, mode)
	}
	return
}

func validMode(m string) error {
	if m != "sandbox" && m != "live" {
		return fmt.Errorf("mode must be sandbox or live")
	}
	return nil
}

// CreateProject creates a project with a sandbox (unrestricted, capture-only)
// and a live (recipient-restricted by default) environment.
func (s *Service) CreateProject(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("name required")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var pid string
	if err := tx.QueryRow(ctx, `INSERT INTO projects(name) VALUES($1) RETURNING id::text`, name).Scan(&pid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO environments(project_id, mode, restrict_recipients) VALUES ($1,'sandbox',false),($1,'live',true)`, pid); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) SetProjectEnabled(ctx context.Context, name string, enabled bool) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE projects SET enabled=$2 WHERE name=$1`, name, enabled)
	if err == nil && tag.RowsAffected() == 0 {
		err = errors.New("project not found")
	}
	return err
}

func (s *Service) SetQuotas(ctx context.Context, name string, perMin, perDay, rcptMin, rcptHour int) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE projects SET
 send_per_minute=CASE WHEN $2>0 THEN $2 ELSE send_per_minute END,
 send_per_day=CASE WHEN $3>0 THEN $3 ELSE send_per_day END,
 recipient_per_minute=CASE WHEN $4>0 THEN $4 ELSE recipient_per_minute END,
 recipient_per_hour=CASE WHEN $5>0 THEN $5 ELSE recipient_per_hour END WHERE name=$1`, name, perMin, perDay, rcptMin, rcptHour)
	if err == nil && tag.RowsAffected() == 0 {
		err = errors.New("project not found")
	}
	return err
}

// SetOrigins replaces the approved https origins (reset-link validation).
func (s *Service) SetOrigins(ctx context.Context, project, mode string, origins []string) error {
	if err := validMode(mode); err != nil {
		return err
	}
	norm := []string{}
	for _, o := range origins {
		n, err := tmpl.NormalizeOrigin(o)
		if err != nil {
			return err
		}
		norm = append(norm, n)
	}
	_, eid, err := s.envID(ctx, project, mode)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `UPDATE environments SET approved_origins=$2 WHERE id=$1`, eid, norm)
	return err
}

// AllowRecipient adds an exact address or "@domain" to the live allowlist.
func (s *Service) AllowRecipient(ctx context.Context, project, mode, pattern string, remove bool) error {
	if err := validMode(mode); err != nil {
		return err
	}
	pat := strings.ToLower(strings.TrimSpace(pattern))
	if strings.HasPrefix(pat, "@") {
		if _, ok := NormalizeEmail("x" + pat); !ok {
			return errors.New("invalid domain pattern")
		}
	} else if _, ok := NormalizeEmail(pat); !ok {
		return errors.New("invalid email address")
	}
	_, eid, err := s.envID(ctx, project, mode)
	if err != nil {
		return err
	}
	if remove {
		_, err = s.Pool.Exec(ctx, `UPDATE environments SET recipient_allowlist=array_remove(recipient_allowlist,$2) WHERE id=$1`, eid, pat)
	} else {
		_, err = s.Pool.Exec(ctx, `UPDATE environments SET recipient_allowlist=(SELECT array_agg(DISTINCT x) FROM unnest(recipient_allowlist || $2::text) x) WHERE id=$1`, eid, pat)
	}
	return err
}

func (s *Service) SetRestrictRecipients(ctx context.Context, project, mode string, restrict bool) error {
	if err := validMode(mode); err != nil {
		return err
	}
	_, eid, err := s.envID(ctx, project, mode)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `UPDATE environments SET restrict_recipients=$2 WHERE id=$1`, eid, restrict)
	return err
}

func (s *Service) AddSender(ctx context.Context, project, mode, email, display string) error {
	if err := validMode(mode); err != nil {
		return err
	}
	e, ok := NormalizeEmail(email)
	if !ok {
		return errors.New("invalid sender address")
	}
	if len(display) > 64 {
		return errors.New("display name too long")
	}
	for _, r := range display {
		if unicode.IsControl(r) {
			return errors.New("display name contains control characters")
		}
	}
	pid, eid, err := s.envID(ctx, project, mode)
	if err != nil {
		return err
	}
	domain := e[strings.LastIndexByte(e, '@')+1:]
	if mode == "live" {
		// Ownership and the claim are required to create a Live sender; DNS
		// (SPF/DKIM/DMARC) is enforced when mail is accepted.
		if err := s.requireLiveDomain(ctx, s.Pool, pid, domain, false); err != nil {
			return err
		}
	}
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM sender_identities WHERE project_id=$1 AND split_part(email,'@',2)=$2 AND email <> $3`, pid, domain, e).Scan(&n); err != nil {
		return err
	}
	if n >= 100 {
		return errors.New("a domain can have at most 100 sender identities")
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO sender_identities(project_id, environment_id, email, display_name) VALUES ($1,$2,$3,$4)
ON CONFLICT (environment_id, email) DO UPDATE SET display_name=EXCLUDED.display_name`, pid, eid, e, display)
	return err
}

// CreateKey issues a key. The secret is returned once and never stored.
func (s *Service) CreateKey(ctx context.Context, project, mode string, scopes []string, label string, ttl time.Duration) (token, prefix string, err error) {
	if err = validMode(mode); err != nil {
		return
	}
	for _, sc := range scopes {
		if !validScope(sc) {
			return "", "", fmt.Errorf("unknown scope %q", sc)
		}
	}
	if len(scopes) == 0 {
		return "", "", errors.New("at least one scope required")
	}
	pid, eid, err := s.envID(ctx, project, mode)
	if err != nil {
		return "", "", err
	}
	token, prefix, hash := newKeyMaterial(mode)
	var exp *time.Time
	if ttl > 0 {
		t := time.Now().Add(ttl)
		exp = &t
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO api_keys(prefix, secret_hash, project_id, environment_id, scopes, label, expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		prefix, hash, pid, eid, scopes, label, exp)
	if err != nil {
		return "", "", err
	}
	return token, prefix, nil
}

func (s *Service) RevokeKey(ctx context.Context, prefix string) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE api_keys SET revoked_at=COALESCE(revoked_at, now()) WHERE prefix=$1`, prefix)
	if err == nil && tag.RowsAffected() == 0 {
		err = errors.New("key not found")
	}
	return err
}

// RotateKey issues a replacement with identical project/env/scopes and lets
// the old key live for grace (0 = revoke immediately).
func (s *Service) RotateKey(ctx context.Context, prefix string, grace time.Duration) (token, newPrefix string, err error) {
	var pid, eid, mode, label string
	var scopes []string
	err = s.Pool.QueryRow(ctx, `SELECT k.project_id::text, k.environment_id::text, e.mode, k.scopes, k.label FROM api_keys k JOIN environments e ON e.id=k.environment_id WHERE k.prefix=$1 AND k.revoked_at IS NULL`, prefix).
		Scan(&pid, &eid, &mode, &scopes, &label)
	if err != nil {
		return "", "", errors.New("active key not found")
	}
	token, newPrefix, hash := newKeyMaterial(mode)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO api_keys(prefix, secret_hash, project_id, environment_id, scopes, label) VALUES ($1,$2,$3,$4,$5,$6)`,
		newPrefix, hash, pid, eid, scopes, label); err != nil {
		return "", "", err
	}
	if grace <= 0 {
		_, err = tx.Exec(ctx, `UPDATE api_keys SET revoked_at=now() WHERE prefix=$1`, prefix)
	} else {
		_, err = tx.Exec(ctx, `UPDATE api_keys SET expires_at=LEAST(COALESCE(expires_at,'infinity'), now()+make_interval(secs=>$2::float8)) WHERE prefix=$1`, prefix, grace.Seconds())
	}
	if err != nil {
		return "", "", err
	}
	return token, newPrefix, tx.Commit(ctx)
}

type KeyInfo struct {
	Prefix, Project, Mode, Label string
	Scopes                       []string
	Expires, Revoked             *time.Time
}

func (s *Service) ListKeys(ctx context.Context, project string) ([]KeyInfo, error) {
	rows, err := s.Pool.Query(ctx, `SELECT k.prefix, p.name, e.mode, k.label, k.scopes, k.expires_at, k.revoked_at
FROM api_keys k JOIN projects p ON p.id=k.project_id JOIN environments e ON e.id=k.environment_id
WHERE ($1='' OR p.name=$1) ORDER BY k.created_at`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KeyInfo
	for rows.Next() {
		var k KeyInfo
		if err := rows.Scan(&k.Prefix, &k.Project, &k.Mode, &k.Label, &k.Scopes, &k.Expires, &k.Revoked); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Service) Suppress(ctx context.Context, project, mode, email, reason string) error {
	if err := validMode(mode); err != nil {
		return err
	}
	e, ok := NormalizeEmail(email)
	if !ok {
		return errors.New("invalid email address")
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("reason required")
	}
	pid, eid, err := s.envID(ctx, project, mode)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO suppressions(project_id, environment_id, recipient_hash, recipient_masked, reason) VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (environment_id, recipient_hash) DO UPDATE SET reason=EXCLUDED.reason`, pid, eid, s.Box.MAC("recipient", []byte(e)), maskEmail(e), reason)
	return err
}

func (s *Service) Unsuppress(ctx context.Context, project, mode, email string) error {
	e, ok := NormalizeEmail(email)
	if !ok {
		return errors.New("invalid email address")
	}
	_, eid, err := s.envID(ctx, project, mode)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `DELETE FROM suppressions WHERE environment_id=$1 AND recipient_hash=$2`, eid, s.Box.MAC("recipient", []byte(e)))
	return err
}

type MessageRow struct {
	ID, State, Template, Project, Mode string
	Attempts                           int
	Created                            time.Time
}

func (s *Service) ListMessages(ctx context.Context, state string, limit int) ([]MessageRow, error) {
	rows, err := s.Pool.Query(ctx, `SELECT m.id::text, m.state, m.template, p.name, e.mode, m.attempts, m.created_at
FROM messages m JOIN projects p ON p.id=m.project_id JOIN environments e ON e.id=m.environment_id
WHERE ($1='' OR m.state=$1) ORDER BY m.created_at DESC LIMIT $2`, state, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MessageRow
	for rows.Next() {
		var r MessageRow
		if err := rows.Scan(&r.ID, &r.State, &r.Template, &r.Project, &r.Mode, &r.Attempts, &r.Created); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ResolveUnknown records an operator's reconciliation of an unknown outcome
// after inspecting Mailcow logs. It never causes a resend.
func (s *Service) ResolveUnknown(ctx context.Context, id, to, note string) error {
	if to != "submitted" && to != "failed" {
		return errors.New("resolution must be submitted or failed")
	}
	if strings.TrimSpace(note) == "" {
		return errors.New("note required (e.g. mailcow queue id or log reference)")
	}
	if len(note) > 120 {
		note = note[:120]
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE messages SET state=$2, updated_at=now(), submitted_at=CASE WHEN $2='submitted' THEN now() ELSE submitted_at END WHERE id=$1 AND state='unknown'`, id, to)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("message not found or not in unknown state")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO message_events(message_id, event_type, reason_code) VALUES ($1,'operator_resolved',$2)`, id, to+": "+note); err != nil {
		return err
	}
	// The webhook carries a fixed reason code, never the operator's note.
	if err := emitMessageEvent(ctx, tx, id, "email."+to, "operator_resolved"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ShowCapture decrypts a sandbox capture (operator-only; honours expiry).
func (s *Service) ShowCapture(ctx context.Context, id string) ([]byte, error) {
	var enc []byte
	err := s.Pool.QueryRow(ctx, `SELECT raw_enc FROM sandbox_captures WHERE message_id=$1 AND expires_at > now()`, id).Scan(&enc)
	if err != nil {
		return nil, errors.New("capture not found or expired")
	}
	return s.Box.Open(enc, []byte("capture:"+id))
}

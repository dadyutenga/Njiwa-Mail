package service

import (
	"context"
	"errors"
	"strings"

	"github.com/njiwa-tz/mail/internal/db"
)

// Sender identities of a project environment, with a purpose and links to the
// things that send as them.

var senderPurposes = map[string]bool{"transactional": true, "marketing": true, "support": true, "system": true}

// senderPick orders the senders to fall back on: the default transactional
// sender, then any default, then the oldest. Disabled senders never apply.
const senderPick = `(is_default AND purpose='transactional') DESC, is_default DESC, created_at`

// WSSenders lists the environment's senders with the state of their domain and
// how much they are used.
func (s *Service) WSSenders(ctx context.Context, sc *Scope, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT si.id::text AS id, si.email, si.display_name, si.purpose, si.reply_to, si.disabled_at,
  split_part(si.email,'@',2) AS domain,
  (SELECT d.id::text FROM domains d WHERE d.project_id=si.project_id AND d.name=split_part(si.email,'@',2) ORDER BY d.purpose LIMIT 1) AS domain_id,
  CASE WHEN `+senderVerifiedSQL+` THEN 'verified'
       WHEN EXISTS (SELECT 1 FROM domain_claims c WHERE c.name=split_part(si.email,'@',2) AND c.project_id IS DISTINCT FROM si.project_id) THEN 'claimed_elsewhere'
       WHEN EXISTS (SELECT 1 FROM domains d WHERE d.project_id=si.project_id AND d.name=split_part(si.email,'@',2)) THEN 'needs_dns'
       ELSE 'domain_not_added' END AS status,
  si.is_default, si.last_used_at, si.created_at,
  (SELECT count(*) FROM messages m WHERE m.environment_id=si.environment_id AND m.sender=si.email) AS messages
FROM sender_identities si WHERE si.environment_id=$1 ORDER BY (si.disabled_at IS NOT NULL), si.purpose, si.is_default DESC, si.email`, sc.EnvID)
}

// SenderInput is what a person sets on a sender. Nil fields stay as they are.
type SenderInput struct {
	DisplayName *string `json:"display_name"`
	ReplyTo     *string `json:"reply_to"`
	Purpose     *string `json:"purpose"`
	Disabled    *bool   `json:"disabled"`
}

// checkPurpose validates a purpose for an address: marketing mail must come
// from a domain marked for bulk mail, to protect transactional delivery.
func (s *Service) checkPurpose(ctx context.Context, projectID, email, purpose string) error {
	if !senderPurposes[purpose] {
		return badRequest("the purpose is transactional, marketing, support or system")
	}
	if purpose != "marketing" {
		return nil
	}
	domain := email[strings.LastIndexByte(email, '@')+1:]
	var bulk bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM domains WHERE project_id=$1 AND name=$2 AND bulk)`, projectID, domain).Scan(&bulk); err != nil {
		return unavailable("sender_purpose", err)
	}
	if !bulk {
		return &Error{Status: 409, Code: "marketing_needs_bulk_domain",
			Message: "a marketing sender must use a bulk domain; mark " + domain + " for bulk mail on the Domains page, or use another domain"}
	}
	return nil
}

// WSAddSender adds a sender on a verified sending domain of this project.
func (s *Service) WSAddSender(ctx context.Context, sc *Scope, email string, in SenderInput) (string, error) {
	e, ok := NormalizeEmail(strings.ToLower(strings.TrimSpace(email)))
	if !ok {
		return "", badRequest("enter a full email address")
	}
	domain := e[strings.LastIndexByte(e, '@')+1:]
	var verified bool
	err := s.Pool.QueryRow(ctx, `SELECT bool_or(verified_at IS NOT NULL) FROM domains WHERE project_id=$1 AND name=$2 AND purpose IN ('sending','both')`,
		sc.ProjectID, domain).Scan(&verified)
	if err != nil || !verified {
		return "", &Error{Status: 409, Code: "domain_not_verified", Message: "add " + domain + " on the Domains page and verify it before adding senders on it"}
	}
	purpose := "transactional"
	if in.Purpose != nil && *in.Purpose != "" {
		purpose = *in.Purpose
	}
	if err := s.checkPurpose(ctx, sc.ProjectID, e, purpose); err != nil {
		return "", err
	}
	display := ""
	if in.DisplayName != nil {
		display = *in.DisplayName
	}
	var existed bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sender_identities WHERE environment_id=$1 AND email=$2)`, sc.EnvID, e).Scan(&existed); err != nil {
		return "", unavailable("sender_lookup", err)
	}
	if err := s.AddSender(ctx, sc.Project, sc.Mode, e, display); err != nil {
		return "", opErr(err)
	}
	var id string
	if err := s.Pool.QueryRow(ctx, `SELECT id::text FROM sender_identities WHERE environment_id=$1 AND email=$2`, sc.EnvID, e).Scan(&id); err != nil {
		return "", notFound(err)
	}
	if !existed {
		if _, err := s.Pool.Exec(ctx, `UPDATE sender_identities SET purpose=$2 WHERE id=$1`, id, purpose); err != nil {
			return "", unavailable("sender_purpose", err)
		}
	}
	if in.ReplyTo != nil {
		if err := s.UpdateSender(ctx, sc, id, SenderInput{ReplyTo: in.ReplyTo}); err != nil {
			return "", err
		}
	}
	// The first sender of a purpose becomes its default.
	_, _ = s.Pool.Exec(ctx, `UPDATE sender_identities si SET is_default=true WHERE si.id=$1 AND si.disabled_at IS NULL AND NOT EXISTS
  (SELECT 1 FROM sender_identities o WHERE o.environment_id=si.environment_id AND o.purpose=si.purpose AND o.is_default)`, id)
	return id, nil
}

// UpdateSender edits a sender in place, or disables it so its history stays.
func (s *Service) UpdateSender(ctx context.Context, sc *Scope, id string, in SenderInput) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	var email, purpose string
	err := s.Pool.QueryRow(ctx, `SELECT email, purpose FROM sender_identities WHERE id=$1 AND environment_id=$2`, id, sc.EnvID).Scan(&email, &purpose)
	if errors.Is(err, db.ErrNoRows) {
		return newErr(404, "not_found")
	}
	if err != nil {
		return unavailable("sender_lookup", err)
	}
	if in.DisplayName != nil {
		d := strings.TrimSpace(*in.DisplayName)
		if len(d) > 64 || strings.ContainsAny(d, "\r\n\x00") {
			return badRequest("the display name is at most 64 characters on one line")
		}
		in.DisplayName = &d
	}
	if in.ReplyTo != nil {
		r := strings.ToLower(strings.TrimSpace(*in.ReplyTo))
		if r != "" {
			n, ok := NormalizeEmail(r)
			if !ok {
				return badRequest("the reply-to is not an email address")
			}
			r = n
		}
		in.ReplyTo = &r
	}
	changed := in.Purpose != nil && *in.Purpose != purpose
	if changed {
		if err := s.checkPurpose(ctx, sc.ProjectID, email, *in.Purpose); err != nil {
			return err
		}
	}
	// A disabled sender, or one moved to another purpose, stops being a default.
	_, err = s.Pool.Exec(ctx, `UPDATE sender_identities SET
  display_name=COALESCE($3, display_name), reply_to=COALESCE($4, reply_to), purpose=COALESCE($5, purpose),
  disabled_at=CASE WHEN $6::bool IS NULL THEN disabled_at WHEN $6 THEN COALESCE(disabled_at, now()) ELSE NULL END,
  is_default=is_default AND NOT $7::bool AND NOT COALESCE($6::bool, false)
WHERE id=$1 AND environment_id=$2`, id, sc.EnvID, in.DisplayName, in.ReplyTo, in.Purpose, in.Disabled, changed)
	if err != nil {
		return unavailable("sender_update", err)
	}
	return nil
}

// SetDefaultSender makes a sender the default of its purpose.
func (s *Service) SetDefaultSender(ctx context.Context, sc *Scope, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	var purpose string
	var disabled bool
	err = tx.QueryRow(ctx, `SELECT purpose, disabled_at IS NOT NULL FROM sender_identities WHERE id=$1 AND environment_id=$2 FOR UPDATE`, id, sc.EnvID).Scan(&purpose, &disabled)
	if errors.Is(err, db.ErrNoRows) {
		return newErr(404, "not_found")
	}
	if err != nil {
		return unavailable("default_sender", err)
	}
	if disabled {
		return &Error{Status: 409, Code: "sender_disabled", Message: "enable the sender before making it the default"}
	}
	if _, err := tx.Exec(ctx, `UPDATE sender_identities SET is_default=false WHERE environment_id=$1 AND purpose=$2 AND is_default`, sc.EnvID, purpose); err != nil {
		return unavailable("default_sender", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE sender_identities SET is_default=true WHERE id=$1`, id); err != nil {
		return unavailable("default_sender", err)
	}
	return opErr(tx.Commit(ctx))
}

// A template pins an address, which stays valid while any environment of the
// project still has it. Uses $1 project, $2 environment, $3 address.
const templatePins = `FROM templates t WHERE t.project_id=$1 AND t.default_sender=$3 AND NOT EXISTS
  (SELECT 1 FROM sender_identities o WHERE o.project_id=$1 AND o.environment_id<>$2 AND o.email=$3)`

type queryRows interface {
	Query(ctx context.Context, q string, args ...any) (*db.Rows, error)
}

// SenderUsage lists what would be left pointing at nothing without this sender.
type SenderUsage struct {
	Templates []SenderLink `json:"templates"`
	Keys      []SenderLink `json:"api_keys"`
	Campaigns []SenderLink `json:"campaigns"`
	Forms     []SenderLink `json:"forms"`
	Chats     []SenderLink `json:"chats"`
	Reports   []SenderLink `json:"reports"`
}

// SenderLink is one thing that uses a sender.
type SenderLink struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// InUse reports whether anything uses the sender.
func (u *SenderUsage) InUse() bool {
	return len(u.Templates)+len(u.Keys)+len(u.Campaigns)+len(u.Forms)+len(u.Chats)+len(u.Reports) > 0
}

func links(ctx context.Context, q queryRows, query string, args ...any) ([]SenderLink, error) {
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SenderLink{}
	for rows.Next() {
		var l SenderLink
		if err := rows.Scan(&l.ID, &l.Name); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func senderUsage(ctx context.Context, q queryRows, projectID, envID, id, email string) (*SenderUsage, error) {
	u := &SenderUsage{}
	var err error
	for _, part := range []struct {
		into  *[]SenderLink
		query string
		args  []any
	}{
		{&u.Templates, `SELECT t.id::text, t.slug ` + templatePins, []any{projectID, envID, email}},
		{&u.Keys, `SELECT prefix, label FROM api_keys WHERE environment_id=$1 AND revoked_at IS NULL AND $2=ANY(allowed_senders) ORDER BY label`, []any{envID, email}},
		{&u.Campaigns, `SELECT id::text, name FROM campaigns WHERE environment_id=$1 AND sender_email=$2 AND state IN ('draft','scheduled','sending','paused') ORDER BY name`, []any{envID, email}},
		{&u.Forms, `SELECT id::text, name FROM forms WHERE environment_id=$1 AND reply_from=$2 ORDER BY name`, []any{envID, email}},
		{&u.Chats, `SELECT id::text, name FROM chat_widgets WHERE environment_id=$1 AND reply_from=$2 ORDER BY name`, []any{envID, email}},
		{&u.Reports, `SELECT id::text, name FROM report_schedules WHERE sender_identity_id=$1 ORDER BY name`, []any{id}},
	} {
		if *part.into, err = links(ctx, q, part.query, part.args...); err != nil {
			return nil, unavailable("sender_usage", err)
		}
	}
	return u, nil
}

// SenderUsageOf lists the relations of one sender.
func (s *Service) SenderUsageOf(ctx context.Context, sc *Scope, id string) (*SenderUsage, error) {
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	var email string
	err := s.Pool.QueryRow(ctx, `SELECT email FROM sender_identities WHERE id=$1 AND environment_id=$2`, id, sc.EnvID).Scan(&email)
	if errors.Is(err, db.ErrNoRows) {
		return nil, newErr(404, "not_found")
	}
	if err != nil {
		return nil, unavailable("sender_lookup", err)
	}
	return senderUsage(ctx, s.Pool, sc.ProjectID, sc.EnvID, id, email)
}

// WSDeleteSender removes a sender. When something uses it, replaceWith (another
// enabled sender of the environment) takes its place everywhere; without one
// the delete is refused. Nothing is left pointing at a missing sender.
func (s *Service) WSDeleteSender(ctx context.Context, sc *Scope, id, replaceWith string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("sender_delete", err)
	}
	defer tx.Rollback(ctx)
	var email string
	err = tx.QueryRow(ctx, `SELECT email FROM sender_identities WHERE id=$1 AND environment_id=$2 FOR UPDATE`, id, sc.EnvID).Scan(&email)
	if errors.Is(err, db.ErrNoRows) {
		return newErr(404, "not_found")
	}
	if err != nil {
		return unavailable("sender_delete", err)
	}
	use, err := senderUsage(ctx, tx, sc.ProjectID, sc.EnvID, id, email)
	if err != nil {
		return err
	}
	if use.InUse() {
		if replaceWith == "" {
			return &Error{Status: 409, Code: "sender_in_use", Message: "this sender is in use; choose a sender to replace it with, or cancel"}
		}
		if !uuidRe.MatchString(replaceWith) || replaceWith == id {
			return badRequest("choose another sender of this environment")
		}
		var to string
		err := tx.QueryRow(ctx, `SELECT email FROM sender_identities WHERE id=$1 AND environment_id=$2 AND disabled_at IS NULL`, replaceWith, sc.EnvID).Scan(&to)
		if errors.Is(err, db.ErrNoRows) {
			return badRequest("choose an enabled sender of this environment")
		}
		if err != nil {
			return unavailable("sender_delete", err)
		}
		if len(use.Campaigns) > 0 {
			if err := s.checkPurpose(ctx, sc.ProjectID, to, "marketing"); err != nil {
				return err
			}
		}
		for _, step := range []struct {
			query string
			args  []any
		}{
			{`UPDATE templates SET default_sender=$4 WHERE id IN (SELECT t.id ` + templatePins + `)`, []any{sc.ProjectID, sc.EnvID, email, to}},
			{`UPDATE api_keys SET allowed_senders = ARRAY(SELECT DISTINCT unnest(array_replace(allowed_senders, $2, $3))) WHERE environment_id=$1 AND $2=ANY(allowed_senders)`, []any{sc.EnvID, email, to}},
			{`UPDATE campaigns SET sender_email=$3 WHERE environment_id=$1 AND sender_email=$2 AND state IN ('draft','scheduled','sending','paused')`, []any{sc.EnvID, email, to}},
			{`UPDATE forms SET reply_from=$3 WHERE environment_id=$1 AND reply_from=$2`, []any{sc.EnvID, email, to}},
			{`UPDATE chat_widgets SET reply_from=$3 WHERE environment_id=$1 AND reply_from=$2`, []any{sc.EnvID, email, to}},
			{`UPDATE report_schedules SET sender_identity_id=$2 WHERE sender_identity_id=$1`, []any{id, replaceWith}},
		} {
			if _, err := tx.Exec(ctx, step.query, step.args...); err != nil {
				return unavailable("sender_replace", err)
			}
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sender_identities WHERE id=$1`, id); err != nil {
		return unavailable("sender_delete", err)
	}
	return opErr(tx.Commit(ctx))
}

// SetKeySenders limits an API key of the environment to the given senders; an
// empty list removes the limit.
func (s *Service) SetKeySenders(ctx context.Context, sc *Scope, prefix string, senders []string) error {
	list, err := s.knownSenders(ctx, sc, senders)
	if err != nil {
		return err
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE api_keys SET allowed_senders=$3 WHERE prefix=$1 AND environment_id=$2`, prefix, sc.EnvID, list)
	if err != nil {
		return unavailable("key_senders", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

// knownSenders normalises addresses and requires each to be a sender of the
// environment.
func (s *Service) knownSenders(ctx context.Context, sc *Scope, senders []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range senders {
		e, ok := NormalizeEmail(strings.ToLower(strings.TrimSpace(raw)))
		if !ok {
			return nil, badRequest("not an email address: " + clean(raw, 100))
		}
		if seen[e] {
			continue
		}
		var exists bool
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sender_identities WHERE environment_id=$1 AND email=$2)`, sc.EnvID, e).Scan(&exists); err != nil {
			return nil, unavailable("sender_lookup", err)
		}
		if !exists {
			return nil, badRequest(e + " is not a sender of this environment")
		}
		seen[e], out = true, append(out, e)
	}
	return out, nil
}

// SetTemplateSender pins (or with "" removes) the sender a template is sent
// from when a request names none.
func (s *Service) SetTemplateSender(ctx context.Context, sc *Scope, templateID, email string) error {
	if email = strings.ToLower(strings.TrimSpace(email)); email != "" {
		list, err := s.knownSenders(ctx, sc, []string{email})
		if err != nil {
			return err
		}
		email = list[0]
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE templates SET default_sender=$3 WHERE id=$1 AND project_id=$2`, templateID, sc.ProjectID, email)
	if err != nil {
		return unavailable("template_sender", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

// templateSender is the sender pinned by the template a request names, or "".
func (s *Service) templateSender(ctx context.Context, projectID, slug string) string {
	var from string
	_ = s.Pool.QueryRow(ctx, `SELECT default_sender FROM templates WHERE slug=$2 AND project_id=$1 AND NOT archived`, projectID, slug).Scan(&from)
	return from
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/secure"
)

// Automations act on incoming mail. Rules run in position order inside the
// transaction that stores the message; each run is recorded. Auto-replies
// never answer automatic mail and go at most once per sender per rule per day.

type RuleInput struct {
	Name              *string `json:"name"`
	MailboxID         *string `json:"mailbox_id"` // "" = every mailbox
	Enabled           *bool   `json:"enabled"`
	CondSender        *string `json:"cond_sender"`
	CondSubject       *string `json:"cond_subject"`
	CondHasAttachment *bool   `json:"cond_has_attachment"`
	Action            *string `json:"action"`
	Target            *string `json:"target"`
	TemplateID        *string `json:"template_id"`
}

type rule struct {
	ID, Name, Action, Target string
	AddressID, TemplateID    *string
	CondSender, CondSubject  string
	CondAttachment, Enabled  bool
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

func (s *Service) checkRule(ctx context.Context, sc *Scope, r *rule) error {
	if r.Name == "" || len(r.Name) > 100 {
		return badRequest("give the rule a name (up to 100 characters)")
	}
	if r.AddressID != nil && *r.AddressID != "" {
		if err := s.InScope(ctx, sc, "inbound_addresses", *r.AddressID); err != nil {
			return badRequest("choose a mailbox of this project")
		}
	} else {
		r.AddressID = nil
	}
	r.CondSender, r.CondSubject = clean(strings.ToLower(r.CondSender), 200), clean(r.CondSubject, 200)
	switch r.Action {
	case "forward":
		e, ok := NormalizeEmail(strings.ToLower(r.Target))
		if !ok {
			return badRequest("forward needs an email address")
		}
		r.Target = e
	case "webhook":
		if err := s.InScope(ctx, sc, "webhook_endpoints", r.Target); err != nil {
			return badRequest("choose a webhook endpoint of this project")
		}
	case "auto_reply":
		if r.TemplateID == nil || *r.TemplateID == "" {
			return badRequest("auto-reply needs a template")
		}
		if _, err := s.TemplateInScope(ctx, sc, *r.TemplateID); err != nil {
			return badRequest("choose a template of this project")
		}
		var published bool
		_ = s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM template_versions WHERE template_id=$1 AND published_at IS NOT NULL)`, *r.TemplateID).Scan(&published)
		if !published {
			return badRequest("publish the template before using it for auto-replies")
		}
	case "label":
		r.Target = clean(strings.ToLower(r.Target), 40)
		if r.Target == "" {
			return badRequest("label needs a name")
		}
	case "archive":
		r.Target = ""
	default:
		return badRequest("action must be forward, webhook, auto_reply, label or archive")
	}
	if r.Action != "auto_reply" {
		r.TemplateID = nil
	}
	return nil
}

func (s *Service) CreateRule(ctx context.Context, sc *Scope, in RuleInput) (string, error) {
	r := rule{Name: str(in.Name), AddressID: in.MailboxID, CondSender: str(in.CondSender), CondSubject: str(in.CondSubject),
		Action: str(in.Action), Target: str(in.Target), TemplateID: in.TemplateID}
	if in.CondHasAttachment != nil {
		r.CondAttachment = *in.CondHasAttachment
	}
	if err := s.checkRule(ctx, sc, &r); err != nil {
		return "", err
	}
	var id string
	err := s.Pool.QueryRow(ctx, `INSERT INTO automation_rules(project_id, environment_id, address_id, position, name, enabled, cond_sender, cond_subject,
  cond_has_attachment, action, target, template_id)
VALUES ($1,$2,$3,(SELECT COALESCE(max(position),0)+1 FROM automation_rules WHERE environment_id=$2),$4,false,$5,$6,$7,$8,$9,$10) RETURNING id::text`,
		sc.ProjectID, sc.EnvID, r.AddressID, r.Name, r.CondSender, r.CondSubject, r.CondAttachment, r.Action, r.Target, r.TemplateID).Scan(&id)
	if err != nil {
		return "", unavailable("rule_create", err)
	}
	return id, nil
}

func (s *Service) loadRule(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) db.Row
}, id string) (*rule, error) {
	var r rule
	err := q.QueryRow(ctx, `SELECT id::text, name, action, target, address_id::text, template_id::text, cond_sender, cond_subject, cond_has_attachment, enabled
FROM automation_rules WHERE id=$1`, id).Scan(&r.ID, &r.Name, &r.Action, &r.Target, &r.AddressID, &r.TemplateID, &r.CondSender, &r.CondSubject, &r.CondAttachment, &r.Enabled)
	if err != nil {
		return nil, notFound(err)
	}
	return &r, nil
}

func (s *Service) UpdateRule(ctx context.Context, sc *Scope, id string, in RuleInput) error {
	if err := s.InScope(ctx, sc, "automation_rules", id); err != nil {
		return err
	}
	r, err := s.loadRule(ctx, s.Pool, id)
	if err != nil {
		return err
	}
	if in.Name != nil {
		r.Name = str(in.Name)
	}
	if in.MailboxID != nil {
		r.AddressID = in.MailboxID
	}
	if in.CondSender != nil {
		r.CondSender = str(in.CondSender)
	}
	if in.CondSubject != nil {
		r.CondSubject = str(in.CondSubject)
	}
	if in.CondHasAttachment != nil {
		r.CondAttachment = *in.CondHasAttachment
	}
	if in.Action != nil {
		r.Action = str(in.Action)
	}
	if in.Target != nil {
		r.Target = str(in.Target)
	}
	if in.TemplateID != nil {
		r.TemplateID = in.TemplateID
	}
	if in.Enabled != nil {
		r.Enabled = *in.Enabled
	}
	if err := s.checkRule(ctx, sc, r); err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `UPDATE automation_rules SET name=$2, address_id=$3, cond_sender=$4, cond_subject=$5, cond_has_attachment=$6,
  action=$7, target=$8, template_id=$9, enabled=$10 WHERE id=$1`, id, r.Name, r.AddressID, r.CondSender, r.CondSubject, r.CondAttachment,
		r.Action, r.Target, r.TemplateID, r.Enabled)
	return opErr(err)
}

func (s *Service) DeleteRule(ctx context.Context, sc *Scope, id string) error {
	if err := s.InScope(ctx, sc, "automation_rules", id); err != nil {
		return err
	}
	_, err := s.Pool.Exec(ctx, `DELETE FROM automation_rules WHERE id=$1`, id)
	return opErr(err)
}

// ReorderRules sets the run order to the given ids (all must be in scope).
func (s *Service) ReorderRules(ctx context.Context, sc *Scope, ids []string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	for i, id := range ids {
		if !uuidRe.MatchString(id) {
			return badRequest("unknown rule")
		}
		tag, err := tx.Exec(ctx, `UPDATE automation_rules SET position=$3 WHERE id=$1 AND environment_id=$2`, id, sc.EnvID, i+1)
		if err != nil {
			return unavailable("reorder", err)
		}
		if tag.RowsAffected() == 0 {
			return badRequest("unknown rule")
		}
	}
	return opErr(tx.Commit(ctx))
}

func (s *Service) ListRules(ctx context.Context, sc *Scope) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT r.id::text AS id, r.position, r.name, r.enabled, r.address_id::text AS mailbox_id,
  a.local_part || '@' || a.domain AS mailbox, r.cond_sender, r.cond_subject, r.cond_has_attachment, r.action, r.target,
  r.template_id::text AS template_id, t.slug AS template, r.created_at,
  (SELECT jsonb_build_object('result', x.result, 'detail', x.detail, 'at', x.at) FROM automation_runs x WHERE x.rule_id=r.id AND NOT x.dry_run ORDER BY x.at DESC LIMIT 1) AS last_run,
  (SELECT count(*) FROM automation_runs x WHERE x.rule_id=r.id AND NOT x.dry_run) AS runs
FROM automation_rules r LEFT JOIN inbound_addresses a ON a.id=r.address_id LEFT JOIN templates t ON t.id=r.template_id
WHERE r.environment_id=$1 ORDER BY r.position, r.created_at`, sc.EnvID)
}

func (s *Service) RuleRuns(ctx context.Context, sc *Scope, id string, pg Page) ([]map[string]any, bool, error) {
	if err := s.InScope(ctx, sc, "automation_rules", id); err != nil {
		return nil, false, err
	}
	return s.page(ctx, pg, `SELECT r.id, r.result, r.detail, r.dry_run, r.at, r.inbound_id::text AS inbound_id, r.message_id::text AS message_id, m.subject, m.header_from
FROM automation_runs r LEFT JOIN inbound_messages m ON m.id=r.inbound_id WHERE r.rule_id=$1 ORDER BY r.at DESC`, id)
}

// ---------------------------------------------------------------- execution

type inboundFacts struct {
	ID, AddressID, ProjectID, EnvID string
	EnvelopeFrom, HeaderFrom        string
	Subject, Text, HTML             string
	Headers                         map[string][]string
	Attachments                     int
	Recipient                       string
}

func (r *rule) matches(f *inboundFacts) bool {
	if r.AddressID != nil && *r.AddressID != f.AddressID {
		return false
	}
	if r.CondSender != "" && !strings.Contains(strings.ToLower(f.EnvelopeFrom+" "+f.HeaderFrom), r.CondSender) {
		return false
	}
	if r.CondSubject != "" && !strings.Contains(strings.ToLower(f.Subject), strings.ToLower(r.CondSubject)) {
		return false
	}
	if r.CondAttachment && f.Attachments == 0 {
		return false
	}
	return true
}

// automaticMail reports why a message must never get an auto-reply.
func automaticMail(f *inboundFacts) string {
	h := func(n string) string { return strings.ToLower(strings.TrimSpace(headerOne(f.Headers, n))) }
	from := strings.ToLower(strings.Trim(f.EnvelopeFrom, "<> "))
	switch {
	case from == "":
		return "bounce or empty sender"
	case strings.HasPrefix(from, "mailer-daemon@") || strings.HasPrefix(from, "postmaster@") || strings.HasPrefix(from, "noreply") ||
		strings.HasPrefix(from, "no-reply") || strings.HasPrefix(from, "bounce"):
		return "system sender"
	case h("Auto-Submitted") != "" && h("Auto-Submitted") != "no":
		return "Auto-Submitted: " + h("Auto-Submitted")
	case h("Precedence") == "bulk" || h("Precedence") == "list" || h("Precedence") == "junk":
		return "Precedence: " + h("Precedence")
	case h("List-Id") != "" || h("List-Unsubscribe") != "":
		return "mailing list"
	case h("X-Auto-Response-Suppress") != "":
		return "X-Auto-Response-Suppress"
	}
	return ""
}

// runAutomations runs the matching rules for one stored message, in order.
func (s *Service) runAutomations(ctx context.Context, tx *db.Tx, f *inboundFacts) error {
	rows, err := tx.Query(ctx, `SELECT id::text FROM automation_rules WHERE environment_id=$1 AND enabled AND (address_id IS NULL OR address_id=$2::uuid) ORDER BY position, created_at`,
		f.EnvID, f.AddressID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		r, err := s.loadRule(ctx, tx, id)
		if err != nil {
			return err
		}
		if !r.matches(f) {
			continue
		}
		result, detail, msgID, err := s.applyRule(ctx, tx, r, f, false)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO automation_runs(rule_id, inbound_id, result, detail, message_id) VALUES ($1,$2,$3,$4,NULLIF($5,'')::uuid)`,
			r.ID, f.ID, result, clean(detail, 300), msgID); err != nil {
			return err
		}
	}
	return nil
}

// applyRule performs (or, in a dry run, describes) one action.
func (s *Service) applyRule(ctx context.Context, tx *db.Tx, r *rule, f *inboundFacts, dry bool) (result, detail, msgID string, err error) {
	done := "done"
	if dry {
		done = "would_run"
	}
	switch r.Action {
	case "label":
		if !dry {
			if _, err := tx.Exec(ctx, `UPDATE inbound_messages SET labels=array_append(array_remove(labels,$2),$2) WHERE id=$1`, f.ID, r.Target); err != nil {
				return "", "", "", err
			}
		}
		return done, "label " + r.Target, "", nil
	case "archive":
		if !dry {
			if _, err := tx.Exec(ctx, `UPDATE inbound_messages SET archived_at=COALESCE(archived_at, now()) WHERE id=$1`, f.ID); err != nil {
				return "", "", "", err
			}
		}
		return done, "archived", "", nil
	case "webhook":
		if !dry {
			if _, err := tx.Exec(ctx, `SELECT dm_emit($1::uuid, 'inbound.received', jsonb_build_object('inbound_id', $2::text, 'address_id', $3::text, 'rule_id', $4::text), $5::uuid)`,
				f.EnvID, f.ID, f.AddressID, r.ID, r.Target); err != nil {
				return "", "", "", err
			}
		}
		return done, "webhook delivery queued", "", nil
	case "forward":
		if strings.EqualFold(r.Target, f.Recipient) {
			return "skipped", "forward target is the mailbox itself", "", nil
		}
		if dry {
			return done, "forward to " + maskEmail(r.Target), "", nil
		}
		id, why, err := s.queueAutomationMail(ctx, tx, f, r.Target, "Fwd: "+f.Subject, f.Text, f.HTML, nil)
		if err != nil || why != "" {
			return "failed", why, "", err
		}
		return done, "forwarded to " + maskEmail(r.Target), id, nil
	case "auto_reply":
		if why := automaticMail(f); why != "" {
			return "skipped", "not answering automatic mail (" + why + ")", "", nil
		}
		to, ok := NormalizeEmail(strings.ToLower(strings.Trim(f.EnvelopeFrom, "<> ")))
		if !ok {
			return "skipped", "sender address is not valid", "", nil
		}
		hash := s.Box.MAC("recipient", []byte(to))
		if dry {
			var sent bool
			_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM autoreply_log WHERE rule_id=$1 AND sender_hash=$2 AND day=current_date)`, r.ID, hash).Scan(&sent)
			if sent {
				return "would_skip", "already answered this sender today", "", nil
			}
			return done, "auto-reply to " + maskEmail(to), "", nil
		}
		tag, err := tx.Exec(ctx, `INSERT INTO autoreply_log(rule_id, sender_hash, day) VALUES ($1,$2,current_date) ON CONFLICT DO NOTHING`, r.ID, hash)
		if err != nil {
			return "", "", "", err
		}
		if tag.RowsAffected() == 0 {
			return "skipped", "already answered this sender today", "", nil
		}
		subject, text, html, rerr := s.renderAutoReply(ctx, tx, r, f)
		if rerr != nil {
			return "failed", "template could not be rendered: " + rerr.Error(), "", nil
		}
		sys := map[string]string{}
		if mid := strings.TrimSpace(headerOne(f.Headers, "Message-Id")); mid != "" && !strings.ContainsAny(mid, "\r\n") {
			sys["In-Reply-To"], sys["References"] = mid, mid
		}
		id, why, err := s.queueAutomationMail(ctx, tx, f, to, subject, text, html, sys)
		if err != nil || why != "" {
			return "failed", why, "", err
		}
		return done, "auto-replied to " + maskEmail(to), id, nil
	}
	return "skipped", "unknown action", "", nil
}

func (s *Service) renderAutoReply(ctx context.Context, tx *db.Tx, r *rule, f *inboundFacts) (string, string, string, error) {
	var versionID string
	if r.TemplateID == nil {
		return "", "", "", errors.New("no template")
	}
	err := tx.QueryRow(ctx, `SELECT id::text FROM template_versions WHERE template_id=$1 AND published_at IS NOT NULL ORDER BY version DESC LIMIT 1`, *r.TemplateID).Scan(&versionID)
	if err != nil {
		return "", "", "", errors.New("no published version")
	}
	vars := map[string]string{"sender": f.HeaderFrom, "subject": f.Subject, "mailbox": f.Recipient}
	return s.RenderPayload(ctx, &Payload{VersionID: versionID, Locale: "en", Vars: vars})
}

// queueAutomationMail queues a raw message from the mailbox's sender in the
// same environment. why is set when no sender is available.
func (s *Service) queueAutomationMail(ctx context.Context, tx *db.Tx, f *inboundFacts, to, subject, text, html string, sys map[string]string) (string, string, error) {
	var from, name string
	_, domain, _ := splitAddr(f.Recipient)
	err := tx.QueryRow(ctx, `SELECT email, display_name FROM sender_identities WHERE environment_id=$1
ORDER BY (split_part(email,'@',2)=$2) DESC, is_default DESC, created_at LIMIT 1`, f.EnvID, domain).Scan(&from, &name)
	if errors.Is(err, db.ErrNoRows) {
		return "", "no sender address in this environment", nil
	}
	if err != nil {
		return "", "", err
	}
	if text == "" && html == "" {
		text = "(no content)"
	}
	id := secure.NewUUID()
	body, _ := json.Marshal(Payload{From: from, FromName: name, To: to, Template: "raw", Raw: true, Subject: clean(subject, 300), Text: text, HTML: html, System: sys})
	sealed, err := s.Box.Seal(body, []byte(id))
	if err != nil {
		return "", "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO messages (id, project_id, environment_id, template, template_version, state, payload_enc,
recipient_hash, request_hmac, idempotency_key, expires_at, locale, priority, source, sender, recipient_masked, reply_to_inbound_id)
VALUES ($1,$2,$3,'raw',0,'queued',$4,$5,$6,$7, now() + interval '1 day', 'en', 'normal', 'automation', $8, $9, $10)`,
		id, f.ProjectID, f.EnvID, sealed, s.Box.MAC("recipient", []byte(to)), s.Box.MAC("request", body), "auto-"+id, from, maskEmail(to), f.ID)
	if err != nil {
		return "", "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO message_events(message_id, event_type) VALUES ($1,'accepted')`, id); err != nil {
		return "", "", err
	}
	return id, "", nil
}

// TestRule runs a rule against a stored message without acting, and records the dry run.
func (s *Service) TestRule(ctx context.Context, sc *Scope, ruleID, inboundID string) (map[string]any, error) {
	if err := s.InScope(ctx, sc, "automation_rules", ruleID); err != nil {
		return nil, err
	}
	f, err := s.inboundFacts(ctx, sc, inboundID)
	if err != nil {
		return nil, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	r, err := s.loadRule(ctx, tx, ruleID)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"matches": r.matches(f)}
	result, detail := "would_skip", "the message does not match the conditions"
	if r.matches(f) {
		if result, detail, _, err = s.applyRule(ctx, tx, r, f, true); err != nil {
			return nil, unavailable("rule_test", err)
		}
	}
	out["result"], out["detail"] = result, detail
	if _, err := tx.Exec(ctx, `INSERT INTO automation_runs(rule_id, inbound_id, result, detail, dry_run) VALUES ($1,$2,$3,$4,true)`, ruleID, f.ID, result, detail); err != nil {
		return nil, unavailable("rule_test", err)
	}
	return out, opErr(tx.Commit(ctx))
}

func (s *Service) inboundFacts(ctx context.Context, sc *Scope, id string) (*inboundFacts, error) {
	m, err := s.GetInbound(ctx, sc.EnvID, id, true)
	if err != nil {
		return nil, err
	}
	f := &inboundFacts{ID: id, ProjectID: sc.ProjectID, EnvID: sc.EnvID}
	f.AddressID, _ = m["address_id"].(string)
	f.EnvelopeFrom, _ = m["envelope_from"].(string)
	f.HeaderFrom, _ = m["header_from"].(string)
	f.Subject, _ = m["subject"].(string)
	f.Text, _ = m["text"].(string)
	f.HTML, _ = m["html"].(string)
	f.Recipient, _ = m["envelope_to"].(string)
	if n, ok := m["attachment_count"].(int64); ok {
		f.Attachments = int(n)
	}
	hb, _ := json.Marshal(m["headers"])
	_ = json.Unmarshal(hb, &f.Headers)
	return f, nil
}

var _ = time.Second

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

// The workspace API is scoped by the URL: /admin/v1/w/{project}/{mode}/...
// Every function here takes a resolved Scope and filters by BOTH the project
// and the environment, so no workspace page can read another project's data.

type Scope struct {
	ProjectID string `json:"-"`
	EnvID     string `json:"-"`
	Project   string `json:"project"`
	Mode      string `json:"mode"`
}

// ResolveScope maps a project name and mode to ids. An unknown project or
// mode is 404 project_not_found (the UI shows "Project not found").
func (s *Service) ResolveScope(ctx context.Context, project, mode string) (*Scope, error) {
	if mode != "sandbox" && mode != "live" {
		return nil, newErr(404, "project_not_found")
	}
	sc := &Scope{Project: project, Mode: mode}
	err := s.Pool.QueryRow(ctx, `SELECT p.id::text, e.id::text FROM projects p JOIN environments e ON e.project_id=p.id WHERE p.name=$1 AND e.mode=$2`,
		project, mode).Scan(&sc.ProjectID, &sc.EnvID)
	if errors.Is(err, db.ErrNoRows) {
		return nil, newErr(404, "project_not_found")
	}
	if err != nil {
		return nil, unavailable("scope", err)
	}
	return sc, nil
}

// principal is the sending context of this scope for operator actions.
func (s *Service) principal(ctx context.Context, sc *Scope) (*Principal, error) {
	p, err := s.principalFor(ctx, sc.Project, sc.Mode)
	if err != nil {
		return nil, err
	}
	p.Scopes = []string{ScopeSend, ScopeSendRaw, ScopeRead}
	return p, nil
}

// ---------------------------------------------------------------- context

func (s *Service) WorkspaceContext(ctx context.Context, sc *Scope) (map[string]any, error) {
	m, err := s.one(ctx, `SELECT p.name AS project, e.mode, p.enabled AS project_enabled, e.enabled AS environment_enabled,
  e.restrict_recipients, to_jsonb(e.recipient_allowlist) AS recipient_allowlist,
  p.bulk_max_per_campaign, p.bulk_max_per_day, p.temp_inbox_limit,
  (SELECT count(*) FROM sender_identities si WHERE si.environment_id=e.id) AS senders,
  (SELECT count(*) FROM sender_identities si WHERE si.environment_id=e.id AND `+senderVerifiedSQL+`) AS verified_senders,
  (SELECT si.email FROM sender_identities si WHERE si.environment_id=e.id AND si.is_default) AS default_sender,
  (SELECT count(*) FROM domains d WHERE d.project_id=p.id) AS domains,
  (SELECT count(*) FROM inbound_addresses a WHERE a.environment_id=e.id AND NOT a.temporary) AS mailboxes,
  (SELECT count(*) FROM inbound_messages im WHERE im.environment_id=e.id AND im.read_at IS NULL AND im.archived_at IS NULL) AS unread
FROM projects p JOIN environments e ON e.project_id=p.id WHERE e.id=$1`, sc.EnvID)
	if err != nil {
		return nil, err
	}
	m["inbound_domain"] = s.Opts.InboundDomain
	m["api_host"] = s.Opts.PublicURL
	return m, nil
}

// senderVerifiedSQL: a sender is verified when its domain has been added to
// the project and passes ownership, SPF, DKIM and DMARC. Uses alias si.
const senderVerifiedSQL = `EXISTS (SELECT 1 FROM domains d WHERE d.project_id=si.project_id AND d.name=split_part(si.email,'@',2)
  AND d.purpose IN ('sending','both') AND d.verified_at IS NOT NULL AND d.spf_ok AND d.dkim_ok AND d.dmarc_ok)`

// ---------------------------------------------------------------- checklist

type ChecklistStep struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Done     bool   `json:"done"`
	Optional bool   `json:"optional,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// Checklist reports each setup step from real data. Step 7 needs a captured
// sandbox message and a submitted live message in this project.
func (s *Service) Checklist(ctx context.Context, sc *Scope, smtpEnv bool) ([]ChecklistStep, bool, error) {
	var serverOK, projects, domains, dnsOK, senders, keys, captured, submitted, mailboxes bool
	// Mail server step. Two records exist: a check bound to the ACTIVE profile
	// at its CURRENT revision, and the legacy last_test (cleared whenever the
	// active settings change; the only record for environment SMTP that is not
	// attributable to a profile, and for installs migrated from the singleton
	// row). The newer of the two decides, so a pass for other settings, another
	// profile or an older revision never counts.
	var result string
	var checkedAt, legacyAt *time.Time
	var lastTest []byte
	_ = s.Pool.QueryRow(ctx, `SELECT c.result, c.checked_at FROM installation_checks c JOIN transport_profiles p ON p.id=c.profile_id
WHERE p.is_active AND c.config_revision=p.config_revision AND c.check_key IN ('connection','test_send')
ORDER BY c.checked_at DESC LIMIT 1`).Scan(&result, &checkedAt)
	_ = s.Pool.QueryRow(ctx, `SELECT last_test, last_test_at FROM mail_server WHERE id=1`).Scan(&lastTest, &legacyAt)
	if checkedAt != nil && (legacyAt == nil || lastTest == nil || !legacyAt.After(*checkedAt)) {
		serverOK = result == "pass"
	} else if lastTest != nil {
		var t struct {
			OK bool `json:"ok"`
		}
		_ = json.Unmarshal(lastTest, &t)
		serverOK = t.OK
	}
	err := s.Pool.QueryRow(ctx, `SELECT
  EXISTS (SELECT 1 FROM projects WHERE id=$1),
  EXISTS (SELECT 1 FROM domains WHERE project_id=$1),
  EXISTS (SELECT 1 FROM domains WHERE project_id=$1 AND verified_at IS NOT NULL AND spf_ok AND dkim_ok AND dmarc_ok
          AND (purpose='sending' OR mx_ok)),
  EXISTS (SELECT 1 FROM sender_identities si WHERE si.project_id=$1 AND `+senderVerifiedSQL+`),
  EXISTS (SELECT 1 FROM api_keys WHERE project_id=$1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())),
  EXISTS (SELECT 1 FROM messages m JOIN environments e ON e.id=m.environment_id WHERE m.project_id=$1 AND e.mode='sandbox' AND m.state='captured'),
  EXISTS (SELECT 1 FROM messages m JOIN environments e ON e.id=m.environment_id WHERE m.project_id=$1 AND e.mode='live' AND m.state='submitted'),
  EXISTS (SELECT 1 FROM inbound_addresses WHERE project_id=$1 AND NOT temporary)`, sc.ProjectID).
		Scan(&projects, &domains, &dnsOK, &senders, &keys, &captured, &submitted, &mailboxes)
	if err != nil {
		return nil, false, unavailable("checklist", err)
	}
	detail := ""
	if !serverOK && smtpEnv {
		detail = "SMTP is set in the server environment; run the connection test on the Mail server page to confirm it."
	}
	steps := []ChecklistStep{
		{Key: "mail_server", Title: "Connect mail server", Done: serverOK, Detail: detail},
		{Key: "project", Title: "Create a project", Done: projects},
		{Key: "domain", Title: "Add a domain", Done: domains},
		{Key: "dns", Title: "Publish DNS records", Done: dnsOK},
		{Key: "sender", Title: "Add a sender address", Done: senders},
		{Key: "api_key", Title: "Create an API key", Done: keys},
		{Key: "test_email", Title: "Send a test email", Done: captured && submitted,
			Detail: map[bool]string{true: "", false: "Needs one captured sandbox message and one submitted live message."}[captured && submitted]},
		{Key: "mailbox", Title: "Create a mailbox", Done: mailboxes, Optional: true},
	}
	complete := true
	for _, st := range steps[:7] {
		complete = complete && st.Done
	}
	return steps, complete, nil
}

// ---------------------------------------------------------------- search

// Search finds messages by id or full recipient address, and domains,
// senders, templates and API keys by name, in this scope only.
func (s *Service) Search(ctx context.Context, sc *Scope, q string) ([]map[string]any, error) {
	q = strings.TrimSpace(q)
	if len(q) < 2 {
		return []map[string]any{}, nil
	}
	var out []map[string]any
	add := func(rows []map[string]any, err error) error {
		if err != nil {
			return err
		}
		out = append(out, rows...)
		return nil
	}
	if uuidRe.MatchString(strings.ToLower(q)) {
		if err := add(s.maps(ctx, `SELECT 'message' AS kind, id::text AS id, COALESCE(NULLIF(subject,''), template) AS label, state AS detail
FROM messages WHERE environment_id=$1 AND id=$2::uuid`, sc.EnvID, strings.ToLower(q))); err != nil {
			return nil, err
		}
	}
	if e, ok := NormalizeEmail(strings.ToLower(q)); ok {
		if err := add(s.maps(ctx, `SELECT 'message' AS kind, id::text AS id, recipient_masked || ' · ' || COALESCE(NULLIF(subject,''), template) AS label, state AS detail
FROM messages WHERE environment_id=$1 AND recipient_hash=$2 ORDER BY created_at DESC LIMIT 10`, sc.EnvID, s.Box.MAC("recipient", []byte(e)))); err != nil {
			return nil, err
		}
	}
	like := "%" + strings.ToLower(q) + "%"
	if err := add(s.maps(ctx, `
(SELECT 'domain' AS kind, id::text AS id, name AS label, purpose AS detail FROM domains WHERE project_id=$1 AND name LIKE $3 LIMIT 5)
UNION ALL
(SELECT 'sender', id::text, email, display_name FROM sender_identities WHERE environment_id=$2 AND email LIKE $3 LIMIT 5)
UNION ALL
(SELECT 'template', id::text, slug, description FROM templates WHERE (project_id=$1 OR project_id IS NULL) AND lower(slug) LIKE $3 LIMIT 5)
UNION ALL
(SELECT 'api_key', prefix, prefix, label FROM api_keys WHERE environment_id=$2 AND (lower(prefix) LIKE $3 OR lower(label) LIKE $3) LIMIT 5)
UNION ALL
(SELECT 'mailbox', id::text, local_part || '@' || domain, display_name FROM inbound_addresses WHERE environment_id=$2 AND (local_part || '@' || domain) LIKE $3 LIMIT 5)`,
		sc.ProjectID, sc.EnvID, like)); err != nil {
		return nil, err
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}

// ---------------------------------------------------------------- overview

func (s *Service) WorkspaceOverview(ctx context.Context, sc *Scope) (map[string]any, error) {
	counts, err := s.maps(ctx, `SELECT state, count(*) AS n FROM messages WHERE environment_id=$1 GROUP BY state`, sc.EnvID)
	if err != nil {
		return nil, err
	}
	byState := map[string]any{}
	for _, st := range consoleStates {
		byState[st] = 0
	}
	for _, r := range counts {
		byState[r["state"].(string)] = r["n"]
	}
	m, err := s.one(ctx, `SELECT
 (SELECT count(*) FROM messages WHERE environment_id=$1 AND submitted_at >= now() - interval '24 hours') AS submitted_24h,
 (SELECT count(*) FROM messages WHERE environment_id=$1 AND state IN ('queued','retry_wait','submitting')) AS queue_depth,
 (SELECT count(*) FROM messages WHERE environment_id=$1 AND state='scheduled') AS scheduled,
 (SELECT count(*) FROM messages WHERE environment_id=$1 AND state='unknown') AS unknown,
 (SELECT count(*) FROM inbound_messages WHERE environment_id=$1 AND read_at IS NULL AND archived_at IS NULL) AS unread,
 (SELECT count(*) FROM bounces WHERE environment_id=$1 AND type='hard' AND received_at >= now() - interval '7 days') AS hard_bounces_7d,
 (SELECT count(*) FROM bounces WHERE environment_id=$1 AND type='complaint' AND received_at >= now() - interval '7 days') AS complaints_7d,
 (SELECT count(*) FROM suppressions WHERE environment_id=$1) AS suppressions,
 (SELECT count(*) FROM webhook_deliveries d JOIN webhook_endpoints w ON w.id=d.endpoint_id WHERE w.environment_id=$1 AND d.state='dead') AS webhook_failed,
 (SELECT count(*) FROM campaigns WHERE environment_id=$1 AND state IN ('sending','paused','scheduled')) AS active_campaigns`, sc.EnvID)
	if err != nil {
		return nil, err
	}
	m["counts"] = byState
	return m, nil
}

// ---------------------------------------------------------------- messages

type WSMessageFilter struct {
	State, Source, Sender, Template, Recipient, APIKey, Campaign string
	From, To                                                     *time.Time
}

const wsMessageCols = `m.id::text AS id, m.state, m.bounce_state, m.template, m.template_version, m.locale, m.priority, m.source,
  m.sender, m.recipient_masked AS recipient, m.subject, m.send_at, m.attempts, m.created_at, m.updated_at, m.expires_at,
  m.submitted_at, m.completed_at, m.smtp_reply, k.prefix AS api_key_prefix, m.campaign_id::text AS campaign_id,
  m.reply_to_inbound_id::text AS reply_to_inbound_id`

func (s *Service) WSMessages(ctx context.Context, sc *Scope, f WSMessageFilter, pg Page) ([]map[string]any, bool, error) {
	if f.State != "" && !validConsoleState(f.State) {
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
	if f.Campaign != "" && !uuidRe.MatchString(f.Campaign) {
		return nil, false, badRequest("unknown campaign")
	}
	return s.page(ctx, pg, `SELECT `+wsMessageCols+`
FROM messages m LEFT JOIN api_keys k ON k.id=m.api_key_id
WHERE m.environment_id=$1 AND ($2='' OR m.state=$2) AND ($3='' OR m.source=$3) AND ($4='' OR m.sender=lower($4))
  AND ($5='' OR m.template=$5) AND ($6::bytea IS NULL OR m.recipient_hash=$6) AND ($7='' OR k.prefix=$7)
  AND ($8='' OR m.campaign_id=NULLIF($8,'')::uuid)
  AND ($9::timestamptz IS NULL OR m.created_at >= $9) AND ($10::timestamptz IS NULL OR m.created_at < $10)
ORDER BY CASE WHEN $2='scheduled' THEN m.send_at END ASC NULLS LAST, m.created_at DESC`,
		sc.EnvID, f.State, f.Source, f.Sender, f.Template, hash, f.APIKey, f.Campaign, f.From, f.To)
}

// WSMessage returns one message with everything it links to.
func (s *Service) WSMessage(ctx context.Context, sc *Scope, id string) (map[string]any, error) {
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	m, err := s.one(ctx, `SELECT `+wsMessageCols+`, m.template_version_id::text AS template_version_id,
  (SELECT t.id::text FROM templates t WHERE t.slug=m.template AND (t.project_id=m.project_id OR t.project_id IS NULL) ORDER BY t.project_id NULLS LAST LIMIT 1) AS template_id,
  (SELECT si.id::text FROM sender_identities si WHERE si.environment_id=m.environment_id AND si.email=m.sender) AS sender_id,
  (SELECT d.id::text FROM domains d WHERE d.project_id=m.project_id AND d.name=split_part(m.sender,'@',2) ORDER BY d.purpose LIMIT 1) AS domain_id,
  (SELECT su.id::text FROM suppressions su WHERE su.environment_id=m.environment_id AND su.recipient_hash=m.recipient_hash) AS suppression_id,
  (SELECT c.name FROM campaigns c WHERE c.id=m.campaign_id) AS campaign_name,
  EXISTS (SELECT 1 FROM sent_copies sc WHERE sc.message_id=m.id) AS has_copy,
  EXISTS (SELECT 1 FROM sandbox_captures c WHERE c.message_id=m.id AND c.expires_at > now()) AS has_capture
FROM messages m LEFT JOIN api_keys k ON k.id=m.api_key_id WHERE m.id=$1 AND m.environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return nil, err
	}
	if m["events"], err = s.maps(ctx, `SELECT event_type AS type, reason_code AS reason, created_at AS at FROM message_events WHERE message_id=$1 ORDER BY id`, id); err != nil {
		return nil, err
	}
	if m["bounces"], err = s.maps(ctx, `SELECT id::text AS id, type, status_code, diagnostic, recipient_masked AS recipient, received_at FROM bounces WHERE message_id=$1 ORDER BY received_at`, id); err != nil {
		return nil, err
	}
	if m["webhook_deliveries"], err = s.maps(ctx, `SELECT d.id::text AS id, d.endpoint_id::text AS endpoint_id, w.url, d.event_type, d.state, d.attempts, d.last_response_code, d.created_at
FROM webhook_deliveries d JOIN webhook_endpoints w ON w.id=d.endpoint_id
WHERE w.environment_id=$2 AND d.payload->>'message_id'=$1 ORDER BY d.created_at`, id, sc.EnvID); err != nil {
		return nil, err
	}
	// Thread: replies received to this message, and the inbound message it answered.
	if m["replies"], err = s.maps(ctx, `SELECT id::text AS id, header_from, subject, received_at FROM inbound_messages
WHERE environment_id=$2 AND in_reply_to_message_id=$1 ORDER BY received_at`, id, sc.EnvID); err != nil {
		return nil, err
	}
	return m, nil
}

// SentCopy decrypts what an operator composed (Sent → open, send again).
func (s *Service) SentCopy(ctx context.Context, sc *Scope, id string) (map[string]any, error) {
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	var enc []byte
	err := s.Pool.QueryRow(ctx, `SELECT c.data_enc FROM sent_copies c JOIN messages m ON m.id=c.message_id WHERE m.id=$1 AND m.environment_id=$2`, id, sc.EnvID).Scan(&enc)
	if err != nil {
		return nil, notFound(err)
	}
	raw, err := s.Box.Open(enc, []byte("sent:"+id))
	if err != nil {
		return nil, unavailable("copy_open", err)
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, unavailable("copy_decode", err)
	}
	return out, nil
}

// CancelScheduled cancels a queued or scheduled message of this scope.
func (s *Service) WSCancel(ctx context.Context, sc *Scope, id string) error {
	p := &Principal{ProjectID: sc.ProjectID, EnvironmentID: sc.EnvID}
	return s.Cancel(ctx, p, id)
}

// SendNow moves a scheduled message to the queue immediately.
func (s *Service) SendNow(ctx context.Context, sc *Scope, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE messages SET state='queued', next_attempt_at=now(), expires_at = now() + (expires_at - COALESCE(send_at, now())), updated_at=now()
WHERE id=$1 AND environment_id=$2 AND state='scheduled'`, id, sc.EnvID)
	if err != nil {
		return unavailable("send_now", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(409, "not_scheduled")
	}
	if err := emitMessageEvent(ctx, tx, id, "send_now", "operator"); err != nil {
		return unavailable("event", err)
	}
	return opErr(tx.Commit(ctx))
}

// Retry sends a workspace-composed message again as a new message, when the
// first attempt ended failed or expired. An unknown outcome must be resolved
// first, because the mail server may already have accepted it.
func (s *Service) Retry(ctx context.Context, sc *Scope, id, user string) (*Accepted, error) {
	m, err := s.one(ctx, `SELECT state, source FROM messages WHERE id=$1 AND environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return nil, err
	}
	switch m["state"] {
	case "failed", "expired":
	case "unknown":
		return nil, &Error{Status: 409, Code: "resolve_unknown_first", Message: "the outcome is unknown: check the mail server log and resolve it before sending again"}
	default:
		return nil, &Error{Status: 409, Code: "not_retryable", Message: "only failed or expired messages can be sent again"}
	}
	cp, err := s.SentCopy(ctx, sc, id)
	if err != nil {
		return nil, &Error{Status: 409, Code: "no_copy", Message: "only mail composed in the workspace can be sent again from here"}
	}
	var in ComposeInput
	b, _ := json.Marshal(cp)
	_ = json.Unmarshal(b, &in)
	in.SendAt = ""
	return s.Compose(ctx, sc, user, "retry-"+id, in)
}

// ---------------------------------------------------------------- compose

type ComposeInput struct {
	From         string            `json:"from"`
	To           string            `json:"to"`
	CC           []string          `json:"cc"`
	BCC          []string          `json:"bcc"`
	Subject      string            `json:"subject"`
	HTML         string            `json:"html"`
	Text         string            `json:"text"`
	Template     string            `json:"template"`
	Variables    map[string]any    `json:"variables"`
	Locale       string            `json:"locale"`
	SendAt       string            `json:"send_at"`
	Attachments  []AttachmentIn    `json:"attachments"`
	InReplyTo    string            `json:"in_reply_to"` // inbound message id being answered
	DraftID      string            `json:"draft_id"`
	ReplyTo      string            `json:"reply_to"`
	extraHeaders map[string]string // unexported: system headers set by callers inside the package
}

// Compose sends one message written in the workspace through the same accept
// pipeline as the API (sender approval, suppressions, quotas, idempotency).
func (s *Service) Compose(ctx context.Context, sc *Scope, user, idemKey string, in ComposeInput) (*Accepted, error) {
	if !idemRe.MatchString(idemKey) {
		return nil, invalid("idempotency_key_invalid")
	}
	p, err := s.principal(ctx, sc)
	if err != nil {
		return nil, err
	}
	req := SendRequest{From: in.From, To: in.To, CC: in.CC, BCC: in.BCC, Subject: in.Subject, HTML: in.HTML, Text: in.Text,
		Template: in.Template, Variables: in.Variables, Locale: in.Locale, SendAt: in.SendAt, Attachments: in.Attachments, ReplyTo: in.ReplyTo}
	if in.Template != "" {
		req.Subject, req.HTML, req.Text = "", "", ""
	} else if req.Text == "" && req.HTML != "" {
		req.Text = htmlToText(req.HTML)
	}
	v, err := s.validateRequest(ctx, p, req, nil)
	if err != nil {
		return nil, err
	}
	v.source, v.personal = "ui", true
	if in.Template != "" {
		v.subject = "Template: " + in.Template
	}
	sys := map[string]string{}
	for k, val := range in.extraHeaders {
		sys[k] = val
	}
	if in.InReplyTo != "" {
		if !uuidRe.MatchString(in.InReplyTo) {
			return nil, badRequest("unknown message to reply to")
		}
		var hdrs []byte
		err := s.Pool.QueryRow(ctx, `SELECT headers FROM inbound_messages WHERE id=$1 AND environment_id=$2`, in.InReplyTo, sc.EnvID).Scan(&hdrs)
		if err != nil {
			return nil, notFound(err)
		}
		var h map[string][]string
		_ = json.Unmarshal(hdrs, &h)
		if mid := strings.TrimSpace(headerOne(h, "Message-Id")); mid != "" && !strings.ContainsAny(mid, "\r\n") {
			if !strings.HasPrefix(mid, "<") {
				mid = "<" + strings.Trim(mid, "<>") + ">"
			}
			sys["In-Reply-To"] = mid
			refs := strings.TrimSpace(headerOne(h, "References"))
			if refs != "" && !strings.ContainsAny(refs, "\r\n") && len(refs) < 900 {
				sys["References"] = refs + " " + mid
			} else {
				sys["References"] = mid
			}
		}
		v.replyInboundID = in.InReplyTo
	}
	if len(sys) > 0 {
		v.system = sys
	}
	copyDoc := map[string]any{"from": in.From, "to": in.To, "cc": in.CC, "bcc": in.BCC, "subject": in.Subject, "html": in.HTML,
		"text": req.Text, "template": in.Template, "variables": in.Variables, "locale": in.Locale, "in_reply_to": in.InReplyTo, "reply_to": in.ReplyTo}
	v.copy, _ = json.Marshal(copyDoc)
	acc, err := s.acceptValidated(ctx, p, v, idemKey)
	if err != nil {
		return nil, err
	}
	if in.DraftID != "" && uuidRe.MatchString(in.DraftID) {
		_, _ = s.Pool.Exec(ctx, `DELETE FROM drafts WHERE id=$1 AND environment_id=$2`, in.DraftID, sc.EnvID)
	}
	return acc, nil
}

// htmlToText is a small plain-text fallback for a rich-text body: block tags
// become line breaks, other tags are dropped, common entities are decoded.
func htmlToText(h string) string {
	r := strings.NewReplacer("<br>", "\n", "<br/>", "\n", "<br />", "\n", "</p>", "\n\n", "</div>", "\n", "</li>", "\n", "<li>", "• ",
		"</h1>", "\n\n", "</h2>", "\n\n", "</h3>", "\n\n")
	h = r.Replace(h)
	var b strings.Builder
	in := false
	for _, c := range h {
		switch {
		case c == '<':
			in = true
		case c == '>':
			in = false
		case !in:
			b.WriteRune(c)
		}
	}
	out := strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", "\"", "&#39;", "'").Replace(b.String())
	lines := strings.Split(out, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// CheckRecipients reports which addresses would be refused: invalid,
// suppressed, not on the live allowlist, or (bulk) unsubscribed.
func (s *Service) CheckRecipients(ctx context.Context, sc *Scope, emails []string, bulk bool) ([]map[string]string, error) {
	if len(emails) > 10000 {
		return nil, badRequest("too many addresses")
	}
	p, err := s.principal(ctx, sc)
	if err != nil {
		return nil, err
	}
	out := []map[string]string{}
	for _, raw := range emails {
		e, ok := NormalizeEmail(strings.ToLower(strings.TrimSpace(raw)))
		if !ok {
			out = append(out, map[string]string{"email": raw, "reason": "invalid"})
			continue
		}
		h := s.Box.MAC("recipient", []byte(e))
		var reason string
		err := s.Pool.QueryRow(ctx, `SELECT CASE
  WHEN EXISTS (SELECT 1 FROM suppressions WHERE environment_id=$1 AND recipient_hash=$2) THEN 'suppressed'
  WHEN $3 AND EXISTS (SELECT 1 FROM bulk_unsubscribes WHERE environment_id=$1 AND recipient_hash=$2) THEN 'unsubscribed'
  ELSE '' END`, sc.EnvID, h, bulk).Scan(&reason)
		if err != nil {
			return nil, unavailable("recipient_check", err)
		}
		if reason == "" && p.Mode == "live" && p.RestrictRecipients && !allowlisted(p.Allowlist, e) {
			reason = "not_on_live_allowlist"
		}
		if reason != "" {
			out = append(out, map[string]string{"email": e, "reason": reason})
		}
	}
	return out, nil
}

// ---------------------------------------------------------------- drafts

func (s *Service) SaveDraftMail(ctx context.Context, sc *Scope, userID, id string, data map[string]any) (string, error) {
	raw, err := json.Marshal(data)
	if err != nil || len(raw) > 6<<20 {
		return "", badRequest("draft is too large")
	}
	subject, _ := data["subject"].(string)
	subject = clean(subject, 200)
	if id == "" {
		id = secure.NewUUID()
	} else if !uuidRe.MatchString(id) {
		return "", newErr(404, "not_found")
	}
	enc, err := s.Box.Seal(raw, []byte("draft:"+id))
	if err != nil {
		return "", unavailable("seal", err)
	}
	tag, err := s.Pool.Exec(ctx, `INSERT INTO drafts(id, project_id, environment_id, user_id, data_enc, subject) VALUES ($1,$2,$3,NULLIF($4,'')::uuid,$5,$6)
ON CONFLICT (id) DO UPDATE SET data_enc=EXCLUDED.data_enc, subject=EXCLUDED.subject, updated_at=now() WHERE drafts.environment_id=EXCLUDED.environment_id`,
		id, sc.ProjectID, sc.EnvID, userID, enc, subject)
	if err != nil {
		return "", unavailable("draft_save", err)
	}
	if tag.RowsAffected() == 0 {
		return "", newErr(404, "not_found")
	}
	return id, nil
}

func (s *Service) ListDraftsMail(ctx context.Context, sc *Scope, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT id::text AS id, subject, created_at, updated_at FROM drafts WHERE environment_id=$1 ORDER BY updated_at DESC`, sc.EnvID)
}

func (s *Service) GetDraftMail(ctx context.Context, sc *Scope, id string) (map[string]any, error) {
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	var enc []byte
	if err := s.Pool.QueryRow(ctx, `SELECT data_enc FROM drafts WHERE id=$1 AND environment_id=$2`, id, sc.EnvID).Scan(&enc); err != nil {
		return nil, notFound(err)
	}
	raw, err := s.Box.Open(enc, []byte("draft:"+id))
	if err != nil {
		return nil, unavailable("draft_open", err)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	out["id"] = id
	return out, nil
}

func (s *Service) DeleteDraftMail(ctx context.Context, sc *Scope, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM drafts WHERE id=$1 AND environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return unavailable("draft_delete", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

// ---------------------------------------------------------------- senders

func (s *Service) WSSenders(ctx context.Context, sc *Scope, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT si.id::text AS id, si.email, si.display_name, split_part(si.email,'@',2) AS domain,
  (SELECT d.id::text FROM domains d WHERE d.project_id=si.project_id AND d.name=split_part(si.email,'@',2) ORDER BY d.purpose LIMIT 1) AS domain_id,
  CASE WHEN `+senderVerifiedSQL+` THEN 'verified'
       WHEN EXISTS (SELECT 1 FROM domains d WHERE d.project_id=si.project_id AND d.name=split_part(si.email,'@',2)) THEN 'domain_pending'
       ELSE 'domain_not_added' END AS status,
  si.is_default, si.last_used_at, si.created_at
FROM sender_identities si WHERE si.environment_id=$1 ORDER BY si.is_default DESC, si.email`, sc.EnvID)
}

// WSAddSender adds a sender, but only on a sending domain of this project
// whose ownership is verified.
func (s *Service) WSAddSender(ctx context.Context, sc *Scope, email, display string) (string, error) {
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
	if err := s.AddSender(ctx, sc.Project, sc.Mode, e, display); err != nil {
		return "", opErr(err)
	}
	var id string
	if err := s.Pool.QueryRow(ctx, `SELECT id::text FROM sender_identities WHERE environment_id=$1 AND email=$2`, sc.EnvID, e).Scan(&id); err != nil {
		return "", notFound(err)
	}
	// The first sender becomes the default.
	_, _ = s.Pool.Exec(ctx, `UPDATE sender_identities SET is_default=true WHERE id=$1 AND NOT EXISTS (SELECT 1 FROM sender_identities WHERE environment_id=$2 AND is_default)`, id, sc.EnvID)
	return id, nil
}

func (s *Service) SetDefaultSender(ctx context.Context, sc *Scope, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE sender_identities SET is_default=false WHERE environment_id=$1 AND is_default`, sc.EnvID); err != nil {
		return unavailable("default_sender", err)
	}
	tag, err := tx.Exec(ctx, `UPDATE sender_identities SET is_default=true WHERE id=$1 AND environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return unavailable("default_sender", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return opErr(tx.Commit(ctx))
}

func (s *Service) WSDeleteSender(ctx context.Context, sc *Scope, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM sender_identities WHERE id=$1 AND environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return unavailable("sender_delete", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

// ---------------------------------------------------------------- scoped views of existing records

func (s *Service) WSKeys(ctx context.Context, sc *Scope, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT k.prefix, k.label, to_jsonb(k.scopes) AS scopes, k.created_at, k.expires_at, k.revoked_at, k.last_used_at,
  CASE WHEN k.revoked_at IS NOT NULL THEN 'revoked' WHEN k.expires_at IS NOT NULL AND k.expires_at <= now() THEN 'expired' ELSE 'active' END AS status,
  (SELECT count(*) FROM messages m WHERE m.api_key_id=k.id) AS messages
FROM api_keys k WHERE k.environment_id=$1 ORDER BY k.created_at DESC`, sc.EnvID)
}

// KeyInScope checks that an API key prefix belongs to this scope.
func (s *Service) KeyInScope(ctx context.Context, sc *Scope, prefix string) error {
	var one int
	return notFound(s.Pool.QueryRow(ctx, `SELECT 1 FROM api_keys WHERE prefix=$1 AND environment_id=$2`, prefix, sc.EnvID).Scan(&one))
}

func (s *Service) WSWebhooks(ctx context.Context, sc *Scope, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT w.id::text AS id, w.url, to_jsonb(w.events) AS events, w.enabled, w.consecutive_failures, w.disabled_reason, w.created_at,
  (SELECT count(*) FROM webhook_deliveries d WHERE d.endpoint_id=w.id AND d.state='dead') AS failed_deliveries
FROM webhook_endpoints w WHERE w.environment_id=$1 ORDER BY w.created_at`, sc.EnvID)
}

// InScope checks that a row of table with an environment_id column belongs to this scope.
func (s *Service) InScope(ctx context.Context, sc *Scope, table, id string) error {
	allowed := map[string]bool{"webhook_endpoints": true, "inbound_addresses": true, "suppressions": true, "automation_rules": true, "campaigns": true}
	if !allowed[table] || !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	var one int
	return notFound(s.Pool.QueryRow(ctx, `SELECT 1 FROM `+table+` WHERE id=$1 AND environment_id=$2`, id, sc.EnvID).Scan(&one))
}

func (s *Service) WSDeliveries(ctx context.Context, sc *Scope, endpointID, state string, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT d.id::text AS id, d.endpoint_id::text AS endpoint_id, w.url, d.event_type, d.state, d.attempts, d.last_response_code,
  d.last_error, d.created_at, d.completed_at, d.payload->>'message_id' AS message_id
FROM webhook_deliveries d JOIN webhook_endpoints w ON w.id=d.endpoint_id
WHERE w.environment_id=$1 AND ($2='' OR d.endpoint_id=NULLIF($2,'')::uuid) AND ($3='' OR d.state=$3) ORDER BY d.created_at DESC`, sc.EnvID, endpointID, state)
}

func (s *Service) DeliveryInScope(ctx context.Context, sc *Scope, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	var one int
	return notFound(s.Pool.QueryRow(ctx, `SELECT 1 FROM webhook_deliveries d JOIN webhook_endpoints w ON w.id=d.endpoint_id WHERE d.id=$1 AND w.environment_id=$2`, id, sc.EnvID).Scan(&one))
}

func (s *Service) WSSuppressions(ctx context.Context, sc *Scope, source string, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT `+suppressionCols+`, b.message_id::text AS bounce_message_id, b.type AS bounce_type, b.status_code AS bounce_status
FROM suppressions s LEFT JOIN bounces b ON b.id=s.bounce_id
WHERE s.environment_id=$1 AND ($2='' OR s.source=$2) ORDER BY s.created_at DESC`, sc.EnvID, source)
}

func (s *Service) WSUnsubscribes(ctx context.Context, sc *Scope, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT u.recipient_masked AS recipient, u.campaign_id::text AS campaign_id, c.name AS campaign, u.created_at
FROM bulk_unsubscribes u LEFT JOIN campaigns c ON c.id=u.campaign_id WHERE u.environment_id=$1 ORDER BY u.created_at DESC`, sc.EnvID)
}

func (s *Service) WSBounces(ctx context.Context, sc *Scope, typ string, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT b.id::text AS id, b.message_id::text AS message_id, b.recipient_masked AS recipient, b.type, b.status_code, b.diagnostic, b.received_at,
  (SELECT su.id::text FROM suppressions su WHERE su.bounce_id=b.id) AS suppression_id
FROM bounces b WHERE b.environment_id=$1 AND ($2='' OR b.type=$2) ORDER BY b.received_at DESC`, sc.EnvID, typ)
}

func (s *Service) WSCaptures(ctx context.Context, sc *Scope, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT m.id::text AS id, m.template, m.template_version, m.locale, m.subject, m.sender, m.recipient_masked AS recipient, m.source, m.created_at, c.expires_at
FROM sandbox_captures c JOIN messages m ON m.id=c.message_id
WHERE m.environment_id=$1 AND c.expires_at > now() ORDER BY m.created_at DESC`, sc.EnvID)
}

func (s *Service) CaptureInScope(ctx context.Context, sc *Scope, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	var one int
	return notFound(s.Pool.QueryRow(ctx, `SELECT 1 FROM messages WHERE id=$1 AND environment_id=$2`, id, sc.EnvID).Scan(&one))
}

func (s *Service) MessageInScope(ctx context.Context, sc *Scope, id string) error {
	return s.CaptureInScope(ctx, sc, id)
}

// ---------------------------------------------------------------- templates

func (s *Service) WSTemplates(ctx context.Context, sc *Scope, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT t.id::text AS id, t.slug, t.description, t.archived, t.project_id IS NULL AS shared,
  (SELECT max(version) FROM template_versions v WHERE v.template_id=t.id AND v.published_at IS NOT NULL) AS published_version,
  (SELECT max(version) FROM template_versions v WHERE v.template_id=t.id AND v.published_at IS NULL) AS draft_version,
  (SELECT count(*) FROM messages m WHERE m.environment_id=$2 AND m.template=t.slug) AS messages, t.created_at
FROM templates t WHERE t.project_id=$1 OR t.project_id IS NULL ORDER BY t.archived, t.slug`, sc.ProjectID, sc.EnvID)
}

// TemplateInScope allows this project's templates and shared ones (project_id NULL).
// writable reports whether the template belongs to this project.
func (s *Service) TemplateInScope(ctx context.Context, sc *Scope, id string) (writable bool, err error) {
	if !uuidRe.MatchString(id) {
		return false, newErr(404, "not_found")
	}
	var pid *string
	if err := s.Pool.QueryRow(ctx, `SELECT project_id::text FROM templates WHERE id=$1`, id).Scan(&pid); err != nil {
		return false, notFound(err)
	}
	if pid != nil && *pid != sc.ProjectID {
		return false, newErr(404, "not_found")
	}
	return pid != nil, nil
}

// DuplicateTemplate copies the newest version of a template into a new draft template.
func (s *Service) DuplicateTemplate(ctx context.Context, sc *Scope, id, slug string) (string, error) {
	if _, err := s.TemplateInScope(ctx, sc, id); err != nil {
		return "", err
	}
	src, err := s.GetTemplate(ctx, id)
	if err != nil {
		return "", err
	}
	desc, _ := src["description"].(string)
	newID, err := s.CreateTemplate(ctx, TemplateInput{Project: sc.Project, Slug: slug, Description: desc})
	if err != nil {
		return "", err
	}
	vers, _ := src["versions"].([]map[string]any)
	if len(vers) == 0 {
		return newID, nil
	}
	last := vers[0] // newest first
	b, _ := json.Marshal(map[string]any{"variable_schema": last["variable_schema"], "content": last["content"]})
	var in VersionInput
	if json.Unmarshal(b, &in) == nil {
		_, _ = s.SaveDraft(ctx, newID, in)
	}
	return newID, nil
}

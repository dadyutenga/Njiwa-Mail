package service

import (
	"context"
	"crypto/hmac"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
)

// Bulk announcements go to people who already use the product. Every
// recipient is one message with idempotency key campaignID:recipientHash, so
// a restart never sends twice. Bulk mail is normal priority, paced by the
// campaign's own rate, and never uses the transactional quota.

type CampaignInput struct {
	Name          string `json:"name"`
	SenderEmail   string `json:"sender_email"`
	Subject       string `json:"subject"`
	HTML          string `json:"html"`
	Text          string `json:"text"`
	TemplateID    string `json:"template_id"`
	Locale        string `json:"locale"`
	Recipients    string `json:"recipients"` // pasted addresses or CSV with an "email" column
	SendAt        string `json:"send_at"`
	RatePerMinute int    `json:"rate_per_minute"`
	// Audience from contacts instead of pasted Recipients: a list or a
	// segment. Only contacts that can receive marketing mail are included.
	ListID    string `json:"list_id"`
	SegmentID string `json:"segment_id"`
	// Opt-in tracking (written messages only); off unless asked for.
	TrackOpens  bool `json:"track_opens"`
	TrackClicks bool `json:"track_clicks"`
}

type parsedRecipient struct {
	Email     string
	Vars      map[string]string
	ContactID string
}

type RecipientReport struct {
	Valid        int      `json:"valid"`
	Duplicates   []string `json:"duplicates"`
	Invalid      []string `json:"invalid"`
	Suppressed   []string `json:"suppressed"`
	Unsubscribed []string `json:"unsubscribed"`
	NotAllowed   []string `json:"not_allowed"`
	Columns      []string `json:"columns"`
	recipients   []parsedRecipient
}

func capList(l []string) []string {
	if len(l) > 100 {
		return append(l[:100], fmt.Sprintf("… and %d more", len(l)-100))
	}
	if l == nil {
		return []string{}
	}
	return l
}

// parseRecipients reads pasted addresses (one per line, or separated by
// commas or semicolons) or a CSV whose header row has an "email" column;
// the other CSV columns become per-recipient variables.
func parseRecipients(text string) ([]parsedRecipient, []string, []string, error) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return nil, nil, nil, nil
	}
	first := strings.ToLower(strings.SplitN(text, "\n", 2)[0])
	var out []parsedRecipient
	var invalidRaw, cols []string
	if strings.Contains(first, "email") && strings.Contains(first, ",") {
		r := csv.NewReader(strings.NewReader(text))
		r.FieldsPerRecord = -1
		rows, err := r.ReadAll()
		if err != nil {
			return nil, nil, nil, badRequest("the CSV could not be read: " + err.Error())
		}
		idx := -1
		for i, h := range rows[0] {
			h = strings.ToLower(strings.TrimSpace(h))
			rows[0][i] = h
			if h == "email" {
				idx = i
			}
		}
		if idx < 0 {
			return nil, nil, nil, badRequest("the CSV needs an email column")
		}
		for i, h := range rows[0] {
			if i != idx {
				cols = append(cols, h)
			}
		}
		for _, row := range rows[1:] {
			if idx >= len(row) {
				continue
			}
			vars := map[string]string{}
			for i, h := range rows[0] {
				if i != idx && i < len(row) && slugRe.MatchString(strings.ReplaceAll(h, "_", "-")) {
					vars[h] = clean(strings.TrimSpace(row[i]), 500)
				}
			}
			out = append(out, parsedRecipient{Email: strings.TrimSpace(row[idx]), Vars: vars})
		}
	} else {
		for _, f := range strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == ',' || r == ';' }) {
			if f = strings.TrimSpace(f); f != "" {
				out = append(out, parsedRecipient{Email: f})
			}
		}
	}
	return out, invalidRaw, cols, nil
}

// CheckCampaignRecipients reports unique valid addresses, duplicates,
// invalid, suppressed and unsubscribed addresses before anything is sent.
func (s *Service) CheckCampaignRecipients(ctx context.Context, sc *Scope, text string) (*RecipientReport, error) {
	list, _, cols, err := parseRecipients(text)
	if err != nil {
		return nil, err
	}
	if len(list) > 100000 {
		return nil, badRequest("at most 100000 rows")
	}
	p, err := s.principal(ctx, sc)
	if err != nil {
		return nil, err
	}
	rep := &RecipientReport{Columns: cols}
	seen := map[string]bool{}
	for _, r := range list {
		e, ok := NormalizeEmail(strings.ToLower(r.Email))
		if !ok {
			rep.Invalid = append(rep.Invalid, clean(r.Email, 100))
			continue
		}
		if seen[e] {
			rep.Duplicates = append(rep.Duplicates, e)
			continue
		}
		seen[e] = true
		h := s.Box.MAC("recipient", []byte(e))
		var reason string
		if err := s.Pool.QueryRow(ctx, `SELECT CASE
  WHEN EXISTS (SELECT 1 FROM suppressions WHERE environment_id=$1 AND recipient_hash=$2) THEN 'suppressed'
  WHEN EXISTS (SELECT 1 FROM bulk_unsubscribes WHERE environment_id=$1 AND recipient_hash=$2) THEN 'unsubscribed' ELSE '' END`, sc.EnvID, h).Scan(&reason); err != nil {
			return nil, unavailable("recipient_check", err)
		}
		switch {
		case reason == "suppressed":
			rep.Suppressed = append(rep.Suppressed, e)
		case reason == "unsubscribed":
			rep.Unsubscribed = append(rep.Unsubscribed, e)
		case p.Mode == "live" && p.RestrictRecipients && !allowlisted(p.Allowlist, e):
			rep.NotAllowed = append(rep.NotAllowed, e)
		default:
			rep.Valid++
			rep.recipients = append(rep.recipients, parsedRecipient{Email: e, Vars: r.Vars})
		}
	}
	rep.Duplicates, rep.Invalid, rep.Suppressed = capList(rep.Duplicates), capList(rep.Invalid), capList(rep.Suppressed)
	rep.Unsubscribed, rep.NotAllowed = capList(rep.Unsubscribed), capList(rep.NotAllowed)
	if rep.Columns == nil {
		rep.Columns = []string{}
	}
	return rep, nil
}

// BulkSenders lists senders on this project's verified bulk domains.
func (s *Service) BulkSenders(ctx context.Context, sc *Scope) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT si.id::text AS id, si.email, si.display_name, si.is_default FROM sender_identities si
WHERE si.environment_id=$1 AND EXISTS (SELECT 1 FROM domains d WHERE d.project_id=si.project_id AND d.name=split_part(si.email,'@',2)
  AND d.bulk AND d.purpose IN ('sending','both') AND d.verified_at IS NOT NULL AND d.spf_ok AND d.dkim_ok AND d.dmarc_ok)
ORDER BY si.is_default DESC, si.email`, sc.EnvID)
}

func (s *Service) CreateCampaign(ctx context.Context, sc *Scope, user string, in CampaignInput) (string, *RecipientReport, error) {
	if strings.TrimSpace(s.Opts.PublicURL) == "" {
		return "", nil, &Error{Status: 409, Code: "public_url_missing", Message: "set PUBLIC_URL on the server: bulk mail needs it for unsubscribe links"}
	}
	in.Name = clean(strings.TrimSpace(in.Name), 120)
	if in.Name == "" {
		return "", nil, badRequest("give the announcement a name")
	}
	sender, ok := NormalizeEmail(strings.ToLower(strings.TrimSpace(in.SenderEmail)))
	if !ok {
		return "", nil, badRequest("choose a sender")
	}
	var bulkOK bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sender_identities si WHERE si.environment_id=$1 AND si.email=$2
  AND EXISTS (SELECT 1 FROM domains d WHERE d.project_id=si.project_id AND d.name=split_part(si.email,'@',2) AND d.bulk
    AND d.verified_at IS NOT NULL AND d.spf_ok AND d.dkim_ok AND d.dmarc_ok))`, sc.EnvID, sender).Scan(&bulkOK); err != nil {
		return "", nil, unavailable("bulk_sender", err)
	}
	if !bulkOK {
		return "", nil, &Error{Status: 409, Code: "bulk_domain_required", Message: "bulk mail must come from a sender on a verified bulk domain (for example news.yourdomain), so problems there do not affect transactional mail"}
	}
	var tplID *string
	if in.TemplateID != "" {
		if _, err := s.TemplateInScope(ctx, sc, in.TemplateID); err != nil {
			return "", nil, badRequest("choose a template of this project")
		}
		tplID = &in.TemplateID
	} else {
		in.Subject = strings.TrimSpace(in.Subject)
		if in.Subject == "" || strings.ContainsAny(in.Subject, "\r\n") || len(in.Subject) > 300 {
			return "", nil, badRequest("enter a subject (one line, up to 300 characters)")
		}
		if strings.TrimSpace(in.HTML) == "" && strings.TrimSpace(in.Text) == "" {
			return "", nil, badRequest("write the message")
		}
		if in.Text == "" {
			in.Text = htmlToText(in.HTML)
		}
	}
	if in.Locale == "" {
		in.Locale = "en"
	}
	if in.RatePerMinute == 0 {
		in.RatePerMinute = 60
	}
	if in.RatePerMinute < 1 || in.RatePerMinute > 10000 {
		return "", nil, badRequest("rate must be between 1 and 10000 per minute")
	}
	var sendAt *time.Time
	if in.SendAt != "" {
		t, err := time.Parse(time.RFC3339, in.SendAt)
		if err != nil || t.Before(time.Now().Add(-time.Minute)) || t.After(time.Now().Add(30*24*time.Hour)) {
			return "", nil, badRequest("send time must be in the next 30 days")
		}
		sendAt = &t
	}
	if (in.TrackOpens || in.TrackClicks) && tplID != nil {
		return "", nil, badRequest("open and click tracking work with written messages; a template is sent as it is")
	}
	var rep *RecipientReport
	var err error
	var listID, segmentID *string
	switch {
	case in.ListID != "" || in.SegmentID != "":
		if strings.TrimSpace(in.Recipients) != "" || (in.ListID != "" && in.SegmentID != "") {
			return "", nil, badRequest("choose one audience: pasted addresses, a list or a segment")
		}
		rep, err = s.audienceRecipients(ctx, sc, in.ListID, in.SegmentID)
		if in.ListID != "" {
			listID = &in.ListID
		} else {
			segmentID = &in.SegmentID
		}
	default:
		rep, err = s.CheckCampaignRecipients(ctx, sc, in.Recipients)
	}
	if err != nil {
		return "", nil, err
	}
	if rep.Valid == 0 {
		return "", rep, badRequest("there are no valid recipients")
	}
	var maxPer int
	if err := s.Pool.QueryRow(ctx, `SELECT bulk_max_per_campaign FROM projects WHERE id=$1`, sc.ProjectID).Scan(&maxPer); err != nil {
		return "", nil, unavailable("bulk_limits", err)
	}
	if rep.Valid > maxPer {
		return "", rep, &Error{Status: 422, Code: "too_many_recipients", Message: fmt.Sprintf("this project allows %d recipients per announcement", maxPer)}
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", nil, unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO campaigns(project_id, environment_id, name, sender_email, subject, html, text_body, template_id, locale, send_at, rate_per_minute, created_by,
  list_id, segment_id, track_opens, track_clicks)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING id::text`,
		sc.ProjectID, sc.EnvID, in.Name, sender, in.Subject, in.HTML, in.Text, tplID, in.Locale, sendAt, in.RatePerMinute, user,
		listID, segmentID, in.TrackOpens, in.TrackClicks).Scan(&id)
	if err != nil {
		return "", nil, unavailable("campaign_create", err)
	}
	for _, r := range rep.recipients {
		h := s.Box.MAC("recipient", []byte(r.Email))
		enc, err := s.Box.Seal([]byte(r.Email), []byte("campaign:"+id))
		if err != nil {
			return "", nil, unavailable("seal", err)
		}
		var varsEnc []byte
		if len(r.Vars) > 0 {
			vb, _ := json.Marshal(r.Vars)
			if varsEnc, err = s.Box.Seal(vb, []byte("campaign-vars:"+id)); err != nil {
				return "", nil, unavailable("seal", err)
			}
		}
		var contactID *string
		if r.ContactID != "" {
			contactID = &r.ContactID
		}
		if _, err := tx.Exec(ctx, `INSERT INTO campaign_recipients(campaign_id, recipient_hash, recipient_masked, email_enc, vars_enc, contact_id) VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT DO NOTHING`, id, h, maskEmail(r.Email), enc, varsEnc, contactID); err != nil {
			return "", nil, unavailable("campaign_recipient", err)
		}
	}
	if in.TrackClicks {
		if err := storeCampaignLinks(ctx, tx, id, in.HTML); err != nil {
			return "", nil, unavailable("campaign_links", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", nil, unavailable("commit", err)
	}
	return id, rep, nil
}

// audienceRecipients takes the contacts of a list or segment that can
// receive marketing mail now: active (not unsubscribed, bounced or
// complained), on the list without having left it, and on the live
// allowlist when the environment restricts recipients.
func (s *Service) audienceRecipients(ctx context.Context, sc *Scope, listID, segmentID string) (*RecipientReport, error) {
	where, args := "", []any{sc.EnvID}
	if listID != "" {
		if !uuidRe.MatchString(listID) {
			return nil, newErr(404, "list_not_found")
		}
		var ok bool
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM contact_lists WHERE id=$1 AND environment_id=$2)`, listID, sc.EnvID).Scan(&ok); err != nil || !ok {
			return nil, newErr(404, "list_not_found")
		}
		where, args = "EXISTS (SELECT 1 FROM contact_list_members m WHERE m.contact_id=c.id AND m.list_id=$2::uuid AND m.unsubscribed_at IS NULL)", append(args, listID)
	} else {
		r, err := s.segmentRules(ctx, sc, segmentID)
		if err != nil {
			return nil, err
		}
		w, a, err := segmentSQL(r, 2)
		if err != nil {
			return nil, err
		}
		where, args = w, append(args, a...)
	}
	rows, err := s.Pool.Query(ctx, `SELECT c.id::text, c.data_enc, c.attributes, (`+contactStatusSQL+`) FROM contacts c
WHERE c.environment_id=$1 AND `+where+` ORDER BY c.created_at`, args...)
	if err != nil {
		return nil, unavailable("audience", err)
	}
	defer rows.Close()
	p, err := s.principal(ctx, sc)
	if err != nil {
		return nil, err
	}
	rep := &RecipientReport{Columns: []string{}}
	cols := map[string]bool{}
	for rows.Next() {
		var id, status string
		var enc, aj []byte
		if err := rows.Scan(&id, &enc, &aj, &status); err != nil {
			return nil, unavailable("audience", err)
		}
		var attrs map[string]any
		_ = json.Unmarshal(aj, &attrs)
		email, vars := s.contactVars(id, enc, attrs)
		if email == "" {
			continue
		}
		switch {
		case status == "bounced" || status == "complained":
			rep.Suppressed = append(rep.Suppressed, maskEmail(email))
		case status == "unsubscribed":
			rep.Unsubscribed = append(rep.Unsubscribed, maskEmail(email))
		case p.Mode == "live" && p.RestrictRecipients && !allowlisted(p.Allowlist, email):
			rep.NotAllowed = append(rep.NotAllowed, maskEmail(email))
		default:
			rep.Valid++
			for k := range vars {
				cols[k] = true
			}
			rep.recipients = append(rep.recipients, parsedRecipient{Email: email, Vars: vars, ContactID: id})
		}
	}
	for k := range cols {
		rep.Columns = append(rep.Columns, k)
	}
	rep.Duplicates, rep.Invalid = []string{}, []string{}
	rep.Suppressed, rep.Unsubscribed, rep.NotAllowed = capList(rep.Suppressed), capList(rep.Unsubscribed), capList(rep.NotAllowed)
	return rep, rows.Err()
}

func (s *Service) ListCampaigns(ctx context.Context, sc *Scope, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT c.id::text AS id, c.name, c.sender_email, c.state, c.pause_reason, c.send_at, c.created_at, c.started_at, c.completed_at,
  (SELECT count(*) FROM campaign_recipients r WHERE r.campaign_id=c.id) AS recipients,
  (SELECT count(*) FROM campaign_recipients r WHERE r.campaign_id=c.id AND r.state='queued') AS accepted
FROM campaigns c WHERE c.environment_id=$1 ORDER BY c.created_at DESC`, sc.EnvID)
}

func (s *Service) GetCampaign(ctx context.Context, sc *Scope, id string) (map[string]any, error) {
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	c, err := s.one(ctx, `SELECT c.id::text AS id, c.name, c.sender_email, c.subject, c.html, c.text_body, c.template_id::text AS template_id, t.slug AS template,
  c.locale, c.state, c.pause_reason, c.send_at, c.rate_per_minute, c.created_by, c.created_at, c.started_at, c.completed_at,
  (SELECT count(*) FROM campaign_recipients r WHERE r.campaign_id=c.id) AS recipients,
  (SELECT count(*) FROM campaign_recipients r WHERE r.campaign_id=c.id AND r.state='pending') AS pending,
  (SELECT count(*) FROM campaign_recipients r WHERE r.campaign_id=c.id AND r.state='queued') AS accepted,
  (SELECT count(*) FROM campaign_recipients r WHERE r.campaign_id=c.id AND r.state='skipped') AS skipped,
  (SELECT count(*) FROM campaign_recipients r WHERE r.campaign_id=c.id AND r.state='failed') AS failed,
  (SELECT count(*) FROM messages m WHERE m.campaign_id=c.id AND m.state='submitted') AS submitted,
  (SELECT count(*) FROM messages m WHERE m.campaign_id=c.id AND m.state IN ('failed','expired','unknown')) AS send_failed,
  (SELECT count(*) FROM messages m WHERE m.campaign_id=c.id AND m.bounce_state='hard') AS bounced,
  (SELECT count(*) FROM bounces b JOIN messages m ON m.id=b.message_id WHERE m.campaign_id=c.id AND b.type='complaint') AS complaints,
  (SELECT count(*) FROM bulk_unsubscribes u WHERE u.campaign_id=c.id)
    + (SELECT count(*) FROM tracking_events e WHERE e.campaign_id=c.id AND e.kind='unsubscribe') AS unsubscribed,
  c.list_id::text AS list_id, (SELECT name FROM contact_lists WHERE id=c.list_id) AS list_name,
  c.segment_id::text AS segment_id, (SELECT name FROM contact_segments WHERE id=c.segment_id) AS segment_name,
  c.track_opens, c.track_clicks
FROM campaigns c LEFT JOIN templates t ON t.id=c.template_id WHERE c.id=$1 AND c.environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return nil, err
	}
	if c["track_opens"] == true || c["track_clicks"] == true {
		if c["tracking"], err = s.trackingStats(ctx, id); err != nil {
			return nil, err
		}
	}
	if c["skip_reasons"], err = s.maps(ctx, `SELECT skip_reason AS reason, count(*) AS n FROM campaign_recipients WHERE campaign_id=$1 AND state IN ('skipped','failed') GROUP BY 1 ORDER BY 2 DESC`, id); err != nil {
		return nil, err
	}
	return c, nil
}

// CampaignAction: start, pause, resume, cancel.
func (s *Service) CampaignAction(ctx context.Context, sc *Scope, id, action string) error {
	if err := s.InScope(ctx, sc, "campaigns", id); err != nil {
		return err
	}
	var q string
	switch action {
	case "start":
		q = `UPDATE campaigns SET state=CASE WHEN send_at IS NOT NULL AND send_at > now() THEN 'scheduled' ELSE 'sending' END,
  started_at=CASE WHEN send_at IS NOT NULL AND send_at > now() THEN NULL ELSE now() END WHERE id=$1 AND state='draft'`
	case "pause":
		q = `UPDATE campaigns SET state='paused', pause_reason='paused by an operator' WHERE id=$1 AND state IN ('sending','scheduled')`
	case "resume":
		q = `UPDATE campaigns SET state='sending', pause_reason='', started_at=COALESCE(started_at, now()) WHERE id=$1 AND state='paused'`
	case "cancel":
		q = `UPDATE campaigns SET state='cancelled', completed_at=now() WHERE id=$1 AND state IN ('draft','scheduled','sending','paused')`
	default:
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, q, id)
	if err != nil {
		return unavailable("campaign_action", err)
	}
	if tag.RowsAffected() == 0 {
		return &Error{Status: 409, Code: "invalid_state", Message: "the announcement cannot be " + map[string]string{"start": "started", "pause": "paused", "resume": "resumed", "cancel": "cancelled"}[action] + " in its current state"}
	}
	if action == "cancel" {
		_, _ = s.Pool.Exec(ctx, `UPDATE messages SET state='cancelled', payload_enc=NULL, completed_at=now(), updated_at=now()
WHERE campaign_id=$1 AND state IN ('queued','scheduled','retry_wait')`, id)
	}
	return nil
}

// checkCampaignHealth pauses a campaign on any complaint, or when its hard
// bounce rate passes 5% (judged once 20 messages were accepted), and raises an alert.
func checkCampaignHealth(ctx context.Context, tx *db.Tx, campaignID, typ string) error {
	reason := ""
	if typ == "complaint" {
		reason = "a recipient marked the announcement as spam"
	} else {
		var accepted, hard int
		if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM messages WHERE campaign_id=$1),
  (SELECT count(*) FROM messages WHERE campaign_id=$1 AND bounce_state='hard')`, campaignID).Scan(&accepted, &hard); err != nil {
			return err
		}
		// The bounce being recorded has not updated bounce_state yet.
		hard++
		if accepted >= 20 && hard*100 > accepted*5 {
			reason = fmt.Sprintf("hard bounce rate %d%% is above 5%%", hard*100/accepted)
		}
	}
	if reason == "" {
		return nil
	}
	tag, err := tx.Exec(ctx, `UPDATE campaigns SET state='paused', pause_reason=$2 WHERE id=$1 AND state IN ('sending','scheduled')`, campaignID, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return raiseAlert(ctx, tx, "campaign_paused", "A bulk announcement was paused: "+reason, map[string]any{"campaign_id": campaignID})
	}
	return nil
}

// ---------------------------------------------------------------- unsubscribe

func (s *Service) unsubToken(campaignID string, hash []byte) string {
	hx := hex.EncodeToString(hash)
	mac := s.Box.MAC("unsubscribe", []byte(campaignID+":"+hx))
	return campaignID + "." + hx + "." + base64.RawURLEncoding.EncodeToString(mac[:16])
}

func (s *Service) UnsubscribeURL(campaignID string, hash []byte) string {
	return strings.TrimRight(s.Opts.PublicURL, "/") + "/v1/unsubscribe/" + s.unsubToken(campaignID, hash)
}

// UnsubscribeScope answers what an unsubscribe link applies to: the name of
// the campaign's list, or "" for all bulk mail of the environment.
func (s *Service) UnsubscribeScope(ctx context.Context, token string) (string, error) {
	campaignID, _, err := s.checkUnsubToken(token)
	if err != nil {
		return "", err
	}
	var name *string
	if err := s.Pool.QueryRow(ctx, `SELECT l.name FROM campaigns c LEFT JOIN contact_lists l ON l.id=c.list_id WHERE c.id=$1`, campaignID).Scan(&name); err != nil {
		return "", notFound(err)
	}
	if name == nil {
		return "", nil
	}
	return *name, nil
}

func (s *Service) checkUnsubToken(token string) (string, []byte, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || !uuidRe.MatchString(parts[0]) {
		return "", nil, newErr(404, "not_found")
	}
	hash, err := hex.DecodeString(parts[1])
	if err != nil {
		return "", nil, newErr(404, "not_found")
	}
	if !hmac.Equal([]byte(s.unsubToken(parts[0], hash)), []byte(token)) {
		return "", nil, newErr(404, "not_found")
	}
	return parts[0], hash, nil
}

// Unsubscribe records an unsubscribe from a signed token. A campaign sent to
// a list unsubscribes from that list only, unless all is set (the reader
// chose "all marketing mail"); any other campaign unsubscribes from all bulk
// mail of the environment. Transactional mail is never affected. It is
// idempotent, emits email.unsubscribed, and says nothing about whether the
// address existed.
func (s *Service) Unsubscribe(ctx context.Context, token string, all bool) error {
	campaignID, hash, err := s.checkUnsubToken(token)
	if err != nil {
		return err
	}
	var envID, masked string
	var listID, contactID *string
	var rid int64
	err = s.Pool.QueryRow(ctx, `SELECT c.environment_id::text, r.recipient_masked, c.list_id::text, r.contact_id::text, r.id
FROM campaigns c JOIN campaign_recipients r ON r.campaign_id=c.id WHERE c.id=$1 AND r.recipient_hash=$2`, campaignID, hash).
		Scan(&envID, &masked, &listID, &contactID, &rid)
	if err != nil {
		return notFound(err)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	scope := "all"
	if listID != nil && !all {
		scope = "list"
		tag, err := tx.Exec(ctx, `UPDATE contact_list_members m SET unsubscribed_at=now() FROM contacts k
WHERE m.list_id=$1 AND m.contact_id=k.id AND k.environment_id=$2 AND k.email_hash=$3 AND m.unsubscribed_at IS NULL`, *listID, envID, hash)
		if err != nil {
			return unavailable("unsubscribe", err)
		}
		if tag.RowsAffected() == 0 {
			return tx.Commit(ctx) // already unsubscribed (or removed): nothing new
		}
		if _, err := tx.Exec(ctx, `INSERT INTO tracking_events(campaign_id, recipient_id, kind) VALUES ($1,$2,'unsubscribe')`, campaignID, rid); err != nil {
			return unavailable("unsubscribe", err)
		}
	} else {
		tag, err := tx.Exec(ctx, `INSERT INTO bulk_unsubscribes(environment_id, recipient_hash, recipient_masked, campaign_id) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
			envID, hash, masked, campaignID)
		if err != nil {
			return unavailable("unsubscribe", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE contacts SET unsubscribed_at=now(), updated_at=now() WHERE environment_id=$1 AND email_hash=$2 AND unsubscribed_at IS NULL`, envID, hash); err != nil {
			return unavailable("unsubscribe", err)
		}
		if tag.RowsAffected() == 0 {
			return tx.Commit(ctx)
		}
	}
	if _, err := tx.Exec(ctx, `SELECT dm_emit($1::uuid, 'email.unsubscribed', jsonb_build_object('campaign_id', $2::text, 'scope', $3::text,
  'list_id', $4::text, 'contact_id', $5::text, 'recipient', $6::text))`, envID, campaignID, scope, listID, contactID, masked); err != nil {
		return unavailable("unsubscribe_event", err)
	}
	return tx.Commit(ctx)
}

// ---------------------------------------------------------------- pump

// PumpCampaigns starts due scheduled campaigns and queues the next slice of
// recipients of each sending campaign. Called every 15 seconds, it queues
// up to a quarter of the per-minute rate per campaign per call.
func (s *Service) PumpCampaigns(ctx context.Context) (int, error) {
	if _, err := s.Pool.Exec(ctx, `UPDATE campaigns SET state='sending', started_at=now() WHERE state='scheduled' AND send_at <= now()`); err != nil {
		return 0, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT c.id::text, p.name, e.mode, c.rate_per_minute, p.bulk_max_per_day,
  (SELECT count(*) FROM messages m WHERE m.environment_id=c.environment_id AND m.source='bulk' AND m.created_at > now() - interval '24 hours')
FROM campaigns c JOIN projects p ON p.id=c.project_id JOIN environments e ON e.id=c.environment_id WHERE c.state='sending'`)
	if err != nil {
		return 0, err
	}
	type job struct {
		id, project, mode  string
		rate, maxDay, sent int
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.project, &j.mode, &j.rate, &j.maxDay, &j.sent); err == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()
	total := 0
	for _, j := range jobs {
		n := (j.rate + 3) / 4
		if left := j.maxDay - j.sent; left < n {
			n = left
		}
		if n <= 0 {
			continue // daily bulk limit reached; continues tomorrow
		}
		sc, err := s.ResolveScope(ctx, j.project, j.mode)
		if err != nil {
			continue
		}
		queued, err := s.pumpOne(ctx, sc, j.id, n)
		if err != nil {
			return total, err
		}
		total += queued
	}
	return total, nil
}

func (s *Service) pumpOne(ctx context.Context, sc *Scope, campaignID string, n int) (int, error) {
	var sender, subject, htmlBody, textBody, locale, state string
	var tplSlug *string
	var trackOpens, trackClicks bool
	err := s.Pool.QueryRow(ctx, `SELECT c.sender_email, c.subject, c.html, c.text_body, c.locale, t.slug, c.state, c.track_opens, c.track_clicks
FROM campaigns c LEFT JOIN templates t ON t.id=c.template_id WHERE c.id=$1`,
		campaignID).Scan(&sender, &subject, &htmlBody, &textBody, &locale, &tplSlug, &state, &trackOpens, &trackClicks)
	if err != nil || state != "sending" {
		return 0, nil
	}
	// Someone who left the list (or unsubscribed from everything) since the
	// campaign was created is not sent to.
	if _, err := s.Pool.Exec(ctx, `UPDATE campaign_recipients r SET state='skipped', skip_reason='unsubscribed'
FROM campaigns c WHERE c.id=r.campaign_id AND r.campaign_id=$1 AND r.state='pending' AND r.contact_id IS NOT NULL AND (
  EXISTS (SELECT 1 FROM contacts k WHERE k.id=r.contact_id AND k.unsubscribed_at IS NOT NULL)
  OR (c.list_id IS NOT NULL AND EXISTS (SELECT 1 FROM contact_list_members m WHERE m.list_id=c.list_id AND m.contact_id=r.contact_id AND m.unsubscribed_at IS NOT NULL)))`, campaignID); err != nil {
		return 0, err
	}
	var links map[string]int
	if trackClicks {
		if links, err = s.campaignLinks(ctx, campaignID); err != nil {
			return 0, err
		}
	}
	p, err := s.principal(ctx, sc)
	if err != nil {
		_, _ = s.Pool.Exec(ctx, `UPDATE campaigns SET state='paused', pause_reason='the project or environment is disabled' WHERE id=$1`, campaignID)
		return 0, nil
	}
	rows, err := s.Pool.Query(ctx, `SELECT id, recipient_hash, email_enc, vars_enc FROM campaign_recipients WHERE campaign_id=$1 AND state='pending' ORDER BY id LIMIT $2`, campaignID, n)
	if err != nil {
		return 0, err
	}
	type rcpt struct {
		id        int64
		hash, enc []byte
		varsEnc   []byte
	}
	var list []rcpt
	for rows.Next() {
		var r rcpt
		if rows.Scan(&r.id, &r.hash, &r.enc, &r.varsEnc) == nil {
			list = append(list, r)
		}
	}
	rows.Close()
	if len(list) == 0 {
		_, err := s.Pool.Exec(ctx, `UPDATE campaigns SET state='completed', completed_at=now() WHERE id=$1 AND state='sending'
AND NOT EXISTS (SELECT 1 FROM campaign_recipients WHERE campaign_id=$1 AND state='pending')`, campaignID)
		return 0, err
	}
	queued := 0
	for _, r := range list {
		email, err := s.Box.Open(r.enc, []byte("campaign:"+campaignID))
		if err != nil {
			s.markRecipient(ctx, r.id, "failed", "unreadable", "")
			continue
		}
		vars := map[string]string{}
		if r.varsEnc != nil {
			if vb, err := s.Box.Open(r.varsEnc, []byte("campaign-vars:"+campaignID)); err == nil {
				_ = json.Unmarshal(vb, &vars)
			}
		}
		unsub := s.UnsubscribeURL(campaignID, r.hash)
		req := SendRequest{From: sender, To: string(email), Locale: locale, Priority: "normal"}
		if tplSlug != nil {
			req.Template = *tplSlug
			req.Variables = map[string]any{"unsubscribe_url": unsub}
			for k, v := range vars {
				req.Variables[k] = v
			}
		} else {
			req.Subject = fill(subject, vars, false)
			req.Text = fill(textBody, vars, false) + "\n\n--\nTo stop receiving these announcements: " + unsub
			if htmlBody != "" {
				body := fill(htmlBody, vars, true)
				if trackOpens || trackClicks {
					body = s.applyTracking(body, r.id, links, trackOpens)
				}
				req.HTML = body + `<hr><p style="font-size:12px;color:#666">To stop receiving these announcements, <a href="` + html.EscapeString(unsub) + `">unsubscribe</a>.</p>`
			}
		}
		v, err := s.validateRequest(ctx, p, req, nil)
		if err != nil {
			s.markRecipient(ctx, r.id, "failed", errCode(err), "")
			continue
		}
		v.source, v.skipQuota, v.campaignID = "bulk", true, campaignID
		v.system = map[string]string{"List-Unsubscribe": "<" + unsub + ">", "List-Unsubscribe-Post": "List-Unsubscribe=One-Click"}
		acc, err := s.acceptValidated(ctx, p, v, campaignID+":"+hex.EncodeToString(r.hash)[:32])
		if err != nil {
			code := errCode(err)
			switch code {
			case "rate_limited":
				return queued, nil // the per-recipient limit; try again on the next tick
			case "recipient_suppressed", "recipient_unsubscribed", "recipient_not_allowed":
				s.markRecipient(ctx, r.id, "skipped", code, "")
			default:
				var e *Error
				if errors.As(err, &e) && e.Status >= 500 {
					return queued, err
				}
				s.markRecipient(ctx, r.id, "failed", code, "")
			}
			continue
		}
		s.markRecipient(ctx, r.id, "queued", "", acc.ID)
		queued++
	}
	return queued, nil
}

func (s *Service) markRecipient(ctx context.Context, id int64, state, reason, msgID string) {
	_, _ = s.Pool.Exec(ctx, `UPDATE campaign_recipients SET state=$2, skip_reason=$3, message_id=NULLIF($4,'')::uuid WHERE id=$1`, id, state, reason, msgID)
}

func errCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "error"
}

// fill replaces {{name}} placeholders with per-recipient values.
func fill(body string, vars map[string]string, escape bool) string {
	for k, v := range vars {
		if escape {
			v = html.EscapeString(v)
		}
		body = strings.ReplaceAll(body, "{{"+k+"}}", v)
		body = strings.ReplaceAll(body, "{{ "+k+" }}", v)
		body = strings.ReplaceAll(body, "{{."+k+"}}", v)
		body = strings.ReplaceAll(body, "{{ ."+k+" }}", v)
	}
	return body
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/secure"
)

// Newsletters are written as blocks. Sending one goes through the campaign
// pipeline, so every recipient gets the one-click unsubscribe header and link,
// and suppressed or unsubscribed contacts are never sent to.

// NewsletterBlock is one part of an issue: heading, text, image, button,
// divider or footer.
type NewsletterBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	URL  string `json:"url,omitempty"`
	Alt  string `json:"alt,omitempty"`
}

// NewsletterInput is the editable content of an issue.
type NewsletterInput struct {
	Name        string            `json:"name"`
	Subject     string            `json:"subject"`
	PreviewText string            `json:"preview_text"`
	SenderEmail string            `json:"sender_email"`
	ListID      string            `json:"list_id"`
	SegmentID   string            `json:"segment_id"`
	Blocks      []NewsletterBlock `json:"blocks"`
	Track       bool              `json:"track"`
}

const maxNewsletterBlocks = 100

func webURL(raw string, httpsOnly bool) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || strings.ContainsAny(raw, " \r\n\"<>") {
		return false
	}
	return u.Scheme == "https" || (!httpsOnly && u.Scheme == "http")
}

func checkBlocks(blocks []NewsletterBlock) error {
	if len(blocks) > maxNewsletterBlocks {
		return badRequest("a newsletter has at most 100 blocks")
	}
	for i, b := range blocks {
		bad := func(why string) error { return badRequest(fmt.Sprintf("block %d: %s", i+1, why)) }
		if len(b.Text) > 20000 || len(b.URL) > 2000 || len(b.Alt) > 300 {
			return bad("too long")
		}
		switch b.Type {
		case "heading", "text", "footer", "divider":
		case "image":
			if b.URL != "" && !webURL(b.URL, true) {
				return bad("the image needs an https address")
			}
		case "button":
			if b.URL != "" && !webURL(b.URL, false) {
				return bad("the button needs a web address")
			}
		default:
			return bad("unknown block type")
		}
	}
	return nil
}

func paragraphs(text string) string {
	return strings.ReplaceAll(html.EscapeString(strings.TrimSpace(text)), "\n", "<br>")
}

// RenderNewsletter turns blocks into the HTML and the plain-text version. All
// writing is escaped. The unsubscribe link is not part of the blocks: it is
// added to every copy when the issue is sent.
func RenderNewsletter(preview string, blocks []NewsletterBlock) (htmlBody, textBody string) {
	var h, t strings.Builder
	h.WriteString(`<div style="max-width:600px;margin:0 auto;padding:16px;font-family:Arial,Helvetica,sans-serif;font-size:16px;line-height:1.55;color:#222">`)
	if p := strings.TrimSpace(preview); p != "" {
		h.WriteString(`<div style="display:none;max-height:0;overflow:hidden;opacity:0">` + html.EscapeString(p) + `</div>`)
	}
	for _, b := range blocks {
		text := strings.TrimSpace(b.Text)
		switch b.Type {
		case "heading":
			if text != "" {
				h.WriteString(`<h2 style="font-size:22px;line-height:1.3;margin:24px 0 12px">` + paragraphs(text) + `</h2>`)
				t.WriteString(strings.ToUpper(text) + "\n\n")
			}
		case "text":
			if text != "" {
				h.WriteString(`<p style="margin:0 0 16px">` + paragraphs(text) + `</p>`)
				t.WriteString(text + "\n\n")
			}
		case "image":
			if b.URL != "" {
				h.WriteString(`<p style="margin:0 0 16px"><img src="` + html.EscapeString(b.URL) + `" alt="` + html.EscapeString(b.Alt) + `" style="max-width:100%;height:auto;border:0"></p>`)
				if b.Alt != "" {
					t.WriteString("[" + b.Alt + "]\n\n")
				}
			}
		case "button":
			if b.URL != "" && text != "" {
				h.WriteString(`<p style="margin:24px 0"><a href="` + html.EscapeString(b.URL) + `" style="display:inline-block;padding:12px 20px;background:#2b2d33;color:#ffffff;text-decoration:none;border-radius:8px">` + html.EscapeString(text) + `</a></p>`)
				t.WriteString(text + ": " + b.URL + "\n\n")
			}
		case "divider":
			h.WriteString(`<hr style="border:0;border-top:1px solid #dddddd;margin:24px 0">`)
			t.WriteString("----------\n\n")
		case "footer":
			if text != "" {
				h.WriteString(`<p style="margin:24px 0 0;font-size:12px;color:#666666">` + paragraphs(text) + `</p>`)
				t.WriteString(text + "\n\n")
			}
		}
	}
	h.WriteString(`</div>`)
	return h.String(), strings.TrimSpace(t.String())
}

func (s *Service) cleanNewsletter(ctx context.Context, sc *Scope, in *NewsletterInput) error {
	in.Name = clean(strings.TrimSpace(in.Name), 120)
	if in.Name == "" {
		return badRequest("give the newsletter a name")
	}
	in.Subject = strings.TrimSpace(in.Subject)
	in.PreviewText = strings.TrimSpace(in.PreviewText)
	if len(in.Subject) > 300 || len(in.PreviewText) > 300 || strings.ContainsAny(in.Subject+in.PreviewText, "\r\n") {
		return badRequest("the subject and preview text are single lines of at most 300 characters")
	}
	in.SenderEmail = strings.ToLower(strings.TrimSpace(in.SenderEmail))
	if in.ListID != "" && in.SegmentID != "" {
		return badRequest("choose one audience: a list or a segment")
	}
	for _, a := range []struct{ table, id string }{{"contact_lists", in.ListID}, {"contact_segments", in.SegmentID}} {
		if a.id == "" {
			continue
		}
		var ok bool
		if !uuidRe.MatchString(a.id) {
			return badRequest("unknown audience")
		}
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+a.table+` WHERE id=$1 AND environment_id=$2)`, a.id, sc.EnvID).Scan(&ok); err != nil {
			return unavailable("newsletter_audience", err)
		}
		if !ok {
			return badRequest("unknown audience")
		}
	}
	if in.Blocks == nil {
		in.Blocks = []NewsletterBlock{}
	}
	return checkBlocks(in.Blocks)
}

// SaveNewsletter creates a draft (id "") or saves one that was not sent yet.
func (s *Service) SaveNewsletter(ctx context.Context, sc *Scope, id, user string, in NewsletterInput) (string, error) {
	if err := s.cleanNewsletter(ctx, sc, &in); err != nil {
		return "", err
	}
	blocks, err := json.Marshal(in.Blocks)
	if err != nil {
		return "", unavailable("newsletter_save", err)
	}
	if id == "" {
		err = s.Pool.QueryRow(ctx, `INSERT INTO newsletter_issues(project_id, environment_id, name, subject, preview_text, sender_email, list_id, segment_id, blocks, track, created_by)
VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,'')::uuid,NULLIF($8,'')::uuid,$9::jsonb,$10,$11) RETURNING id::text`,
			sc.ProjectID, sc.EnvID, in.Name, in.Subject, in.PreviewText, in.SenderEmail, in.ListID, in.SegmentID, blocks, in.Track, user).Scan(&id)
		if err != nil {
			return "", unavailable("newsletter_create", err)
		}
		return id, nil
	}
	if !uuidRe.MatchString(id) {
		return "", newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE newsletter_issues SET name=$3, subject=$4, preview_text=$5, sender_email=$6, list_id=NULLIF($7,'')::uuid,
  segment_id=NULLIF($8,'')::uuid, blocks=$9::jsonb, track=$10, updated_at=now() WHERE id=$1 AND environment_id=$2 AND campaign_id IS NULL`,
		id, sc.EnvID, in.Name, in.Subject, in.PreviewText, in.SenderEmail, in.ListID, in.SegmentID, blocks, in.Track)
	if err != nil {
		return "", unavailable("newsletter_save", err)
	}
	if tag.RowsAffected() == 0 {
		return "", s.newsletterGone(ctx, sc, id)
	}
	return id, nil
}

// newsletterGone explains why a draft could not be changed.
func (s *Service) newsletterGone(ctx context.Context, sc *Scope, id string) error {
	var exists bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM newsletter_issues WHERE id=$1 AND environment_id=$2)`, id, sc.EnvID).Scan(&exists); err != nil {
		return unavailable("newsletter", err)
	}
	if !exists {
		return newErr(404, "not_found")
	}
	return &Error{Status: 409, Code: "newsletter_already_sent", Message: "this newsletter was already sent or scheduled; duplicate it to change it"}
}

const newsletterCols = `n.id::text AS id, n.name, n.subject, n.preview_text, n.sender_email, n.list_id::text AS list_id,
  (SELECT name FROM contact_lists WHERE id=n.list_id) AS list_name, n.segment_id::text AS segment_id,
  (SELECT name FROM contact_segments WHERE id=n.segment_id) AS segment_name, n.track, n.campaign_id::text AS campaign_id,
  COALESCE(c.state, 'draft') AS state, c.send_at, n.created_at, n.updated_at,
  (SELECT count(*) FROM campaign_recipients r WHERE r.campaign_id=c.id) AS recipients,
  (SELECT count(*) FROM messages m WHERE m.campaign_id=c.id AND m.state='submitted') AS sent,
  (SELECT count(*) FROM messages m WHERE m.campaign_id=c.id AND m.bounce_state='hard') AS bounced,
  (SELECT count(*) FROM bulk_unsubscribes u WHERE u.campaign_id=c.id)
    + (SELECT count(*) FROM tracking_events e WHERE e.campaign_id=c.id AND e.kind='unsubscribe') AS unsubscribed,
  CASE WHEN c.track_opens THEN (SELECT count(DISTINCT e.recipient_id) FROM tracking_events e WHERE e.campaign_id=c.id AND e.kind='open') END AS opened,
  CASE WHEN c.track_clicks THEN (SELECT count(DISTINCT e.recipient_id) FROM tracking_events e WHERE e.campaign_id=c.id AND e.kind='click') END AS clicked`

// ListNewsletters lists the issues with their state and results.
func (s *Service) ListNewsletters(ctx context.Context, sc *Scope, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT `+newsletterCols+` FROM newsletter_issues n LEFT JOIN campaigns c ON c.id=n.campaign_id
WHERE n.environment_id=$1 ORDER BY n.created_at DESC`, sc.EnvID)
}

// GetNewsletter returns one issue with its blocks and results.
func (s *Service) GetNewsletter(ctx context.Context, sc *Scope, id string) (map[string]any, error) {
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	return s.one(ctx, `SELECT `+newsletterCols+`, n.blocks FROM newsletter_issues n LEFT JOIN campaigns c ON c.id=n.campaign_id
WHERE n.id=$1 AND n.environment_id=$2`, id, sc.EnvID)
}

// DeleteNewsletter removes a draft. Sent issues stay as history.
func (s *Service) DeleteNewsletter(ctx context.Context, sc *Scope, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM newsletter_issues WHERE id=$1 AND environment_id=$2 AND campaign_id IS NULL`, id, sc.EnvID)
	if err != nil {
		return unavailable("newsletter_delete", err)
	}
	if tag.RowsAffected() == 0 {
		return s.newsletterGone(ctx, sc, id)
	}
	return nil
}

func (s *Service) loadNewsletter(ctx context.Context, q queryer, sc *Scope, id, lock string) (in NewsletterInput, campaignID *string, err error) {
	if !uuidRe.MatchString(id) {
		return in, nil, newErr(404, "not_found")
	}
	var blocks []byte
	var list, segment *string
	err = q.QueryRow(ctx, `SELECT name, subject, preview_text, sender_email, list_id::text, segment_id::text, blocks, track, campaign_id::text
FROM newsletter_issues WHERE id=$1 AND environment_id=$2 `+lock, id, sc.EnvID).
		Scan(&in.Name, &in.Subject, &in.PreviewText, &in.SenderEmail, &list, &segment, &blocks, &in.Track, &campaignID)
	if errors.Is(err, db.ErrNoRows) {
		return in, nil, newErr(404, "not_found")
	}
	if err != nil {
		return in, nil, unavailable("newsletter", err)
	}
	if list != nil {
		in.ListID = *list
	}
	if segment != nil {
		in.SegmentID = *segment
	}
	if err := json.Unmarshal(blocks, &in.Blocks); err != nil {
		return in, nil, unavailable("newsletter", err)
	}
	return in, campaignID, nil
}

// ready reports what an issue still lacks before it can go anywhere.
func newsletterContent(in NewsletterInput) (htmlBody, textBody string, err error) {
	if in.Subject == "" {
		return "", "", badRequest("enter a subject")
	}
	htmlBody, textBody = RenderNewsletter(in.PreviewText, in.Blocks)
	if textBody == "" {
		return "", "", badRequest("write the newsletter: it has no text yet")
	}
	return htmlBody, textBody, nil
}

// TestNewsletter sends the saved issue to one address, normally the writer's
// own, through the normal rules of the environment.
func (s *Service) TestNewsletter(ctx context.Context, sc *Scope, id, user, to string) (*Accepted, error) {
	in, _, err := s.loadNewsletter(ctx, s.Pool, sc, id, "")
	if err != nil {
		return nil, err
	}
	htmlBody, textBody, err := newsletterContent(in)
	if err != nil {
		return nil, err
	}
	if in.SenderEmail == "" {
		return nil, badRequest("choose a sender")
	}
	note := "This is a test. Each real recipient also gets their own unsubscribe link."
	return s.Compose(ctx, sc, user, "newsletter-test-"+secure.NewUUID(), ComposeInput{From: in.SenderEmail, To: to, Subject: "[Test] " + in.Subject,
		HTML: htmlBody + `<hr><p style="font-size:12px;color:#666">` + note + `</p>`, Text: textBody + "\n\n--\n" + note})
}

// SendNewsletter sends the issue now, or schedules it for sendAt (RFC3339).
func (s *Service) SendNewsletter(ctx context.Context, sc *Scope, id, user, sendAt string) (string, *RecipientReport, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", nil, unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	// The row lock makes a second click wait, then find the campaign.
	in, campaignID, err := s.loadNewsletter(ctx, tx, sc, id, "FOR UPDATE")
	if err != nil {
		return "", nil, err
	}
	if campaignID != nil {
		return "", nil, &Error{Status: 409, Code: "newsletter_already_sent", Message: "this newsletter was already sent or scheduled"}
	}
	htmlBody, textBody, err := newsletterContent(in)
	if err != nil {
		return "", nil, err
	}
	if in.ListID == "" && in.SegmentID == "" {
		return "", nil, badRequest("choose a list or a segment to send to")
	}
	campaign, rep, err := s.CreateCampaign(ctx, sc, user, CampaignInput{Name: in.Name, SenderEmail: in.SenderEmail, Subject: in.Subject,
		HTML: htmlBody, Text: textBody, ListID: in.ListID, SegmentID: in.SegmentID, SendAt: sendAt, TrackOpens: in.Track, TrackClicks: in.Track})
	if err != nil {
		return "", rep, err
	}
	if err := s.CampaignAction(ctx, sc, campaign, "start"); err != nil {
		return "", rep, err
	}
	if _, err := tx.Exec(ctx, `UPDATE newsletter_issues SET campaign_id=$2, updated_at=now() WHERE id=$1`, id, campaign); err != nil {
		return "", rep, unavailable("newsletter_send", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", rep, unavailable("newsletter_send", err)
	}
	return campaign, rep, nil
}

func laterTime(sendAt string, horizon time.Duration) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, sendAt)
	if err != nil || t.Before(time.Now()) || t.After(time.Now().Add(horizon)) {
		return t, badRequest(fmt.Sprintf("the new send time must be in the next %d days", int(horizon.Hours()/24)))
	}
	return t, nil
}

// RescheduleCampaign moves a scheduled campaign to another time.
func (s *Service) RescheduleCampaign(ctx context.Context, sc *Scope, id, sendAt string) error {
	if err := s.InScope(ctx, sc, "campaigns", id); err != nil {
		return err
	}
	t, err := laterTime(sendAt, 30*24*time.Hour)
	if err != nil {
		return err
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE campaigns SET send_at=$2 WHERE id=$1 AND state='scheduled'`, id, t)
	if err != nil {
		return unavailable("campaign_reschedule", err)
	}
	if tag.RowsAffected() == 0 {
		return &Error{Status: 409, Code: "not_scheduled", Message: "only a scheduled campaign can be moved"}
	}
	return nil
}

// RescheduleMessage moves a scheduled message, keeping how long it stays
// valid after its send time.
func (s *Service) RescheduleMessage(ctx context.Context, sc *Scope, id, sendAt string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	t, err := laterTime(sendAt, 7*24*time.Hour)
	if err != nil {
		return err
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE messages SET send_at=$3, next_attempt_at=$3, expires_at = $3::timestamptz + (expires_at - COALESCE(send_at, now())), updated_at=now()
WHERE id=$1 AND environment_id=$2 AND state='scheduled'`, id, sc.EnvID, t)
	if err != nil {
		return unavailable("reschedule", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(409, "not_scheduled")
	}
	return nil
}

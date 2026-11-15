package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/secure"
)

// The daily report by email. The scheduler already records one report per UTC
// day for every project (GenerateDailyReport); this mails it to the addresses an
// operator chose, once per day, through the normal queue like any other message.
// "Submitted" in the report means the mail server accepted the message, never
// that it reached an inbox, and the email says so.

const maxReportRecipients = 20

type ReportEmailInput struct {
	Enabled          bool     `json:"enabled"`
	Recipients       []string `json:"recipients"`
	SenderIdentityID string   `json:"sender_identity_id"`
}

// ReportEmailSettings returns the settings and the senders an operator may choose.
func (s *Service) ReportEmailSettings(ctx context.Context) (map[string]any, error) {
	m, err := s.one(ctx, `SELECT r.enabled, r.recipients, COALESCE(r.sender_identity_id::text,'') AS sender_identity_id, r.last_emailed_day, r.updated_by, r.updated_at
FROM report_email_settings r WHERE r.id=1`)
	if err != nil {
		return nil, err
	}
	senders, err := s.maps(ctx, `SELECT si.id::text AS id, si.email, si.display_name, p.name AS project, e.mode, si.is_default
FROM sender_identities si JOIN environments e ON e.id=si.environment_id JOIN projects p ON p.id=si.project_id
ORDER BY (e.mode='live') DESC, p.name, si.is_default DESC, si.email LIMIT 200`)
	if err != nil {
		return nil, err
	}
	m["senders"] = senders
	from, _, _, ferr := s.reportSender(ctx)
	if ferr == nil {
		m["effective_sender"] = from
	} else {
		m["effective_sender"] = ""
	}
	return m, nil
}

func (s *Service) SetReportEmailSettings(ctx context.Context, by string, in ReportEmailInput) error {
	seen := map[string]bool{}
	rec := []string{}
	for _, r := range in.Recipients {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		e, ok := NormalizeEmail(r)
		if !ok {
			return badRequest("one of the recipients is not an email address")
		}
		if !seen[e] {
			seen[e] = true
			rec = append(rec, e)
		}
	}
	if len(rec) > maxReportRecipients {
		return badRequest("at most 20 recipients")
	}
	if in.Enabled && len(rec) == 0 {
		return badRequest("add at least one recipient before turning the report on")
	}
	var sid any
	if in.SenderIdentityID != "" {
		if !uuidRe.MatchString(in.SenderIdentityID) {
			return badRequest("unknown sender")
		}
		var ok bool
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sender_identities WHERE id=$1)`, in.SenderIdentityID).Scan(&ok); err != nil || !ok {
			return badRequest("unknown sender")
		}
		sid = in.SenderIdentityID
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE report_email_settings SET enabled=$1, recipients=$2, sender_identity_id=$3, updated_by=$4, updated_at=now() WHERE id=1`,
		in.Enabled, rec, sid, by); err != nil {
		return unavailable("report_email", err)
	}
	return nil
}

// reportSender is the address, display name, project and environment the report
// is sent from: the chosen identity, else the default sender of a Live
// environment, else any sender.
func (s *Service) reportSender(ctx context.Context) (from string, fromName string, ids [2]string, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT si.email, si.display_name, si.project_id::text, si.environment_id::text
FROM sender_identities si JOIN environments e ON e.id=si.environment_id
LEFT JOIN report_email_settings r ON r.id=1
ORDER BY (si.id = r.sender_identity_id) DESC NULLS LAST, (e.mode='live') DESC, si.is_default DESC, si.created_at LIMIT 1`).Scan(&from, &fromName, &ids[0], &ids[1])
	if errors.Is(err, db.ErrNoRows) {
		return "", "", ids, errors.New("no sender address exists yet")
	}
	return from, fromName, ids, err
}

// ReportEmailText is the plain-text body of one day's report.
func (s *Service) reportEmailText(day string, data []byte) (string, string) {
	var d struct {
		Day      string           `json:"day"`
		Totals   map[string]int64 `json:"totals"`
		Projects []map[string]any `json:"projects"`
		Open     int64            `json:"open_alerts"`
	}
	_ = json.Unmarshal(data, &d)
	var b strings.Builder
	fmt.Fprintf(&b, "Njiwa-Mail daily report for %s (UTC).\n\n", day)
	t := d.Totals
	fmt.Fprintf(&b, "Messages: %d\nAccepted by the mail server (submitted): %d\nFailed or expired: %d\nUnknown outcome: %d\nHard bounces: %d\nComplaints: %d\nInbound received: %d\nOpen alerts: %d\n",
		t["messages"], t["submitted"], t["failed"], t["unknown"], t["hard_bounces"], t["complaints"], t["inbound"], d.Open)
	if len(d.Projects) > 0 {
		b.WriteString("\nBy project and environment:\n")
		sort.SliceStable(d.Projects, func(i, j int) bool {
			return toF(d.Projects[i]["messages"]) > toF(d.Projects[j]["messages"])
		})
		for i, p := range d.Projects {
			if i == 20 {
				fmt.Fprintf(&b, "  … and %d more\n", len(d.Projects)-20)
				break
			}
			fmt.Fprintf(&b, "  %s (%s): %d messages, %d failed, %d hard bounces, %d complaints, %d inbound\n", p["project"], p["mode"],
				int64(toF(p["messages"])), int64(toF(p["failed"])), int64(toF(p["hard_bounces"])), int64(toF(p["complaints"])), int64(toF(p["inbound"])))
		}
	}
	b.WriteString("\n\"Submitted\" means the mail server accepted the message for delivery. It does not mean the message reached an inbox.\n")
	if pu := strings.TrimRight(s.Opts.PublicURL, "/"); pu != "" {
		fmt.Fprintf(&b, "\nOpen the dashboard: %s\n", pu)
	}
	return fmt.Sprintf("Njiwa-Mail daily report %s: %d messages, %d failed", day, t["messages"], t["failed"]), b.String()
}

// EmailDailyReport mails the newest recorded report (from the last two days)
// that has not been mailed yet. It does nothing unless an operator turned it on
// and chose recipients. The mail is queued from the chosen sender; with no
// sender at all the report stays unmailed and is tried again next hour.
func (s *Service) EmailDailyReport(ctx context.Context) (int, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var enabled bool
	var rec []string
	var last *time.Time
	if err := tx.QueryRow(ctx, `SELECT enabled, recipients, last_emailed_day FROM report_email_settings WHERE id=1 FOR UPDATE`).Scan(&enabled, &rec, &last); err != nil {
		return 0, err
	}
	if !enabled || len(rec) == 0 {
		return 0, nil
	}
	var day time.Time
	var data []byte
	err = tx.QueryRow(ctx, `SELECT day, data FROM daily_reports WHERE day >= (now() AT TIME ZONE 'UTC')::date - 2 AND ($1::date IS NULL OR day > $1::date) ORDER BY day DESC LIMIT 1`, last).Scan(&day, &data)
	if errors.Is(err, db.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	from, fromName, ids, err := s.reportSender(ctx)
	if err != nil {
		return 0, err
	}
	subject, text := s.reportEmailText(day.Format("2006-01-02"), data)
	for _, addr := range rec {
		id := secure.NewUUID()
		body, _ := json.Marshal(Payload{From: from, FromName: fromName, To: addr, Template: "raw", Raw: true, Subject: clean(subject, 300), Text: text})
		sealed, err := s.Box.Seal(body, []byte(id))
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO messages (id, project_id, environment_id, template, template_version, state, payload_enc,
recipient_hash, request_hmac, idempotency_key, expires_at, locale, priority, source, sender, recipient_masked)
VALUES ($1,$2,$3,'raw',0,'queued',$4,$5,$6,$7, now() + interval '1 day', 'en', 'normal', 'automation', $8, $9)`,
			id, ids[0], ids[1], sealed, s.Box.MAC("recipient", []byte(addr)), s.Box.MAC("request", body),
			"daily-report-"+day.Format("2006-01-02")+"-"+id, from, maskEmail(addr)); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO message_events(message_id, event_type) VALUES ($1,'accepted')`, id); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE report_email_settings SET last_emailed_day=$1 WHERE id=1`, day); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	slog.Info("daily report emailed", "day", day.Format("2006-01-02"), "recipients", len(rec))
	return len(rec), nil
}

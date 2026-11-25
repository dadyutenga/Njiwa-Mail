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

// Report schedules. The scheduler records one report per UTC day for every project
// (GenerateDailyReport). A schedule mails one report from the catalogue (a daily or
// weekly summary built from those records) to its recipients, for all projects or
// one, at a chosen hour (UTC), once per period, through the normal queue like any
// other message. "Submitted" in a report means the mail server accepted the
// message, never that it reached an inbox, and the email says so.

const (
	maxReportRecipients = 20
	maxReportSchedules  = 50
)

// ReportType is one report a schedule can send.
type ReportType struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Cadence     string `json:"cadence"` // "daily" or "weekly"
}

// ReportCatalog lists the reports that can be scheduled.
var ReportCatalog = []ReportType{
	{ID: "daily_summary", Name: "Daily summary", Cadence: "daily",
		Description: "Yesterday (UTC): messages by outcome, hard bounces, complaints, inbound mail and open alerts, per project and environment."},
	{ID: "weekly_summary", Name: "Weekly summary", Cadence: "weekly",
		Description: "Last week, Monday to Sunday (UTC): the same numbers added up over seven days. Sent on Mondays."},
}

func reportType(id string) (ReportType, bool) {
	for _, t := range ReportCatalog {
		if t.ID == id {
			return t, true
		}
	}
	return ReportType{}, false
}

// ReportScheduleInput creates or replaces a schedule. Enabled and SendHour are
// pointers so that leaving them out means "on" and 06:00.
type ReportScheduleInput struct {
	Name             string   `json:"name"`
	Report           string   `json:"report"`
	Enabled          *bool    `json:"enabled"`
	Recipients       []string `json:"recipients"`
	SenderIdentityID string   `json:"sender_identity_id"`
	ProjectID        string   `json:"project_id"`
	SendHour         *int     `json:"send_hour"`
}

type reportSchedule struct {
	name, report, senderID, projectID string
	enabled                           bool
	recipients                        []string
	sendHour                          int
}

func (s *Service) checkReportSchedule(ctx context.Context, in ReportScheduleInput) (reportSchedule, error) {
	r := reportSchedule{name: strings.TrimSpace(in.Name), report: in.Report, enabled: in.Enabled == nil || *in.Enabled, sendHour: 6}
	if r.name == "" || len([]rune(r.name)) > 100 {
		return r, badRequest("give the schedule a name of up to 100 characters")
	}
	if _, ok := reportType(r.report); !ok {
		return r, badRequest("unknown report")
	}
	if in.SendHour != nil {
		if *in.SendHour < 0 || *in.SendHour > 23 {
			return r, badRequest("the send hour is 0 to 23 (UTC)")
		}
		r.sendHour = *in.SendHour
	}
	seen := map[string]bool{}
	for _, a := range in.Recipients {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		e, ok := NormalizeEmail(a)
		if !ok {
			return r, badRequest("one of the recipients is not an email address")
		}
		if !seen[e] {
			seen[e] = true
			r.recipients = append(r.recipients, e)
		}
	}
	if len(r.recipients) == 0 {
		return r, badRequest("add at least one recipient")
	}
	if len(r.recipients) > maxReportRecipients {
		return r, badRequest("at most 20 recipients")
	}
	if in.ProjectID != "" {
		var ok bool
		if !uuidRe.MatchString(in.ProjectID) {
			return r, badRequest("unknown project")
		}
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM projects WHERE id=$1)`, in.ProjectID).Scan(&ok); err != nil {
			return r, unavailable("report_schedule", err)
		} else if !ok {
			return r, badRequest("unknown project")
		}
		r.projectID = in.ProjectID
	}
	if in.SenderIdentityID != "" {
		var ok bool
		if !uuidRe.MatchString(in.SenderIdentityID) {
			return r, badRequest("unknown sender")
		}
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sender_identities WHERE id=$1)`, in.SenderIdentityID).Scan(&ok); err != nil {
			return r, unavailable("report_schedule", err)
		} else if !ok {
			return r, badRequest("unknown sender")
		}
		r.senderID = in.SenderIdentityID
	}
	return r, nil
}

func nullUUID(v string) any {
	if v == "" {
		return nil
	}
	return v
}

const reportScheduleColumns = `r.id::text AS id, r.name, r.report, r.enabled, r.recipients,
  COALESCE(r.sender_identity_id::text,'') AS sender_identity_id, COALESCE(si.email,'') AS sender_email,
  COALESCE(r.project_id::text,'') AS project_id, COALESCE(p.name,'') AS project,
  r.send_hour, r.last_sent_period, r.last_sent_at, r.last_error,
  r.created_by, r.created_at, r.updated_by, r.updated_at
FROM report_schedules r
LEFT JOIN sender_identities si ON si.id=r.sender_identity_id
LEFT JOIN projects p ON p.id=r.project_id`

// ReportSchedules returns every schedule, the report catalogue, and the senders and
// projects an operator may choose.
func (s *Service) ReportSchedules(ctx context.Context) (map[string]any, error) {
	items, err := s.maps(ctx, `SELECT `+reportScheduleColumns+` ORDER BY r.name, r.created_at`)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for _, it := range items {
		if t, ok := reportType(fmt.Sprint(it["report"])); ok {
			it["next_send_at"] = nextReportSend(t.Cadence, int(toF(it["send_hour"])), now)
		}
	}
	senders, err := s.maps(ctx, `SELECT si.id::text AS id, si.email, si.display_name, p.id::text AS project_id, p.name AS project, e.mode, si.is_default
FROM sender_identities si JOIN environments e ON e.id=si.environment_id JOIN projects p ON p.id=si.project_id
ORDER BY (e.mode='live') DESC, p.name, si.is_default DESC, si.email LIMIT 200`)
	if err != nil {
		return nil, err
	}
	projects, err := s.maps(ctx, `SELECT id::text AS id, name FROM projects ORDER BY name LIMIT 500`)
	if err != nil {
		return nil, err
	}
	effective := ""
	if from, _, _, err := s.reportSender(ctx, "", ""); err == nil {
		effective = from
	}
	return map[string]any{"items": items, "reports": ReportCatalog, "senders": senders, "projects": projects,
		"effective_sender": effective, "limit": maxReportSchedules}, nil
}

func (s *Service) ReportSchedule(ctx context.Context, id string) (map[string]any, error) {
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	return s.one(ctx, `SELECT `+reportScheduleColumns+` WHERE r.id=$1`, id)
}

func (s *Service) CreateReportSchedule(ctx context.Context, by string, in ReportScheduleInput) (string, error) {
	r, err := s.checkReportSchedule(ctx, in)
	if err != nil {
		return "", err
	}
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM report_schedules`).Scan(&n); err != nil {
		return "", unavailable("report_schedule", err)
	}
	if n >= maxReportSchedules {
		return "", badRequest("at most 50 report schedules")
	}
	var id string
	err = s.Pool.QueryRow(ctx, `INSERT INTO report_schedules (name, report, enabled, recipients, sender_identity_id, project_id, send_hour, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8) RETURNING id::text`,
		r.name, r.report, r.enabled, r.recipients, nullUUID(r.senderID), nullUUID(r.projectID), r.sendHour, by).Scan(&id)
	if err != nil {
		return "", unavailable("report_schedule", err)
	}
	return id, nil
}

// UpdateReportSchedule replaces a schedule. Choosing another report, or turning the
// schedule back on, starts it afresh: it waits for the next send time.
func (s *Service) UpdateReportSchedule(ctx context.Context, by, id string, in ReportScheduleInput) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	r, err := s.checkReportSchedule(ctx, in)
	if err != nil {
		return err
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE report_schedules SET
  active_since = CASE WHEN report <> $2 OR (NOT enabled AND $3) THEN now() ELSE active_since END,
  last_sent_period = CASE WHEN report <> $2 THEN NULL ELSE last_sent_period END,
  last_error = CASE WHEN report <> $2 OR NOT $3 THEN '' ELSE last_error END,
  name=$1, report=$2, enabled=$3, recipients=$4, sender_identity_id=$5, project_id=$6, send_hour=$7,
  updated_by=$8, updated_at=now()
WHERE id=$9`, r.name, r.report, r.enabled, r.recipients, nullUUID(r.senderID), nullUUID(r.projectID), r.sendHour, by, id)
	if err != nil {
		return unavailable("report_schedule", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

func (s *Service) DeleteReportSchedule(ctx context.Context, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM report_schedules WHERE id=$1`, id)
	if err != nil {
		return unavailable("report_schedule", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

// ---------------------------------------------------------------- periods

func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// reportPeriod is the newest period whose send time has passed at now: for a daily
// report the day before the send day, for a weekly report the Monday of the week
// before. sendAt is when that period was due.
func reportPeriod(cadence string, hour int, now time.Time) (period, sendAt time.Time) {
	today := utcDay(now)
	if cadence == "weekly" {
		monday := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
		sendAt = monday.Add(time.Duration(hour) * time.Hour)
		if now.Before(sendAt) {
			sendAt = sendAt.AddDate(0, 0, -7)
		}
		return utcDay(sendAt).AddDate(0, 0, -7), sendAt
	}
	sendAt = today.Add(time.Duration(hour) * time.Hour)
	if now.Before(sendAt) {
		sendAt = sendAt.AddDate(0, 0, -1)
	}
	return utcDay(sendAt).AddDate(0, 0, -1), sendAt
}

func nextReportSend(cadence string, hour int, now time.Time) time.Time {
	_, last := reportPeriod(cadence, hour, now)
	if cadence == "weekly" {
		return last.AddDate(0, 0, 7)
	}
	return last.AddDate(0, 0, 1)
}

// ---------------------------------------------------------------- content

type reportTotals struct {
	totals   map[string]int64
	projects []map[string]any
	open     int64
	days     int
}

var reportKeys = []string{"messages", "submitted", "failed", "unknown", "hard_bounces", "complaints", "inbound"}

// reportData adds up the recorded daily reports from start for n days, limited to one
// project when projectID is set (older records without ids match by name).
func (s *Service) reportData(ctx context.Context, q *db.Tx, start time.Time, n int, projectID, projectName string) (reportTotals, error) {
	t := reportTotals{totals: map[string]int64{}}
	rows, err := q.Query(ctx, `SELECT day, data FROM daily_reports WHERE day >= $1 AND day < $2 ORDER BY day`, start, start.AddDate(0, 0, n))
	if err != nil {
		return t, err
	}
	defer rows.Close()
	byKey := map[string]map[string]any{}
	var order []string
	var newest time.Time
	for rows.Next() {
		var day time.Time
		var data []byte
		if err := rows.Scan(&day, &data); err != nil {
			return t, err
		}
		var d struct {
			Projects []map[string]any `json:"projects"`
			Open     int64            `json:"open_alerts"`
		}
		_ = json.Unmarshal(data, &d)
		t.days++
		if !day.Before(newest) {
			newest, t.open = day, d.Open
		}
		for _, p := range d.Projects {
			if projectID != "" && fmt.Sprint(p["project_id"]) != projectID &&
				(p["project_id"] != nil || fmt.Sprint(p["project"]) != projectName) {
				continue
			}
			key := fmt.Sprint(p["project"]) + "\x00" + fmt.Sprint(p["mode"])
			agg, ok := byKey[key]
			if !ok {
				agg = map[string]any{"project": p["project"], "mode": p["mode"]}
				byKey[key] = agg
				order = append(order, key)
			}
			for _, k := range reportKeys {
				v := int64(toF(p[k]))
				agg[k] = int64(toF(agg[k])) + v
				t.totals[k] += v
			}
		}
	}
	if err := rows.Err(); err != nil {
		return t, err
	}
	for _, k := range order {
		t.projects = append(t.projects, byKey[k])
	}
	return t, nil
}

// reportEmailText is the subject and plain-text body of one report.
func (s *Service) reportEmailText(rt ReportType, period time.Time, scope string, d reportTotals) (string, string) {
	label := period.Format("2006-01-02")
	heading := fmt.Sprintf("Njiwa-Mail daily report for %s (UTC)", label)
	if rt.Cadence == "weekly" {
		label = period.Format("2006-01-02") + " to " + period.AddDate(0, 0, 6).Format("2006-01-02")
		heading = fmt.Sprintf("Njiwa-Mail weekly report for %s (UTC)", label)
	}
	if scope != "" {
		heading += ", project " + scope
	}
	var b strings.Builder
	b.WriteString(heading + ".\n\n")
	if rt.Cadence == "weekly" && d.days < 7 {
		fmt.Fprintf(&b, "Only %d of the 7 days were recorded; the numbers cover those days.\n\n", d.days)
	}
	t := d.totals
	fmt.Fprintf(&b, "Messages: %d\nAccepted by the mail server (submitted): %d\nFailed or expired: %d\nUnknown outcome: %d\nHard bounces: %d\nComplaints: %d\nInbound received: %d\n",
		t["messages"], t["submitted"], t["failed"], t["unknown"], t["hard_bounces"], t["complaints"], t["inbound"])
	if scope == "" {
		fmt.Fprintf(&b, "Open alerts: %d\n", d.open)
	}
	if len(d.projects) > 0 {
		b.WriteString("\nBy project and environment:\n")
		sort.SliceStable(d.projects, func(i, j int) bool {
			return toF(d.projects[i]["messages"]) > toF(d.projects[j]["messages"])
		})
		for i, p := range d.projects {
			if i == 20 {
				fmt.Fprintf(&b, "  … and %d more\n", len(d.projects)-20)
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
	subject := fmt.Sprintf("Njiwa-Mail %s report %s: %d messages, %d failed", rt.Cadence, label, t["messages"], t["failed"])
	if scope != "" {
		subject += " (" + scope + ")"
	}
	return subject, b.String()
}

// ---------------------------------------------------------------- sending

// reportSender is the address, display name, project and environment a report is
// sent from: the chosen identity, else the default sender of a Live environment
// (in the schedule's project first), else any sender.
func (s *Service) reportSender(ctx context.Context, senderID, projectID string) (from string, fromName string, ids [2]string, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT si.email, si.display_name, si.project_id::text, si.environment_id::text
FROM sender_identities si JOIN environments e ON e.id=si.environment_id
ORDER BY (si.id::text = $1) DESC, (si.project_id::text = $2) DESC, (e.mode='live') DESC, si.is_default DESC, si.created_at LIMIT 1`,
		senderID, projectID).Scan(&from, &fromName, &ids[0], &ids[1])
	if errors.Is(err, db.ErrNoRows) {
		return "", "", ids, errors.New("no sender address exists yet")
	}
	return from, fromName, ids, err
}

type dueSchedule struct {
	id, name, report, senderID, projectID, project string
	recipients                                     []string
	sendHour                                       int
	lastSent                                       *time.Time
	activeSince                                    time.Time
}

const dueScheduleSelect = `SELECT r.id::text, r.name, r.report, COALESCE(r.sender_identity_id::text,''), COALESCE(r.project_id::text,''),
  COALESCE(p.name,''), r.recipients, r.send_hour, r.last_sent_period, r.active_since
FROM report_schedules r LEFT JOIN projects p ON p.id=r.project_id`

func scanDue(row db.Row) (dueSchedule, error) {
	var d dueSchedule
	err := row.Scan(&d.id, &d.name, &d.report, &d.senderID, &d.projectID, &d.project, &d.recipients, &d.sendHour, &d.lastSent, &d.activeSince)
	return d, err
}

// mailReport queues one report for the schedule's recipients inside tx. It returns
// how many messages were queued; zero when the period has not been recorded yet.
func (s *Service) mailReport(ctx context.Context, tx *db.Tx, d dueSchedule, period time.Time, key string) (int, error) {
	rt, _ := reportType(d.report)
	days := 1
	last := period
	if rt.Cadence == "weekly" {
		days, last = 7, period.AddDate(0, 0, 6)
	}
	var recorded bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM daily_reports WHERE day=$1)`, last).Scan(&recorded); err != nil {
		return 0, err
	}
	if !recorded {
		return 0, nil
	}
	data, err := s.reportData(ctx, tx, period, days, d.projectID, d.project)
	if err != nil {
		return 0, err
	}
	from, fromName, ids, err := s.reportSender(ctx, d.senderID, d.projectID)
	if err != nil {
		return 0, err
	}
	subject, text := s.reportEmailText(rt, period, d.project, data)
	for _, addr := range d.recipients {
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
			key+"-"+id, from, maskEmail(addr)); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO message_events(message_id, event_type) VALUES ($1,'accepted')`, id); err != nil {
			return 0, err
		}
	}
	return len(d.recipients), nil
}

// RunReportSchedules mails every enabled schedule whose newest period is due and not
// yet mailed. Each schedule is handled in its own transaction and locked, so two
// workers never mail the same period. A schedule that cannot be sent (no sender
// yet) keeps the reason in last_error and is tried again next hour. It returns how
// many messages were queued.
func (s *Service) RunReportSchedules(ctx context.Context) (int, error) {
	ids, err := s.maps(ctx, `SELECT id::text AS id FROM report_schedules WHERE enabled ORDER BY created_at`)
	if err != nil {
		return 0, err
	}
	total := 0
	var errs []error
	for _, row := range ids {
		n, err := s.runReportSchedule(ctx, fmt.Sprint(row["id"]), time.Now().UTC())
		total += n
		if err != nil {
			errs = append(errs, err)
		}
	}
	return total, errors.Join(errs...)
}

func (s *Service) runReportSchedule(ctx context.Context, id string, now time.Time) (int, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	d, err := scanDue(tx.QueryRow(ctx, dueScheduleSelect+` WHERE r.id=$1 AND r.enabled FOR UPDATE OF r SKIP LOCKED`, id))
	if errors.Is(err, db.ErrNoRows) {
		return 0, nil // taken by another worker, deleted or turned off
	}
	if err != nil {
		return 0, err
	}
	rt, ok := reportType(d.report)
	if !ok {
		return 0, nil
	}
	period, sendAt := reportPeriod(rt.Cadence, d.sendHour, now)
	if sendAt.Before(d.activeSince) || (d.lastSent != nil && !d.lastSent.Before(period)) {
		return 0, nil
	}
	n, err := s.mailReport(ctx, tx, d, period, "report-"+d.id+"-"+period.Format("2006-01-02"))
	if err != nil {
		tx.Rollback(ctx)
		if _, uerr := s.Pool.Exec(ctx, `UPDATE report_schedules SET last_error=$2 WHERE id=$1`, d.id, clean(err.Error(), 300)); uerr != nil {
			slog.Error("report schedule error not recorded", "schedule", d.id, "err", uerr)
		}
		return 0, fmt.Errorf("report schedule %s: %w", d.id, err)
	}
	if n == 0 {
		return 0, nil // the period is not recorded yet
	}
	if _, err := tx.Exec(ctx, `UPDATE report_schedules SET last_sent_period=$2, last_sent_at=now(), last_error='' WHERE id=$1`, d.id, period); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	slog.Info("report emailed", "schedule", d.id, "report", d.report, "period", period.Format("2006-01-02"), "recipients", n)
	return n, nil
}

// SendReportScheduleNow mails the schedule's newest recorded period straight away,
// whether or not it is enabled. The regular schedule is not changed.
func (s *Service) SendReportScheduleNow(ctx context.Context, id string) (map[string]any, error) {
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	d, err := scanDue(tx.QueryRow(ctx, dueScheduleSelect+` WHERE r.id=$1`, id))
	if err != nil {
		return nil, notFound(err)
	}
	rt, _ := reportType(d.report)
	period, _ := reportPeriod(rt.Cadence, 0, time.Now().UTC())
	n, err := s.mailReport(ctx, tx, d, period, "report-now-"+d.id+"-"+period.Format("2006-01-02"))
	if err != nil {
		if strings.Contains(err.Error(), "no sender address") {
			return nil, badRequest("no sender address exists yet: add a sender to a project first")
		}
		return nil, unavailable("report_send", err)
	}
	if n == 0 {
		return nil, badRequest("the report for that period has not been recorded yet; try again later")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, unavailable("report_send", err)
	}
	return map[string]any{"queued": n, "period": period.Format("2006-01-02")}, nil
}

package test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// openReport returns the plain-text payload of the report queued for addr.
func openReport(t *testing.T, e *env, addr string) string {
	t.Helper()
	var id string
	var enc []byte
	e.must(e.pool.QueryRow(context.Background(), `SELECT id::text, payload_enc FROM messages WHERE source='automation' AND recipient_hash=$1 ORDER BY created_at DESC LIMIT 1`,
		e.box.MAC("recipient", []byte(addr))).Scan(&id, &enc))
	pt, err := e.box.Open(enc, []byte(id))
	e.must(err)
	return string(pt)
}

// Report schedules: operators only, validated, mailed once per period from the
// catalogue, managed (edit, turn off, send now, delete), never claims delivery.
func TestReportSchedules(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	op := e.user("op@njiwa.co.tz", "admin")
	_, err := e.svc.CreateUserWith(ctx, "dev@njiwa.co.tz", adminPass, "admin", false)
	e.must(err)
	dev := e.login("dev@njiwa.co.tz")
	e.provision("sacas")
	var projectID string
	e.must(e.pool.QueryRow(ctx, `SELECT id::text FROM projects WHERE name='sacas'`).Scan(&projectID))

	daily := map[string]any{"name": "Ops daily", "report": "daily_summary", "send_hour": 0,
		"recipients": []string{"Boss@Example.org", "ops@example.org", "boss@example.org"}}
	for _, r := range []resp{
		e.adm(dev, "GET", "/admin/v1/report-schedules", nil),
		e.adm(dev, "POST", "/admin/v1/report-schedules", daily),
	} {
		if r.Code != 403 {
			t.Fatalf("non-operator: %d", r.Code)
		}
	}
	st := e.admOK(op, "GET", "/admin/v1/report-schedules", nil)
	if len(st.JSON["items"].([]any)) != 0 || len(st.JSON["reports"].([]any)) != 2 || len(st.JSON["senders"].([]any)) == 0 ||
		len(st.JSON["projects"].([]any)) != 1 || str(st.JSON, "effective_sender") != liveFrom {
		t.Fatalf("defaults: %s", st.Raw)
	}

	// Validation.
	for _, bad := range []map[string]any{
		{"name": "x", "report": "daily_summary", "recipients": []string{}},
		{"name": "x", "report": "daily_summary", "recipients": []string{"nope"}},
		{"name": "", "report": "daily_summary", "recipients": []string{"a@example.org"}},
		{"name": "x", "report": "monthly", "recipients": []string{"a@example.org"}},
		{"name": "x", "report": "daily_summary", "recipients": []string{"a@example.org"}, "send_hour": 24},
		{"name": "x", "report": "daily_summary", "recipients": []string{"a@example.org"}, "sender_identity_id": "00000000-0000-4000-8000-000000000000"},
		{"name": "x", "report": "daily_summary", "recipients": []string{"a@example.org"}, "project_id": "not-a-uuid"},
	} {
		if r := e.adm(op, "POST", "/admin/v1/report-schedules", bad); r.Code != 400 {
			t.Fatalf("%v: %d %s", bad, r.Code, r.Raw)
		}
	}

	id := str(e.admOK(op, "POST", "/admin/v1/report-schedules", daily).JSON, "id")
	got := e.admOK(op, "GET", "/admin/v1/report-schedules/"+id, nil).JSON
	if got["enabled"] != true || len(got["recipients"].([]any)) != 2 || got["send_hour"].(float64) != 0 {
		t.Fatalf("created: %v", got)
	}

	// A new schedule waits for its next send time: it does not mail an old report at once.
	made, err := e.svc.GenerateDailyReport(ctx, time.Now().UTC().Add(-24*time.Hour))
	e.must(err)
	if !made {
		t.Fatal("report not made")
	}
	if n, err := e.svc.RunReportSchedules(ctx); err != nil || n != 0 {
		t.Fatalf("mailed straight after creation: %d %v", n, err)
	}
	// Once its send time has come, the period goes out once, to each address once.
	e.must2(e.pool.Exec(ctx, `UPDATE report_schedules SET active_since='epoch'`))
	if n, err := e.svc.RunReportSchedules(ctx); err != nil || n != 2 {
		t.Fatalf("mailed %d (duplicates removed): %v", n, err)
	}
	if again, _ := e.svc.RunReportSchedules(ctx); again != 0 {
		t.Fatalf("mailed twice: %d", again)
	}
	for _, addr := range []string{"boss@example.org", "ops@example.org"} {
		if c := e.count(`SELECT count(*) FROM messages WHERE source='automation' AND state='queued' AND sender=$2 AND recipient_hash=$1`, e.box.MAC("recipient", []byte(addr)), liveFrom); c != 1 {
			t.Fatalf("%s: %d queued", addr, c)
		}
	}
	if pt := openReport(t, e, "ops@example.org"); !strings.Contains(pt, "does not mean the message reached an inbox") || !strings.Contains(pt, "daily report for") {
		t.Fatalf("payload: %s", pt)
	}
	if got := e.admOK(op, "GET", "/admin/v1/report-schedules/"+id, nil).JSON; got["last_sent_period"] == nil || str(got, "last_error") != "" {
		t.Fatalf("status after sending: %v", got)
	}

	// Turning it off stops it; the edit is kept.
	off := map[string]any{"name": "Ops daily (paused)", "report": "daily_summary", "enabled": false, "send_hour": 0, "recipients": []string{"ops@example.org"}}
	e.admOK(op, "PUT", "/admin/v1/report-schedules/"+id, off)
	e.must2(e.pool.Exec(ctx, `UPDATE report_schedules SET last_sent_period=NULL`))
	if n, _ := e.svc.RunReportSchedules(ctx); n != 0 {
		t.Fatalf("mailed after turning off: %d", n)
	}
	if got := e.admOK(op, "GET", "/admin/v1/report-schedules/"+id, nil).JSON; str(got, "name") != "Ops daily (paused)" || got["enabled"] != false {
		t.Fatalf("edit not kept: %v", got)
	}
	// Send now works while it is off and does not change the schedule.
	sent := e.admOK(op, "POST", "/admin/v1/report-schedules/"+id+"/send", nil).JSON
	if sent["queued"].(float64) != 1 {
		t.Fatalf("send now: %v", sent)
	}
	if got := e.admOK(op, "GET", "/admin/v1/report-schedules/"+id, nil).JSON; got["last_sent_period"] != nil {
		t.Fatalf("send now changed the schedule: %v", got)
	}

	// A weekly summary for one project adds up last week's recorded days.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	monday := today.AddDate(0, 0, -((int(today.Weekday())+6)%7)-7)
	for i := range 7 {
		_, err := e.svc.GenerateDailyReport(ctx, monday.AddDate(0, 0, i))
		e.must(err)
	}
	weekly := str(e.admOK(op, "POST", "/admin/v1/report-schedules", map[string]any{
		"name": "Sacas weekly", "report": "weekly_summary", "send_hour": 0, "project_id": projectID, "recipients": []string{"lead@example.org"},
	}).JSON, "id")
	e.must2(e.pool.Exec(ctx, `UPDATE report_schedules SET active_since='epoch' WHERE id=$1`, weekly))
	if n, err := e.svc.RunReportSchedules(ctx); err != nil || n != 1 {
		t.Fatalf("weekly: %d %v", n, err)
	}
	if pt := openReport(t, e, "lead@example.org"); !strings.Contains(pt, "weekly report for "+monday.Format("2006-01-02")) || !strings.Contains(pt, "project sacas") {
		t.Fatalf("weekly payload: %s", pt)
	}
	// Changing the report starts the schedule afresh.
	e.admOK(op, "PUT", "/admin/v1/report-schedules/"+weekly, map[string]any{
		"name": "Sacas daily", "report": "daily_summary", "send_hour": 0, "project_id": projectID, "recipients": []string{"lead@example.org"},
	})
	if n, _ := e.svc.RunReportSchedules(ctx); n != 0 {
		t.Fatalf("mailed straight after changing the report: %d", n)
	}

	// Recipient addresses never reach the audit log.
	if c := e.count(`SELECT count(*) FROM audit_log WHERE action LIKE 'report_schedule.%'`); c != 5 {
		t.Fatalf("audit entries: %d", c)
	}
	if c := e.count(`SELECT count(*) FROM audit_log WHERE action LIKE 'report_schedule.%' AND details::text LIKE '%example.org%'`); c != 0 {
		t.Fatal("recipient addresses in the audit log")
	}

	// Delete.
	e.admOK(op, "DELETE", "/admin/v1/report-schedules/"+id, nil)
	for _, r := range []resp{
		e.adm(op, "GET", "/admin/v1/report-schedules/"+id, nil),
		e.adm(op, "DELETE", "/admin/v1/report-schedules/"+id, nil),
		e.adm(op, "PUT", "/admin/v1/report-schedules/"+id, off),
		e.adm(op, "POST", "/admin/v1/report-schedules/"+id+"/send", nil),
	} {
		if r.Code != 404 {
			t.Fatalf("after delete: %d %s", r.Code, r.Raw)
		}
	}
	if items := e.admOK(op, "GET", "/admin/v1/report-schedules", nil).JSON["items"].([]any); len(items) != 1 {
		t.Fatalf("left: %d", len(items))
	}
}

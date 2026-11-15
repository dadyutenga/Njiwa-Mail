package test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The daily report by email: off until an operator turns it on, mailed once per
// day to the chosen recipients through the normal queue, never claims delivery.
func TestDailyReportEmail(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	op := e.user("op@njiwa.co.tz", "admin")
	_, err := e.svc.CreateUserWith(ctx, "dev@njiwa.co.tz", adminPass, "admin", false)
	e.must(err)
	dev := e.login("dev@njiwa.co.tz")
	e.provision("sacas")

	if r := e.adm(dev, "GET", "/admin/v1/report-email", nil); r.Code != 403 {
		t.Fatalf("non-operator read: %d", r.Code)
	}
	if r := e.adm(dev, "PUT", "/admin/v1/report-email", map[string]any{"enabled": false}); r.Code != 403 {
		t.Fatalf("non-operator write: %d", r.Code)
	}
	st := e.admOK(op, "GET", "/admin/v1/report-email", nil)
	if st.JSON["enabled"] != false || len(st.JSON["senders"].([]any)) == 0 || str(st.JSON, "effective_sender") != liveFrom {
		t.Fatalf("defaults: %s", st.Raw)
	}

	// Validation.
	for _, bad := range []map[string]any{
		{"enabled": true, "recipients": []string{}},
		{"enabled": true, "recipients": []string{"nope"}},
		{"enabled": false, "recipients": []string{"a@example.org"}, "sender_identity_id": "00000000-0000-4000-8000-000000000000"},
		{"enabled": false, "recipients": []string{"a@example.org"}, "sender_identity_id": "not-a-uuid"},
	} {
		if r := e.adm(op, "PUT", "/admin/v1/report-email", bad); r.Code != 400 {
			t.Fatalf("%v: %d %s", bad, r.Code, r.Raw)
		}
	}

	made, err := e.svc.GenerateDailyReport(ctx, time.Now().UTC().Add(-24*time.Hour))
	e.must(err)
	if !made {
		t.Fatal("report not made")
	}
	// Off: nothing is mailed.
	if n, err := e.svc.EmailDailyReport(ctx); err != nil || n != 0 {
		t.Fatalf("mailed while off: %d %v", n, err)
	}
	e.admOK(op, "PUT", "/admin/v1/report-email", map[string]any{"enabled": true, "recipients": []string{"Boss@Example.org", "ops@example.org", "boss@example.org"}})
	n, err := e.svc.EmailDailyReport(ctx)
	if err != nil || n != 2 {
		t.Fatalf("mailed %d (duplicates removed): %v", n, err)
	}
	if again, _ := e.svc.EmailDailyReport(ctx); again != 0 {
		t.Fatalf("mailed twice: %d", again)
	}
	for _, addr := range []string{"boss@example.org", "ops@example.org"} {
		if c := e.count(`SELECT count(*) FROM messages WHERE source='automation' AND state='queued' AND sender=$2 AND recipient_hash=$1`, e.box.MAC("recipient", []byte(addr)), liveFrom); c != 1 {
			t.Fatalf("%s: %d queued", addr, c)
		}
	}
	// The text is honest about what "submitted" means.
	var id string
	var enc []byte
	e.must(e.pool.QueryRow(ctx, `SELECT id::text, payload_enc FROM messages WHERE source='automation' AND recipient_hash=$1`, e.box.MAC("recipient", []byte("ops@example.org"))).Scan(&id, &enc))
	pt, err := e.box.Open(enc, []byte(id))
	if err != nil || !strings.Contains(string(pt), "does not mean the message reached an inbox") || !strings.Contains(string(pt), "daily report for") {
		t.Fatalf("payload: %v %s", err, pt)
	}

	// A new day's report is mailed again; a day already mailed is not.
	e.must2(e.pool.Exec(ctx, `UPDATE daily_reports SET day = day - 1`))
	e.must2(e.pool.Exec(ctx, `UPDATE report_email_settings SET last_emailed_day = last_emailed_day - 2`))
	if n, err := e.svc.EmailDailyReport(ctx); err != nil || n != 2 {
		t.Fatalf("next day: %d %v", n, err)
	}

	// Turning it off stops it.
	e.admOK(op, "PUT", "/admin/v1/report-email", map[string]any{"enabled": false, "recipients": []string{"ops@example.org"}})
	e.must2(e.pool.Exec(ctx, `UPDATE report_email_settings SET last_emailed_day = NULL`))
	if n, _ := e.svc.EmailDailyReport(ctx); n != 0 {
		t.Fatalf("mailed after turning off: %d", n)
	}
}

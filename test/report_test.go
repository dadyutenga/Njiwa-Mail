package test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Analytics: a period against the one before it, timing percentiles (to the
// mail server, and to the recipient's server where there is evidence), by
// mailbox provider, and CSV per table.
func TestReportPeriodsProvidersAndCSV(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")
	var pid, eid string
	e.must(e.pool.QueryRow(ctx, `SELECT e.project_id::text, e.id::text FROM environments e JOIN projects p ON p.id=e.project_id WHERE p.name='sacas' AND e.mode='sandbox'`).Scan(&pid, &eid))
	n := 0
	add := func(created time.Time, rcpt, state, bounce string, submitAfter time.Duration, remoteAfter time.Duration) string {
		n++
		var id string
		var sub any
		if submitAfter > 0 {
			sub = created.Add(submitAfter)
		}
		e.must(e.pool.QueryRow(ctx, `INSERT INTO messages (id, project_id, environment_id, template, template_version, state, recipient_hash, request_hmac,
  idempotency_key, expires_at, created_at, submitted_at, recipient_masked, bounce_state, sender)
VALUES (gen_random_uuid(),$1,$2,'otp',1,$3,$4,$4,$5, now()+interval '1 day', $6, $7, $8, COALESCE(NULLIF($9,''),'none'), 'security@notify.example.com') RETURNING id::text`,
			pid, eid, state, []byte(fmt.Sprint(n)), fmt.Sprintf("report-%04d", n), created, sub, rcpt, bounce).Scan(&id))
		if remoteAfter > 0 {
			_, err := e.pool.Exec(ctx, `INSERT INTO delivery_events(message_id, source, event, dedupe_key, observed_at) VALUES ($1,'mta_log','remote_accepted',$2,$3)`,
				id, "k"+id, created.Add(remoteAfter))
			e.must(err)
		}
		return id
	}
	day := time.Now().UTC().Truncate(24 * time.Hour)
	// This period (the last 7 days): 4 messages. The 7 before: 2.
	add(day.Add(-1*24*time.Hour), "a***@gmail.com", "submitted", "", 2*time.Second, 10*time.Second)
	add(day.Add(-2*24*time.Hour), "b***@gmail.com", "submitted", "", 4*time.Second, 0)
	add(day.Add(-3*24*time.Hour), "c***@outlook.com", "failed", "hard", 0, 0)
	add(day.Add(-3*24*time.Hour), "d***@company.co.tz", "submitted", "", 6*time.Second, 0)
	add(day.Add(-9*24*time.Hour), "e***@yahoo.com", "submitted", "", time.Second, 0)
	add(day.Add(-10*24*time.Hour), "f***@yahoo.com", "submitted", "", time.Second, 0)

	from, to := day.Add(-6*24*time.Hour).Format("2006-01-02"), day.Add(24*time.Hour).Format("2006-01-02")
	r := e.admOK(admin, "GET", wsBase+"/report?from="+from+"&to="+to, nil)
	tot := r.JSON["totals"].(map[string]any)
	prev := r.JSON["previous"].(map[string]any)
	ch := r.JSON["change_percent"].(map[string]any)
	if tot["messages"] != float64(4) || prev["messages"] != float64(2) || ch["messages"] != float64(100) ||
		tot["remote_accepted"] != float64(1) || tot["hard_bounces"] != float64(1) || ch["hard_bounces"] != nil {
		t.Fatalf("totals: %v / %v / %v", tot, prev, ch)
	}
	tm := r.JSON["timing_seconds"].(map[string]any)
	if tm["submit_p50"] != float64(4) || tm["submit_samples"] != float64(3) || tm["remote_p50"] != float64(10) {
		t.Fatalf("timing: %v", tm)
	}
	prov := map[string]float64{}
	for _, p := range r.JSON["providers"].([]any) {
		m := p.(map[string]any)
		prov[m["provider"].(string)] = m["messages"].(float64)
	}
	if prov["Gmail"] != 2 || prov["Outlook"] != 1 || prov["Other"] != 1 {
		t.Fatalf("providers: %v", prov)
	}
	if len(r.JSON["daily"].([]any)) != 3 {
		t.Fatalf("daily: %v", r.JSON["daily"])
	}
	csv := e.adm(admin, "GET", wsBase+"/report?format=csv&table=providers&from="+from+"&to="+to, nil)
	if csv.Code != 200 || !strings.HasPrefix(csv.Hdr.Get("Content-Type"), "text/csv") || !strings.HasPrefix(csv.Raw, "provider,messages") ||
		!strings.Contains(csv.Raw, "Gmail,2") || !strings.Contains(csv.Hdr.Get("Content-Disposition"), "providers") {
		t.Fatalf("csv: %d %v %s", csv.Code, csv.Hdr, csv.Raw)
	}
	tcsv := e.adm(admin, "GET", wsBase+"/report?format=csv&table=totals&from="+from+"&to="+to, nil)
	if !strings.Contains(tcsv.Raw, "messages,4,2,100.0") {
		t.Fatalf("totals csv: %s", tcsv.Raw)
	}
	if r := e.adm(admin, "GET", wsBase+"/report?from=2026-01-10&to=2026-01-01", nil); r.Code != 400 {
		t.Fatalf("reversed period: %d", r.Code)
	}
}

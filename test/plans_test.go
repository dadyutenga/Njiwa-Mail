package test

import (
	"context"
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/service"
)

// Usage plans: assigning copies the limits onto the project, editing the
// plan updates its projects, the monthly quota counts Live mail only, and
// the contact limit stops new contacts.
func TestUsagePlansAndQuotas(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	op := e.user("op@njiwa.co.tz", "admin")
	_, err := e.svc.CreateUserWith(ctx, "dev@njiwa.co.tz", adminPass, "admin", false)
	e.must(err)
	dev := e.login("dev@njiwa.co.tz")
	k := e.provision("sacas")

	plan := map[string]any{"name": "Starter", "send_per_minute": 100, "send_per_day": 1000, "recipient_per_minute": 5,
		"recipient_per_hour": 20, "bulk_max_per_campaign": 100, "bulk_max_per_day": 200, "monthly_messages": 2, "max_contacts": 1}
	if r := e.adm(dev, "POST", "/admin/v1/plans", plan); r.Code != 403 {
		t.Fatalf("non-operator: %d", r.Code)
	}
	if r := e.adm(op, "POST", "/admin/v1/plans", map[string]any{"name": "Bad", "send_per_minute": 0}); r.Code != 400 {
		t.Fatalf("bad plan: %d", r.Code)
	}
	pid := str(e.admOK(op, "POST", "/admin/v1/plans", plan).JSON, "id")
	e.admOK(op, "POST", "/admin/v1/projects/sacas/plan", map[string]any{"plan_id": pid})
	var perMin, monthly int
	e.must(e.pool.QueryRow(ctx, `SELECT send_per_minute, monthly_messages FROM projects WHERE name='sacas'`).Scan(&perMin, &monthly))
	if perMin != 100 || monthly != 2 {
		t.Fatalf("assigned limits: %d %d", perMin, monthly)
	}
	// Sandbox mail does not count; the third Live message in a month is refused.
	for i := 0; i < 3; i++ {
		if r := e.post(k.sbx, idem("sbx-"+string(rune('a'+i))), otpBody(testRcpt)); r.Code != 202 {
			t.Fatalf("sandbox %d: %d %s", i, r.Code, r.Raw)
		}
	}
	for i, want := range []int{202, 202, 429} {
		r := e.post(k.live, idem("live-"+string(rune('a'+i))), otpBody("q"+string(rune('a'+i))+"@example.org"))
		if r.Code != want {
			t.Fatalf("live %d: %d %s", i, r.Code, r.Raw)
		}
		if want == 429 && (errCode(r) != "monthly_quota_exceeded" || r.Hdr.Get("Retry-After") == "") {
			t.Fatalf("quota answer: %s %v", r.Raw, r.Hdr)
		}
	}
	// The refused message reserved nothing.
	use := e.admOK(op, "GET", "/admin/v1/usage", nil)
	if !strings.Contains(use.Raw, `"messages_this_month":2`) || !strings.Contains(use.Raw, `"plan":"Starter"`) {
		t.Fatalf("usage: %s", use.Raw)
	}
	// Raising the plan's quota updates the project at once.
	plan["monthly_messages"] = 10
	if r := e.admOK(op, "PUT", "/admin/v1/plans/"+pid, plan); r.JSON["projects_updated"] != float64(1) {
		t.Fatalf("update: %s", r.Raw)
	}
	if r := e.post(k.live, idem("live-after"), otpBody("qz@example.org")); r.Code != 202 {
		t.Fatalf("after raising the quota: %d %s", r.Code, r.Raw)
	}
	// One contact allowed.
	sc, err := e.svc.ResolveScope(ctx, "sacas", "live")
	e.must(err)
	_, _, err = e.svc.UpsertContact(ctx, sc, service.ContactInput{Email: "one@example.org"})
	e.must(err)
	if _, _, err := e.svc.UpsertContact(ctx, sc, service.ContactInput{Email: "two@example.org"}); err == nil || !strings.Contains(err.Error(), "contact_limit_reached") {
		t.Fatalf("second contact: %v", err)
	}
	// Updating an existing contact is not a new one.
	if _, _, err := e.svc.UpsertContact(ctx, sc, service.ContactInput{Email: "one@example.org", Name: "One"}); err != nil {
		t.Fatalf("update over the limit: %v", err)
	}
	if r := e.adm(op, "DELETE", "/admin/v1/plans/"+pid, nil); r.Code != 409 {
		t.Fatalf("delete a plan in use: %d", r.Code)
	}
}

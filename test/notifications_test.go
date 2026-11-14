package test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The notification inbox: operator announcements for everyone (retractable),
// the daily report (made once per day), and open alerts for operators only.
// Read and dismissed are per user.
func TestNotificationsAnnouncementsAndDailyReport(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	op := e.user("op@njiwa.co.tz", "admin")
	_, err := e.svc.CreateUserWith(ctx, "dev@njiwa.co.tz", adminPass, "admin", false)
	e.must(err)
	dev := e.login("dev@njiwa.co.tz")

	if r := e.adm(dev, "POST", "/admin/v1/announcements", map[string]any{"title": "x"}); r.Code != 403 {
		t.Fatalf("non-operator announced: %d", r.Code)
	}
	if r := e.adm(op, "POST", "/admin/v1/announcements", map[string]any{"title": "x", "link": "javascript:alert(1)"}); r.Code != 400 {
		t.Fatalf("javascript link: %d", r.Code)
	}
	a := e.admOK(op, "POST", "/admin/v1/announcements", map[string]any{"title": "Maintenance on Sunday", "body": "Sending pauses for 10 minutes.", "link": "/p/demo/dashboard"})
	aid := str(a.JSON, "id")
	_, err = e.pool.Exec(ctx, `INSERT INTO alerts(kind, summary) VALUES ('dns_check_failed', 'SPF failed for notify.example.com')`)
	e.must(err)
	made, err := e.svc.GenerateDailyReport(ctx, time.Now().UTC().Add(-24*time.Hour))
	e.must(err)
	again, err := e.svc.GenerateDailyReport(ctx, time.Now().UTC().Add(-24*time.Hour))
	e.must(err)
	if !made || again {
		t.Fatalf("daily report made %v, again %v", made, again)
	}

	opIn := e.admOK(op, "GET", "/admin/v1/notifications", nil)
	devIn := e.admOK(dev, "GET", "/admin/v1/notifications", nil)
	if opIn.JSON["unread"] != float64(3) || devIn.JSON["unread"] != float64(2) || strings.Contains(devIn.Raw, "SPF failed") {
		t.Fatalf("op %s\ndev %s", opIn.Raw, devIn.Raw)
	}
	// The developer dismisses the announcement: gone for them only.
	e.admOK(dev, "POST", "/admin/v1/notifications/announcement/"+aid+"/dismiss", nil)
	if r := e.admOK(dev, "GET", "/admin/v1/notifications", nil); strings.Contains(r.Raw, "Maintenance") || r.JSON["unread"] != float64(1) {
		t.Fatalf("after dismiss: %s", r.Raw)
	}
	if r := e.admOK(op, "GET", "/admin/v1/notifications", nil); !strings.Contains(r.Raw, "Maintenance") {
		t.Fatalf("dismiss leaked to another user: %s", r.Raw)
	}
	// A developer cannot touch alert state; read-all marks everything read.
	if r := e.adm(dev, "POST", "/admin/v1/notifications/alert/1/dismiss", nil); r.Code != 404 {
		t.Fatalf("dev dismissing an alert: %d", r.Code)
	}
	e.admOK(op, "POST", "/admin/v1/notifications/read-all", nil)
	if r := e.admOK(op, "GET", "/admin/v1/notifications", nil); r.JSON["unread"] != float64(0) || len(r.JSON["items"].([]any)) != 3 {
		t.Fatalf("after read-all: %s", r.Raw)
	}
	// Retracting removes it from every inbox; operators still see it listed.
	e.admOK(op, "POST", "/admin/v1/announcements/"+aid+"/retract", nil)
	if r := e.admOK(op, "GET", "/admin/v1/notifications", nil); strings.Contains(r.Raw, "Maintenance") {
		t.Fatalf("retracted still in inbox: %s", r.Raw)
	}
	if r := e.admOK(op, "GET", "/admin/v1/announcements", nil); !strings.Contains(r.Raw, "retracted_at\":\"") {
		t.Fatalf("announcement list: %s", r.Raw)
	}
}

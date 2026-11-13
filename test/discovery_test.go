package test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/installer/discover"
	"github.com/njiwa-tz/mail/internal/service"
)

// Installation step 2: the host CLI records a read-only discovery report; the
// setup wizard reads it back. Operator only; audited; nothing secret stored.
func TestDiscoveryIsRecordedForTheWizard(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	owner := e.user("owner@njiwa.co.tz", "owner")
	if r := e.admOK(owner, "GET", "/admin/v1/installation/discovery", nil); r.JSON["report"] != nil {
		t.Fatalf("no discovery yet: %s", r.Raw)
	}
	rep := &discover.Report{ScannedAt: time.Now().UTC(), Host: discover.Host{OS: "Debian 12", OSID: "debian", OSVersion: "12", Systemd: true},
		Mailcow: &discover.Mailcow{Path: "/opt/mailcow-dockerized", Hostname: "mail.example.co.tz", Running: true}}
	rec := discover.Recommend(rep, discover.DefaultNeeds())
	rep.Recommendation = &rec
	e.must(e.svc.RecordDiscovery(ctx, service.CLIActor, rep))
	r := e.admOK(owner, "GET", "/admin/v1/installation/discovery", nil)
	got, _ := r.JSON["report"].(map[string]any)
	recm, _ := got["recommendation"].(map[string]any)
	if recm["mode"] != "existing_smtp" || !strings.Contains(r.Raw, "mail.example.co.tz") || r.JSON["recorded_at"] == nil {
		t.Fatalf("%s", r.Raw)
	}
	if e.count(`SELECT count(*) FROM audit_log WHERE action='installation.discovery'`) != 1 {
		t.Fatal("discovery not audited")
	}
	if err := e.svc.RecordDiscovery(ctx, service.CLIActor, &discover.Report{}); err == nil {
		t.Fatal("incomplete report accepted")
	}
	// A project member who is not an operator cannot read the host report.
	if _, err := e.svc.CreateUserWith(ctx, "dev@njiwa.co.tz", adminPass, "admin", false); err != nil {
		t.Fatal(err)
	}
	member := e.login("dev@njiwa.co.tz")
	if r := e.adm(member, "GET", "/admin/v1/installation/discovery", nil); r.Code != 403 {
		t.Fatalf("non-operator: %d %s", r.Code, r.Raw)
	}
}

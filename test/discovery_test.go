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

// The ten-step setup journey is derived from server state: it resumes after
// a restart or on another device, never claims a step without evidence, and
// is the same for both delivery modes except step 4.
func TestSetupJourneyResumesFromServerState(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	owner := e.user("owner@njiwa.co.tz", "owner")
	journey := func() map[string]map[string]any {
		r := e.admOK(owner, "GET", "/admin/v1/installation/setup", nil)
		out := map[string]map[string]any{}
		for _, s := range r.JSON["steps"].([]any) {
			m := s.(map[string]any)
			out[m["id"].(string)] = m
		}
		out["_"] = r.JSON
		return out
	}
	j := journey()
	if j["_"]["total"].(float64) != 10 || j["install"]["status"] != "ready" || j["detect"]["status"] != "needs_action" ||
		j["detect"]["command"] != "njiwa-mail discover -record" || j["_"]["next"].(float64) != 2 {
		t.Fatalf("fresh: %v", j)
	}
	for _, id := range []string{"domain", "dns", "sender", "api_key", "test_send"} {
		if j[id]["status"] != "not_started" {
			t.Fatalf("%s without a project: %v", id, j[id])
		}
	}
	// Step 2 recorded: the recommendation is offered at step 3.
	rep := &discover.Report{ScannedAt: time.Now().UTC(), Host: discover.Host{OSID: "ubuntu", OSVersion: "24.04", Systemd: true, PublicIPs: []string{"203.0.113.10"}, DockerPresent: true}}
	rec := discover.Recommend(rep, discover.DefaultNeeds())
	rep.Recommendation = &rec
	e.must(e.svc.RecordDiscovery(ctx, service.CLIActor, rep))
	j = journey()
	if j["detect"]["status"] != "ready" || !strings.Contains(j["choose_mode"]["detail"].(string), "Recommended for this server: Send from my VPS") {
		t.Fatalf("after discovery: %v %v", j["detect"], j["choose_mode"])
	}
	// The operator chooses existing SMTP instead; the journey says so honestly.
	e.admOK(owner, "PUT", "/admin/v1/installation/wizard", map[string]any{"selected_mode": "existing_smtp"})
	j = journey()
	if j["choose_mode"]["status"] != "ready" || !strings.Contains(j["choose_mode"]["detail"].(string), "The scan recommended Send from my VPS") ||
		j["connect"]["status"] != "needs_action" || j["connect"]["title"] != "Connect your existing mail server" {
		t.Fatalf("after choosing: %v %v", j["choose_mode"], j["connect"])
	}
	// Step 4 done.
	e.saveSMTP("mail.example.com", "pw")
	e.must(e.svc.RecordMailTest(ctx, e.pin(), "connection", true, okSteps))
	e.must(e.svc.RecordMailTest(ctx, e.pin(), "test_send", true, map[string]any{"state": "submitted"}))
	if j = journey(); j["connect"]["status"] != "ready" || j["_"]["next"].(float64) != 5 {
		t.Fatalf("after connecting: %v next %v", j["connect"], j["_"]["next"])
	}
	// Steps 5-8 from a provisioned project; step 9 only after a Live hand-over.
	k := e.provision("acme")
	j = journey()
	for _, id := range []string{"domain", "dns", "sender", "api_key"} {
		if j[id]["status"] != "ready" {
			t.Fatalf("%s: %v", id, j[id])
		}
	}
	if j["_"]["project"] != "acme" || j["test_send"]["status"] != "needs_action" || j["dashboard"]["status"] != "not_started" {
		t.Fatalf("before the test send: %v", j)
	}
	if r := e.post(k.live, idem("setup-test"), otpBody(testRcpt)); r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	e.runWorker()
	j = journey()
	if j["test_send"]["status"] != "ready" || j["dashboard"]["status"] != "ready" || j["_"]["next"].(float64) != 0 || j["_"]["done"].(float64) != 10 {
		t.Fatalf("complete: %v", j)
	}
	if strings.Contains(strings.ToLower(j["test_send"]["detail"].(string)), "delivered") && !strings.Contains(j["test_send"]["detail"].(string), "not by this step") {
		t.Fatal("the journey must not claim delivery")
	}
	// A revoked key reopens step 8 (and the journey) without any stored state.
	_, err := e.pool.Exec(ctx, `UPDATE api_keys SET revoked_at=now()`)
	e.must(err)
	if j = journey(); j["api_key"]["status"] != "needs_action" || j["_"]["next"].(float64) != 8 {
		t.Fatalf("after revoking keys: %v", j["api_key"])
	}
}

// T7.2: in managed mode, a claimed domain the mail host has no DKIM key for
// reopens the DNS step with the installer command.
func TestSetupJourneyAsksForTheInstallerAfterANewDomain(t *testing.T) {
	e := setup(t)
	owner := e.user("owner@njiwa.co.tz", "owner")
	e.admOK(owner, "PUT", "/admin/v1/installation/wizard", map[string]any{"selected_mode": "managed_mta"})
	e.provision("acme")
	var dns map[string]any
	for _, s := range e.admOK(owner, "GET", "/admin/v1/installation/setup", nil).JSON["steps"].([]any) {
		if m := s.(map[string]any); m["id"] == "dns" {
			dns = m
		}
	}
	if dns["status"] != "needs_action" || dns["command"] != "njiwa-mail installer plan" || !strings.Contains(dns["detail"].(string), "no DKIM key yet") {
		t.Fatalf("%v", dns)
	}
}

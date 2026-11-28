package test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/installer/discover"
	"github.com/njiwa-tz/mail/internal/installer/managed"
	"github.com/njiwa-tz/mail/internal/mtaagent"
	"github.com/njiwa-tz/mail/internal/service"
)

type fakeHostAgent struct {
	disabled bool
	applied  []mtaagent.InstallRequest
	job      mtaagent.InstallJob
	domains  []string
	dkim     string
}

func (f *fakeHostAgent) Queue(context.Context) ([]mtaagent.QueueEntry, error) { return nil, nil }
func (f *fakeHostAgent) Status(context.Context) (*mtaagent.Status, error) {
	return &mtaagent.Status{}, nil
}
func (f *fakeHostAgent) Act(context.Context, string, mtaagent.Action) error { return nil }
func (f *fakeHostAgent) Flush(context.Context) error                        { return nil }
func (f *fakeHostAgent) off() error {
	if f.disabled {
		return &mtaagent.AgentError{Status: 403, Code: "host_install_disabled"}
	}
	return nil
}
func (f *fakeHostAgent) InstallPlan(_ context.Context, in mtaagent.InstallRequest) (*mtaagent.InstallPlan, error) {
	return &mtaagent.InstallPlan{Digest: "abcdef0123456789", Summary: strings.Join(in.Domains, ",")}, f.off()
}
func (f *fakeHostAgent) InstallApply(_ context.Context, in mtaagent.InstallRequest) error {
	f.applied = append(f.applied, in)
	return f.off()
}
func (f *fakeHostAgent) InstallRollback(context.Context, mtaagent.InstallRequest) error {
	return f.off()
}
func (f *fakeHostAgent) InstallJob(context.Context) (*mtaagent.InstallJob, error) {
	return &f.job, f.off()
}

// Plan, approve, install and record from the dashboard, with the host agent
// faked. Only operators, only the approved digest, and no key in any answer.
func TestDashboardHostInstall(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.provision("sacas")
	op := e.user("admin@njiwa.co.tz", "admin")
	base := "/admin/v1/installation/host"
	if r := e.adm(op, "POST", base+"/plan", map[string]any{}); r.Code != 409 || errCode(r) != "mta_agent_not_configured" {
		t.Fatalf("no agent: %d %s", r.Code, r.Raw)
	}
	agent := &fakeHostAgent{disabled: true}
	e.srv.MTA = agent
	if r := e.adm(op, "POST", base+"/plan", map[string]any{}); r.Code != 409 || errCode(r) != "host_install_disabled" {
		t.Fatalf("disabled: %d %s", r.Code, r.Raw)
	}
	agent.disabled = false
	e.admOK(op, "PUT", "/admin/v1/installation/wizard", map[string]any{"selected_mode": "managed_mta"})
	pl := e.admOK(op, "POST", base+"/plan", map[string]any{})
	if str(pl.JSON, "digest") == "" || !strings.Contains(pl.Raw, "notify.example.com") {
		t.Fatalf("%s", pl.Raw)
	}
	if r := e.adm(op, "POST", base+"/apply", map[string]any{}); r.Code != 422 {
		t.Fatalf("apply without approval: %d %s", r.Code, r.Raw)
	}
	if r := e.adm(op, "POST", base+"/plan", map[string]any{"hostname": "a.example';reboot"}); r.Code != 400 {
		t.Fatalf("hostname: %d", r.Code)
	}
	e.admOK(op, "POST", base+"/apply", map[string]any{"digest": "abcdef0123456789"})
	if len(agent.applied) != 1 || agent.applied[0].Digest != "abcdef0123456789" || agent.applied[0].SubmissionBind != "172.30.0.1:10587" {
		t.Fatalf("%+v", agent.applied)
	}
	cert, key := pemPair(t, "njiwa-worker")
	ca, _ := pemPair(t, "ca")
	agent.job = mtaagent.InstallJob{ID: "j1", Kind: "apply", State: "done", Lines: []string{"ok"},
		Receipt: &managed.Receipt{ID: "20261128T000000Z", Result: "ok", PlanDigest: "abcdef0123456789",
			Params:      managed.Params{Hostname: "mail.example.com", Selector: "njiwa", SubmissionBind: "172.30.0.1:10587"},
			DKIMRecords: map[string]string{"notify.example.com": dkimRecord(t)}},
		Material: &mtaagent.InstallMaterial{ClientCert: cert, ClientKey: key, CA: ca}}
	for range 2 {
		j := e.admOK(op, "GET", base+"/job", nil)
		if j.JSON["recorded"] != true || strings.Contains(j.Raw, "PRIVATE KEY") || strings.Contains(j.Raw, "client_key") {
			t.Fatalf("%s", j.Raw)
		}
	}
	if n := e.count(`SELECT count(*) FROM installer_receipts WHERE plan_digest='abcdef0123456789' AND verified_at IS NOT NULL`); n != 1 {
		t.Fatalf("receipts: %d", n)
	}
	if r := e.adm(op, "POST", base+"/rollback", map[string]any{"confirm": "yes"}); r.Code != 422 {
		t.Fatalf("rollback without confirmation: %d", r.Code)
	}
	e.admOK(op, "POST", base+"/rollback", map[string]any{"confirm": "ROLL BACK"})
	agent.job = mtaagent.InstallJob{ID: "j2", Kind: "rollback", State: "done", Receipt: &managed.Receipt{ID: "20261128T000000Z", PlanDigest: "abcdef0123456789"}}
	e.admOK(op, "GET", base+"/job", nil)
	if n := e.count(`SELECT count(*) FROM installer_receipts WHERE rollback_state='rolled_back'`); n != 1 {
		t.Fatalf("rolled back: %d", n)
	}
	// The host still reports its old jobs: neither may change the record again.
	rolledBack := agent.job
	finished := func(receipt string) mtaagent.InstallJob {
		return mtaagent.InstallJob{ID: "apply-" + receipt, Kind: "apply", State: "done",
			Material: &mtaagent.InstallMaterial{ClientCert: cert, ClientKey: key, CA: ca},
			Receipt: &managed.Receipt{ID: receipt, Result: "ok", PlanDigest: "abcdef0123456789",
				Params:      managed.Params{Hostname: "mail.example.com", SubmissionBind: "172.30.0.1:10587"},
				DKIMRecords: map[string]string{"notify.example.com": dkimRecord(t)}}}
	}
	agent.job = finished("20261128T000000Z")
	e.must(e.srv.SyncHost(ctx))
	if n := e.count(`SELECT count(*) FROM installer_receipts`); n != 1 {
		t.Fatalf("a rolled-back install was recorded again: %d", n)
	}
	agent.job = finished("20261128T020000Z") // installed again on the host
	e.must(e.srv.SyncHost(ctx))
	agent.job = rolledBack
	e.must(e.srv.SyncHost(ctx))
	if n := e.count(`SELECT count(*) FROM installer_receipts WHERE rollback_state <> 'rolled_back'`); n != 1 {
		t.Fatalf("an old rollback undid a later install: %d", n)
	}
	if e.count(`SELECT count(*) FROM audit_log WHERE action='installer.apply_requested'`) != 1 {
		t.Fatal("the approval is not audited")
	}
}

// PlanDomains and ApplyDomains make the fake agent a domain manager too.
func (f *fakeHostAgent) PlanDomains(_ context.Context, d []string) (*managed.Plan, error) {
	return &managed.Plan{Digest: "domainsdigest01", Params: managed.Params{Domains: d}}, nil
}

func (f *fakeHostAgent) ApplyDomains(_ context.Context, d []string, digest string) (*managed.DomainResult, error) {
	f.domains = append(f.domains, d...)
	records := map[string]string{}
	for _, name := range d {
		records[name] = f.dkim
	}
	return &managed.DomainResult{ReceiptID: "20261128T010000Z", Digest: digest, Selector: "njiwa", DKIMRecords: records}, nil
}

// The guided flow follows what is recorded; a finished job is recorded without
// anyone watching; and a newly verified domain gets its key by itself, only
// when the agent may change the host.
func TestHostFlowAndAutomaticDomainKeys(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.provision("sacas")
	op := e.user("admin@njiwa.co.tz", "admin")
	agent := &fakeHostAgent{dkim: dkimRecord(t)}
	e.srv.MTA = agent
	flow := func() string {
		r := e.admOK(op, "GET", "/admin/v1/installation/host/flow", nil)
		out := ""
		for _, s := range r.JSON["steps"].([]any) {
			m := s.(map[string]any)
			out += str(m, "id") + "=" + str(m, "state") + " "
		}
		return strings.TrimSpace(out)
	}
	if got := flow(); !strings.HasPrefix(got, "scan=current choose=todo") {
		t.Fatalf("start: %s", got)
	}
	e.must(e.svc.RecordDiscovery(ctx, service.CLIActor, &discover.Report{ScannedAt: time.Now(),
		Recommendation: &discover.Recommendation{Mode: "existing_smtp", ManagedBlockers: []discover.Issue{{Code: "MAILCOW_PRESENT"}}}}))
	if got := flow(); !strings.Contains(got, "scan=done choose=blocked install=blocked") || !strings.Contains(got, "activate=blocked") {
		t.Fatalf("a host with a mail server: %s", got)
	}
	e.must(e.svc.RecordDiscovery(ctx, service.CLIActor, &discover.Report{ScannedAt: time.Now(),
		Recommendation: &discover.Recommendation{Mode: "managed_mta"}}))
	e.admOK(op, "PUT", "/admin/v1/installation/wizard", map[string]any{"selected_mode": "managed_mta"})
	if got := flow(); !strings.Contains(got, "choose=done install=current") {
		t.Fatalf("chosen: %s", got)
	}
	cert, key := pemPair(t, "njiwa-worker")
	ca, _ := pemPair(t, "ca")
	agent.job = mtaagent.InstallJob{ID: "j1", Kind: "apply", State: "done",
		Receipt: &managed.Receipt{ID: "20261128T000000Z", Result: "ok", PlanDigest: "abcdef0123456789",
			Params:      managed.Params{Hostname: "mail.example.com", Selector: "njiwa", SubmissionBind: "172.30.0.1:10587"},
			DKIMRecords: map[string]string{"notify.example.com": agent.dkim}},
		Material: &mtaagent.InstallMaterial{ClientCert: cert, ClientKey: key, CA: ca}}
	// Nobody opens the page: the background round records the install.
	e.must(e.srv.SyncHost(ctx))
	// The domain passed before this key existed, which proves nothing about it.
	if got := flow(); !strings.Contains(got, "install=done dns=current") {
		t.Fatalf("installed: %s", got)
	}
	_, err := e.pool.Exec(ctx, `UPDATE domains SET last_checked_at=now()`)
	e.must(err)
	if got := flow(); !strings.Contains(got, "dns=done test=current activate=todo") {
		t.Fatalf("checked: %s", got)
	}
	if len(agent.domains) != 0 {
		t.Fatalf("every domain has a key already: %v", agent.domains)
	}
	e.provisionFrom("second", "x@second.example.org")
	agent.disabled = true
	e.must(e.srv.SyncHost(ctx))
	if len(agent.domains) != 0 {
		t.Fatal("an agent that may not change the host was asked to")
	}
	agent.disabled = false
	e.must(e.srv.SyncHost(ctx))
	e.must(e.srv.SyncHost(ctx))
	if strings.Join(agent.domains, ",") != "second.example.org" {
		t.Fatalf("keys created for: %v", agent.domains)
	}
	if got := flow(); !strings.Contains(got, "dns=current") {
		t.Fatalf("a new key needs a new check: %s", got)
	}
	missing, err := e.svc.DomainsMissingKey(ctx)
	e.must(err)
	if len(missing) != 0 {
		t.Fatalf("still missing: %v", missing)
	}
}

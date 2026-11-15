package test

import (
	"context"
	"testing"

	"github.com/njiwa-tz/mail/internal/installer/managed"
	"github.com/njiwa-tz/mail/internal/mtaagent"
	"github.com/njiwa-tz/mail/internal/service"
)

// domainAgent is the host agent as the API sees it: queue methods stay unused,
// and domain expansion returns only the public DKIM record for an approved digest.
type domainAgent struct {
	digest  string
	records map[string]string
}

func (domainAgent) Queue(context.Context) ([]mtaagent.QueueEntry, error) { return nil, nil }
func (domainAgent) Status(context.Context) (*mtaagent.Status, error) {
	return &mtaagent.Status{}, nil
}
func (domainAgent) Act(context.Context, string, mtaagent.Action) error { return nil }
func (domainAgent) Flush(context.Context) error                        { return nil }
func (a domainAgent) PlanDomains(_ context.Context, domains []string) (*managed.Plan, error) {
	return &managed.Plan{Digest: a.digest, Params: managed.Params{Domains: domains, Selector: "njiwa"},
		Files: []managed.File{{Path: "/etc/opendkim/keys/" + domains[0] + "/njiwa.private", Change: "create"}}}, nil
}
func (a domainAgent) ApplyDomains(_ context.Context, _ []string, digest string) (*managed.DomainResult, error) {
	if digest != a.digest {
		return nil, &mtaagent.AgentError{Status: 409, Code: "managed_domain_plan_changed_or_blocked"}
	}
	return &managed.DomainResult{ReceiptID: "host-receipt", Digest: digest, Selector: "njiwa", DKIMRecords: a.records}, nil
}

func TestManagedDomainOnboarding(t *testing.T) {
	e := setup(t)
	op := e.user("op@njiwa.co.tz", "admin")
	e.admOK(op, "POST", "/admin/v1/users", map[string]any{"email": "limited@njiwa.co.tz", "password": adminPass, "installation_operator": false})
	limited := e.login("limited@njiwa.co.tz")
	e.provision("da")
	e.ownDomain("da", "second.example.org")
	cert, key := pemPair(t, "njiwa-worker")
	ca, _ := pemPair(t, "ca")
	e.must(e.svc.RecordManagedInstall(context.Background(), service.CLIActor, service.ManagedInstall{
		ReceiptID: "r1", PlanDigest: "abcdef0123456789abcd", ApprovedBy: "cli", PostfixVersion: "3.8.6",
		SubmissionHost: "172.30.0.1", SubmissionPort: 10587, ServerName: "mailout.example.com", Selector: "njiwa",
		DKIMRecords: map[string]string{"notify.example.com": dkimRecord(t)}, ClientCertPEM: cert, ClientKeyPEM: key, CAPEM: ca,
	}))

	body := map[string]any{"domains": []string{"second.example.org"}}
	if r := e.adm(op, "POST", "/admin/v1/installation/domains/plan", body); r.Code != 409 || errCode(r) != "mta_agent_not_configured" {
		t.Fatalf("no agent: %d %s", r.Code, r.Raw)
	}
	rec := dkimRecord(t)
	e.srv.MTA = domainAgent{digest: "abc123digest", records: map[string]string{"second.example.org": rec}}
	if r := e.adm(limited, "POST", "/admin/v1/installation/domains/plan", body); r.Code != 403 || errCode(r) != "operator_required" {
		t.Fatalf("restricted admin: %d %s", r.Code, r.Raw)
	}
	if r := e.adm(op, "POST", "/admin/v1/installation/domains/plan", map[string]any{"domains": []string{"other.example.org"}}); r.Code != 422 || errCode(r) != "domain_not_claimed" {
		t.Fatalf("unclaimed: %d %s", r.Code, r.Raw)
	}
	plan := e.admOK(op, "POST", "/admin/v1/installation/domains/plan", body)
	if plan.JSON["digest"] != "abc123digest" || plan.JSON["selector"] != "njiwa" {
		t.Fatalf("plan: %s", plan.Raw)
	}
	if r := e.adm(op, "POST", "/admin/v1/installation/domains/apply", map[string]any{"domains": []string{"second.example.org"}, "digest": "stale"}); r.Code != 409 || errCode(r) != "managed_domain_plan_changed_or_blocked" {
		t.Fatalf("stale: %d %s", r.Code, r.Raw)
	}
	applied := e.admOK(op, "POST", "/admin/v1/installation/domains/apply", map[string]any{"domains": []string{"second.example.org"}, "digest": "abc123digest"})
	got := applied.JSON["dkim_records"].(map[string]any)["second.example.org"]
	if got != rec {
		t.Fatalf("apply: %s", applied.Raw)
	}
	mh := e.admOK(op, "GET", "/admin/v1/installation/mail-host", nil).JSON
	if missing, _ := mh["domains_missing_key"].([]any); len(missing) != 0 {
		t.Fatalf("key still missing: %v", missing)
	}
	if e.count(`SELECT count(*) FROM audit_log WHERE action='installer.domain_keys'`) != 1 {
		t.Fatal("domain key recording was not audited")
	}
}

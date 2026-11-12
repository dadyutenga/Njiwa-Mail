package test

import (
	"context"
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/service"
)

// Mail host screens: receipts, delivery-evidence flow, domains still lacking a
// DKIM key, the command to run, and a staged rotation checked against DNS.
func TestMailHostView(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	op := e.user("op@njiwa.co.tz", "admin")
	e.admOK(op, "POST", "/admin/v1/users", map[string]any{"email": "limited@njiwa.co.tz", "password": adminPass, "installation_operator": false})
	if r := e.adm(e.login("limited@njiwa.co.tz"), "GET", "/admin/v1/installation/mail-host", nil); r.Code != 403 {
		t.Fatalf("restricted admin: %d", r.Code)
	}

	k := e.provision("mh") // claims notify.example.com
	e.ownDomain("mh", "second.example.org")
	cert, key := pemPair(t, "njiwa-worker")
	ca, _ := pemPair(t, "ca")
	e.must(e.svc.RecordManagedInstall(ctx, service.CLIActor, service.ManagedInstall{ReceiptID: "r1", PlanDigest: "abcdef0123456789abcd", ApprovedBy: "cli",
		PostfixVersion: "3.8.6", SubmissionHost: "172.30.0.1", SubmissionPort: 10587, ServerName: "mailout.example.com", Selector: "njiwa",
		DKIMRecords: map[string]string{"notify.example.com": dkimRecord(t)}, ClientCertPEM: cert, ClientKeyPEM: key, CAPEM: ca}))

	mh := e.admOK(op, "GET", "/admin/v1/installation/mail-host", nil).JSON
	rc := mh["receipts"].([]any)
	if len(rc) != 1 || rc[0].(map[string]any)["result_code"] != "ok" || len(str(rc[0].(map[string]any), "plan_digest")) != 12 {
		t.Fatalf("receipts: %v", rc)
	}
	missing := mh["domains_missing_key"].([]any)
	if len(missing) != 1 || missing[0] != "second.example.org" {
		t.Fatalf("a claimed domain without a key should be listed: %v", missing)
	}
	cmds := mh["suggested_commands"].([]any)
	if len(cmds) != 2 || !strings.Contains(cmds[0].(string), "-domains notify.example.com,second.example.org") ||
		!strings.Contains(cmds[0].(string), "-submission 172.30.0.1:10587") || !strings.Contains(cmds[1].(string), "-record") {
		t.Fatalf("commands: %v", cmds)
	}
	if r := e.admOK(op, "GET", "/admin/v1/installation/mail-host", nil); strings.Contains(r.Raw, "BEGIN") {
		t.Fatal("key material in the mail host view")
	}

	// Evidence flow is reported as facts.
	acc := e.post(k.live, idem("mh1"), otpBody(testRcpt))
	id := str(acc.JSON, "id")
	e.runWorker()
	e.must(execOK(e, `INSERT INTO delivery_events(message_id, source, event, dedupe_key) VALUES ('`+id+`','mta_log','remote_accepted','mh-a')`))
	ev := e.admOK(op, "GET", "/admin/v1/installation/mail-host", nil).JSON["evidence"].(map[string]any)
	if ev["mta_log"].(map[string]any)["events_24h"] != float64(1) || ev["mta_log"].(map[string]any)["last_at"] == nil || ev["dsn"].(map[string]any)["last_at"] != nil {
		t.Fatalf("evidence: %v", ev)
	}

	// Rotation: nothing, then prepared (checked against DNS), then activated.
	if rot := e.admOK(op, "GET", "/admin/v1/installation/mail-host", nil).JSON["dkim_rotation"].(map[string]any); rot["state"] != "none" {
		t.Fatalf("rotation: %v", rot)
	}
	e.must(e.svc.RecordRotation(ctx, service.CLIActor, map[string]any{"state": "prepared", "from": "njiwa", "to": "njiwa2",
		"records": map[string]any{"notify.example.com": dkimRecord(t)}}))
	dns := newDNS()
	e.srv.Resolver = dns
	rot := e.admOK(op, "GET", "/admin/v1/installation/mail-host", nil).JSON["dkim_rotation"].(map[string]any)
	if rot["ready_to_activate"] != false || rot["state"] != "prepared" || rot["records"] != nil {
		t.Fatalf("not published yet: %v", rot)
	}
	dns.txt["njiwa2._domainkey.notify.example.com"] = []string{dkimRecord(t)}
	rot = e.admOK(op, "GET", "/admin/v1/installation/mail-host", nil).JSON["dkim_rotation"].(map[string]any)
	checks := rot["dns"].([]any)
	if rot["ready_to_activate"] != true || len(checks) != 1 || checks[0].(map[string]any)["published"] != true {
		t.Fatalf("published: %v", rot)
	}
	e.must(e.svc.RecordRotation(ctx, service.CLIActor, map[string]any{"state": "activated", "from": "njiwa", "to": "njiwa2"}))
	if rot := e.admOK(op, "GET", "/admin/v1/installation/mail-host", nil).JSON["dkim_rotation"].(map[string]any); rot["state"] != "activated" {
		t.Fatalf("activated: %v", rot)
	}
	if e.count(`SELECT count(*) FROM audit_log WHERE action='installer.dkim_rotation'`) != 2 {
		t.Fatal("rotation records not audited")
	}

	// A domain's page says its key is missing when it is a managed domain without one.
	e.must(execOK(e, `UPDATE transport_profiles SET is_active=true WHERE mode='managed_mta'`))
	var secondID string
	e.must(e.pool.QueryRow(ctx, `SELECT id::text FROM domains WHERE name='second.example.org'`).Scan(&secondID))
	if d := e.admOK(op, "GET", wsPath("mh", "/domains/"+secondID), nil); d.JSON["managed_key_missing"] != true {
		t.Fatalf("domain detail: %v", d.JSON["managed_key_missing"])
	}
}

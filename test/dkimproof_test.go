package test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func signedFixture(t *testing.T) ([]byte, string) {
	t.Helper()
	msg, err := os.ReadFile("../internal/dnsauth/testdata/opendkim-signed.eml")
	e := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	e(err)
	key, err := os.ReadFile("../internal/dnsauth/testdata/opendkim-signed.key.txt")
	e(err)
	return msg, strings.TrimSpace(string(key))
}

// D06: a published key is not proof; a received message with a valid,
// aligned signature is. Checked with a message really signed by OpenDKIM.
func TestSignedMailEvidence(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	raw, key := signedFixture(t)
	dns := newDNS()
	dns.txt["njiwa._domainkey.notify.example.test"] = []string{key}
	e.srv.Resolver = dns
	e.must(e.svc.CreateProject(ctx, "proof"))
	d := e.admOK(admin, "POST", wsPath("proof", "/domains"), map[string]any{"name": "notify.example.test", "purpose": "sending"})
	id := str(d.JSON, "id")
	path := wsPath("proof", "/domains/"+id)
	if v := e.admOK(admin, "GET", path, nil).JSON["dkim_verified_at"]; v != nil {
		t.Fatalf("evidence before any message: %v", v)
	}
	// Tampered message: not recorded.
	bad := bytes.Replace(raw, []byte("hello from the managed MTA test"), []byte("changed"), 1)
	r := e.admOK(admin, "POST", path+"/verify-message", map[string]any{"raw": string(bad)})
	if r.JSON["aligned"] != false || r.JSON["recorded"] != false || !strings.Contains(str(r.JSON, "message"), "body hash") {
		t.Fatalf("tampered: %s", r.Raw)
	}
	// A message from another domain cannot prove this one.
	other := e.admOK(admin, "POST", wsPath("proof", "/domains"), map[string]any{"name": "other.example.test", "purpose": "sending"})
	r = e.admOK(admin, "POST", wsPath("proof", "/domains/"+str(other.JSON, "id")+"/verify-message"), map[string]any{"raw": string(raw)})
	if r.JSON["recorded"] != false || !strings.Contains(str(r.JSON, "message"), "is from notify.example.test") {
		t.Fatalf("other domain: %s", r.Raw)
	}
	// The genuine message: valid, aligned, recorded.
	r = e.admOK(admin, "POST", path+"/verify-message", map[string]any{"raw": string(raw)})
	if r.JSON["aligned"] != true || r.JSON["recorded"] != true {
		t.Fatalf("genuine: %s", r.Raw)
	}
	det := e.admOK(admin, "GET", path, nil).JSON
	if det["dkim_verified_at"] == nil || det["dkim_verified_selector"] != "njiwa" || det["dkim_verified_source"] != "upload" {
		t.Fatalf("evidence: %v", det)
	}
	if e.count(`SELECT count(*) FROM audit_log WHERE action='domain.verify_message'`) != 3 {
		t.Fatal("not audited")
	}
	if r := e.adm(admin, "POST", path+"/verify-message", map[string]any{"raw": ""}); r.Code != 400 {
		t.Fatalf("empty message: %d", r.Code)
	}
}

// The same evidence is recorded automatically when such a message is
// received into the project's own inbox.
func TestInboundMessageRecordsSignedEvidence(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	raw, key := signedFixture(t)
	dns := newDNS()
	dns.txt["njiwa._domainkey.notify.example.test"] = []string{key}
	e.provision("inb")
	_, err := e.pool.Exec(ctx, `INSERT INTO domains(project_id, name, purpose, verify_token) SELECT id, 'notify.example.test', 'sending', 't' FROM projects WHERE name='inb'`)
	e.must(err)
	_, err = e.svc.CreateInboundAddress(ctx, "inb", "live", "proof", nil, "store", "")
	e.must(err)
	e.svc.RecordInboundDKIM(ctx, []string{"proof@" + inDomain}, raw, dns)
	if e.count(`SELECT count(*) FROM domains WHERE name='notify.example.test' AND dkim_verified_at IS NOT NULL AND dkim_verified_source='inbound'`) != 1 {
		t.Fatal("inbound evidence not recorded")
	}
	// Received by a different project: no evidence for a domain it does not own.
	e.provision("stranger")
	_, err = e.svc.CreateInboundAddress(ctx, "stranger", "live", "box", nil, "store", "")
	e.must(err)
	_, err = e.pool.Exec(ctx, `UPDATE domains SET dkim_verified_at=NULL`)
	e.must(err)
	e.svc.RecordInboundDKIM(ctx, []string{"box@" + inDomain}, raw, dns)
	if e.count(`SELECT count(*) FROM domains WHERE dkim_verified_at IS NOT NULL`) != 0 {
		t.Fatal("another project's inbox recorded evidence")
	}
}

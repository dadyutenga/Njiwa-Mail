package test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/service"
)

// D04: a domain from another organisation names its own return-path domain, so
// SPF aligns with its From domain; routing, envelope sender and the DNS check follow.
func TestPerDomainReturnPath(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.must(e.svc.CreateProject(ctx, "acme"))
	const name, rp = "acme.co.tz", "bounce.acme.co.tz"
	d := e.admOK(admin, "POST", wsPath("acme", "/domains"), map[string]any{"name": name, "purpose": "sending"})
	id := str(d.JSON, "id")
	base := wsPath("acme", "/domains/"+id)
	e.saveSMTP("smtp.provider.example", "pw")
	e.must(execOK(e, `UPDATE transport_profiles SET provider_type='mailcow'`)) // known egress IP 203.0.113.10
	dns := newDNS()
	dns.a = map[string][]string{}
	e.srv.Resolver = dns
	dns.txt[name] = []string{str(d.JSON, "txt_challenge"), "v=spf1 ip4:203.0.113.10 -all"}
	dns.txt["_dmarc."+name] = []string{"v=DMARC1; p=reject"} // no DKIM published: DMARC must come from SPF
	dns.txt["notify.example.com"] = []string{"v=spf1 ip4:203.0.113.10 -all"}
	check := func() resp { return e.admOK(admin, "POST", base+"/check", nil) }

	// Without its own return path the installation's bounce domain (another organisation) is not aligned.
	if r := recordByKey(check(), "dmarc"); r["pass"] != false || !strings.Contains(str(r, "hint"), "not aligned") {
		t.Fatalf("default return path should not align: %v", r)
	}
	if recordByKey(check(), "return_path_spf") != nil {
		t.Fatal("no per-domain record expected before it is set")
	}
	if got, _ := e.svc.ResolveRecipient(ctx, "bounces@"+rp); got != "" {
		t.Fatalf("an unregistered return-path domain is routed as %q", got)
	}

	// Validation.
	for body, code := range map[string]string{"example.org": "return_path_not_under_domain", "notacme.co.tz": "return_path_not_under_domain",
		"bad domain": "", "notify.example.com": "return_path_not_under_domain"} {
		r := e.adm(admin, "PUT", base+"/return-path", map[string]any{"bounce_domain": body})
		if r.Code == 200 || (code != "" && errCode(r) != code) {
			t.Errorf("%q: %d %s", body, r.Code, r.Raw)
		}
	}
	// A project cannot set another project's domain.
	e.must(e.svc.CreateProject(ctx, "other"))
	if r := e.adm(admin, "PUT", wsPath("other", "/domains/"+id+"/return-path"), map[string]any{"bounce_domain": rp}); r.Code != 404 {
		t.Fatalf("cross-project: %d", r.Code)
	}

	// Set it: the domain must be checked again, records name the new domain.
	e.must(execOK(e, `UPDATE domains SET spf_ok=true, dmarc_ok=true WHERE id='`+id+`'`))
	got := e.admOK(admin, "PUT", base+"/return-path", map[string]any{"bounce_domain": "Bounce.Acme.co.tz."})
	if got.JSON["bounce_domain"] != rp {
		t.Fatalf("not canonical: %v", got.JSON["bounce_domain"])
	}
	if e.count(`SELECT count(*) FROM domains WHERE id=$1 AND NOT spf_ok AND NOT dmarc_ok`, id) != 1 {
		t.Fatal("changing the return path must withdraw the SPF/DMARC pass until the next check")
	}
	if e.count(`SELECT count(*) FROM audit_log WHERE action='domain.return_path'`) < 1 {
		t.Fatal("not audited")
	}
	r := check()
	if mx := recordByKey(r, "return_path"); mx == nil || mx["host"] != rp || !strings.Contains(str(mx, "hint"), "bounces@"+rp) {
		t.Fatalf("return_path record: %v", mx)
	}
	if sp := recordByKey(r, "return_path_spf"); sp == nil || sp["host"] != rp || sp["pass"] != false {
		t.Fatalf("return_path_spf must fail until published: %v", sp)
	}
	if recordByKey(r, "dmarc")["pass"] != false {
		t.Fatal("DMARC must not pass while the return-path SPF is missing")
	}
	// Publish SPF and MX at the return-path domain: aligned SPF carries DMARC.
	dns.txt[rp] = []string{"v=spf1 ip4:203.0.113.10 -all"}
	dns.mx[rp] = []string{"mail.example.com."}
	r = check()
	if recordByKey(r, "return_path_spf")["pass"] != true || recordByKey(r, "return_path")["pass"] != true {
		t.Fatalf("return path records: %v", r.JSON["records"])
	}
	if dm := recordByKey(r, "dmarc"); dm["pass"] != true {
		t.Fatalf("aligned SPF should satisfy DMARC: %v", dm)
	}

	// Routing: both plain and signed bounce mail at the new domain is a bounce.
	for _, a := range []string{"bounces@" + rp, "bounces+" + strings.Repeat("a", 32) + "." + strings.Repeat("b", 16) + "@" + rp} {
		if got, err := e.svc.ResolveRecipient(ctx, a); err != nil || got != service.KindBounce {
			t.Errorf("ResolveRecipient(%s) = %q %v", a, got, err)
		}
	}
	if got, _ := e.svc.ResolveRecipient(ctx, "other@"+rp); got == service.KindBounce {
		t.Error("a non-bounce mailbox at the return-path domain was treated as a bounce")
	}

	// Envelope sender: this domain's mail uses its own return path, other domains keep the default.
	k := e.provisionFrom("acme2", "hello@"+name)
	e.must(execOK(e, `UPDATE domains SET bounce_domain='`+rp+`' WHERE name='`+name+`'`)) // the project that holds the claim
	e.w.VERP = "off"
	body := otpBody(testRcpt)
	body["from"] = "hello@" + name
	if res := e.post(k.live, idem("rp1"), body); res.Code != 202 {
		t.Fatalf("send: %d %s", res.Code, res.Raw)
	}
	e.runWorker()
	if e.tr.last.From != "bounces@"+rp {
		t.Fatalf("envelope sender %q", e.tr.last.From)
	}
	e.w.VERP = "on"
	if res := e.post(k.live, idem("rp2"), body); res.Code != 202 {
		t.Fatalf("send: %d %s", res.Code, res.Raw)
	}
	e.runWorker()
	if !regexp.MustCompile(`^bounces\+[0-9a-f]{32}\.[0-9a-f]{16}@bounce\.acme\.co\.tz$`).MatchString(e.tr.last.From) {
		t.Fatalf("signed envelope sender %q", e.tr.last.From)
	}
	// A DSN to the signed per-domain address correlates and verifies.
	mid := e.lastMessageID()
	e.must(e.svc.RecordBounce(ctx, service.SpoolItem{ID: "00000000-0000-4000-8000-0000000000aa", To: []string{e.tr.last.From}}, []service.BounceReport{
		{Recipient: testRcpt, Status: "5.1.1", Action: "failed", DSNID: "dsn-rp"}}))
	if e.count(`SELECT count(*) FROM bounces WHERE message_id=$1 AND correlated_by='verp'`, mid) != 1 {
		t.Fatal("a bounce to the per-domain signed return path was not correlated")
	}
	// Another domain keeps the installation's bounce address.
	if got := e.svc.EnvelopeSender(ctx, mid, "security@notify.example.com", false); got != bounceAddr {
		t.Fatalf("a domain without its own return path used %q", got)
	}

	// Clearing returns to the installation's bounce domain and unroutes the old one
	// (the helper project's copy of the row is cleared too).
	e.must(execOK(e, `UPDATE domains SET bounce_domain='' WHERE project_id=(SELECT id FROM projects WHERE name='acme2')`))
	e.admOK(admin, "PUT", base+"/return-path", map[string]any{"bounce_domain": ""})
	if got, _ := e.svc.ResolveRecipient(ctx, "bounces@"+rp); got == service.KindBounce {
		t.Fatal("a cleared return-path domain is still routed")
	}
}

func (e *env) lastMessageID() string {
	var id string
	e.must(e.pool.QueryRow(context.Background(), `SELECT id::text FROM messages ORDER BY created_at DESC LIMIT 1`).Scan(&id))
	return id
}

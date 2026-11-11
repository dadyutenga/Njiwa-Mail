package test

import (
	"context"
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/service"
)

func wsPath(project, rest string) string { return "/admin/v1/w/" + project + "/live" + rest }

// addAndPublish adds the domain to a project through the workspace API and
// publishes that project's ownership token plus passing SPF/DKIM/DMARC.
func (e *env) addAndPublish(admin session, dns fakeDNS, project, name string) string {
	e.t.Helper()
	d := e.admOK(admin, "POST", wsPath(project, "/domains"), map[string]any{"name": name, "purpose": "sending"})
	dns.txt[name] = append(dns.txt[name], str(d.JSON, "txt"))
	if len(dns.txt["_dmarc."+name]) == 0 {
		dns.txt[name] = append(dns.txt[name], "v=spf1 mx -all")
		dns.txt["dkim._domainkey."+name] = []string{"v=DKIM1; k=rsa; p=MIIB"}
		dns.txt["_dmarc."+name] = []string{"v=DMARC1; p=none"}
	}
	return str(d.JSON, "id")
}

func ownershipHint(r resp) string {
	for _, rec := range r.JSON["records"].([]any) {
		if m := rec.(map[string]any); m["key"] == "ownership" {
			h, _ := m["hint"].(string)
			if m["pass"] == true {
				return "pass"
			}
			return h
		}
	}
	return ""
}

// D01: the first project to prove a domain holds the claim; another project
// with the same DNS access still cannot use it until an operator transfers it.
func TestDomainClaimIsInstallationWide(t *testing.T) {
	e := setup(t)
	dns := newDNS()
	e.srv.Resolver = dns
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	const name = "shared.example.com"
	const from = "hello@" + name
	for _, p := range []string{"alpha", "beta"} {
		e.must(e.svc.CreateProject(ctx, p))
		e.must(e.svc.AllowRecipient(ctx, p, "live", "@example.org", false))
	}
	aID := e.addAndPublish(admin, dns, "alpha", name)
	bID := e.addAndPublish(admin, dns, "beta", name) // beta publishes its own token too

	if h := ownershipHint(e.admOK(admin, "POST", wsPath("alpha", "/domains/"+aID+"/check"), nil)); h != "pass" {
		t.Fatalf("alpha ownership: %s", h)
	}
	r := e.admOK(admin, "POST", wsPath("beta", "/domains/"+bID+"/check"), nil)
	if h := ownershipHint(r); !strings.Contains(h, "another project") {
		t.Fatalf("beta must not verify a domain alpha holds: %q", h)
	}
	if e.count(`SELECT count(*) FROM domains WHERE id=$1 AND verified_at IS NULL`, bID) != 1 {
		t.Fatal("beta's row was marked verified")
	}
	// Beta can neither create a Live sender nor send as the domain.
	if r := e.adm(admin, "POST", wsPath("beta", "/senders"), map[string]any{"email": from}); r.Code == 200 {
		t.Fatalf("beta created a live sender on alpha's domain: %s", r.Raw)
	}
	if err := e.svc.AddSender(ctx, "beta", "live", from, "x"); errCodeOf(err) != "domain_claimed_elsewhere" {
		t.Fatalf("CLI path: %v", err)
	}
	e.must(e.svc.AddSender(ctx, "alpha", "live", from, "Alpha"))
	if r := e.adm(admin, "POST", "/admin/v1/senders", map[string]any{"project": "beta", "mode": "live", "email": from}); r.Code != 403 || errCode(r) != "domain_claimed_elsewhere" {
		t.Fatalf("admin API path: %d %s", r.Code, r.Raw)
	}
	senders := items(e.admOK(admin, "GET", wsPath("alpha", "/senders"), nil))
	if len(senders) != 1 || senders[0]["status"] != "verified" {
		t.Fatalf("alpha sender: %v", senders)
	}

	// Operator transfer: audited, resets every project's proof, and the old
	// owner's sender stops counting as verified at once.
	if r := e.adm(admin, "POST", "/admin/v1/domain-claims/transfer", map[string]any{"name": name, "project": "gamma"}); r.Code != 404 {
		t.Fatalf("unknown project: %d", r.Code)
	}
	e.admOK(admin, "POST", "/admin/v1/domain-claims/transfer", map[string]any{"name": "Shared.Example.com.", "project": "beta"})
	if e.count(`SELECT count(*) FROM domains WHERE name=$1 AND verified_at IS NOT NULL`, name) != 0 {
		t.Fatal("transfer must reset ownership proof")
	}
	if e.count(`SELECT count(*) FROM audit_log WHERE action='domain.claim_transfer' AND target_id=$1`, name) != 1 {
		t.Fatal("transfer not audited")
	}
	senders = items(e.admOK(admin, "GET", wsPath("alpha", "/senders"), nil))
	if senders[0]["status"] == "verified" {
		t.Fatal("old owner's sender still verified after transfer")
	}
	if h := ownershipHint(e.admOK(admin, "POST", wsPath("alpha", "/domains/"+aID+"/check"), nil)); !strings.Contains(h, "another project") {
		t.Fatalf("alpha must not re-take the claim: %q", h)
	}
	if h := ownershipHint(e.admOK(admin, "POST", wsPath("beta", "/domains/"+bID+"/check"), nil)); h != "pass" {
		t.Fatalf("beta after transfer: %s", h)
	}
	e.must(e.svc.AddSender(ctx, "beta", "live", from, "Beta"))
	claims := items(e.admOK(admin, "GET", "/admin/v1/domain-claims", nil))
	if len(claims) != 1 || claims[0]["project"] != "beta" || claims[0]["state"] != "active" {
		t.Fatalf("claims: %v", claims)
	}

	// Deleting the domain releases the claim so another project can prove it.
	e.admOK(admin, "DELETE", wsPath("beta", "/senders/"+str(items(e.admOK(admin, "GET", wsPath("beta", "/senders"), nil))[0], "id")), nil)
	e.admOK(admin, "DELETE", wsPath("beta", "/domains/"+bID), nil)
	if e.count(`SELECT count(*) FROM domain_claims WHERE name=$1`, name) != 0 {
		t.Fatal("claim not released on delete")
	}
	if h := ownershipHint(e.admOK(admin, "POST", wsPath("alpha", "/domains/"+aID+"/check"), nil)); h != "pass" {
		t.Fatalf("alpha after release: %s", h)
	}
}

// D03: names are canonical (lowercase ASCII/punycode, no trailing dot), so
// two spellings of one domain cannot become two claims.
func TestDomainNamesAreCanonical(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.must(e.svc.CreateProject(ctx, "idn"))
	d := e.admOK(admin, "POST", wsPath("idn", "/domains"), map[string]any{"name": " Bücher.Example. ", "purpose": "sending"})
	if e.count(`SELECT count(*) FROM domains WHERE id=$1 AND name='xn--bcher-kva.example'`, str(d.JSON, "id")) != 1 {
		t.Fatal("IDN domain not stored as punycode")
	}
	if r := e.adm(admin, "POST", wsPath("idn", "/domains"), map[string]any{"name": "xn--bcher-kva.example", "purpose": "sending"}); r.Code != 409 {
		t.Fatalf("same domain in another spelling accepted: %d", r.Code)
	}
	for _, bad := range []string{"", "http://x.example", "a@b.example", "no_tld", "x..example"} {
		if r := e.adm(admin, "POST", wsPath("idn", "/domains"), map[string]any{"name": bad, "purpose": "sending"}); r.Code != 400 {
			t.Fatalf("%q accepted: %d", bad, r.Code)
		}
	}
	if n, ok := service.NormalizeDomain("Notify.Example.COM."); !ok || n != "notify.example.com" {
		t.Fatalf("%q %v", n, ok)
	}
}

// Migration 0011 never hands a domain verified by two projects to either:
// it is flagged for review, nobody can send, and a transfer resolves it.
func TestDuplicateVerifiedDomainsNeedReview(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	for _, q := range []string{`DROP TABLE domain_claims`, `DELETE FROM schema_migrations WHERE version='0011_domain_claims.sql'`} {
		_, err := e.pool.Exec(ctx, q)
		e.must(err)
	}
	for _, p := range []string{"one", "two", "solo"} {
		e.must(e.svc.CreateProject(ctx, p))
	}
	_, err := e.pool.Exec(ctx, `INSERT INTO domains(project_id, name, purpose, verify_token, verified_at, spf_ok, dkim_ok, dmarc_ok)
SELECT id, CASE WHEN name='solo' THEN 'solo.example.com' ELSE 'dup.example.com' END, 'sending', 't-'||name, now(), true, true, true FROM projects`)
	e.must(err)
	e.must(db.Migrate(ctx, e.pool))
	if e.count(`SELECT count(*) FROM domain_claims WHERE name='dup.example.com' AND state='review_required' AND project_id IS NULL`) != 1 ||
		e.count(`SELECT count(*) FROM domain_claims c JOIN projects p ON p.id=c.project_id WHERE c.name='solo.example.com' AND c.state='active' AND p.name='solo'`) != 1 {
		t.Fatal("backfill")
	}
	for _, p := range []string{"one", "two"} {
		if err := e.svc.AddSender(ctx, p, "live", "a@dup.example.com", "x"); errCodeOf(err) != "domain_claimed_elsewhere" {
			t.Fatalf("%s may use a domain under review: %v", p, err)
		}
	}
	e.admOK(admin, "POST", "/admin/v1/domain-claims/transfer", map[string]any{"name": "dup.example.com", "project": "two"})
	_, err = e.pool.Exec(ctx, `UPDATE domains SET verified_at=now() WHERE name='dup.example.com' AND project_id=(SELECT id FROM projects WHERE name='two')`)
	e.must(err) // what a successful re-check by "two" does
	e.must(e.svc.AddSender(ctx, "two", "live", "a@dup.example.com", "x"))
	if err := e.svc.AddSender(ctx, "one", "live", "a@dup.example.com", "x"); errCodeOf(err) != "domain_claimed_elsewhere" {
		t.Fatalf("one after transfer: %v", err)
	}
}

// The upgrade escape hatch allows only names nobody has claimed; it is never a
// way to send as another project's domain. Off by default.
func TestUnregisteredDomainEscapeHatchIsNarrow(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.provision("owner") // holds notify.example.com
	k := e.provisionFrom("legacy", "a@legacy.example.com")
	_, err := e.pool.Exec(ctx, `DELETE FROM domain_claims WHERE name='legacy.example.com'; DELETE FROM domains WHERE name='legacy.example.com'`)
	e.must(err) // an installation upgraded with an unregistered sending domain
	body := otpBody(testRcpt)
	body["from"] = "a@legacy.example.com"
	if r := e.post(k.live, idem("strict"), body); r.Code != 403 || errCode(r) != "domain_not_registered" {
		t.Fatalf("default must be strict: %d %s", r.Code, r.Raw)
	}
	e.svc.Opts.AllowUnregisteredLiveDomains = true
	t.Cleanup(func() { e.svc.Opts.AllowUnregisteredLiveDomains = false })
	if r := e.post(k.live, idem("hatch"), body); r.Code != 202 {
		t.Fatalf("escape hatch: %d %s", r.Code, r.Raw)
	}
	if err := e.svc.AddSender(ctx, "legacy", "live", liveFrom, "x"); errCodeOf(err) != "domain_claimed_elsewhere" {
		t.Fatalf("escape hatch reached another project's claimed domain: %v", err)
	}
}

func recordByKey(r resp, key string) map[string]any {
	for _, rec := range r.JSON["records"].([]any) {
		if m := rec.(map[string]any); m["key"] == key {
			return m
		}
	}
	return nil
}

// SPF and DKIM must describe the server that actually sends. With a generic
// SMTP server Njiwa-Mail cannot see its egress or signer, so it shows marked
// templates and never authorises OUTBOUND_IP or MAIL_HOSTNAME. MX guidance
// names exactly the host the check compares, at priority 10.
func TestDNSRecordsFollowTheActualSender(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.must(e.svc.CreateProject(ctx, "rec"))
	d := e.admOK(admin, "POST", wsPath("rec", "/domains"), map[string]any{"name": "rec.example.com", "purpose": "both"})
	path := wsPath("rec", "/domains/"+str(d.JSON, "id"))

	e.saveSMTP("smtp.provider.example", "pw") // generic provider (default)
	r := e.admOK(admin, "GET", path, nil)
	spf, dkim, mx := recordByKey(r, "spf"), recordByKey(r, "dkim"), recordByKey(r, "mx")
	if spf["placeholder"] != true || strings.Contains(str(spf, "value"), "203.0.113.10") || strings.Contains(str(spf, "value"), "mail.example.com") {
		t.Fatalf("generic SPF authorises this host: %v", spf)
	}
	if dkim["placeholder"] != true || strings.Contains(str(dkim, "value"), "Mailcow") || strings.Contains(str(dkim, "hint"), "Mailcow") {
		t.Fatalf("generic DKIM: %v", dkim)
	}
	if mx["placeholder"] == true || str(mx, "value") != "mail.example.com" || mx["priority"].(float64) != 10 ||
		!strings.Contains(str(mx, "hint"), "at mail.example.com with") || !strings.Contains(str(mx, "hint"), "priority 10") {
		t.Fatalf("MX guidance must name the checked host: %v", mx)
	}
	if rp := recordByKey(r, "return_path"); !strings.Contains(str(rp, "hint"), "at mail.example.com with") {
		t.Fatalf("return path guidance: %v", rp)
	}
	// The check itself is unchanged: an MX at the configured host passes.
	dns := newDNS()
	dns.mx["rec.example.com"] = []string{"mail.example.com."}
	e.srv.Resolver = dns
	if m := recordByKey(e.admOK(admin, "POST", path+"/check", nil), "mx"); m["pass"] != true {
		t.Fatalf("MX check: %v", m)
	}

	// Mailcow chosen: OUTBOUND_IP describes the Mailcow server, so it is used.
	owner := e.user("owner@njiwa.co.tz", "owner")
	e.admOK(owner, "PUT", "/admin/v1/mail-server", map[string]any{"smtp_host": "smtp.provider.example", "smtp_port": 587,
		"smtp_tls_mode": "starttls", "smtp_username": "njiwa@example.com", "provider_type": "mailcow"})
	// The page shows the records of the last check; a re-check uses the new sender.
	r = e.admOK(admin, "POST", path+"/check", nil)
	if spf := recordByKey(r, "spf"); spf["placeholder"] == true || str(spf, "value") != "v=spf1 mx ip4:203.0.113.10 -all" {
		t.Fatalf("mailcow SPF: %v", spf)
	}
	if dkim := recordByKey(r, "dkim"); dkim["placeholder"] != true || !strings.Contains(str(dkim, "value"), "Mailcow") {
		t.Fatalf("mailcow DKIM without the API is still a template: %v", dkim)
	}
}

// D05/D07 on the Domains page: SPF is evaluated, not just found; DMARC is
// parsed and must be able to align.
func TestDomainCheckEvaluatesSPFAndDMARC(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.must(e.svc.CreateProject(ctx, "auth"))
	const name = "notify.example.com" // also the bounce address domain in tests
	d := e.admOK(admin, "POST", wsPath("auth", "/domains"), map[string]any{"name": name, "purpose": "sending"})
	path := wsPath("auth", "/domains/"+str(d.JSON, "id")+"/check")
	dns := newDNS()
	dns.a = map[string][]string{}
	e.srv.Resolver = dns
	set := func(spf ...string) {
		dns.txt[name] = append([]string{str(d.JSON, "txt")}, spf...)
	}
	dns.txt["dkim._domainkey."+name] = []string{"v=DKIM1; k=rsa; p=MIIB"}
	dns.txt["_dmarc."+name] = []string{"v=DMARC1; p=quarantine"}
	rec := func(key string) map[string]any { return recordByKey(e.admOK(admin, "POST", path, nil), key) }

	// Mailcow (known egress 203.0.113.10): the IP must really be authorised.
	e.saveSMTP("smtp.provider.example", "pw")
	_, err := e.pool.Exec(ctx, `UPDATE transport_profiles SET provider_type='mailcow'`)
	e.must(err)
	set("v=spf1 ip4:198.51.100.0/24 -all")
	if r := rec("spf"); r["pass"] != false || !strings.Contains(str(r, "hint"), "fail for 203.0.113.10") {
		t.Fatalf("wrong source IP passed: %v", r)
	}
	set("v=spf1 include:_spf.provider.example -all")
	dns.txt["_spf.provider.example"] = []string{"v=spf1 ip4:203.0.113.0/24 -all"}
	if r := rec("spf"); r["pass"] != true {
		t.Fatalf("authorised through include: %v", r)
	}
	set("v=spf1 mx -all")
	dns.mx[name] = []string{"mx.example.com."}
	dns.a["mx.example.com"] = []string{"203.0.113.10"}
	if r := rec("spf"); r["pass"] != true {
		t.Fatalf("authorised through mx: %v", r)
	}
	set("v=spf1 ip4:203.0.113.10 -all", "v=spf1 mx -all")
	if r := rec("spf"); r["pass"] != false || !strings.Contains(str(r, "hint"), "Two SPF") {
		t.Fatalf("duplicate SPF passed: %v", r)
	}
	set("v=spf1 ip4:203.0.113.10 +all")
	if r := rec("spf"); r["pass"] != false || !strings.Contains(str(r, "hint"), "+all") {
		t.Fatalf("+all passed: %v", r)
	}

	// Generic SMTP: the egress is unknown, so a valid record passes with an
	// honest hint, and an invalid one (lookup loop) fails.
	_, err = e.pool.Exec(ctx, `UPDATE transport_profiles SET provider_type='generic'`)
	e.must(err)
	set("v=spf1 ip4:198.51.100.0/24 -all")
	if r := rec("spf"); r["pass"] != true || !strings.Contains(str(r, "hint"), "only a test message") {
		t.Fatalf("generic valid SPF: %v", r)
	}
	set("v=spf1 include:" + name + " -all")
	if r := rec("spf"); r["pass"] != false || !strings.Contains(str(r, "hint"), "loop") {
		t.Fatalf("SPF loop passed: %v", r)
	}

	// DMARC: invalid syntax fails with the reason; a valid policy passes.
	set("v=spf1 ip4:198.51.100.0/24 -all")
	dns.txt["_dmarc."+name] = []string{"v=DMARC1; p=block"}
	if r := rec("dmarc"); r["pass"] != false || !strings.Contains(str(r, "hint"), "p must be") {
		t.Fatalf("invalid DMARC passed: %v", r)
	}
	dns.txt["_dmarc."+name] = []string{"v=DMARC1; p=reject; aspf=s"}
	if r := rec("dmarc"); r["pass"] != true {
		t.Fatalf("valid DMARC: %v", r)
	}
	// No DKIM and SPF on a non-aligned envelope domain: DMARC cannot pass.
	e.svc.Opts.BounceAddress = "bounces@bounce-provider.net"
	t.Cleanup(func() { e.svc.Opts.BounceAddress = bounceAddr })
	dns.txt["dkim._domainkey."+name] = nil
	dns.txt["bounce-provider.net"] = []string{"v=spf1 ip4:198.51.100.0/24 -all"}
	if r := rec("dmarc"); r["pass"] != false || !strings.Contains(str(r, "hint"), "not aligned") {
		t.Fatalf("misaligned DMARC passed: %v", r)
	}
	// With DKIM back, DMARC passes and explains it relies on DKIM.
	dns.txt["dkim._domainkey."+name] = []string{"v=DKIM1; k=rsa; p=MIIB"}
	if r := rec("dmarc"); r["pass"] != true || !strings.Contains(str(r, "hint"), "through DKIM only") {
		t.Fatalf("DKIM-aligned DMARC: %v", r)
	}
	// Organisational-domain fallback, reported as such.
	dns.txt["_dmarc."+name] = nil
	dns.txt["_dmarc.example.com"] = []string{"v=DMARC1; p=none"}
	if r := rec("dmarc"); r["pass"] != true || !strings.Contains(str(r, "found"), "_dmarc.example.com") {
		t.Fatalf("org-domain DMARC: %v", r)
	}
}

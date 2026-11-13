package test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/dnsprovider"
	"github.com/njiwa-tz/mail/internal/service"
)

const (
	cfToken  = "tok_0123456789abcdefghijklmnopqrstuv"
	pubIP    = "93.184.216.34" // a public address: 203.0.113.x is documentation space and is refused as a sending address
	zoneName = "example.com"
	domName  = "notify.example.com"
)

// nsDNS adds NS lookups to the shared fake resolver.
type nsDNS struct {
	fakeDNS
	ns map[string][]string
}

func (f nsDNS) LookupNS(_ context.Context, n string) ([]*net.NS, error) {
	var out []*net.NS
	for _, h := range f.ns[n] {
		out = append(out, &net.NS{Host: h})
	}
	if len(out) == 0 {
		return nil, &net.DNSError{Err: "no such host", Name: n, IsNotFound: true}
	}
	return out, nil
}

type dnsEnv struct {
	*env
	mem      *dnsprovider.Memory
	admin    session
	dns      nsDNS
	project  string
	domainID string
	conn     string
	zone     string
	sleeps   []time.Duration
	bodies   []string // every response body, scanned for secrets
	mu       sync.Mutex
}

func (d *dnsEnv) path(rest string) string { return wsPath(d.project, rest) }

func (d *dnsEnv) call(method, rest string, body any) resp {
	d.t.Helper()
	r := d.adm(d.admin, method, d.path(rest), body)
	d.mu.Lock()
	d.bodies = append(d.bodies, r.Raw)
	d.mu.Unlock()
	return r
}

func (d *dnsEnv) ok(method, rest string, body any) resp {
	d.t.Helper()
	r := d.call(method, rest, body)
	if r.Code != 200 {
		d.t.Fatalf("%s %s: %d %s", method, rest, r.Code, r.Raw)
	}
	return r
}

// publish makes the zone's records visible to the checker, as propagation would.
func (d *dnsEnv) publish() {
	for k := range d.dns.txt {
		delete(d.dns.txt, k)
	}
	d.dns.mx[domName] = nil
	for _, r := range d.mem.Snapshot(d.zone) {
		switch r.Type {
		case "TXT":
			d.dns.txt[r.Name] = append(d.dns.txt[r.Name], r.Content)
		case "MX":
			d.dns.mx[r.Name] = append(d.dns.mx[r.Name], r.Content+".")
		}
	}
}

func (d *dnsEnv) activateManaged(project string) {
	t, ctx := d.t, context.Background()
	owner := d.user("owner@njiwa.co.tz", "owner")
	d.admOK(owner, "PUT", "/admin/v1/installation/wizard", map[string]any{"selected_mode": "managed_mta"})
	cert, key := pemPair(t, "njiwa-worker")
	ca, _ := pemPair(t, "ca")
	d.must(d.svc.RecordManagedInstall(ctx, service.CLIActor, service.ManagedInstall{ReceiptID: "r1", PlanDigest: "abc123", ApprovedBy: "cli", PostfixVersion: "3.8.6",
		SubmissionHost: "127.0.0.1", SubmissionPort: 10587, ServerName: "mailout.example.com", Selector: "dkim",
		DKIMRecords: map[string]string{domName: dkimRecord(t)}, ClientCertPEM: cert, ClientKeyPEM: key, CAPEM: ca}))
	var id string
	d.must(d.pool.QueryRow(ctx, `SELECT id::text FROM transport_profiles WHERE mode='managed_mta'`).Scan(&id))
	_, _, rev, err := d.svc.ProfileSMTP(ctx, id)
	d.must(err)
	d.must(d.svc.RecordInstallationCheck(ctx, id, rev, "connection", true, "", nil, false))
	_, err = d.svc.ActivateTransport(ctx, service.CLIActor, id, false)
	d.must(err)
}

// newDNSEnv builds a managed-delivery installation with one project, one
// sending domain and a Cloudflare zone, with no connection yet.
func newDNSEnv(t *testing.T) *dnsEnv { return newDNSEnvMode(t, true) }

// newDNSEnvMode builds a managed (true) or an existing-SMTP (false) installation.
func newDNSEnvMode(t *testing.T, managed bool) *dnsEnv {
	e := setup(t)
	mem := dnsprovider.NewMemory()
	d := &dnsEnv{env: e, mem: mem, project: "dnsp"}
	e.svc.Opts.OutboundIP = pubIP
	e.svc.Opts.NewDNSProvider = mem.Factory()
	e.svc.Opts.DNSSleep = func(_ context.Context, w time.Duration) error { d.sleeps = append(d.sleeps, w); return nil }
	e.svc.Opts.Port25Probe = func(context.Context) error { return nil }
	d.zone = mem.AddZone("zone1", zoneName).ID
	d.dns = nsDNS{
		fakeDNS: fakeDNS{txt: map[string][]string{}, mx: map[string][]string{}, ptr: map[string][]string{pubIP: {"mail.example.com."}},
			a: map[string][]string{"mail.example.com": {pubIP}}},
		ns: map[string][]string{zoneName: {"ada.ns.cloudflare.com", "bob.ns.cloudflare.com"}},
	}
	e.srv.Resolver = d.dns
	d.admin = e.user("admin@njiwa.co.tz", "admin")
	if managed {
		d.activateManaged(d.project)
	} else {
		e.saveSMTP("smtp.provider.example", "pw")
	}
	e.must(e.svc.CreateProject(context.Background(), d.project))
	r := d.ok("POST", "/domains", map[string]any{"name": domName, "purpose": "sending"})
	d.domainID = str(r.JSON, "id")
	return d
}

func (d *dnsEnv) connect() string {
	d.t.Helper()
	r := d.ok("POST", "/dns/connections", map[string]any{"provider": "cloudflare", "token": cfToken, "label": "main"})
	d.conn = str(r.JSON, "id")
	return d.conn
}

func (d *dnsEnv) start() resp {
	d.t.Helper()
	return d.call("POST", "/domains/"+d.domainID+"/dns-setup", map[string]any{"connection_id": d.conn, "zone_id": d.zone})
}

func (d *dnsEnv) startOK() map[string]any {
	d.t.Helper()
	if d.conn == "" {
		d.connect()
	}
	r := d.start()
	if r.Code != 200 {
		d.t.Fatalf("start: %d %s", r.Code, r.Raw)
	}
	return r.JSON
}

func planItems(v map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, it := range v["plan"].(map[string]any)["items"].([]any) {
		m := it.(map[string]any)
		out[m["key"].(string)] = m
	}
	return out
}

func (d *dnsEnv) approve(v map[string]any) resp {
	acks := v["plan"].(map[string]any)["required_acks"]
	return d.call("POST", "/dns-setups/"+str(v, "id")+"/approve", map[string]any{"digest": str(v, "digest"), "acknowledge": acks})
}

func (d *dnsEnv) applied() map[string]any {
	d.t.Helper()
	v := d.startOK()
	if r := d.approve(v); r.Code != 200 {
		d.t.Fatalf("approve: %d %s", r.Code, r.Raw)
	}
	r := d.call("POST", "/dns-setups/"+str(v, "id")+"/apply", nil)
	if r.Code != 200 || str(r.JSON, "status") != "applied" {
		d.t.Fatalf("apply: %d %s", r.Code, r.Raw)
	}
	return r.JSON
}

func contents(rs []dnsprovider.Record, typ string) []string {
	var out []string
	for _, r := range rs {
		if r.Type == typ {
			out = append(out, r.Name+" "+r.Content)
		}
	}
	return out
}

func (d *dnsEnv) noSecretAnywhere(token string) {
	d.t.Helper()
	for _, b := range d.bodies {
		if strings.Contains(b, token) {
			d.t.Fatalf("a response contained the provider token: %s", b)
		}
	}
	for _, q := range []string{
		`SELECT count(*) FROM audit_log WHERE details::text LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM dns_changes WHERE (previous::text || new::text || note) LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM dns_setups WHERE plan::text LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM dns_connections WHERE label LIKE '%' || $1 || '%' OR status_detail LIKE '%' || $1 || '%' OR encode(token_enc,'escape') LIKE '%' || $1 || '%'`,
	} {
		if n := d.count(q, token); n != 0 {
			d.t.Fatalf("the token is stored in the clear (%d rows): %s", n, q)
		}
	}
}

// ---------------------------------------------------------------- connections

func TestDNSConnectDisconnectAndCredentialStates(t *testing.T) {
	d := newDNSEnv(t)

	// An empty token is refused before any request. (The Cloudflare adapter also refuses
	// Global API Keys and malformed tokens: see dnsprovider tests.)
	if r := d.call("POST", "/dns/connections", map[string]any{"token": "  "}); r.Code != 400 {
		t.Fatalf("empty token accepted: %d %s", r.Code, r.Raw)
	}
	// A token the provider does not accept.
	d.mem.Token = "tok_the_only_good_one_0123456789"
	if r := d.call("POST", "/dns/connections", map[string]any{"token": cfToken}); r.Code != 409 || errCode(r) != "dns_credential_rejected" {
		t.Fatalf("rejected token: %d %s", r.Code, r.Raw)
	}
	d.mem.Token = ""
	// An expired token.
	d.mem.Expires = time.Now().Add(-time.Hour)
	if r := d.call("POST", "/dns/connections", map[string]any{"token": cfToken}); r.Code != 409 || errCode(r) != "dns_credential_rejected" {
		t.Fatalf("expired token: %d %s", r.Code, r.Raw)
	}
	d.mem.Expires = time.Time{}

	r := d.ok("POST", "/dns/connections", map[string]any{"token": cfToken, "label": "main"})
	id := str(r.JSON, "id")
	if r.JSON["status"] != "active" || r.JSON["zone_count"] != float64(1) || strings.Contains(r.Raw, cfToken) {
		t.Fatalf("connect: %s", r.Raw)
	}
	if n := d.count(`SELECT count(*) FROM dns_connections WHERE token_enc IS NOT NULL AND length(token_enc) > length($1)`, cfToken); n != 1 {
		t.Fatal("the token must be stored sealed")
	}
	if got := items(d.ok("GET", "/dns/connections", nil)); len(got) != 1 || strings.Contains(d.bodies[len(d.bodies)-1], "token_enc") {
		t.Fatalf("list: %v", got)
	}
	zs := items(d.ok("GET", "/dns/connections/"+id+"/zones", nil))
	if len(zs) != 1 || zs[0]["name"] != zoneName {
		t.Fatalf("zones: %v", zs)
	}

	// Revoked at the provider while in use: reported as such, connection marked, nothing leaks.
	d.conn = id
	d.mem.Revoked = true
	if r := d.start(); r.Code != 409 || errCode(r) != "dns_credential_rejected" {
		t.Fatalf("revoked: %d %s", r.Code, r.Raw)
	}
	if n := d.count(`SELECT count(*) FROM dns_connections WHERE id=$1 AND status='error'`, id); n != 1 {
		t.Fatal("a rejected credential must mark the connection")
	}
	if r := d.call("GET", "/dns/connections/"+id+"/zones", nil); r.Code != 409 || errCode(r) != "dns_connection_inactive" {
		t.Fatalf("inactive connection used: %d %s", r.Code, r.Raw)
	}
	d.mem.Revoked = false

	// Rotation validates the new token, then reactivates.
	d.mem.Token = "tok_the_new_token_0123456789abc"
	if r := d.call("PUT", "/dns/connections/"+id+"/token", map[string]any{"token": cfToken}); r.Code != 409 {
		t.Fatalf("rotation to a bad token: %d %s", r.Code, r.Raw)
	}
	rot := d.ok("PUT", "/dns/connections/"+id+"/token", map[string]any{"token": d.mem.Token})
	if rot.JSON["status"] != "active" || rot.JSON["rotated_at"] == nil {
		t.Fatalf("rotate: %s", rot.Raw)
	}
	d.mem.Token = ""

	// Disconnect wipes the credential and deletes no DNS record.
	d.mem.Seed(d.zone, dnsprovider.Record{Type: "TXT", Name: domName, Content: "keep-me"})
	before := len(d.mem.Snapshot(d.zone))
	d.ok("DELETE", "/dns/connections/"+id, nil)
	if d.count(`SELECT count(*) FROM dns_connections WHERE id=$1 AND status='disconnected' AND token_enc IS NULL AND refresh_enc IS NULL`, id) != 1 {
		t.Fatal("credential not wiped")
	}
	if len(d.mem.Snapshot(d.zone)) != before || d.mem.Count("delete") != 0 {
		t.Fatal("disconnecting touched DNS")
	}
	if r := d.call("GET", "/dns/connections/"+id+"/zones", nil); r.Code != 409 {
		t.Fatalf("disconnected connection still usable: %d", r.Code)
	}
	if got := items(d.ok("GET", "/dns/connections", nil)); len(got) != 0 {
		t.Fatalf("a disconnected connection is still listed: %v", got)
	}
	for _, a := range []string{"dns.connect", "dns.rotate", "dns.disconnect"} {
		if d.count(`SELECT count(*) FROM audit_log WHERE action=$1`, a) == 0 {
			t.Errorf("missing audit %s", a)
		}
	}
	d.noSecretAnywhere(cfToken)
}

func TestDNSConnectionRateLimit(t *testing.T) {
	d := newDNSEnv(t)
	var last resp
	for i := 0; i < 21; i++ {
		last = d.call("POST", "/dns/connections", map[string]any{"token": "tok_0123456789abcdefghijklmnopq" + string(rune('a'+i%26))})
		if last.Code == 429 {
			break
		}
		if i == 9 { // keep under the ten-connection cap
			for _, c := range items(d.ok("GET", "/dns/connections", nil)) {
				d.ok("DELETE", "/dns/connections/"+str(c, "id"), nil)
			}
		}
	}
	if last.Code != 429 || last.Hdr.Get("Retry-After") == "" {
		t.Fatalf("connect attempts are not limited: %d %s", last.Code, last.Raw)
	}
}

// ---------------------------------------------------------------- the journey

func TestDNSOneClickManagedJourney(t *testing.T) {
	d := newDNSEnv(t)
	// Existing website records in the zone must come through untouched.
	d.mem.Seed(d.zone, dnsprovider.Record{Type: "A", Name: zoneName, Content: "198.51.100.20", Proxied: true})
	d.mem.Seed(d.zone, dnsprovider.Record{Type: "CNAME", Name: "www.example.com", Content: "example.com", Proxied: true})
	website := d.mem.Snapshot(d.zone)

	v := d.startOK()
	if v["status"] != "analyzed" {
		t.Fatalf("status %v: %v", v["status"], v["plan"])
	}
	it := planItems(v)
	for _, k := range []string{"ownership", "spf", "dkim", "dmarc", "return_path"} {
		if it[k]["action"] != "create" {
			t.Fatalf("%s: %v", k, it[k])
		}
	}
	if it["spf"]["content"] != "v=spf1 mx ip4:"+pubIP+" -all" {
		t.Fatalf("SPF is built from the real sender: %v", it["spf"]["content"])
	}
	if it["dkim"]["content"] != dkimRecord(t) {
		t.Fatal("DKIM must be the signer's real key")
	}
	if !strings.Contains(v["plan"].(map[string]any)["rollback"].(string), "Rolling back removes only records Njiwa-Mail created") {
		t.Fatal("the rollback explanation must be part of the plan")
	}
	if d.mem.Count("create")+d.mem.Count("update")+d.mem.Count("delete") != 0 {
		t.Fatal("analysis wrote to DNS")
	}

	// Approval needs the digest of the plan that was reviewed and the confirmation of the MX.
	id := str(v, "id")
	if r := d.call("POST", "/dns-setups/"+id+"/approve", map[string]any{"digest": "not-the-digest"}); r.Code != 409 || errCode(r) != "plan_changed" {
		t.Fatalf("wrong digest: %d %s", r.Code, r.Raw)
	}
	if r := d.call("POST", "/dns-setups/"+id+"/approve", map[string]any{"digest": str(v, "digest")}); r.Code != 400 || errCode(r) != "confirmation_required" {
		t.Fatalf("MX confirmation: %d %s", r.Code, r.Raw)
	}
	if r := d.call("POST", "/dns-setups/"+id+"/apply", nil); r.Code != 409 || errCode(r) != "setup_not_approved" {
		t.Fatalf("applied without approval: %d %s", r.Code, r.Raw)
	}
	if r := d.approve(v); r.Code != 200 || r.JSON["status"] != "approved" || r.JSON["approved_by"] != "admin@njiwa.co.tz" {
		t.Fatalf("approve: %d %s", r.Code, r.Raw)
	}

	r := d.ok("POST", "/dns-setups/"+id+"/apply", nil)
	if r.JSON["status"] != "applied" {
		t.Fatalf("apply: %s", r.Raw)
	}
	recs := d.mem.Snapshot(d.zone)
	for _, want := range []string{domName + " njiwa-mail-verify=", domName + " v=spf1 mx ip4:" + pubIP + " -all", "_dmarc." + domName + " v=DMARC1; p=none"} {
		found := false
		for _, c := range contents(recs, "TXT") {
			found = found || strings.HasPrefix(c, want)
		}
		if !found {
			t.Errorf("missing TXT %q in %v", want, contents(recs, "TXT"))
		}
	}
	if mx := contents(recs, "MX"); len(mx) != 1 || mx[0] != domName+" mail.example.com" {
		t.Fatalf("MX: %v", mx)
	}
	for _, rec := range recs {
		if (rec.Type == "TXT" || rec.Type == "MX") && rec.Proxied {
			t.Errorf("mail record proxied: %+v", rec)
		}
	}
	// The website records are exactly as they were.
	for _, w := range website {
		found := false
		for _, rec := range recs {
			found = found || (rec.ID == w.ID && rec.Content == w.Content && rec.Proxied == w.Proxied && rec.Modified == w.Modified)
		}
		if !found {
			t.Fatalf("a website record changed: %+v", w)
		}
	}
	// Ledger and audit: every change, with its actor and previous/new values.
	if n := d.count(`SELECT count(*) FROM dns_changes WHERE setup_id=$1 AND status='applied' AND actor='admin@njiwa.co.tz' AND record_id <> ''`, id); n != 5 {
		t.Fatalf("ledger rows: %d", n)
	}
	if n := d.count(`SELECT count(*) FROM audit_log WHERE action='dns.record_create' AND details->>'domain'=$1 AND details->>'record_id' <> ''`, domName); n != 5 {
		t.Fatalf("record audit rows: %d", n)
	}

	// Cloudflare accepted the records, but DNS has not propagated: not ready, and it says why.
	ver := d.ok("POST", "/dns-setups/"+id+"/verify", nil).JSON
	if ver["ready"] == true || ver["state"] != "pending" {
		t.Fatalf("an API success must not be readiness: %v", ver)
	}
	states := map[string]string{}
	for _, x := range ver["items"].([]any) {
		m := x.(map[string]any)
		states[m["key"].(string)] = m["state"].(string)
	}
	for _, k := range []string{"ownership", "spf", "dkim", "dmarc"} {
		if states[k] != "pending" {
			t.Errorf("%s should be pending before propagation: %v", k, states)
		}
	}
	if d.count(`SELECT count(*) FROM domains WHERE id=$1 AND verified_at IS NOT NULL`, d.domainID) != 0 {
		t.Fatal("the domain became verified without DNS answering")
	}

	// Propagation completes.
	d.publish()
	ver = d.ok("POST", "/dns-setups/"+id+"/verify", nil).JSON
	if ver["ready"] != true || ver["state"] != "verified" || !strings.Contains(ver["message"].(string), "does not guarantee inbox delivery") {
		t.Fatalf("verified: %v", ver)
	}
	for _, x := range ver["items"].([]any) {
		m := x.(map[string]any)
		if m["state"] != "verified" {
			t.Errorf("%v", m)
		}
	}
	for _, k := range []string{"public_ip", "ptr", "port25"} {
		if !strings.Contains(d.bodies[len(d.bodies)-1], `"key":"`+k+`"`) {
			t.Errorf("managed delivery must also check %s", k)
		}
	}
	// The domain now satisfies the same gate Live sending uses.
	if d.count(`SELECT count(*) FROM domains WHERE id=$1 AND verified_at IS NOT NULL AND spf_ok AND dkim_ok AND dmarc_ok`, d.domainID) != 1 {
		t.Fatal("domain flags not set after real verification")
	}
	// The wizard can resume from the saved state.
	if got := d.ok("GET", "/domains/"+d.domainID+"/dns-setup", nil).JSON["setup"].(map[string]any); got["id"] != id || got["status"] != "applied" {
		t.Fatalf("resume: %v", got)
	}
	d.noSecretAnywhere(cfToken)
}

func TestDNSManagedVerificationFlagsPTRAndPort25(t *testing.T) {
	d := newDNSEnv(t)
	v := d.applied()
	d.publish()
	delete(d.dns.ptr, pubIP)
	d.svc.Opts.Port25Probe = func(context.Context) error { return &net.OpError{Op: "dial", Err: context.DeadlineExceeded} }
	ver := d.ok("POST", "/dns-setups/"+str(v, "id")+"/verify", nil).JSON
	states := map[string]string{}
	for _, x := range ver["items"].([]any) {
		m := x.(map[string]any)
		states[m["key"].(string)] = m["state"].(string)
	}
	if ver["ready"] == true || states["ptr"] != "needs_action" || states["port25"] != "needs_action" {
		t.Fatalf("a missing PTR and a blocked port 25 must stop readiness: %v %v", ver["ready"], states)
	}
	d.dns.ptr[pubIP] = []string{"other.example.net."}
	ver = d.ok("POST", "/dns-setups/"+str(v, "id")+"/verify", nil).JSON
	for _, x := range ver["items"].([]any) {
		if m := x.(map[string]any); m["key"] == "ptr" && m["state"] != "failed" {
			t.Fatalf("a PTR naming another host is wrong, not missing: %v", m)
		}
	}
}

func TestDNSPendingBecomesNeedsActionAfterTheWindow(t *testing.T) {
	d := newDNSEnv(t)
	v := d.applied()
	d.must2(d.pool.Exec(context.Background(), `UPDATE dns_changes SET applied_at = now() - interval '2 days'`))
	ver := d.ok("POST", "/dns-setups/"+str(v, "id")+"/verify", nil).JSON
	if ver["state"] == "pending" {
		t.Fatalf("a record that never appeared must not stay pending forever: %v", ver)
	}
}

func (e *env) must2(_ any, err error) {
	e.t.Helper()
	e.must(err)
}

// ---------------------------------------------------------------- existing DNS

func TestDNSExistingMailAndSPFArePreserved(t *testing.T) {
	d := newDNSEnv(t)
	d.mem.Seed(d.zone, dnsprovider.Record{Type: "MX", Name: domName, Content: "aspmx.l.google.com", Priority: 1, TTL: 300})
	spf := d.mem.Seed(d.zone, dnsprovider.Record{Type: "TXT", Name: domName, Content: "v=spf1 include:_spf.google.com ~all", TTL: 777})
	d.mem.Seed(d.zone, dnsprovider.Record{Type: "A", Name: domName, Content: "198.51.100.9", Proxied: true})
	before := d.mem.Snapshot(d.zone)

	v := d.startOK()
	it := planItems(v)
	if it["spf"]["action"] != "modify" || it["spf"]["risk"] != "high" || it["spf"]["content"] != "v=spf1 include:_spf.google.com mx ip4:"+pubIP+" ~all" {
		t.Fatalf("SPF: %v", it["spf"])
	}
	if it["return_path"]["action"] != "conflict" {
		t.Fatalf("the MX must be a conflict: %v", it["return_path"])
	}
	plan := v["plan"].(map[string]any)
	if !strings.Contains(string(mustJSON(plan["warnings"])), "existing_mail") || !strings.Contains(string(mustJSON(plan["existing_mail"])), "Google Workspace") {
		t.Fatalf("existing mail not reported: %v", plan)
	}
	// The SPF change needs its own explicit confirmation.
	if r := d.call("POST", "/dns-setups/"+str(v, "id")+"/approve", map[string]any{"digest": str(v, "digest"), "acknowledge": []string{}}); r.Code != 400 {
		t.Fatalf("modification approved without confirmation: %d", r.Code)
	}
	if r := d.approve(v); r.Code != 200 {
		t.Fatalf("approve: %s", r.Raw)
	}
	r := d.ok("POST", "/dns-setups/"+str(v, "id")+"/apply", nil)
	if r.JSON["status"] != "applied" {
		t.Fatalf("apply: %s", r.Raw)
	}
	after := d.mem.Snapshot(d.zone)
	for _, rec := range after {
		switch {
		case rec.ID == spf.ID:
			if rec.Content != "v=spf1 include:_spf.google.com mx ip4:"+pubIP+" ~all" || rec.TTL != 777 {
				t.Fatalf("SPF edit: %+v", rec)
			}
		case rec.Type == "MX":
			if rec.Content != "aspmx.l.google.com" {
				t.Fatalf("the existing MX was replaced: %+v", rec)
			}
		}
	}
	if n := len(contents(after, "MX")); n != 1 {
		t.Fatalf("MX count %d", n)
	}
	spfCount := 0
	for _, c := range contents(after, "TXT") {
		if strings.Contains(c, "v=spf1") {
			spfCount++
		}
	}
	if spfCount != 1 {
		t.Fatalf("%d SPF records after apply", spfCount)
	}
	for _, b := range before {
		if b.Type == "A" {
			for _, a := range after {
				if a.ID == b.ID && (a.Content != b.Content || a.Proxied != b.Proxied) {
					t.Fatal("the website record changed")
				}
			}
		}
	}
	// Rollback puts the SPF record back exactly.
	rb := d.ok("POST", "/dns-setups/"+str(v, "id")+"/rollback", nil)
	if rb.JSON["status"] != "rolled_back" {
		t.Fatalf("rollback: %s", rb.Raw)
	}
	for _, rec := range d.mem.Snapshot(d.zone) {
		if rec.ID == spf.ID && rec.Content != "v=spf1 include:_spf.google.com ~all" {
			t.Fatalf("SPF not restored: %+v", rec)
		}
	}
	if got := d.mem.Snapshot(d.zone); len(got) != len(before) {
		t.Fatalf("after rollback %d records, before %d", len(got), len(before))
	}
}

func TestDNSBlockedWhenZoneIsNotDelegatedToCloudflare(t *testing.T) {
	d := newDNSEnv(t)
	d.dns.ns[zoneName] = []string{"ns1.oldhost.example", "ns2.oldhost.example"}
	v := d.startOK()
	if v["status"] != "blocked" {
		t.Fatalf("status %v", v["status"])
	}
	if r := d.call("POST", "/dns-setups/"+str(v, "id")+"/approve", map[string]any{"digest": str(v, "digest")}); r.Code != 409 || errCode(r) != "setup_blocked" {
		t.Fatalf("approved a blocked plan: %d %s", r.Code, r.Raw)
	}
	// Fixing the delegation and analyzing again unblocks it, in the same setup.
	d.dns.ns[zoneName] = []string{"ada.ns.cloudflare.com"}
	again := d.ok("POST", "/dns-setups/"+str(v, "id")+"/analyze", nil).JSON
	if again["status"] != "analyzed" || again["id"] != v["id"] {
		t.Fatalf("re-analysis: %v", again["status"])
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

// ---------------------------------------------------------------- safety under change

func TestDNSChangedAfterApprovalIsRefusedAndNothingIsWritten(t *testing.T) {
	d := newDNSEnv(t)
	v := d.startOK()
	if r := d.approve(v); r.Code != 200 {
		t.Fatal(r.Raw)
	}
	// Another administrator edits DNS at a name the plan touches.
	d.mem.Seed(d.zone, dnsprovider.Record{Type: "TXT", Name: domName, Content: "v=spf1 ip4:198.51.100.1 -all"})
	r := d.call("POST", "/dns-setups/"+str(v, "id")+"/apply", nil)
	if r.Code != 409 || errCode(r) != "dns_changed_concurrently" {
		t.Fatalf("apply: %d %s", r.Code, r.Raw)
	}
	if d.mem.Count("create") != 0 || d.mem.Count("update") != 0 || d.count(`SELECT count(*) FROM dns_changes`) != 0 {
		t.Fatal("something was written although DNS had changed")
	}
	got := d.ok("GET", "/dns-setups/"+str(v, "id"), nil).JSON
	if got["status"] != "needs_review" || got["approved_at"] != nil {
		t.Fatalf("status %v approved_at %v", got["status"], got["approved_at"])
	}
	// The refreshed plan shows the new SPF situation and needs a new approval.
	if planItems(got)["spf"]["action"] != "modify" {
		t.Fatalf("plan not refreshed: %v", planItems(got)["spf"])
	}
	if r := d.call("POST", "/dns-setups/"+str(v, "id")+"/apply", nil); r.Code != 409 || errCode(r) != "setup_not_approved" {
		t.Fatalf("applied a plan that needs review: %d %s", r.Code, r.Raw)
	}
}

func TestDNSApprovalExpires(t *testing.T) {
	d := newDNSEnv(t)
	v := d.startOK()
	if r := d.approve(v); r.Code != 200 {
		t.Fatal(r.Raw)
	}
	d.must2(d.pool.Exec(context.Background(), `UPDATE dns_setups SET approved_at = now() - interval '31 minutes'`))
	if r := d.call("POST", "/dns-setups/"+str(v, "id")+"/apply", nil); r.Code != 409 || errCode(r) != "approval_expired" {
		t.Fatalf("expired approval applied: %d %s", r.Code, r.Raw)
	}
	if d.mem.Count("create") != 0 {
		t.Fatal("wrote with an expired approval")
	}
}

func TestDNSStaleDigestCannotBeApprovedAfterANewAnalysis(t *testing.T) {
	d := newDNSEnv(t)
	v := d.startOK()
	d.mem.Seed(d.zone, dnsprovider.Record{Type: "TXT", Name: "_dmarc." + domName, Content: "v=DMARC1; p=reject"})
	fresh := d.ok("POST", "/dns-setups/"+str(v, "id")+"/analyze", nil).JSON
	if fresh["digest"] == v["digest"] {
		t.Fatal("the digest must change when the zone changes")
	}
	if r := d.call("POST", "/dns-setups/"+str(v, "id")+"/approve", map[string]any{"digest": str(v, "digest"), "acknowledge": v["plan"].(map[string]any)["required_acks"]}); r.Code != 409 || errCode(r) != "plan_changed" {
		t.Fatalf("stale digest: %d %s", r.Code, r.Raw)
	}
	if planItems(fresh)["dmarc"]["action"] != "unchanged" {
		t.Fatalf("an existing DMARC policy must be left alone: %v", planItems(fresh)["dmarc"])
	}
}

func TestDNSTwoAppliesCannotRunTogether(t *testing.T) {
	d := newDNSEnv(t)
	v := d.startOK()
	if r := d.approve(v); r.Code != 200 {
		t.Fatal(r.Raw)
	}
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = d.call("POST", "/dns-setups/"+str(v, "id")+"/apply", nil).Code
		}(i)
	}
	wg.Wait()
	if !((codes[0] == 200 && codes[1] == 409) || (codes[0] == 409 && codes[1] == 200)) {
		t.Fatalf("codes %v", codes)
	}
	seen := map[string]int{}
	for _, r := range d.mem.Snapshot(d.zone) {
		seen[r.Type+r.Name+r.Content]++
	}
	for k, n := range seen {
		if n != 1 {
			t.Fatalf("duplicate record %s x%d", k, n)
		}
	}
}

// ---------------------------------------------------------------- failures and recovery

func TestDNSRateLimitedCreateIsRetriedAfterWaiting(t *testing.T) {
	d := newDNSEnv(t)
	hits := 0
	d.mem.FailOn = func(op string, r dnsprovider.Record) error {
		if op == "create" && r.Type == "TXT" && strings.HasPrefix(r.Content, "njiwa-mail-verify=") && hits < 2 {
			hits++
			return &dnsprovider.RateLimitError{RetryAfter: 3 * time.Second}
		}
		return nil
	}
	d.applied()
	if hits != 2 || len(d.sleeps) < 2 || d.sleeps[0] != 3*time.Second {
		t.Fatalf("hits=%d sleeps=%v", hits, d.sleeps)
	}
	own := 0
	for _, c := range contents(d.mem.Snapshot(d.zone), "TXT") {
		if strings.Contains(c, "njiwa-mail-verify=") {
			own++
		}
	}
	if own != 1 {
		t.Fatalf("%d ownership records", own)
	}
}

func TestDNSLostResponseDoesNotCreateADuplicate(t *testing.T) {
	d := newDNSEnv(t)
	lost := false
	d.mem.AfterCreate = func(r dnsprovider.Record) error {
		if !lost && r.Type == "TXT" && strings.HasPrefix(r.Content, "v=spf1") {
			lost = true
			return &dnsprovider.TransientError{Err: context.DeadlineExceeded} // created, but the answer never arrived
		}
		return nil
	}
	d.applied()
	n := 0
	for _, c := range contents(d.mem.Snapshot(d.zone), "TXT") {
		if strings.Contains(c, "v=spf1") {
			n++
		}
	}
	if !lost || n != 1 {
		t.Fatalf("lost=%v, %d SPF records: a retry after an ambiguous failure must look before creating", lost, n)
	}
}

func TestDNSPartialFailureThenRecovery(t *testing.T) {
	d := newDNSEnv(t)
	d.mem.FailOn = func(op string, r dnsprovider.Record) error {
		if op == "create" && r.Type == "MX" {
			return &dnsprovider.TransientError{Err: context.DeadlineExceeded}
		}
		return nil
	}
	v := d.startOK()
	if r := d.approve(v); r.Code != 200 {
		t.Fatal(r.Raw)
	}
	r := d.call("POST", "/dns-setups/"+str(v, "id")+"/apply", nil)
	if r.Code != 503 || errCode(r) != "dns_provider_unavailable" {
		t.Fatalf("apply: %d %s", r.Code, r.Raw)
	}
	got := d.ok("GET", "/dns-setups/"+str(v, "id"), nil).JSON
	if got["status"] != "partial" {
		t.Fatalf("status %v: a partly applied plan must never read as applied", got["status"])
	}
	ok, bad := 0, 0
	for _, c := range got["changes"].([]any) {
		switch c.(map[string]any)["status"] {
		case "applied":
			ok++
		case "failed":
			bad++
		}
	}
	if ok != 4 || bad != 1 {
		t.Fatalf("changes: %d applied, %d failed", ok, bad)
	}
	if calls := d.mem.Count("create"); calls > 4+4 { // four successes plus at most four attempts at the failing record
		t.Fatalf("unbounded retries: %d create calls", calls)
	}
	if ver := d.ok("POST", "/dns-setups/"+str(v, "id")+"/verify", nil).JSON; ver["ready"] == true {
		t.Fatal("a partial setup reported ready")
	}
	// Recovery: the provider is back; analyze again, only the MX remains.
	d.mem.FailOn = nil
	again := d.ok("POST", "/dns-setups/"+str(v, "id")+"/analyze", nil).JSON
	items := planItems(again)
	creates := 0
	for k, it := range items {
		if it["action"] == "create" {
			creates++
			if k != "return_path" {
				t.Errorf("%s would be created again", k)
			}
		}
	}
	if creates != 1 {
		t.Fatalf("%d creates remain", creates)
	}
	if r := d.approve(again); r.Code != 200 {
		t.Fatal(r.Raw)
	}
	fin := d.ok("POST", "/dns-setups/"+str(again, "id")+"/apply", nil)
	if fin.JSON["status"] != "applied" {
		t.Fatalf("recovery: %s", fin.Raw)
	}
	if n := len(contents(d.mem.Snapshot(d.zone), "TXT")); n != 4 {
		t.Fatalf("TXT records: %d", n)
	}
}

func TestDNSAdoptedRecordsAreNeverRolledBack(t *testing.T) {
	d := newDNSEnv(t)
	// Another administrator creates the same ownership record a moment before we do.
	armed := true
	d.mem.BeforeCreate = func(add func(dnsprovider.Record) dnsprovider.Record, r dnsprovider.Record) {
		if armed && r.Type == "TXT" && strings.HasPrefix(r.Content, "njiwa-mail-verify=") {
			armed = false
			add(r)
		}
	}
	v := d.applied()
	adopted := d.count(`SELECT count(*) FROM dns_changes WHERE setup_id=$1 AND adopted`, str(v, "id"))
	if adopted != 1 {
		t.Fatalf("adopted %d", adopted)
	}
	rb := d.ok("POST", "/dns-setups/"+str(v, "id")+"/rollback", nil)
	if rb.JSON["status"] != "rolled_back" {
		t.Fatalf("rollback: %s", rb.Raw)
	}
	left := 0
	for _, c := range contents(d.mem.Snapshot(d.zone), "TXT") {
		if strings.Contains(c, "njiwa-mail-verify=") {
			left++
		}
	}
	if left != 1 {
		t.Fatal("a record Njiwa-Mail did not create was removed on rollback")
	}
	if len(d.mem.Snapshot(d.zone)) != 1 {
		t.Fatalf("records left: %v", d.mem.Snapshot(d.zone))
	}
}

func TestDNSRollbackNeverOverwritesLegitimateChanges(t *testing.T) {
	d := newDNSEnv(t)
	v := d.applied()
	snap := d.mem.Snapshot(d.zone)
	var dmarc, own, spf dnsprovider.Record
	for _, r := range snap {
		switch {
		case strings.HasPrefix(r.Name, "_dmarc."):
			dmarc = r
		case strings.HasPrefix(r.Content, "njiwa-mail-verify="):
			own = r
		case strings.HasPrefix(r.Content, "v=spf1"):
			spf = r
		}
	}
	// Another administrator tightens DMARC, removes the ownership record, and leaves the rest.
	d.mem.Edit(d.zone, dmarc.ID, "v=DMARC1; p=reject; rua=mailto:sec@example.com")
	d.mem.Remove(d.zone, own.ID)

	rb := d.ok("POST", "/dns-setups/"+str(v, "id")+"/rollback", nil)
	if rb.JSON["status"] != "rollback_partial" {
		t.Fatalf("status %v", rb.JSON["status"])
	}
	notes := map[string]string{}
	for _, c := range rb.JSON["changes"].([]any) {
		m := c.(map[string]any)
		notes[m["key"].(string)] = m["status"].(string) + "|" + m["note"].(string)
	}
	if !strings.HasPrefix(notes["dmarc"], "rollback_skipped|changed since") || !strings.HasPrefix(notes["ownership"], "rollback_skipped|already removed") {
		t.Fatalf("notes: %v", notes)
	}
	left := d.mem.Snapshot(d.zone)
	hasDMARC := false
	for _, r := range left {
		if r.ID == dmarc.ID {
			hasDMARC = r.Content == "v=DMARC1; p=reject; rua=mailto:sec@example.com"
		}
		if r.ID == spf.ID || r.Type == "MX" || strings.HasPrefix(r.Content, "v=DKIM1") {
			t.Fatalf("an unchanged record was left behind: %+v", r)
		}
	}
	if !hasDMARC {
		t.Fatal("the newer DMARC policy was overwritten or removed")
	}
	// Rolling back does not need the connection when records must be removed by hand.
	d.ok("DELETE", "/dns/connections/"+d.conn, nil)
	if r := d.call("POST", "/dns-setups/"+str(v, "id")+"/rollback", nil); r.Code != 409 || errCode(r) != "dns_connection_inactive" {
		t.Fatalf("rollback without a connection: %d %s", r.Code, r.Raw)
	}
}

// ---------------------------------------------------------------- isolation

func TestDNSCrossProjectAndCrossZoneAccess(t *testing.T) {
	d := newDNSEnv(t)
	d.connect()
	v := d.startOK()
	e := d.env
	e.must(e.svc.CreateProject(context.Background(), "other"))
	other := wsPath("other", "")
	od := e.admOK(d.admin, "POST", other+"/domains", map[string]any{"name": "mail.other.org", "purpose": "sending"})
	odID := str(od.JSON, "id")

	for _, c := range []struct{ method, path string }{
		{"GET", "/dns/connections/" + d.conn + "/zones"},
		{"DELETE", "/dns/connections/" + d.conn},
		{"GET", "/dns-setups/" + str(v, "id")},
		{"POST", "/dns-setups/" + str(v, "id") + "/approve"},
		{"POST", "/dns-setups/" + str(v, "id") + "/apply"},
		{"POST", "/dns-setups/" + str(v, "id") + "/rollback"},
		{"POST", "/dns-setups/" + str(v, "id") + "/verify"},
		{"POST", "/dns-setups/" + str(v, "id") + "/analyze"},
	} {
		if r := e.adm(d.admin, c.method, other+c.path, map[string]any{"digest": "x"}); r.Code != 404 {
			t.Errorf("%s %s from another project: %d %s", c.method, c.path, r.Code, r.Raw)
		}
	}
	// Another project's connection cannot be used for this project's domain.
	if r := e.adm(d.admin, "POST", other+"/domains/"+odID+"/dns-setup", map[string]any{"connection_id": d.conn, "zone_id": d.zone}); r.Code != 404 {
		t.Fatalf("foreign connection used: %d %s", r.Code, r.Raw)
	}
	if got := items(e.admOK(d.admin, "GET", other+"/dns/connections", nil)); len(got) != 0 {
		t.Fatalf("connections leak across projects: %v", got)
	}
	// A zone the credential cannot see.
	if r := d.call("POST", "/domains/"+d.domainID+"/dns-setup", map[string]any{"connection_id": d.conn, "zone_id": "zoneNOTMINE"}); r.Code != 403 || errCode(r) != "dns_zone_forbidden" {
		t.Fatalf("foreign zone: %d %s", r.Code, r.Raw)
	}
	// A domain another project already claimed cannot be set up.
	e.ownDomain("other", domName)
	if r := d.start(); r.Code != 403 || errCode(r) != "domain_claimed_elsewhere" {
		t.Fatalf("claimed domain: %d %s", r.Code, r.Raw)
	}
	// A domain outside the zone is refused by the plan, never written.
	dd := d.ok("POST", "/domains", map[string]any{"name": "mail.unrelated.org", "purpose": "sending"})
	r := d.call("POST", "/domains/"+str(dd.JSON, "id")+"/dns-setup", map[string]any{"connection_id": d.conn, "zone_id": d.zone})
	if r.Code != 200 || r.JSON["status"] != "blocked" {
		t.Fatalf("domain outside the zone: %d %s", r.Code, r.Raw)
	}
}

func TestDNSAnalysisIsRateLimitedPerDomain(t *testing.T) {
	d := newDNSEnv(t)
	d.connect()
	var last resp
	for i := 0; i < 45; i++ {
		last = d.start()
		if last.Code == 429 {
			break
		}
	}
	if last.Code != 429 || last.Hdr.Get("Retry-After") == "" {
		t.Fatalf("not limited: %d", last.Code)
	}
}

// ---------------------------------------------------------------- manual fallback

func TestDNSManualFallbackNeedsNoProvider(t *testing.T) {
	d := newDNSEnv(t)
	r := d.ok("GET", "/domains/"+d.domainID+"/dns-manual", nil)
	if d.mem.Count("zones")+d.mem.Count("records")+d.mem.Count("create") != 0 {
		t.Fatal("the manual fallback called a DNS provider")
	}
	byKey := map[string]map[string]any{}
	for _, x := range r.JSON["records"].([]any) {
		m := x.(map[string]any)
		byKey[m["key"].(string)] = m
	}
	if byKey["spf"]["value"] != "v=spf1 mx ip4:"+pubIP+" -all" || byKey["dkim"]["value"] != dkimRecord(t) || byKey["dkim"]["placeholder"] == true {
		t.Fatalf("managed records: %v", byKey)
	}
	if byKey["return_path"]["type"] != "MX" || byKey["ownership"]["host"] != domName {
		t.Fatalf("records: %v", byKey)
	}
}

// An existing SMTP server: Njiwa-Mail cannot see its sending IPs or signing key,
// so manual records say so instead of inventing values; once the operator declares
// them, the automatic plan uses exactly those.
func TestDNSExistingSMTPModeNeverInventsSenderValues(t *testing.T) {
	d := newDNSEnvMode(t, false)
	g := d.ok("GET", "/domains/"+d.domainID+"/dns-manual", nil)
	for _, x := range g.JSON["records"].([]any) {
		m := x.(map[string]any)
		if (m["key"] == "spf" || m["key"] == "dkim") && m["placeholder"] != true {
			t.Fatalf("a value Njiwa-Mail cannot know was invented: %v", m)
		}
	}
	plan := planItems(d.startOK())
	for _, k := range []string{"spf", "dkim"} {
		if plan[k]["action"] != "manual" || plan[k]["placeholder"] != true {
			t.Fatalf("%s must be a manual item, not written: %v", k, plan[k])
		}
	}
	if plan["ownership"]["action"] != "create" {
		t.Fatalf("ownership: %v", plan["ownership"])
	}
	pid := str(d.activeProfile(), "id")
	d.admOK(d.admin, "PUT", "/admin/v1/installation/transport-profiles/"+pid+"/sending-facts", map[string]any{
		"egress_ips": []string{"198.51.100.7"}, "spf_include": "spf.provider.example", "dkim_selector": "s1", "dkim_keys": map[string]string{domName: dkimRecord(t)}})
	v := d.startOK()
	plan = planItems(v)
	if plan["spf"]["action"] != "create" || plan["spf"]["content"] != "v=spf1 include:spf.provider.example ip4:198.51.100.7 -all" {
		t.Fatalf("existing-SMTP SPF: %v", plan["spf"])
	}
	if plan["dkim"]["name"] != "s1._domainkey."+domName || plan["dkim"]["content"] != dkimRecord(t) {
		t.Fatalf("selector and key: %v", plan["dkim"])
	}
	// The MX and sending-address checks belong to managed delivery only.
	ver := d.ok("POST", "/dns-setups/"+str(v, "id")+"/verify", nil)
	for _, k := range []string{"ptr", "port25", "public_ip"} {
		if strings.Contains(ver.Raw, `"key":"`+k+`"`) {
			t.Fatalf("%s is a managed-delivery check", k)
		}
	}
}

// ---------------------------------------------------------------- OAuth

type oauthStub struct {
	srv       *httptest.Server
	mu        sync.Mutex
	challenge string
	redirect  string
	grants    []string
	revoked   []string
	refreshOK bool
	access    string
}

func newOAuthStub(t *testing.T) *oauthStub {
	s := &oauthStub{refreshOK: true, access: "oauth_access_0123456789abcdefghijk"}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		s.mu.Lock()
		defer s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			s.grants = append(s.grants, r.Form.Get("grant_type"))
			if r.Form.Get("client_id") != "cid" || r.Form.Get("client_secret") != "csecret" {
				w.WriteHeader(401)
				return
			}
			switch r.Form.Get("grant_type") {
			case "authorization_code":
				sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
				if base64.RawURLEncoding.EncodeToString(sum[:]) != s.challenge || r.Form.Get("redirect_uri") != s.redirect || r.Form.Get("code") != "good-code" {
					w.WriteHeader(400)
					_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
					return
				}
				_, _ = w.Write([]byte(`{"access_token":"` + s.access + `","refresh_token":"refresh_0123456789abcdef","token_type":"Bearer","expires_in":3600}`))
			case "refresh_token":
				if !s.refreshOK || r.Form.Get("refresh_token") != "refresh_0123456789abcdef" {
					w.WriteHeader(400)
					return
				}
				_, _ = w.Write([]byte(`{"access_token":"oauth_access_refreshed_0123456789","token_type":"Bearer","expires_in":3600}`))
			}
		case "/revoke":
			s.revoked = append(s.revoked, r.Form.Get("token"))
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func TestDNSOAuthFlow(t *testing.T) {
	d := newDNSEnv(t)
	st := newOAuthStub(t)
	if r := d.call("POST", "/dns/oauth/start", map[string]any{}); r.Code != 409 || errCode(r) != "oauth_not_configured" {
		t.Fatalf("unconfigured: %d %s", r.Code, r.Raw)
	}
	if got := d.ok("GET", "/dns/capabilities", nil).JSON["cloudflare"].(map[string]any); got["oauth"] != false || got["domain_connect"] != false {
		t.Fatalf("capabilities: %v", got)
	}
	d.svc.Opts.PublicURL = origin
	d.svc.Opts.DNSAllowPrivate = true
	d.svc.Opts.DNSOAuth = service.DNSOAuth{ClientID: "cid", ClientSecret: "csecret", AuthURL: "https://dash.cloudflare.example/oauth/authorize",
		TokenURL: st.srv.URL + "/token", RevokeURL: st.srv.URL + "/revoke", Scope: "zone:read dns:edit"}
	if got := d.ok("GET", "/dns/capabilities", nil).JSON["cloudflare"].(map[string]any); got["oauth"] != true {
		t.Fatalf("capabilities: %v", got)
	}

	start := func() (state string, q url.Values) {
		r := d.ok("POST", "/dns/oauth/start", map[string]any{"domain_id": d.domainID})
		u, err := url.Parse(str(r.JSON, "url"))
		if err != nil || u.Host != "dash.cloudflare.example" {
			t.Fatalf("authorize URL: %v %v", u, err)
		}
		q = u.Query()
		if q.Get("response_type") != "code" || q.Get("client_id") != "cid" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" ||
			q.Get("redirect_uri") != origin+"/admin/v1/dns/oauth/callback" || q.Get("scope") != "zone:read dns:edit" || strings.Contains(u.String(), "csecret") {
			t.Fatalf("authorize query: %v", q)
		}
		st.mu.Lock()
		st.challenge, st.redirect = q.Get("code_challenge"), q.Get("redirect_uri")
		st.mu.Unlock()
		return q.Get("state"), q
	}
	callback := func(s session, q string) resp {
		r := d.raw("GET", "/admin/v1/dns/oauth/callback?"+q, map[string]string{"Cookie": s.cookie}, nil)
		d.bodies = append(d.bodies, r.Raw)
		return r
	}

	// Without a session the callback does nothing.
	if r := d.raw("GET", "/admin/v1/dns/oauth/callback?code=good-code&state=x", nil, nil); r.Code != 401 {
		t.Fatalf("callback without session: %d", r.Code)
	}
	state, _ := start()
	if n := d.count(`SELECT count(*) FROM dns_oauth_states WHERE encode(state_hash,'escape') LIKE '%' || $1 || '%'`, state); n != 0 {
		t.Fatal("the state must be stored hashed")
	}
	// Another signed-in user cannot finish the flow somebody else started.
	d.must2(d.svc.CreateUser(context.Background(), "second@njiwa.co.tz", adminPass, "admin"))
	second := d.env.login("second@njiwa.co.tz")
	if r := callback(second, "code=good-code&state="+url.QueryEscape(state)); r.Code != 302 || !strings.Contains(r.Hdr.Get("Location"), "dns_error=oauth_state_invalid") {
		t.Fatalf("foreign user: %d %v", r.Code, r.Hdr)
	}
	if d.count(`SELECT count(*) FROM dns_connections`) != 0 {
		t.Fatal("a connection was created for the wrong user")
	}
	// ... and that attempt consumed the state: the rightful user must start again.
	state, _ = start()
	r := callback(d.admin, "code=good-code&state="+url.QueryEscape(state))
	loc := r.Hdr.Get("Location")
	if r.Code != 302 || !strings.HasPrefix(loc, origin+"/p/dnsp/domains/"+d.domainID+"?dns_connection=") {
		t.Fatalf("callback: %d %q", r.Code, loc)
	}
	if d.count(`SELECT count(*) FROM dns_connections WHERE auth_method='oauth' AND status='active' AND refresh_enc IS NOT NULL AND expires_at > now()`) != 1 {
		t.Fatal("OAuth connection missing")
	}
	// Replay: the same state is single-use.
	if r := callback(d.admin, "code=good-code&state="+url.QueryEscape(state)); !strings.Contains(r.Hdr.Get("Location"), "dns_error=oauth_state_invalid") {
		t.Fatalf("replay: %v", r.Hdr)
	}
	if d.count(`SELECT count(*) FROM dns_connections`) != 1 {
		t.Fatal("a replay created a second connection")
	}
	// Garbage state, expired state, denied by the user.
	if r := callback(d.admin, "code=good-code&state=garbage"); !strings.Contains(r.Hdr.Get("Location"), "oauth_state_invalid") {
		t.Fatalf("garbage: %v", r.Hdr)
	}
	state, _ = start()
	d.must2(d.pool.Exec(context.Background(), `UPDATE dns_oauth_states SET expires_at = now() - interval '1 minute' WHERE used_at IS NULL`))
	if r := callback(d.admin, "code=good-code&state="+url.QueryEscape(state)); !strings.Contains(r.Hdr.Get("Location"), "oauth_state_invalid") {
		t.Fatalf("expired: %v", r.Hdr)
	}
	if r := callback(d.admin, "error=access_denied&state=x"); !strings.Contains(r.Hdr.Get("Location"), "dns_error=denied") {
		t.Fatalf("denied: %v", r.Hdr)
	}
	// A wrong code is refused by the provider and creates nothing.
	state, _ = start()
	if r := callback(d.admin, "code=wrong&state="+url.QueryEscape(state)); !strings.Contains(r.Hdr.Get("Location"), "dns_error=") || d.count(`SELECT count(*) FROM dns_connections`) != 1 {
		t.Fatalf("wrong code: %v", r.Hdr)
	}

	// Token refresh when close to expiry, using the sealed refresh token.
	id := str(items(d.ok("GET", "/dns/connections", nil))[0], "id")
	d.must2(d.pool.Exec(context.Background(), `UPDATE dns_connections SET expires_at = now() + interval '30 seconds' WHERE id=$1`, id))
	if zs := items(d.ok("GET", "/dns/connections/"+id+"/zones", nil)); len(zs) != 1 {
		t.Fatalf("zones after refresh: %v", zs)
	}
	st.mu.Lock()
	refreshed := st.grants[len(st.grants)-1] == "refresh_token"
	st.mu.Unlock()
	if !refreshed {
		t.Fatalf("grants: %v", st.grants)
	}
	// A refresh that fails marks the connection expired and asks to reconnect.
	st.mu.Lock()
	st.refreshOK = false
	st.mu.Unlock()
	d.must2(d.pool.Exec(context.Background(), `UPDATE dns_connections SET expires_at = now() + interval '30 seconds' WHERE id=$1`, id))
	if r := d.call("GET", "/dns/connections/"+id+"/zones", nil); r.Code != 409 || errCode(r) != "dns_credential_rejected" {
		t.Fatalf("failed refresh: %d %s", r.Code, r.Raw)
	}
	if d.count(`SELECT count(*) FROM dns_connections WHERE id=$1 AND status='expired'`, id) != 1 {
		t.Fatal("connection not marked expired")
	}
	// Disconnect revokes at the provider.
	d.must2(d.pool.Exec(context.Background(), `UPDATE dns_connections SET status='active' WHERE id=$1`, id))
	d.ok("DELETE", "/dns/connections/"+id, nil)
	st.mu.Lock()
	revoked := len(st.revoked) == 1 && st.revoked[0] != ""
	st.mu.Unlock()
	if !revoked {
		t.Fatalf("revoked: %v", st.revoked)
	}
	d.noSecretAnywhere(st.access)
	d.noSecretAnywhere("refresh_0123456789abcdef")
	d.noSecretAnywhere("csecret")
}

func TestDNSOAuthRefusesPrivateTokenEndpoints(t *testing.T) {
	d := newDNSEnv(t)
	st := newOAuthStub(t)
	d.svc.Opts.PublicURL = origin
	// DNSAllowPrivate is NOT set: a token endpoint on loopback must never be contacted.
	d.svc.Opts.DNSOAuth = service.DNSOAuth{ClientID: "cid", ClientSecret: "csecret", AuthURL: "https://dash.cloudflare.example/a", TokenURL: st.srv.URL + "/token"}
	r := d.ok("POST", "/dns/oauth/start", map[string]any{})
	u, _ := url.Parse(str(r.JSON, "url"))
	cb := d.raw("GET", "/admin/v1/dns/oauth/callback?code=good-code&state="+url.QueryEscape(u.Query().Get("state")), map[string]string{"Cookie": d.admin.cookie}, nil)
	if !strings.Contains(cb.Hdr.Get("Location"), "dns_error=") || len(st.grants) != 0 {
		t.Fatalf("SSRF: %v grants=%v", cb.Hdr, st.grants)
	}
	if err := service.ValidateDNSOAuth(service.DNSOAuth{ClientID: "a", ClientSecret: "b", AuthURL: "http://x.example/a", TokenURL: "https://x.example/t"}); err == nil {
		t.Fatal("a plain-http authorize URL must be refused at start-up")
	}
}

// The sealed provider token must follow an encryption key rotation, or rotating
// the key would strand every DNS connection.
func TestDNSTokenSurvivesEncryptionKeyRotation(t *testing.T) {
	d := newDNSEnv(t)
	d.connect()
	both, onlyNew := rotatedBoxes(t, []byte("0123456789abcdef0123456789abcdef"))
	d.svc.Box = both
	rep, err := d.svc.Rekey(t.Context(), false)
	d.must(err)
	if rep == nil {
		t.Fatal("no report")
	}
	d.svc.Box = onlyNew // the old key is gone
	if zs := items(d.ok("GET", "/dns/connections/"+d.conn+"/zones", nil)); len(zs) != 1 {
		t.Fatalf("the token is unreadable after rotation: %v", zs)
	}
}

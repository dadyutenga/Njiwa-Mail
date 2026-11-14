package test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/service"
)

const siteOrigin = "https://www.example.com"

func (e *env) formPost(path, contentType, body string, hdr map[string]string) resp {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return e.roundTrip(r)
}

func (e *env) formVals(path string, vals map[string]string, hdr map[string]string) resp {
	v := url.Values{}
	for k, x := range vals {
		v.Set(k, x)
	}
	return e.formPost(path, "application/x-www-form-urlencoded", v.Encode(), hdr)
}

func (e *env) resetFormLimits() {
	_, err := e.pool.Exec(context.Background(), `DELETE FROM rate_limit_buckets WHERE scope_key LIKE 'fm:%'`)
	e.must(err)
}

func TestFormsAreOffByDefault(t *testing.T) {
	e := setup(t)
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")
	if r := e.adm(admin, "GET", wsBase+"/forms", nil); r.Code != 404 || errCode(r) != "forms_disabled" {
		t.Fatalf("admin: %d %s", r.Code, r.Raw)
	}
	if r := e.formVals("/v1/forms/00000000-0000-4000-8000-000000000000", map[string]string{"email": "a@example.org"}, nil); r.Code != 404 {
		t.Fatalf("public: %d", r.Code)
	}
}

func TestFormsSubmissionsSpamFiltersAndInbox(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.svc.Opts.FormsEnabled = true
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")
	e.provision("other")

	f := e.admOK(admin, "POST", wsBase+"/forms", map[string]any{"name": "Contact", "allowed_origins": []string{siteOrigin + "/"},
		"notify_emails": []string{"owner@example.org"}, "digest": "immediate", "reply_from": liveFrom})
	fid := str(f.JSON, "id")
	path := "/v1/forms/" + fid
	site := map[string]string{"Origin": siteOrigin}
	good := map[string]string{"name": "Neema", "email": "Visitor@Example.org", "message": "Do you deliver to Arusha? We need a refund policy too."}

	// A plain HTML form post (no JavaScript): a thank-you page.
	r := e.formVals(path, good, site)
	if r.Code != 200 || !strings.Contains(r.Raw, "Thank you") || r.Hdr.Get("Access-Control-Allow-Origin") != siteOrigin {
		t.Fatalf("html post: %d %v %s", r.Code, r.Hdr, r.Raw)
	}
	// JSON, and the origin rules.
	r = e.formPost(path, "application/json", `{"name":"Asha","email":"asha@example.org","message":"Hello there"}`, site)
	if r.Code != 202 || r.JSON["status"] != "received" || str(r.JSON, "id") == "" {
		t.Fatalf("json post: %d %s", r.Code, r.Raw)
	}
	if r := e.formVals(path, good, map[string]string{"Origin": "https://evil.example"}); r.Code != 403 || r.Hdr.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("other origin: %d %v", r.Code, r.Hdr)
	}
	pre := e.raw("OPTIONS", path, map[string]string{"Origin": siteOrigin}, nil)
	if pre.Code != 204 || pre.Hdr.Get("Access-Control-Allow-Origin") != siteOrigin || !strings.Contains(pre.Hdr.Get("Access-Control-Allow-Methods"), "POST") {
		t.Fatalf("preflight: %d %v", pre.Code, pre.Hdr)
	}
	if pre := e.raw("OPTIONS", path, map[string]string{"Origin": "https://evil.example"}, nil); pre.Hdr.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("preflight for another origin: %v", pre.Hdr)
	}
	if r := e.formVals(path, map[string]string{"name": "X", "email": "nope", "message": "m"}, site); r.Code != 422 || !strings.Contains(r.Raw, "valid email") {
		t.Fatalf("bad email: %d %s", r.Code, r.Raw)
	}
	// A filled honeypot looks successful and keeps nothing.
	before := e.count(`SELECT count(*) FROM form_submissions`)
	if r := e.formPost(path, "application/json", `{"email":"bot@example.org","message":"hi","hp_website":"http://spam"}`, site); r.Code != 202 {
		t.Fatalf("honeypot: %d %s", r.Code, r.Raw)
	}
	if e.count(`SELECT count(*) FROM form_submissions`) != before {
		t.Fatal("a honeypot post was kept")
	}

	// Spam ladder: quarantined mail is kept but hidden; the worst is refused.
	e.resetFormLimits()
	quarantine := "Make money fast, click here: http://a.example http://b.example http://c.example"
	if r := e.formVals(path, map[string]string{"name": "Spammer", "email": "s1@mailinator.com", "message": quarantine}, site); r.Code != 200 {
		t.Fatalf("quarantine post: %d %s", r.Code, r.Raw)
	}
	if e.count(`SELECT count(*) FROM form_submissions WHERE state='spam' AND action='quarantine'`) != 1 {
		t.Fatal("not quarantined")
	}
	reject := strings.Repeat("viagra cialis casino forex payday porn ", 3) + strings.Repeat("http://x.example ", 7) + "bit.ly/y SEO SERVICES"
	if r := e.formPost(path, "application/json", `{"email":"s2@mailinator.com","message":"`+reject+`"}`, site); r.Code != 422 || errCode(r) != "spam_rejected" {
		t.Fatalf("reject: %d %s", r.Code, r.Raw)
	}
	inbox := e.admOK(admin, "GET", wsBase+"/forms/submissions", nil)
	if n := len(inbox.JSON["items"].([]any)); n != 2 || strings.Contains(inbox.Raw, "Spammer") || strings.Contains(inbox.Raw, "visitor@") {
		t.Fatalf("inbox hides quarantine and shows no addresses: %d %s", n, inbox.Raw)
	}
	if q := e.admOK(admin, "GET", wsBase+"/forms/submissions?state=spam", nil); len(q.JSON["items"].([]any)) != 1 {
		t.Fatalf("spam view: %s", q.Raw)
	}

	// Filters: a dry run first, then a flag, a rejection by domain.
	e.resetFormLimits()
	dry := e.admOK(admin, "POST", wsBase+"/forms/filters/test", map[string]any{"kind": "keyword", "pattern": "Refund", "action": "flag"})
	if dry.JSON["would_match"] != float64(1) || dry.JSON["checked"] != float64(3) || strings.Contains(dry.Raw, "visitor@") {
		t.Fatalf("dry run: %s", dry.Raw)
	}
	if n := e.count(`SELECT count(*) FROM form_filters`); n != 0 {
		t.Fatal("a dry run saved a filter")
	}
	if r := e.adm(admin, "POST", wsBase+"/forms/filters", map[string]any{"kind": "regex", "pattern": "([", "action": "flag"}); r.Code != 400 {
		t.Fatalf("bad regex: %d", r.Code)
	}
	e.admOK(admin, "POST", wsBase+"/forms/filters", map[string]any{"kind": "keyword", "pattern": "refund", "action": "flag"})
	e.admOK(admin, "POST", wsBase+"/forms/filters", map[string]any{"kind": "domain", "pattern": "blocked.example", "action": "reject"})
	e.admOK(admin, "POST", wsBase+"/forms/filters", map[string]any{"kind": "ip", "pattern": "203.0.113.0/24", "action": "reject"})
	if r := e.formVals(path, map[string]string{"name": "Juma", "email": "juma@example.org", "message": "I want a refund"}, site); r.Code != 200 {
		t.Fatalf("flagged post: %d", r.Code)
	}
	if e.count(`SELECT count(*) FROM form_submissions WHERE action='flag' AND spam_reasons::text LIKE '%filter:keyword%'`) != 1 {
		t.Fatal("keyword filter did not flag")
	}
	if r := e.formVals(path, map[string]string{"name": "Z", "email": "z@sub.blocked.example", "message": "hello"}, site); r.Code != 422 || !strings.Contains(r.Raw, "looks like spam") {
		t.Fatalf("domain filter: %d %s", r.Code, r.Raw)
	}

	// Digests: one message to the nominated address for the kept, unquarantined ones.
	e.resetFormLimits()
	n, err := e.svc.SendFormDigests(ctx)
	e.must(err)
	if n != 3 {
		t.Fatalf("digest covered %d submissions (the quarantined one is not announced)", n)
	}
	var digestMsgs int
	e.must(e.pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE source='automation' AND recipient_hash=$1`, e.box.MAC("recipient", []byte("owner@example.org"))).Scan(&digestMsgs))
	if digestMsgs != 1 {
		t.Fatalf("digest messages: %d", digestMsgs)
	}
	if again, _ := e.svc.SendFormDigests(ctx); again != 0 {
		t.Fatalf("digest sent twice: %d", again)
	}

	// The inbox: open one, assign it, reply through the pipeline.
	items := e.admOK(admin, "GET", wsBase+"/forms/submissions", nil).JSON["items"].([]any)
	var subID string
	for _, it := range items {
		if m := it.(map[string]any); strings.Contains(str(m, "preview"), "Arusha") {
			subID = str(m, "id")
		}
	}
	if subID == "" {
		t.Fatalf("no preview with the message: %v", items)
	}
	d := e.admOK(admin, "GET", wsBase+"/forms/submissions/"+subID, nil)
	fields := d.JSON["fields"].(map[string]any)
	if fields["email"] != "visitor@example.org" || fields["name"] != "Neema" || d.JSON["state"] != "new" {
		t.Fatalf("detail: %s", d.Raw)
	}
	me := str(e.admOK(admin, "GET", "/admin/v1/auth/me", nil).JSON, "id")
	e.admOK(admin, "PATCH", wsBase+"/forms/submissions/"+subID, map[string]any{"state": "open", "assigned_to": me})
	if d := e.admOK(admin, "GET", wsBase+"/forms/submissions/"+subID, nil); d.JSON["state"] != "open" || d.JSON["assigned_to"] != "admin@njiwa.co.tz" {
		t.Fatalf("after update: %s", d.Raw)
	}
	if r := e.adm(admin, "PATCH", wsBase+"/forms/submissions/"+subID, map[string]any{"state": "bogus"}); r.Code != 400 {
		t.Fatalf("bad state: %d", r.Code)
	}
	rep := e.admOK(admin, "POST", wsBase+"/forms/submissions/"+subID+"/reply", map[string]any{"subject": "Re: your question", "text": "Yes, we deliver to Arusha."})
	if str(rep.JSON, "id") == "" {
		t.Fatalf("reply: %s", rep.Raw)
	}
	if d := e.admOK(admin, "GET", wsBase+"/forms/submissions/"+subID, nil); d.JSON["state"] != "replied" || d.JSON["replied_at"] == nil {
		t.Fatalf("after reply: %s", d.Raw)
	}
	if n := e.count(`SELECT count(*) FROM messages WHERE id=$1 AND source='ui'`, str(rep.JSON, "id")); n != 1 {
		t.Fatal("the reply did not go through the pipeline")
	}
	// Another project sees nothing of it.
	if r := e.adm(admin, "GET", "/admin/v1/w/other/sandbox/forms/submissions/"+subID, nil); r.Code != 404 {
		t.Fatalf("other project: %d", r.Code)
	}
	if r := e.admOK(admin, "GET", "/admin/v1/w/other/sandbox/forms/submissions", nil); len(r.JSON["items"].([]any)) != 0 {
		t.Fatalf("other project's inbox: %s", r.Raw)
	}
	e.admOK(admin, "DELETE", wsBase+"/forms/submissions/"+subID, nil)
	if r := e.adm(admin, "GET", wsBase+"/forms/submissions/"+subID, nil); r.Code != 404 {
		t.Fatalf("deleted: %d", r.Code)
	}

	// Limits: the sixth post in a minute from one address is refused.
	e.resetFormLimits()
	var last resp
	for i := 0; i < 6; i++ {
		last = e.formPost(path, "application/json", `{"email":"p`+string(rune('a'+i))+`@example.org","message":"hello"}`, site)
	}
	if last.Code != 429 || last.Hdr.Get("Retry-After") == "" {
		t.Fatalf("rate limit: %d %v", last.Code, last.Hdr)
	}
	// The same sender address three times a minute at most, from any address.
	e.resetFormLimits()
	for i := 0; i < 4; i++ {
		last = e.formPost(path, "application/json", `{"email":"same@example.org","message":"hello"}`, map[string]string{"Origin": siteOrigin})
		_, _ = e.pool.Exec(ctx, `DELETE FROM rate_limit_buckets WHERE scope_key LIKE 'fm:ip:%'`)
	}
	if last.Code != 429 {
		t.Fatalf("sender limit: %d %s", last.Code, last.Raw)
	}
}

func TestFormsNonceAndFiles(t *testing.T) {
	e := setup(t)
	e.svc.Opts.FormsEnabled = true
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")
	fid := str(e.admOK(admin, "POST", wsBase+"/forms", map[string]any{"name": "Quote", "allowed_origins": []string{siteOrigin}, "require_nonce": true,
		"redirect_url": "https://www.example.com/thanks"}).JSON, "id")
	path := "/v1/forms/" + fid
	site := map[string]string{"Origin": siteOrigin}
	if r := e.formVals(path, map[string]string{"email": "a@example.org", "message": "hi"}, site); r.Code != 422 || !strings.Contains(r.Raw, "reload") {
		t.Fatalf("no nonce: %d %s", r.Code, r.Raw)
	}
	if r := e.formVals(path, map[string]string{"email": "b@example.org", "message": "hi", "_nonce": "made-up"}, site); r.Code != 422 {
		t.Fatalf("made-up nonce: %d", r.Code)
	}
	nr := e.raw("GET", path+"/nonce", site, nil)
	nonce := str(nr.JSON, "nonce")
	if nr.Code != 200 || nonce == "" || nr.Hdr.Get("Access-Control-Allow-Origin") != siteOrigin {
		t.Fatalf("nonce: %d %s %v", nr.Code, nr.Raw, nr.Hdr)
	}
	// A file upload (multipart), posted at once: the nonce works, "too fast" adds to the score.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("email", "c@example.org")
	mw.WriteField("message", "See the attached brief.")
	mw.WriteField("_nonce", nonce)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="brief.txt"`)
	h.Set("Content-Type", "text/plain")
	pw, _ := mw.CreatePart(h)
	pw.Write([]byte("the brief"))
	mw.Close()
	r := e.formPost(path, mw.FormDataContentType(), buf.String(), site)
	if r.Code != 303 || r.Hdr.Get("Location") != "https://www.example.com/thanks" {
		t.Fatalf("multipart post: %d %v %s", r.Code, r.Hdr, r.Raw)
	}
	if e.count(`SELECT count(*) FROM form_submissions WHERE spam_reasons::text LIKE '%submitted_too_fast%'`) != 1 {
		t.Fatal("a post made at once with its nonce should be marked too fast")
	}
	// The nonce is single use.
	if r := e.formVals(path, map[string]string{"email": "d@example.org", "message": "again", "_nonce": nonce}, site); r.Code != 422 {
		t.Fatalf("nonce reuse: %d", r.Code)
	}
	items := e.admOK(admin, "GET", wsBase+"/forms/submissions", nil).JSON["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["files"] != float64(1) {
		t.Fatalf("inbox: %v", items)
	}
	sid := str(items[0].(map[string]any), "id")
	d := e.admOK(admin, "GET", wsBase+"/forms/submissions/"+sid, nil)
	fileID := str(d.JSON["files"].([]any)[0].(map[string]any), "id")
	dl := e.adm(admin, "GET", wsBase+"/forms/submissions/"+sid+"/files/"+fileID, nil)
	if dl.Code != 200 || dl.Raw != "the brief" || dl.Hdr.Get("Content-Type") != "application/octet-stream" || !strings.Contains(dl.Hdr.Get("Content-Disposition"), "attachment") {
		t.Fatalf("download: %d %v %q", dl.Code, dl.Hdr, dl.Raw)
	}
	// A file type that is not allowed is refused.
	nonce2 := str(e.raw("GET", path+"/nonce", site, nil).JSON, "nonce")
	var b2 bytes.Buffer
	m2 := multipart.NewWriter(&b2)
	m2.WriteField("email", "e@example.org")
	m2.WriteField("_nonce", nonce2)
	h2 := textproto.MIMEHeader{}
	h2.Set("Content-Disposition", `form-data; name="file"; filename="x.exe"`)
	h2.Set("Content-Type", "application/x-msdownload")
	p2, _ := m2.CreatePart(h2)
	p2.Write([]byte("MZ"))
	m2.Close()
	if r := e.formPost(path, m2.FormDataContentType(), b2.String(), site); r.Code != 422 || !strings.Contains(r.Raw, "files may be") {
		t.Fatalf("bad file type: %d %s", r.Code, r.Raw)
	}
}

func TestFormsFieldsCoveredByRotation(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.svc.Opts.FormsEnabled = true
	e.provision("sacas")
	sc, err := e.svc.ResolveScope(ctx, "sacas", "sandbox")
	e.must(err)
	fid, err := e.svc.CreateForm(ctx, sc, service.FormInput{Name: "Rot", AllowedOrigins: []string{siteOrigin}})
	e.must(err)
	res, err := e.svc.SubmitForm(ctx, service.FormPost{FormID: fid, Origin: siteOrigin, IP: "192.0.2.10",
		Fields: map[string]string{"email": "rot@example.org", "message": "sealed under the old key"}})
	e.must(err)
	both, onlyNew := rotatedBoxes(t, []byte("0123456789abcdef0123456789abcdef"))
	e.svc.Box = both
	_, err = e.svc.Rekey(ctx, false)
	e.must(err)
	e.svc.Box = onlyNew
	d, err := e.svc.GetFormSubmission(ctx, sc, res.ID)
	if err != nil || d["fields"].(map[string]string)["message"] != "sealed under the old key" {
		t.Fatalf("submission with only the new key: %v %v", d, err)
	}
}

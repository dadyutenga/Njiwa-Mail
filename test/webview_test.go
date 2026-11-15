package test

import (
	"context"
	"strings"
	"testing"
)

// "View in browser": a signed, expiring copy of the HTML, behind a link the
// message carries. High-priority mail (codes, resets) may not have one.
func TestWebView(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.provision("news")
	key, _, err := e.svc.CreateKey(ctx, "news", "sandbox", []string{"emails:send_raw"}, "raw", 0)
	e.must(err)
	if r := e.do("POST", "/v1/emails", key, idem("wv-high"), map[string]any{"from": liveFrom, "to": testRcpt,
		"subject": "Code", "html": "<p>123456</p>", "priority": "high", "web_view": true}); r.Code != 422 || errCode(r) != "web_view_not_allowed" {
		t.Fatalf("high priority with a web view: %d %s", r.Code, r.Raw)
	}
	r := e.do("POST", "/v1/emails", key, idem("wv-ok"), map[string]any{"from": liveFrom, "to": testRcpt,
		"subject": "November news", "text": "Hello", "html": "<html><body><h1>News</h1><script>alert(1)</script></body></html>", "web_view": true})
	if r.Code != 202 {
		t.Fatalf("send: %d %s", r.Code, r.Raw)
	}
	id := str(r.JSON, "id")
	if n := e.runWorker(); n != 1 {
		t.Fatalf("worker: %d", n)
	}
	url := e.svc.WebViewURL(id)
	path := url[strings.Index(url, "/v1/view/"):]
	page := e.raw("GET", path, nil, nil)
	if page.Code != 200 || !strings.Contains(page.Raw, "<h1>News</h1>") || strings.Contains(page.Raw, "View this email in your browser") {
		t.Fatalf("page: %d %s", page.Code, page.Raw)
	}
	csp := page.Hdr.Get("Content-Security-Policy")
	if !strings.HasPrefix(csp, "sandbox ") || strings.Contains(csp, "allow-scripts") || strings.Contains(csp, "allow-same-origin") ||
		page.Hdr.Get("X-Robots-Tag") == "" {
		t.Fatalf("headers: %v", page.Hdr)
	}
	// The sent message carries the link (sandbox: the capture).
	var n int
	e.must(e.pool.QueryRow(ctx, `SELECT count(*) FROM web_views WHERE message_id=$1`, id).Scan(&n))
	if n != 1 {
		t.Fatalf("web_views rows: %d", n)
	}
	// A forged, altered or expired link is the same 404.
	for _, p := range []string{path[:len(path)-2] + "xx", "/v1/view/" + id, "/v1/view/nope"} {
		if r := e.raw("GET", p, nil, nil); r.Code != 404 {
			t.Fatalf("%s: %d", p, r.Code)
		}
	}
	_, err = e.pool.Exec(ctx, `UPDATE web_views SET expires_at=now()-interval '1 second'`)
	e.must(err)
	if r := e.raw("GET", path, nil, nil); r.Code != 404 || !strings.Contains(r.Raw, "expired") {
		t.Fatalf("expired: %d", r.Code)
	}
	res, err := e.svc.RunRetention(ctx)
	e.must(err)
	if res.WebViews != 1 {
		t.Fatalf("retention: %+v", res)
	}
}

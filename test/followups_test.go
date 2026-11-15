package test

import (
	"context"
	"testing"

	"github.com/njiwa-tz/mail/internal/service"
)

// The relay's status for the dashboard comes from its heartbeat, and Compose can
// add a "view in browser" link.
func TestRelayStatusAndComposeWebView(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")
	e.svc.Opts.RelayAddr, e.svc.Opts.RelayImplicitTLS = "0.0.0.0:2587", false

	st := e.admOK(admin, "GET", "/admin/v1/relay", nil)
	if st.JSON["running"] != false || str(st.JSON, "port") != "2587" || str(st.JSON, "tls") != "starttls" || str(st.JSON, "scope") != "emails:send_raw" {
		t.Fatalf("before a heartbeat: %s", st.Raw)
	}
	e.must(e.svc.HeartbeatRole(ctx, "relay", "host1"))
	if st := e.admOK(admin, "GET", "/admin/v1/relay", nil); st.JSON["running"] != true || st.JSON["last_seen"] == nil {
		t.Fatalf("after a heartbeat: %s", st.Raw)
	}
	_, err := e.pool.Exec(ctx, `UPDATE worker_heartbeats SET last_seen = now() - interval '5 minutes' WHERE role='relay'`)
	e.must(err)
	if st := e.admOK(admin, "GET", "/admin/v1/relay", nil); st.JSON["running"] != false {
		t.Fatalf("a stale heartbeat still counts: %s", st.Raw)
	}
	e.svc.Opts.RelayImplicitTLS = true
	if st := e.admOK(admin, "GET", "/admin/v1/relay", nil); str(st.JSON, "tls") != "implicit" {
		t.Fatalf("implicit tls: %s", st.Raw)
	}

	// Compose with web_view: the worker stores the copy.
	sc, err := e.svc.ResolveScope(ctx, "sacas", "sandbox")
	e.must(err)
	acc, err := e.svc.Compose(ctx, sc, "admin@njiwa.co.tz", "compose-wv-1", service.ComposeInput{From: liveFrom, To: testRcpt, Subject: "News", Text: "Hello", HTML: "<h1>News</h1>", WebView: true})
	e.must(err)
	if n := e.runWorker(); n != 1 {
		t.Fatalf("worker: %d", n)
	}
	if c := e.count(`SELECT count(*) FROM web_views WHERE message_id=$1`, acc.ID); c != 1 {
		t.Fatalf("web view rows: %d", c)
	}
}

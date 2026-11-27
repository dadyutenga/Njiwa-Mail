package test

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func (e *env) setupPost(body map[string]any, hdr map[string]string) resp {
	h := map[string]string{"X-Njiwa-Setup": "1"}
	for k, v := range hdr {
		h[k] = v
	}
	return e.raw("POST", "/admin/v1/setup", h, body)
}

func firstRunBody(email string) map[string]any {
	return map[string]any{"organization": "Acme Tanzania", "name": "Asha Juma", "email": email, "password": adminPass}
}

// First-run setup works once, signs the operator in, and is then closed.
func TestFirstRunSetup(t *testing.T) {
	e := setup(t)
	status := func() resp { return e.raw("GET", "/admin/v1/setup/status", nil, nil) }
	if r := status(); r.Code != 200 || r.JSON["needs_setup"] != true || r.JSON["token_required"] != false {
		t.Fatalf("%s", r.Raw)
	}
	// Requests another site could make a browser send are refused.
	if r := e.raw("POST", "/admin/v1/setup", nil, firstRunBody("a@acme.tz")); r.Code != 403 {
		t.Fatalf("without the header: %d %s", r.Code, r.Raw)
	}
	for _, h := range []map[string]string{{"Sec-Fetch-Site": "cross-site"}, {"Origin": "https://evil.example"}, {"Content-Type": "text/plain"}} {
		if r := e.setupPost(firstRunBody("a@acme.tz"), h); r.Code != 403 || errCode(r) != "cross_site_request" {
			t.Fatalf("%v: %d %s", h, r.Code, r.Raw)
		}
	}
	if r := e.setupPost(map[string]any{"organization": "A", "name": "Asha", "email": "a@acme.tz", "password": adminPass}, nil); r.Code != 400 {
		t.Fatalf("short organization: %d", r.Code)
	}
	if r := e.setupPost(map[string]any{"organization": "Acme", "name": "Asha", "email": "a@acme.tz", "password": "short"}, nil); r.Code != 400 {
		t.Fatalf("short password: %d", r.Code)
	}
	r := e.setupPost(firstRunBody("Asha@Acme.tz"), nil)
	cookie := r.Hdr.Get("Set-Cookie")
	if r.Code != 200 || r.JSON["installation_operator"] != true || str(r.JSON, "csrf_token") == "" || !strings.Contains(cookie, "HttpOnly") {
		t.Fatalf("setup: %d %s", r.Code, r.Raw)
	}
	s := session{cookie: strings.SplitN(cookie, ";", 2)[0], csrf: str(r.JSON, "csrf_token")}
	org := e.admOK(s, "GET", "/admin/v1/organization", nil)
	if str(org.JSON, "name") != "Acme Tanzania" || str(org.JSON, "slug") != "acme-tanzania" {
		t.Fatalf("%s", org.Raw)
	}
	if r := status(); r.JSON["needs_setup"] != false {
		t.Fatalf("still open: %s", r.Raw)
	}
	if r := e.setupPost(firstRunBody("second@acme.tz"), nil); r.Code != 409 || errCode(r) != "setup_closed" {
		t.Fatalf("second setup: %d %s", r.Code, r.Raw)
	}
	// Closed for good, even if the users table were emptied.
	_, err := e.pool.Exec(context.Background(), `DELETE FROM sessions; DELETE FROM users`)
	e.must(err)
	if r := status(); r.JSON["needs_setup"] != false {
		t.Fatalf("reopened: %s", r.Raw)
	}
}

func TestFirstRunConcurrentCreatesOneUser(t *testing.T) {
	e := setup(t)
	var wg sync.WaitGroup
	codes := make(chan int, 6)
	for _, n := range []string{"a", "b", "c", "d", "e", "f"} {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			codes <- e.setupPost(firstRunBody(n+"@acme.tz"), nil).Code
		}(n)
	}
	wg.Wait()
	close(codes)
	ok := 0
	for c := range codes {
		switch c {
		case 200:
			ok++
		case 409, 503: // closed, or the password hasher was busy
		default:
			t.Errorf("status %d", c)
		}
	}
	if ok != 1 || e.count(`SELECT count(*) FROM users`) != 1 || e.count(`SELECT count(*) FROM organization`) != 1 {
		t.Fatalf("successes=%d users=%d", ok, e.count(`SELECT count(*) FROM users`))
	}
}

func TestFirstRunSetupToken(t *testing.T) {
	e := setup(t)
	e.svc.Opts.SetupToken = "a-long-setup-token"
	if r := e.raw("GET", "/admin/v1/setup/status", nil, nil); r.JSON["token_required"] != true {
		t.Fatalf("%s", r.Raw)
	}
	body := firstRunBody("a@acme.tz")
	for _, tok := range []string{"", "wrong-setup-token"} {
		body["token"] = tok
		if r := e.setupPost(body, nil); r.Code != 403 || errCode(r) != "invalid_setup_token" {
			t.Fatalf("%q: %d %s", tok, r.Code, r.Raw)
		}
	}
	if e.count(`SELECT count(*) FROM users`) != 0 {
		t.Fatal("a wrong token created a user")
	}
	body["token"] = "a-long-setup-token"
	if r := e.setupPost(body, nil); r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
}

// An installation with a user (the CLI path) never offers setup.
func TestFirstRunClosedAfterCLIUser(t *testing.T) {
	e := setup(t)
	e.user("ops@njiwa.co.tz", "admin")
	if r := e.raw("GET", "/admin/v1/setup/status", nil, nil); r.JSON["needs_setup"] != false {
		t.Fatalf("%s", r.Raw)
	}
	if r := e.setupPost(firstRunBody("x@acme.tz"), nil); r.Code != 409 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
}

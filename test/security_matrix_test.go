package test

import (
	"context"

	"github.com/njiwa-tz/mail/internal/service"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// F05: every route the application registers is checked, so a route added
// later without authentication or CSRF protection fails here.

// publicRoutes are reachable without a session or key by design.
var publicRoutes = map[string]string{
	"POST /admin/v1/auth/login":   "login",
	"POST /admin/v1/auth/forgot":  "password reset request (always answers the same)",
	"POST /admin/v1/auth/reset":   "password reset with a single-use token",
	"POST /console/login":         "console login",
	"GET /health/live":            "liveness probe",
	"GET /health/ready":           "readiness probe",
	"GET /metrics":                "bearer-token protected (TestMetricsOverviewAndHealth)",
	"GET /v1/unsubscribe/:token":  "signed unsubscribe link",
	"POST /v1/unsubscribe/:token": "signed one-click unsubscribe (RFC 8058)",
	"GET /v1/t/o/:token":          "signed open pixel of an opt-in tracked campaign (TestCampaignToListTrackingAndScopedUnsubscribe)",
	"GET /v1/t/c/:token":          "signed click redirect, only to the campaign's stored links (TestCampaignToListTrackingAndScopedUnsubscribe)",
	"GET /v1/view/:token":         "signed, expiring \"view in browser\" link (TestWebView)",
}

var paramRe = regexp.MustCompile(`:[a-z]+`)

func concrete(path string) string {
	return paramRe.ReplaceAllStringFunc(path, func(p string) string {
		switch p {
		case ":project":
			return "nope"
		case ":mode":
			return "live"
		case ":name":
			return "nope"
		case ":version":
			return "1"
		case ":prefix":
			return "dmk_none"
		case ":token":
			return "x"
		}
		return "00000000-0000-4000-8000-000000000000"
	})
}

func TestEveryRouteRequiresAuthAndCSRF(t *testing.T) {
	e := setup(t)
	admin := e.user("admin@njiwa.co.tz", "admin")
	seen := map[string]bool{}
	checked := 0
	for _, r := range e.h.GetRoutes(true) {
		key := r.Method + " " + r.Path
		if seen[key] || r.Method == "OPTIONS" || r.Method == "USE" {
			continue
		}
		seen[key] = true
		// Fiber v3 registers HEAD for every GET route; a HEAD is public only
		// when its GET is, and must otherwise need a session or key as well.
		if _, ok := publicRoutes["GET "+r.Path]; ok && r.Method == "HEAD" {
			continue
		}
		if _, ok := publicRoutes[key]; ok {
			continue
		}
		path := concrete(r.Path)
		write := r.Method != http.MethodGet && r.Method != http.MethodHead
		switch {
		case strings.HasPrefix(r.Path, "/admin/v1/"):
			// No session at all.
			if res := e.raw(r.Method, path, nil, map[string]any{}); res.Code != 401 {
				t.Errorf("%s without a session: %d %s", key, res.Code, res.Raw)
			}
			// A valid session but no CSRF token, on a write.
			if write && r.Path != "/admin/v1/auth/logout" {
				res := e.raw(r.Method, path, map[string]string{"Cookie": admin.cookie}, map[string]any{})
				if res.Code != 403 || errCode(res) != "csrf_token_invalid" {
					t.Errorf("%s without CSRF: %d %s", key, res.Code, res.Raw)
				}
			}
		case strings.HasPrefix(r.Path, "/v1/"):
			if res := e.raw(r.Method, path, nil, map[string]any{}); res.Code != 401 {
				t.Errorf("%s without an API key: %d %s", key, res.Code, res.Raw)
			}
		case strings.HasPrefix(r.Path, "/console/"):
			if res := e.raw(r.Method, path, nil, map[string]any{}); res.Code != 401 && res.Code != 403 && res.Code != 404 {
				t.Errorf("%s without a console session: %d %s", key, res.Code, res.Raw)
			}
		default:
			t.Errorf("%s is not covered by this matrix; add it to a group or to publicRoutes with a reason", key)
		}
		checked++
	}
	if checked < 150 {
		t.Fatalf("only %d routes checked; did route registration change?", checked)
	}
	t.Logf("%d routes checked", checked)
}

// F05: no GET response anywhere in the operator API carries a stored secret:
// SMTP passwords, the Mailcow API key, or private key material from the
// managed installer.
func TestNoSecretInAnyAdminResponse(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	owner := e.user("owner@njiwa.co.tz", "owner")
	e.provision("alpha")
	e.admOK(owner, "PUT", "/admin/v1/mail-server", map[string]any{"smtp_host": "mail.example.com", "smtp_port": 587,
		"smtp_tls_mode": "starttls", "smtp_username": "u", "smtp_password": "pw-secret-one", "provider_type": "mailcow",
		"mailcow_api_url": "https://mail.example.com", "mailcow_api_key": "MAILCOW-KEY-SECRET"})
	e.newProfile("relay.example.com") // password api-secret-pw-xyz
	cert, key := pemPair(t, "njiwa-worker")
	ca, _ := pemPair(t, "ca")
	e.must(e.svc.RecordManagedInstall(ctx, service.CLIActor, service.ManagedInstall{ReceiptID: "r1", PlanDigest: "abc",
		ApprovedBy: "cli", PostfixVersion: "3.8.6", SubmissionHost: "127.0.0.1", SubmissionPort: 10587,
		ServerName: "mailout.example.com", Selector: "njiwa", DKIMRecords: map[string]string{"notify.example.com": dkimRecord(t)},
		ClientCertPEM: cert, ClientKeyPEM: key, CAPEM: ca}))

	secrets := []string{"pw-secret-one", "MAILCOW-KEY-SECRET", "api-secret-pw-xyz", "PRIVATE KEY", passSecret}
	n, ok200 := 0, 0
	for _, r := range e.h.GetRoutes(true) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.Path, "/admin/v1/") {
			continue
		}
		path := strings.Replace(concrete(r.Path), "/w/nope/", "/w/alpha/", 1)
		res := e.raw("GET", path, map[string]string{"Cookie": owner.cookie}, nil)
		for _, s := range secrets {
			if strings.Contains(res.Raw, s) {
				t.Errorf("GET %s returned a secret (%q)", r.Path, s)
			}
		}
		n++
		if res.Code == 200 {
			ok200++
		}
	}
	t.Logf("%d GET routes probed, %d answered 200", n, ok200)
	if n < 50 || ok200 < 30 {
		t.Fatalf("only %d GET routes probed (%d with 200)", n, ok200)
	}
}

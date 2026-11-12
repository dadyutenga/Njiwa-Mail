package test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/njiwa-tz/mail/internal/provider"
)

// fakeProvider is an in-memory mail server management API.
type fakeProvider struct {
	mu         sync.Mutex
	domains    map[string]bool
	dkim       map[string]string
	transports map[string]string
	failDKIM   bool
	calls      []string
	key        string
}

func (f *fakeProvider) Name() string               { return "mailcow" }
func (f *fakeProvider) Ping(context.Context) error { return nil }
func (f *fakeProvider) DomainState(_ context.Context, d string) (provider.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.domains[d] {
		return provider.State{"exists": false}, nil
	}
	return provider.State{"exists": true, "active": true}, nil
}
func (f *fakeProvider) AddDomain(_ context.Context, d string, relay bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "add_domain "+d)
	f.domains[d] = true
	return nil
}
func (f *fakeProvider) DKIMRecord(_ context.Context, d string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dkim[d], nil
}
func (f *fakeProvider) DKIMState(_ context.Context, d string) (provider.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dkim[d] == "" {
		return provider.State{"present": false}, nil
	}
	return provider.State{"present": true, "fingerprint": "abc123"}, nil
}
func (f *fakeProvider) AddDKIM(_ context.Context, d, sel string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "add_dkim "+d)
	if f.failDKIM {
		return errors.New("Mailcow: dkim failed with key " + f.key) // a provider error that quotes a secret must not reach the audit log verbatim... it is truncated, so test the content below
	}
	f.dkim[d] = "v=DKIM1; k=rsa; p=AAAA"
	return nil
}
func (f *fakeProvider) TransportState(_ context.Context, d string) (provider.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if nh, ok := f.transports[d]; ok {
		return provider.State{"exists": true, "nexthop": nh}, nil
	}
	return provider.State{"exists": false}, nil
}
func (f *fakeProvider) AddTransport(_ context.Context, d, nh string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "add_transport "+d)
	f.transports[d] = nh
	return nil
}

// C05: the service talks to a Provider, not to a Mailcow client; every write is
// audited with the object's state before and after, and no secret is recorded.
func TestProviderWritesAreAuditedWithBeforeAndAfter(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	const apiKey = "MAILCOW-API-KEY-SECRET-123"
	fp := &fakeProvider{domains: map[string]bool{}, dkim: map[string]string{}, transports: map[string]string{}, key: apiKey}
	var gotKey string
	e.svc.Opts.NewProvider = func(url, key string) provider.Provider { gotKey = key; return fp }
	e.admOK(admin, "PUT", "/admin/v1/mail-server", map[string]any{"smtp_host": "mail.example.com", "smtp_port": 587, "smtp_tls_mode": "starttls",
		"smtp_username": "u", "smtp_password": "pw-secret-xyz", "provider_type": "mailcow", "mailcow_api_url": "https://mail.example.com",
		"mailcow_api_key": apiKey, "inbound_listener": "172.22.1.200:2525"})
	e.must(e.svc.CreateProject(ctx, "mc"))
	d := e.admOK(admin, "POST", wsPath("mc", "/domains"), map[string]any{"name": "mc.example.com", "purpose": "both"})
	id := str(d.JSON, "id")

	steps := e.admOK(admin, "POST", wsPath("mc", "/domains/"+id+"/mailcow"), nil).JSON["steps"].([]any)
	if gotKey != apiKey {
		t.Fatalf("the adapter received the stored key %q", gotKey)
	}
	for _, st := range steps {
		if st.(map[string]any)["ok"] != true {
			t.Fatalf("step failed: %v", steps)
		}
	}
	if got := strings.Join(fp.calls, ","); got != "add_domain mc.example.com,add_dkim mc.example.com,add_transport mc.example.com" {
		t.Fatalf("provider calls: %s", got)
	}
	type row struct{ before, after, changed, ok string }
	audit := func(action string) map[string]any {
		var raw []byte
		e.must(e.pool.QueryRow(ctx, `SELECT details FROM audit_log WHERE action=$1 ORDER BY id DESC LIMIT 1`, action).Scan(&raw))
		var details map[string]any
		e.must(json.Unmarshal(raw, &details))
		return details
	}
	dom := audit("provider.mailcow.add_domain")
	if dom["changed"] != true || dom["ok"] != true || dom["domain"] != "mc.example.com" ||
		dom["before"].(map[string]any)["exists"] != false || dom["after"].(map[string]any)["exists"] != true {
		t.Fatalf("add_domain audit: %v", dom)
	}
	dk := audit("provider.mailcow.add_dkim")
	if dk["before"].(map[string]any)["present"] != false || dk["after"].(map[string]any)["present"] != true || dk["after"].(map[string]any)["fingerprint"] == nil {
		t.Fatalf("add_dkim audit: %v", dk)
	}
	tr := audit("provider.mailcow.add_transport")
	if tr["after"].(map[string]any)["nexthop"] != "[172.22.1.200]:2525" || tr["before"].(map[string]any)["exists"] != false {
		t.Fatalf("add_transport audit: %v", tr)
	}
	actor := ""
	e.must(e.pool.QueryRow(ctx, `SELECT actor FROM audit_log WHERE action='provider.mailcow.add_domain'`).Scan(&actor))
	if actor != "admin@njiwa.co.tz" {
		t.Fatalf("audit actor %q", actor)
	}

	// Run again: the provider already has everything, so nothing changes and the audit says so.
	e.admOK(admin, "POST", wsPath("mc", "/domains/"+id+"/mailcow"), nil)
	if again := audit("provider.mailcow.add_domain"); again["changed"] != false {
		t.Fatalf("a no-op write was reported as a change: %v", again)
	}

	// A failing write is recorded as failed, with a bounded message.
	fp.failDKIM = true
	fp.dkim = map[string]string{}
	e.admOK(admin, "POST", wsPath("mc", "/domains/"+id+"/mailcow"), nil)
	if f := audit("provider.mailcow.add_dkim"); f["ok"] != false || f["error"] == nil || f["changed"] != false || !strings.Contains(f["error"].(string), "[redacted]") {
		t.Fatalf("failed write audit: %v", f)
	}

	// No secret anywhere in the audit log.
	var leaked int
	e.must(e.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE details::text LIKE '%' || $1 || '%' OR details::text LIKE '%pw-secret-xyz%'`, apiKey).Scan(&leaked))
	if leaked != 0 {
		t.Fatalf("%d audit rows contain a secret", leaked)
	}
	_ = row{}
}

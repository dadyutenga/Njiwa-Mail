package test

import (
	"context"
	"net"
	"testing"
)

type verifyDNS struct{}

func (verifyDNS) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	switch name {
	case "example.org", "mailinator.com":
		return []*net.MX{{Host: "mx." + name + ".", Pref: 10}}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (verifyDNS) LookupHost(_ context.Context, name string) ([]string, error) {
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

// Address verification: syntax, MX, disposable and role accounts, and the
// environment's own suppressions. Sends nothing and needs a sending key.
func TestVerifyAddresses(t *testing.T) {
	e := setup(t)
	e.svc.Opts.VerifyResolver = verifyDNS{}
	k := e.provision("alpha")
	e.must(e.svc.Suppress(context.Background(), "alpha", "live", "gone@example.org", "hard_bounce"))

	res := e.do("POST", "/v1/addresses/verify", k.live, "", map[string]any{
		"email":  "Ada@Example.org",
		"emails": []string{"gone@example.org", "x@nowhere.example", "user@mailinator.com", "support@example.org", "bad address"},
	})
	if res.Code != 200 {
		t.Fatalf("%d %s", res.Code, res.Raw)
	}
	want := map[string]string{"ada@example.org": "deliverable", "gone@example.org": "deliverable", "x@nowhere.example": "undeliverable",
		"user@mailinator.com": "risky", "support@example.org": "risky", "bad address": "undeliverable"}
	results := res.JSON["results"].([]any)
	if len(results) != len(want) {
		t.Fatalf("%s", res.Raw)
	}
	for _, r := range results {
		m := r.(map[string]any)
		if want[m["email"].(string)] != m["result"] {
			t.Errorf("%v", m)
		}
		if m["email"] == "gone@example.org" && m["suppressed"] != true {
			t.Errorf("suppression not reported: %v", m)
		}
	}
	// No sending scope, no verification; more than 100 addresses is refused.
	if r := e.do("POST", "/v1/addresses/verify", k.liveRead, "", map[string]any{"email": "a@example.org"}); r.Code != 403 {
		t.Fatalf("read-only key: %d %s", r.Code, r.Raw)
	}
	many := make([]string, 101)
	for i := range many {
		many[i] = "a@example.org"
	}
	if r := e.do("POST", "/v1/addresses/verify", k.live, "", map[string]any{"emails": many}); r.Code != 422 {
		t.Fatalf("101 addresses: %d %s", r.Code, r.Raw)
	}
}

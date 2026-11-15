package verify

import (
	"context"
	"net"
	"strings"
	"testing"
)

type fakeDNS struct {
	mx    map[string][]*net.MX
	hosts map[string][]string
	slow  map[string]bool
	calls int
}

func (f *fakeDNS) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	f.calls++
	if f.slow[name] {
		return nil, &net.DNSError{Err: "i/o timeout", Name: name, IsTimeout: true}
	}
	if m, ok := f.mx[name]; ok {
		return m, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (f *fakeDNS) LookupHost(_ context.Context, name string) ([]string, error) {
	if h, ok := f.hosts[name]; ok {
		return h, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func norm(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndexByte(s, '@'); i < 1 || i == len(s)-1 || strings.ContainsAny(s, " ") {
		return "", false
	}
	return s, true
}

func TestVerify(t *testing.T) {
	dns := &fakeDNS{
		mx: map[string][]*net.MX{
			"example.org":    {{Host: "mx2.example.org.", Pref: 20}, {Host: "mx1.example.org.", Pref: 10}},
			"mailinator.com": {{Host: "mx.mailinator.com.", Pref: 10}},
			"nomail.example": {{Host: ".", Pref: 0}},
		},
		hosts: map[string][]string{"a-only.example": {"192.0.2.1"}},
		slow:  map[string]bool{"slow.example": true},
	}
	v, err := New(dns, norm, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		in, result string
		reasons    string
	}{
		{"Ada@Example.org", Deliverable, ""},
		{"not an address", Undeliverable, "invalid_syntax"},
		{"x@nomail.example", Undeliverable, "null_mx"},
		{"x@missing.example", Undeliverable, "no_mail_server"},
		{"x@a-only.example", Deliverable, ""},
		{"x@slow.example", Unknown, "dns_timeout"},
		{"user@mailinator.com", Risky, "disposable_domain"},
		{"support+eu@example.org", Risky, "role_account"},
		{"info@mailinator.com", Risky, "disposable_domain role_account"},
	} {
		got := v.Verify(context.Background(), c.in)
		if got.Result != c.result || strings.Join(got.Reasons, " ") != c.reasons {
			t.Errorf("%s: %+v", c.in, got)
		}
	}
	if got := v.Verify(context.Background(), "ada@example.org"); strings.Join(got.MX, ",") != "mx1.example.org,mx2.example.org" {
		t.Fatalf("mx %v", got.MX)
	}
	// Cached: another address at a known domain costs no query; a timeout is
	// not cached, so a retry asks DNS again.
	before := dns.calls
	v.Verify(context.Background(), "bob@example.org")
	v.Verify(context.Background(), "y@slow.example")
	if dns.calls != before+1 {
		t.Fatalf("calls %d -> %d", before, dns.calls)
	}
	// Subdomains of a disposable provider are disposable too.
	if !v.IsDisposable("eu.mailinator.com") || v.IsDisposable("example.org") {
		t.Fatal("disposable subdomain")
	}
}

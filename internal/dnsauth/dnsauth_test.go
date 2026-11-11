package dnsauth

import (
	"context"
	"net"
	"strings"
	"testing"
)

type fake struct {
	txt  map[string][]string
	mx   map[string][]string
	a    map[string][]string
	fail map[string]bool // temporary DNS failure for this name
}

func (f fake) err(n string) error {
	if f.fail[n] {
		return &net.DNSError{Err: "timeout", Name: n, IsTemporary: true}
	}
	return &net.DNSError{Err: "no such host", Name: n, IsNotFound: true}
}

func (f fake) LookupTXT(_ context.Context, n string) ([]string, error) {
	if v, ok := f.txt[n]; ok && !f.fail[n] {
		return v, nil
	}
	return nil, f.err(n)
}
func (f fake) LookupMX(_ context.Context, n string) ([]*net.MX, error) {
	if f.fail[n] || f.mx[n] == nil {
		return nil, f.err(n)
	}
	var out []*net.MX
	for _, h := range f.mx[n] {
		out = append(out, &net.MX{Host: h, Pref: 10})
	}
	return out, nil
}
func (f fake) LookupIPAddr(_ context.Context, n string) ([]net.IPAddr, error) {
	if f.fail[n] || f.a[n] == nil {
		return nil, f.err(n)
	}
	var out []net.IPAddr
	for _, s := range f.a[n] {
		out = append(out, net.IPAddr{IP: net.ParseIP(s)})
	}
	return out, nil
}

var world = fake{
	txt: map[string][]string{
		"ex.com":            {"google-site-verification=x", "v=spf1 ip4:203.0.113.0/24 include:_spf.provider.net mx -all"},
		"_spf.provider.net": {"v=spf1 ip4:198.51.100.7 ip6:2001:db8::/32 ~all"},
		"two.com":           {"v=spf1 -all", "v=spf1 a -all"},
		"soft.com":          {"v=spf1 a ~all"},
		"redir.com":         {"v=spf1 redirect=ex.com"},
		"loop.com":          {"v=spf1 include:loop2.com -all"},
		"loop2.com":         {"v=spf1 include:loop.com -all"},
		"many.com":          {"v=spf1 include:i1.com include:i2.com include:i3.com include:i4.com include:i5.com include:i6.com -all"},
		"i1.com":            {"v=spf1 a:x1.com a:x2.com -all"}, "i2.com": {"v=spf1 a:x3.com -all"}, "i3.com": {"v=spf1 a:x4.com -all"},
		"i4.com": {"v=spf1 a:x5.com -all"}, "i5.com": {"v=spf1 a:x6.com -all"}, "i6.com": {"v=spf1 a:x7.com -all"},
		"void.com":  {"v=spf1 a:n1.com a:n2.com a:n3.com -all"},
		"badip.com": {"v=spf1 ip4:300.1.1.1 -all"},
		"macro.com": {"v=spf1 exists:%{i}.rbl.example -all"},
		"tmp.com":   {"v=spf1 include:down.com -all"},
		"cidr.com":  {"v=spf1 a:hosts.cidr.com/28 -all"},
		"noall.com": {"v=spf1 ip4:192.0.2.1"},
	},
	mx: map[string][]string{"ex.com": {"mx1.ex.com."}},
	a: map[string][]string{
		"mx1.ex.com": {"192.0.2.25"}, "soft.com": {"192.0.2.1"}, "hosts.cidr.com": {"192.0.2.17"},
		"x1.com": {"10.0.0.1"}, "x2.com": {"10.0.0.2"}, "x3.com": {"10.0.0.3"}, "x4.com": {"10.0.0.4"}, "x5.com": {"10.0.0.5"},
		"x6.com": {"10.0.0.6"}, "x7.com": {"10.0.0.7"},
	},
	fail: map[string]bool{"down.com": true},
}

func check(t *testing.T, ip, domain string, want Result) SPFOutcome {
	t.Helper()
	o := CheckHost(context.Background(), world, net.ParseIP(ip), domain)
	if o.Result != want {
		t.Fatalf("%s for %s: got %s (%s, matched %q), want %s", ip, domain, o.Result, o.Reason, o.Matched, want)
	}
	return o
}

func TestSPFEvaluation(t *testing.T) {
	check(t, "203.0.113.9", "ex.com", Pass)     // ip4 CIDR
	check(t, "198.51.100.7", "ex.com", Pass)    // through include
	check(t, "2001:db8::25", "ex.com", Pass)    // include ip6 range
	check(t, "192.0.2.25", "ex.com", Pass)      // mx host address
	o := check(t, "192.0.2.99", "ex.com", Fail) // wrong source IP
	if o.Matched != "all" || o.Lookups != 2 {
		t.Fatalf("%+v", o)
	}
	check(t, "192.0.2.1", "soft.com", Pass)
	check(t, "192.0.2.2", "soft.com", SoftFail)
	check(t, "192.0.2.25", "redir.com", Pass) // redirect
	check(t, "192.0.2.17", "cidr.com", Pass)  // a:host/28
	check(t, "192.0.2.33", "cidr.com", Fail)
	check(t, "192.0.2.9", "noall.com", Neutral) // no all
	check(t, "192.0.2.1", "nothing.com", None)
}

func TestSPFErrors(t *testing.T) {
	if o := check(t, "192.0.2.1", "two.com", PermError); !strings.Contains(o.Reason, "more than one") {
		t.Fatal(o.Reason)
	}
	check(t, "192.0.2.1", "loop.com", PermError)
	if o := check(t, "192.0.2.1", "many.com", PermError); !strings.Contains(o.Reason, "10 DNS lookups") {
		t.Fatal(o.Reason)
	}
	if o := check(t, "192.0.2.1", "void.com", PermError); !strings.Contains(o.Reason, "returned nothing") {
		t.Fatal(o.Reason)
	}
	check(t, "192.0.2.1", "badip.com", PermError)
	check(t, "192.0.2.1", "macro.com", PermError)
	check(t, "192.0.2.1", "tmp.com", TempError)
}

func TestSPFProblems(t *testing.T) {
	if len(Problems("v=spf1 ip4:192.0.2.1 +all")) == 0 || len(Problems("v=spf1 all")) == 0 {
		t.Fatal("+all not reported")
	}
	if len(Problems("v=spf1 mx -all")) != 0 || len(Problems("v=spf1 mx ~all")) != 0 {
		t.Fatal("false positive")
	}
	if len(FindSPF([]string{"v=spf10 x", "v=spf1", "V=SPF1 mx -all", "other"})) != 2 {
		t.Fatal("FindSPF")
	}
}

func TestDMARCParse(t *testing.T) {
	d, err := ParseDMARC("v=DMARC1; p=quarantine; sp=reject; adkim=s; pct=50; rua=mailto:a@ex.com,mailto:b@ex.com; fo=1")
	if err != nil || d.Policy != "quarantine" || d.SubPol != "reject" || d.ADKIM != "s" || d.ASPF != "r" || d.Pct != 50 || len(d.RUA) != 2 {
		t.Fatalf("%+v %v", d, err)
	}
	for _, bad := range []string{
		"v=DMARC1", "p=none; v=DMARC1", "v=DMARC1; p=block", "v=DMARC1; p=none; pct=101", "v=DMARC1; p=none; adkim=x",
		"v=DMARC1; p=none; rua=https://ex.com", "v=DMARC1; p=none; p=reject", "v=dmarc1; p=none",
	} {
		if _, err := ParseDMARC(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestDMARCLookupAndAlignment(t *testing.T) {
	r := fake{txt: map[string][]string{
		"_dmarc.example.co.uk": {"v=DMARC1; p=reject; aspf=s"},
		"_dmarc.dup.com":       {"v=DMARC1; p=none", "v=DMARC1; p=reject"},
	}}
	d, err := LookupDMARC(context.Background(), r, "notify.example.co.uk")
	if err != nil || d.FoundAt != "example.co.uk" || d.ASPF != "s" {
		t.Fatalf("org-domain fallback: %+v %v", d, err)
	}
	if _, err := LookupDMARC(context.Background(), r, "none.com"); err != ErrNoDMARC {
		t.Fatal(err)
	}
	if _, err := LookupDMARC(context.Background(), r, "dup.com"); err == nil {
		t.Fatal("two records accepted")
	}
	if OrgDomain("a.b.example.co.uk") != "example.co.uk" {
		t.Fatal(OrgDomain("a.b.example.co.uk"))
	}
	if !Aligned("r", "notify.example.com", "bounce.example.com") || Aligned("s", "notify.example.com", "bounce.example.com") ||
		!Aligned("s", "Notify.Example.com.", "notify.example.com") || Aligned("r", "example.com", "example.net") {
		t.Fatal("alignment")
	}
}

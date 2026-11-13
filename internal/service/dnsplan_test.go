package service

import (
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/dnsprovider"
)

func rec(typ, name, content string, extra ...any) dnsprovider.Record {
	r := dnsprovider.Record{ID: "id-" + typ + "-" + name + "-" + content, Type: typ, Name: name, Content: content, TTL: 300, Modified: "m1"}
	for _, e := range extra {
		switch v := e.(type) {
		case int:
			r.Priority = v
		case bool:
			r.Proxied = v
		}
	}
	return r
}

const (
	pkA = "v=DKIM1; k=rsa; p=MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQC7vbqajDw4o6gJy8UtmIbkcpnkO3Kwc4qsEnSZp/TR+fQi62F79RHWmwKOtFmwteURgLbj7D/WGuNLGOfa/2vse3G2eHnHl5CB8ruRX9fBl/KgwCVr2JaEuUm66bB0zQkFplKhGZJaDgFD1PLxx5uAz0b6v1AQqoRo5Vcb9Vl3gRPq0wIDAQAB"
)

func baseIn(existing ...dnsprovider.Record) PlanInput {
	return PlanInput{Domain: "notify.example.com", Purpose: "both", ZoneID: "z1", ZoneName: "example.com",
		ZoneNS: []string{"ada.ns.cloudflare.com", "bob.ns.cloudflare.com"}, ObservedNS: []string{"ada.ns.cloudflare.com", "bob.ns.cloudflare.com"},
		MailHost: "mail.example.com", DNSSEC: "disabled", Existing: existing,
		Required: []DomainRecord{
			{Key: "ownership", Type: "TXT", Host: "notify.example.com", Value: "njiwa-mail-verify=tok"},
			{Key: "spf", Type: "TXT", Host: "notify.example.com", Value: "v=spf1 mx ip4:203.0.113.10 -all"},
			{Key: "dkim", Type: "TXT", Host: "dkim._domainkey.notify.example.com", Value: pkA},
			{Key: "dmarc", Type: "TXT", Host: "_dmarc.notify.example.com", Value: "v=DMARC1; p=none; rua=mailto:postmaster@notify.example.com"},
			{Key: "mx", Type: "MX", Host: "notify.example.com", Value: "mail.example.com", Priority: 10},
		}}
}

func item(p DNSPlan, key string) PlanItem {
	for _, it := range p.Items {
		if it.Key == key {
			return it
		}
	}
	return PlanItem{}
}

func warnCodes(p DNSPlan) string {
	var c []string
	for _, w := range p.Warnings {
		c = append(c, w.Code)
	}
	return strings.Join(c, ",")
}

func TestPlanCleanZoneCreatesEverythingAndNeverDeletes(t *testing.T) {
	p := BuildDNSPlan(baseIn())
	if p.Blocked || p.Actionable() != 5 {
		t.Fatalf("blocked=%v actionable=%d warnings=%s", p.Blocked, p.Actionable(), warnCodes(p))
	}
	for _, it := range p.Items {
		if it.Action != PlanCreate {
			t.Errorf("%s: %s", it.Key, it.Action)
		}
		if it.TTL != 0 {
			t.Errorf("%s: a created record must use the provider's automatic TTL, got %d", it.Key, it.TTL)
		}
	}
	if len(p.RequiredAcks) != 1 || p.RequiredAcks[0] != AckCreateMX {
		t.Errorf("creating an MX must need confirmation: %v", p.RequiredAcks)
	}
	if p.Digest == "" {
		t.Error("no digest")
	}
}

func TestPlanIsDeterministicAndDigestSeesOutsideChanges(t *testing.T) {
	a := BuildDNSPlan(baseIn(rec("A", "notify.example.com", "198.51.100.5")))
	b := BuildDNSPlan(baseIn(rec("A", "notify.example.com", "198.51.100.5")))
	if a.Digest != b.Digest {
		t.Fatal("digest must be stable")
	}
	changed := rec("A", "notify.example.com", "198.51.100.5")
	changed.Modified = "m2"
	if c := BuildDNSPlan(baseIn(changed)); c.Digest == a.Digest {
		t.Fatal("a change to a record at a touched name must change the digest")
	}
	// Records at unrelated names do not.
	d := BuildDNSPlan(baseIn(rec("A", "notify.example.com", "198.51.100.5"), rec("A", "www.example.com", "1.2.3.4")))
	if d.Digest != a.Digest {
		t.Fatal("an unrelated record must not change the digest")
	}
}

func TestPlanExistingMXIsNeverReplaced(t *testing.T) {
	p := BuildDNSPlan(baseIn(rec("MX", "notify.example.com", "aspmx.l.google.com", 1)))
	mx := item(p, "mx")
	if mx.Action != PlanConflict || mx.Risk != "high" || len(mx.Others) != 1 {
		t.Fatalf("%+v", mx)
	}
	if !strings.Contains(warnCodes(p), "existing_mail") {
		t.Errorf("existing mail must be flagged: %s", warnCodes(p))
	}
	if len(p.Infra) != 1 || p.Infra[0] != "Google Workspace" {
		t.Errorf("infra: %v", p.Infra)
	}
	for _, it := range p.Items {
		if it.Action == PlanCreate && it.Type == "MX" {
			t.Fatal("an MX was planned over an existing one")
		}
	}
}

func TestPlanSameMXIsUnchanged(t *testing.T) {
	p := BuildDNSPlan(baseIn(rec("MX", "notify.example.com", "mail.example.com", 10)))
	if it := item(p, "mx"); it.Action != PlanUnchanged || it.Existing == nil {
		t.Fatalf("%+v", it)
	}
	if len(p.RequiredAcks) != 0 {
		t.Errorf("no MX is created, so no MX confirmation: %v", p.RequiredAcks)
	}
}

func TestPlanSPFMergesIntoTheOneExistingRecordAndKeepsTTL(t *testing.T) {
	p := BuildDNSPlan(baseIn(rec("TXT", "notify.example.com", "v=spf1 include:_spf.google.com ~all")))
	spf := item(p, "spf")
	if spf.Action != PlanModify || spf.Ack != AckModifySPF || spf.Risk != "high" {
		t.Fatalf("%+v", spf)
	}
	if spf.Content != "v=spf1 include:_spf.google.com mx ip4:203.0.113.10 ~all" {
		t.Errorf("merged: %q", spf.Content)
	}
	if spf.TTL != 300 || spf.Existing == nil {
		t.Errorf("the existing TTL must be kept: %+v", spf)
	}
	creates := 0
	for _, it := range p.Items {
		if it.Key == "spf" && it.Action == PlanCreate {
			creates++
		}
	}
	if creates != 0 {
		t.Fatal("a second SPF record was planned")
	}
	if !strings.Contains(strings.Join(p.RequiredAcks, ","), AckModifySPF) {
		t.Errorf("modifying SPF must need confirmation: %v", p.RequiredAcks)
	}
}

func TestPlanSPFAlreadyCoveredIsUnchanged(t *testing.T) {
	p := BuildDNSPlan(baseIn(rec("TXT", "notify.example.com", "v=spf1 ip4:203.0.113.10 mx -all")))
	if it := item(p, "spf"); it.Action != PlanUnchanged {
		t.Fatalf("%+v", it)
	}
}

func TestPlanSPFUnsafeShapesAreConflicts(t *testing.T) {
	for name, existing := range map[string]string{
		"redirect": "v=spf1 redirect=_spf.example.net",
		"plus-all": "v=spf1 include:x.example +all",
		"bare-all": "v=spf1 all",
	} {
		p := BuildDNSPlan(baseIn(rec("TXT", "notify.example.com", existing)))
		if it := item(p, "spf"); it.Action != PlanConflict {
			t.Errorf("%s: %+v", name, it)
		}
	}
	two := BuildDNSPlan(baseIn(rec("TXT", "notify.example.com", "v=spf1 -all"), rec("TXT", "notify.example.com", "v=spf1 include:a.example -all")))
	if it := item(two, "spf"); it.Action != PlanConflict || len(it.Others) != 2 {
		t.Fatalf("two SPF records: %+v", it)
	}
}

func TestPlanDKIMNeverOverwritesADifferentKey(t *testing.T) {
	other := "v=DKIM1; k=rsa; p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAx"
	p := BuildDNSPlan(baseIn(rec("TXT", "dkim._domainkey.notify.example.com", other)))
	if it := item(p, "dkim"); it.Action != PlanConflict || it.Risk != "high" {
		t.Fatalf("%+v", it)
	}
	same := BuildDNSPlan(baseIn(rec("TXT", "dkim._domainkey.notify.example.com", pkA)))
	if it := item(same, "dkim"); it.Action != PlanUnchanged {
		t.Fatalf("%+v", it)
	}
	// An unrelated selector at the same domain is irrelevant.
	un := BuildDNSPlan(baseIn(rec("TXT", "google._domainkey.notify.example.com", other)))
	if it := item(un, "dkim"); it.Action != PlanCreate {
		t.Fatalf("%+v", it)
	}
}

func TestPlanDMARCIsNeverOverwrittenOrWeakened(t *testing.T) {
	p := BuildDNSPlan(baseIn(rec("TXT", "_dmarc.notify.example.com", "v=DMARC1; p=reject")))
	it := item(p, "dmarc")
	if it.Action != PlanUnchanged || !strings.Contains(it.Reason, "p=reject") {
		t.Fatalf("%+v", it)
	}
	// A subdomain inherits a stricter organisational policy: adding p=none would weaken it.
	inh := BuildDNSPlan(baseIn(rec("TXT", "_dmarc.example.com", "v=DMARC1; p=quarantine; sp=quarantine")))
	if it := item(inh, "dmarc"); it.Action != PlanUnchanged || !strings.Contains(it.Reason, "inherits") {
		t.Fatalf("%+v", it)
	}
}

func TestPlanCNAMEAtANameIsAConflictAndIsNeverTouched(t *testing.T) {
	p := BuildDNSPlan(baseIn(rec("CNAME", "notify.example.com", "site.example.net")))
	for _, k := range []string{"ownership", "spf", "mx"} {
		if it := item(p, k); it.Action != PlanConflict {
			t.Errorf("%s: %+v", k, it)
		}
	}
	for _, it := range p.Items {
		if it.Type == "CNAME" || it.Type == "A" || it.Type == "AAAA" {
			t.Errorf("a web record type was planned: %+v", it)
		}
	}
}

func TestPlanBlockers(t *testing.T) {
	t.Run("zone not delegated to Cloudflare", func(t *testing.T) {
		in := baseIn()
		in.ObservedNS = []string{"ns1.otherhost.example", "ns2.otherhost.example"}
		p := BuildDNSPlan(in)
		if !p.Blocked || !strings.Contains(warnCodes(p), "not_delegated") {
			t.Fatalf("%v %s", p.Blocked, warnCodes(p))
		}
	})
	t.Run("nameservers cannot be confirmed", func(t *testing.T) {
		in := baseIn()
		in.ObservedNS, in.NSLookupFail = nil, true
		if p := BuildDNSPlan(in); !p.Blocked || !strings.Contains(warnCodes(p), "ns_unconfirmed") {
			t.Fatalf("%v %s", p.Blocked, warnCodes(p))
		}
	})
	t.Run("domain outside the zone", func(t *testing.T) {
		in := baseIn()
		in.ZoneName = "other.org"
		if p := BuildDNSPlan(in); !p.Blocked || !strings.Contains(warnCodes(p), "domain_not_in_zone") {
			t.Fatalf("%v %s", p.Blocked, warnCodes(p))
		}
	})
	t.Run("a zone name must match on a label boundary", func(t *testing.T) {
		in := baseIn()
		in.Domain, in.ZoneName = "notmyexample.com", "example.com"
		if p := BuildDNSPlan(in); !p.Blocked {
			t.Fatal("notmyexample.com is not inside example.com")
		}
	})
	t.Run("delegated sub-zone", func(t *testing.T) {
		p := BuildDNSPlan(baseIn(rec("NS", "notify.example.com", "ns1.elsewhere.example")))
		if !p.Blocked || !strings.Contains(warnCodes(p), "delegated_subzone") {
			t.Fatalf("%v %s", p.Blocked, warnCodes(p))
		}
	})
}

func TestPlanWarningsAndInfra(t *testing.T) {
	in := baseIn(
		rec("MX", "example.com", "example-com.mail.protection.outlook.com", 0),
		rec("TXT", "example.com", "v=spf1 include:spf.protection.outlook.com -all"),
		rec("CAA", "example.com", `0 issue "letsencrypt.org"`),
		rec("A", "mail.example.com", "203.0.113.10", true),
	)
	in.Domain = "example.com"
	in.DNSSEC = "active"
	in.Required[0].Host, in.Required[1].Host, in.Required[4].Host = "example.com", "example.com", "example.com"
	in.Required[2].Host = "dkim._domainkey.example.com"
	in.Required[3].Host = "_dmarc.example.com"
	p := BuildDNSPlan(in)
	codes := warnCodes(p)
	for _, want := range []string{"existing_mail", "use_subdomain", "dnssec_active", "caa_present", "mail_host_proxied"} {
		if !strings.Contains(codes, want) {
			t.Errorf("missing warning %s in %s", want, codes)
		}
	}
	if len(p.Infra) != 1 || p.Infra[0] != "Microsoft 365" {
		t.Errorf("infra: %v", p.Infra)
	}
	for _, it := range p.Items {
		if it.Type == "A" {
			t.Fatal("the proxied A record must be reported, never changed")
		}
	}
}

func TestPlanPlaceholderAndOutOfZoneAreManual(t *testing.T) {
	in := baseIn()
	in.Required[1].Placeholder, in.Required[1].Value = true, "v=spf1 <your server> -all"
	in.Required[2].Placeholder, in.Required[2].Value = true, "v=DKIM1; k=rsa; p=<key>"
	in.Required = append(in.Required, DomainRecord{Key: "return_path", Type: "MX", Host: "bounces.other.org", Value: "mail.example.com", Priority: 10})
	p := BuildDNSPlan(in)
	for _, k := range []string{"spf", "dkim", "return_path"} {
		if it := item(p, k); it.Action != PlanManual {
			t.Errorf("%s: %+v", k, it)
		}
	}
	if p.Actionable() != 3 { // ownership, dmarc, mx
		t.Errorf("actionable %d", p.Actionable())
	}
}

func TestMergeSPF(t *testing.T) {
	cases := []struct {
		in, want, out string
		ok            bool
	}{
		{"v=spf1 -all", "v=spf1 ip4:1.2.3.4 -all", "v=spf1 ip4:1.2.3.4 -all", true},
		{"v=spf1 ip4:1.2.3.4 -all", "v=spf1 ip4:1.2.3.4 -all", "v=spf1 ip4:1.2.3.4 -all", true},
		{"v=spf1 include:a.example", "v=spf1 mx -all", "v=spf1 include:a.example mx", true},
		{"V=SPF1 IP4:1.2.3.4 ~ALL", "v=spf1 ip4:1.2.3.4 -all", "V=SPF1 IP4:1.2.3.4 ~ALL", true},
		{"v=spf1 redirect=x.example", "v=spf1 mx -all", "", false},
		{"v=spf1 +all", "v=spf1 mx -all", "", false},
	}
	for _, c := range cases {
		got, _, ok, _ := mergeSPF(c.in, c.want)
		if ok != c.ok || (ok && got != c.out) {
			t.Errorf("mergeSPF(%q, %q) = %q, %v; want %q, %v", c.in, c.want, got, ok, c.out, c.ok)
		}
	}
}

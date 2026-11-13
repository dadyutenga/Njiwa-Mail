package managed

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRun struct {
	calls []string
	fail  string            // command prefix that fails
	out   map[string]string // command prefix -> output
}

func (f *fakeRun) Run(name string, args ...string) (string, error) {
	c := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, c)
	if f.fail != "" && strings.HasPrefix(c, f.fail) {
		return "simulated failure", errors.New("exit status 1")
	}
	if c == "postfix status" {
		return "", errors.New("not running")
	}
	for k, v := range f.out {
		if strings.HasPrefix(c, k) {
			return v, nil
		}
	}
	return "", nil
}

func params(t *testing.T) Params {
	root := t.TempDir()
	// The packages are installed (a separate test covers installing them).
	for _, b := range []string{"/usr/sbin/postfix", "/usr/sbin/opendkim"} {
		if err := writeFileAtomic(filepath.Join(root, b), nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return Params{Hostname: "MailOut.Example.com.", BounceDomain: "bounce.example.com", Domains: []string{"notify.example.com", "Notify.Example.com", "other.example.org"},
		Selector: "njiwa", SubmissionBind: "127.0.0.1:10587", InboundRelay: "127.0.0.1:2525", Root: root, StateDir: "/var/lib/njiwa-mail/installer", Now: now}
}

var now = time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)

func read(t *testing.T, p Params, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(p.Root, path))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPlanIsDeterministicAndSafe(t *testing.T) {
	p := params(t)
	a, err := BuildPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := BuildPlan(p)
	if a.Digest != b.Digest || a.Conflict != "" {
		t.Fatalf("digest %s vs %s, conflict %q", a.Digest, b.Digest, a.Conflict)
	}
	if got := a.Params.Domains; len(got) != 2 || got[0] != "notify.example.com" {
		t.Fatalf("domains not normalised: %v", got)
	}
	var main, master string
	for _, f := range a.Files {
		switch f.Path {
		case "/etc/postfix/main.cf":
			main = string(f.Content)
		case "/etc/postfix/master.cf":
			master = string(f.Content)
		}
	}
	// No open relay: relaying is never granted by network membership on port 25.
	for _, want := range []string{"smtpd_relay_restrictions = reject_unauth_destination", "mydestination =\n", "recipient_delimiter = +", "relay_domains = bounce.example.com",
		"local_transport = error:", "njiwa_submission_relay_restrictions = permit_tls_clientcerts, reject"} {
		if !strings.Contains(main, want) {
			t.Errorf("main.cf lacks %q", want)
		}
	}
	if strings.Contains(main, "permit_mynetworks") {
		t.Error("main.cf trusts mynetworks for relay")
	}
	for _, want := range []string{"127.0.0.1:10587 inet", "smtpd_tls_req_ccert=yes", "smtpd_tls_security_level=encrypt", "milter_default_action=tempfail"} {
		if !strings.Contains(master, want) {
			t.Errorf("master.cf lacks %q", want)
		}
	}
	for _, bad := range []Params{
		{Hostname: "", Domains: []string{"a.example"}},
		{Hostname: "m.example.com", Domains: nil},
		{Hostname: "m.example.com", Domains: []string{"a.example"}, SubmissionBind: "0.0.0.0:10587"},
		{Hostname: "m.example.com", Domains: []string{"a.example"}, BounceDomain: "b.example"},
		{Hostname: "m.example.com", Domains: []string{"bad domain"}},
	} {
		bad.Root = t.TempDir()
		if _, err := BuildPlan(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestApplyNeedsTheApprovedDigestAndIsIdempotent(t *testing.T) {
	p := params(t)
	run := &fakeRun{}
	if _, err := Apply(p, "deadbeef", run, now); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("unapproved plan applied: %v", err)
	}
	pl, _ := BuildPlan(p)
	rc, err := Apply(p, pl.Digest, run, now)
	if err != nil {
		t.Fatal(err)
	}
	if rc.Result != "ok" || len(rc.DKIMRecords) != 2 || !strings.HasPrefix(rc.DKIMRecords["notify.example.com"], "v=DKIM1; k=rsa; p=") {
		t.Fatalf("%+v", rc)
	}
	if !strings.Contains(read(t, p, "/etc/postfix/njiwa/relay_clientcerts"), rc.ClientFingerprint+" njiwa-worker") {
		t.Fatal("client fingerprint not authorised")
	}
	for path, mode := range map[string]os.FileMode{"/etc/opendkim/keys/notify.example.com/njiwa.private": 0o600,
		"/etc/njiwa-mail/submission/client.key": 0o600, "/etc/postfix/njiwa/ca.key": 0o600} {
		st, err := os.Stat(filepath.Join(p.Root, path))
		if err != nil || st.Mode().Perm() != mode {
			t.Fatalf("%s: %v %v", path, err, st)
		}
	}
	for path, mode := range map[string]os.FileMode{"/etc/opendkim": 0o755, "/etc/opendkim/keys": 0o750, "/etc/opendkim/keys/notify.example.com": 0o750} {
		st, err := os.Stat(filepath.Join(p.Root, path))
		if err != nil || st.Mode().Perm() != mode {
			t.Fatalf("%s must be %o so the signer can reach its key: %v %v", path, mode, err, st.Mode().Perm())
		}
	}
	if !strings.Contains(read(t, p, "/etc/opendkim/njiwa/signing.table"), "*@notify.example.com njiwa._domainkey.notify.example.com") {
		t.Fatal("signing table")
	}
	// A second run changes nothing and keeps the same keys.
	pl2, _ := BuildPlan(p)
	for _, f := range pl2.Files {
		if f.Change != "unchanged" {
			t.Fatalf("re-run would change %s", f.Path)
		}
	}
	for _, a := range pl2.Actions {
		if a.Needed {
			t.Fatalf("re-run would regenerate %s", a.Target)
		}
	}
	rc2, err := Apply(p, pl2.Digest, run, now.Add(time.Minute))
	if err != nil || rc2.DKIMRecords["notify.example.com"] != rc.DKIMRecords["notify.example.com"] || rc2.ClientFingerprint != rc.ClientFingerprint {
		t.Fatalf("re-run changed keys: %v", err)
	}
}

func TestFailedValidationRollsBackAndForeignConfigIsRefused(t *testing.T) {
	p := params(t)
	// An existing Postfix the installer did not write.
	foreign := "myhostname = old.example\n"
	if err := writeFileAtomic(filepath.Join(p.Root, "/etc/postfix/main.cf"), []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	pl, _ := BuildPlan(p)
	if pl.Conflict == "" {
		t.Fatal("foreign Postfix not detected")
	}
	if _, err := Apply(p, pl.Digest, &fakeRun{}, now); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("foreign config replaced without -adopt: %v", err)
	}
	p.Adopt = true
	pl, _ = BuildPlan(p)
	rc, err := Apply(p, pl.Digest, &fakeRun{fail: "postfix check"}, now)
	if err == nil || rc.Result != "failed_rolled_back" {
		t.Fatalf("validation failure not rolled back: %v %+v", err, rc)
	}
	if got := read(t, p, "/etc/postfix/main.cf"); got != foreign {
		t.Fatalf("main.cf not restored: %q", got)
	}
	if _, err := os.Stat(filepath.Join(p.Root, "/etc/opendkim.conf")); !os.IsNotExist(err) {
		t.Fatal("a file created by the failed apply was left behind")
	}
	// Manual rollback of a successful apply.
	pl, _ = BuildPlan(p)
	if _, err := Apply(p, pl.Digest, &fakeRun{}, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := Rollback(p, "", &fakeRun{}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p, "/etc/postfix/main.cf"); got != foreign {
		t.Fatalf("rollback did not restore: %q", got)
	}
	if _, err := Rollback(p, "", &fakeRun{}); err == nil {
		t.Fatal("double rollback allowed")
	}
}

func TestFirewallTableIsSafeAndScoped(t *testing.T) {
	p := params(t)
	p.SubmissionBind = "172.30.0.1:10587"
	pl, err := BuildPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	var rules string
	for _, f := range pl.Files {
		if f.Path == FirewallFile {
			rules = string(f.Content)
		}
	}
	for _, want := range []string{"policy accept;", `iif "lo" accept`, "tcp dport 10587 ip saddr 172.30.0.0/24 accept",
		"tcp dport { 10587, 2525, 5432, 8080, 3000, 8891 } counter drop", "delete table inet njiwa_mail"} {
		if !strings.Contains(rules, want) {
			t.Errorf("firewall lacks %q:\n%s", want, rules)
		}
	}
	for _, never := range []string{"policy drop", "dport 22", "dport 25 ", "dport { 25"} {
		if strings.Contains(rules, never) {
			t.Errorf("firewall must not contain %q", never)
		}
	}
	// Loopback-only submission: no subnet rule.
	p2 := params(t)
	pl2, _ := BuildPlan(p2)
	for _, f := range pl2.Files {
		if f.Path == FirewallFile && strings.Contains(string(f.Content), "saddr") {
			t.Fatal("loopback submission opened to a network")
		}
	}
	run := &fakeRun{}
	if _, err := Apply(p2, pl2.Digest, run, now); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(run.calls, "\n")
	if !strings.Contains(joined, "nft -c -f "+filepath.Join(p2.Root, FirewallFile)) || !strings.Contains(joined, "nft -f "+filepath.Join(p2.Root, FirewallFile)) {
		t.Fatalf("firewall not validated and loaded: %s", joined)
	}
	// A failing rules check rolls back like any other validation failure.
	p3 := params(t)
	pl3, _ := BuildPlan(p3)
	if _, err := Apply(p3, pl3.Digest, &fakeRun{fail: "nft -c"}, now); err == nil {
		t.Fatal("invalid firewall rules applied")
	}
	if _, err := os.Stat(filepath.Join(p3.Root, FirewallFile)); !os.IsNotExist(err) {
		t.Fatal("firewall file left behind after rollback")
	}
	// -no-firewall: no table, and nothing loaded.
	p4 := params(t)
	p4.NoFirewall = true
	pl4, _ := BuildPlan(p4)
	for _, f := range pl4.Files {
		if f.Path == FirewallFile {
			t.Fatal("firewall planned with NoFirewall")
		}
	}
}

type txtMap map[string][]string

func (m txtMap) LookupTXT(_ context.Context, n string) ([]string, error) { return m[n], nil }

func TestStagedDKIMRotation(t *testing.T) {
	p := params(t) // selector "njiwa"
	pl, _ := BuildPlan(p)
	rc, err := Apply(p, pl.Digest, &fakeRun{}, now)
	if err != nil {
		t.Fatal(err)
	}
	old := rc.DKIMRecords["notify.example.com"]

	if _, err := PrepareRotation(p, "njiwa", now); err == nil {
		t.Fatal("rotating to the current selector allowed")
	}
	rot, err := PrepareRotation(p, "njiwa2", now)
	if err != nil {
		t.Fatal(err)
	}
	if rot.From != "njiwa" || len(rot.Records) != 2 || rot.Records["notify.example.com"] == old {
		t.Fatalf("%+v", rot)
	}
	// Prepare changes nothing about signing.
	if cur, _ := CurrentSelector(p); cur != "njiwa" {
		t.Fatalf("prepare switched signing to %s", cur)
	}
	again, _ := PrepareRotation(p, "njiwa2", now.Add(time.Minute))
	if again.Records["notify.example.com"] != rot.Records["notify.example.com"] {
		t.Fatal("re-running prepare replaced the new key")
	}

	dns := txtMap{}
	if probs := RotationDNSProblems(context.Background(), dns, rot); len(probs) != 2 {
		t.Fatalf("unpublished keys: %v", probs)
	}
	dns["njiwa2._domainkey.notify.example.com"] = []string{rot.Records["notify.example.com"]}
	dns["njiwa2._domainkey.other.example.org"] = []string{old} // the wrong key
	if probs := RotationDNSProblems(context.Background(), dns, rot); len(probs) != 1 || !strings.Contains(probs[0], "other.example.org") {
		t.Fatalf("wrong key not caught: %v", probs)
	}
	dns["njiwa2._domainkey.other.example.org"] = []string{rot.Records["other.example.org"]}
	if probs := RotationDNSProblems(context.Background(), dns, rot); len(probs) != 0 {
		t.Fatalf("published keys: %v", probs)
	}

	// Activation is a normal approved apply with the new selector.
	q := p
	q.Selector = "njiwa2"
	pl2, _ := BuildPlan(q)
	rc2, err := Apply(q, pl2.Digest, &fakeRun{}, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if rc2.DKIMRecords["notify.example.com"] != rot.Records["notify.example.com"] {
		t.Fatal("activation did not use the prepared key")
	}
	if cur, _ := CurrentSelector(q); cur != "njiwa2" {
		t.Fatalf("signing selector after activation: %s", cur)
	}
	e := FinishRotation(q)
	if e != nil {
		t.Fatal(e)
	}
	if _, err := RetireSelector(q, "njiwa2"); err == nil {
		t.Fatal("retired the signing selector")
	}
	removed, err := RetireSelector(q, "njiwa")
	if err != nil || len(removed) != 2 {
		t.Fatalf("retire old selector: %v %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(p.Root, "/etc/opendkim/keys/notify.example.com/njiwa.private")); !os.IsNotExist(err) {
		t.Fatal("old key still on disk")
	}
	if _, err := os.Stat(filepath.Join(p.Root, "/etc/opendkim/keys/notify.example.com/njiwa2.private")); err != nil {
		t.Fatal("new key removed")
	}
}

func TestCancelRotation(t *testing.T) {
	p := params(t)
	pl, _ := BuildPlan(p)
	if _, err := Apply(p, pl.Digest, &fakeRun{}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareRotation(p, "next", now); err != nil {
		t.Fatal(err)
	}
	if _, err := CancelRotation(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.Root, "/etc/opendkim/keys/notify.example.com/next.private")); !os.IsNotExist(err) {
		t.Fatal("cancelled keys left behind")
	}
	if _, err := LoadRotation(p); err == nil {
		t.Fatal("rotation state left behind")
	}
	if cur, _ := CurrentSelector(p); cur != "njiwa" {
		t.Fatal("cancel changed signing")
	}
}

// status, retire and cancel are called without a state directory; they must
// use the default one, not the filesystem root.
func TestRotationUsesDefaultStateDir(t *testing.T) {
	p := params(t)
	pl, _ := BuildPlan(p)
	if _, err := Apply(p, pl.Digest, &fakeRun{}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareRotation(p, "next", now); err != nil {
		t.Fatal(err)
	}
	bare := Params{Root: p.Root} // as the CLI passes it for cancel/status
	if _, err := LoadRotation(bare); err != nil {
		t.Fatal(err)
	}
	if _, err := CancelRotation(bare); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.Root, "/var/lib/njiwa-mail/installer/rotation.json")); !os.IsNotExist(err) {
		t.Fatal("rotation state not removed from the default state directory")
	}
	run := &fakeRun{}
	if _, err := Apply(p, func() string { q, _ := BuildPlan(p); return q.Digest }(), run, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return // with systemd the unit is restarted instead
	}
	if !strings.Contains(strings.Join(run.calls, "\n"), "opendkim -x "+filepath.Join(p.Root, "/etc/opendkim.conf")) {
		t.Fatalf("signer not restarted without systemd: %v", run.calls)
	}
}

// E11: leaf certificates are renewed by a normal plan/apply within
// RenewBefore of expiry; the replaced client certificate stays accepted for a
// grace period so a running worker is not cut off.
func TestCertificateRenewal(t *testing.T) {
	p := params(t)
	run := &fakeRun{}
	pl, _ := BuildPlan(p)
	rc1, err := Apply(p, pl.Digest, run, now)
	if err != nil {
		t.Fatal(err)
	}
	server1 := read(t, p, "/etc/postfix/njiwa/server.crt")
	ca1 := read(t, p, "/etc/postfix/njiwa/ca.crt")

	// Not due yet: nothing to renew.
	p.Now = now.AddDate(1, 0, 0)
	pl, _ = BuildPlan(p)
	for _, a := range pl.Actions {
		if strings.HasPrefix(a.Kind, "renew_") {
			t.Fatalf("renewal planned a year in: %+v", a)
		}
	}
	// 30 days before expiry: both leaves are renewed, the CA is kept.
	exp, _ := CertExpiry(filepath.Join(p.Root, "/etc/njiwa-mail/submission/client.crt"))
	later := exp.Add(-30 * 24 * time.Hour)
	p.Now = later
	pl, _ = BuildPlan(p)
	kinds := map[string]bool{}
	for _, a := range pl.Actions {
		kinds[a.Kind] = a.Needed
	}
	if !kinds["renew_server_certificate"] || !kinds["renew_client_certificate"] || !strings.Contains(pl.Summary(), "renew") {
		t.Fatalf("renewal not planned: %+v", pl.Actions)
	}
	rc2, err := Apply(p, pl.Digest, run, later)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, p, "/etc/postfix/njiwa/server.crt") == server1 || read(t, p, "/etc/postfix/njiwa/ca.crt") != ca1 {
		t.Fatal("server certificate not renewed, or CA replaced")
	}
	if rc2.ClientFingerprint == rc1.ClientFingerprint {
		t.Fatal("client certificate not renewed")
	}
	if e2, _ := CertExpiry(filepath.Join(p.Root, "/etc/njiwa-mail/submission/client.crt")); !e2.After(later.AddDate(4, 0, 0)) {
		t.Fatalf("renewed certificate expires %s", e2)
	}
	relay := read(t, p, "/etc/postfix/njiwa/relay_clientcerts")
	if !strings.Contains(relay, rc2.ClientFingerprint+" njiwa-worker\n") || !strings.Contains(relay, rc1.ClientFingerprint+" njiwa-worker-previous") {
		t.Fatalf("both client certificates must be accepted during the grace period:\n%s", relay)
	}
	// After the grace period the previous certificate is dropped.
	p.Now = later.Add(PreviousClientGrace + time.Hour)
	pl, _ = BuildPlan(p)
	if _, err := Apply(p, pl.Digest, run, p.Now); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, p, "/etc/postfix/njiwa/relay_clientcerts"), rc1.ClientFingerprint) {
		t.Fatal("previous client certificate still accepted after the grace period")
	}
	// Forced renewal works any time.
	p.RenewCerts = true
	if pl, _ = BuildPlan(p); !strings.Contains(pl.Summary(), "renew") {
		t.Fatal("forced renewal not planned")
	}
}

func TestCAExpiryWarning(t *testing.T) {
	p := params(t)
	pl, _ := BuildPlan(p)
	if _, err := Apply(p, pl.Digest, &fakeRun{}, now); err != nil {
		t.Fatal(err)
	}
	if pl, _ = BuildPlan(p); len(pl.Warnings) != 0 {
		t.Fatalf("%v", pl.Warnings)
	}
	p.Now = now.AddDate(9, 6, 0)
	if pl, _ = BuildPlan(p); len(pl.Warnings) != 1 || !strings.Contains(pl.Summary(), "WARNING: the local submission CA expires") {
		t.Fatalf("%v", pl.Warnings)
	}
}

// Replacing the CA reissues both leaf certificates from the new CA.
func TestNewCAReissuesLeaves(t *testing.T) {
	p := params(t)
	pl, _ := BuildPlan(p)
	if _, err := Apply(p, pl.Digest, &fakeRun{}, now); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"ca.crt", "ca.key"} {
		if err := os.Rename(filepath.Join(p.Root, "/etc/postfix/njiwa", f), filepath.Join(p.Root, "/etc/postfix/njiwa", f+".old")); err != nil {
			t.Fatal(err)
		}
	}
	pl, _ = BuildPlan(p)
	if _, err := Apply(p, pl.Digest, &fakeRun{}, now); err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM([]byte(read(t, p, "/etc/postfix/njiwa/ca.crt")))
	for _, c := range []struct {
		path  string
		usage x509.ExtKeyUsage
	}{{"/etc/postfix/njiwa/server.crt", x509.ExtKeyUsageServerAuth}, {"/etc/njiwa-mail/submission/client.crt", x509.ExtKeyUsageClientAuth}} {
		blk, _ := pem.Decode([]byte(read(t, p, c.path)))
		cert, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{c.usage}}); err != nil {
			t.Fatalf("%s not issued by the new CA: %v", c.path, err)
		}
	}
	if read(t, p, "/etc/njiwa-mail/submission/ca.crt") != read(t, p, "/etc/postfix/njiwa/ca.crt") {
		t.Fatal("worker CA copy not updated")
	}
}

// D04: several bounce domains (the installation's and per-domain return paths)
// are all relayed, all allowed as envelope senders, and the plan digest does
// not depend on their order or spelling.
func TestSeveralBounceDomains(t *testing.T) {
	p := params(t)
	p.BounceDomain = "Bounce.Acme.co.tz., bounce.example.com ,bounce.example.com"
	a, err := BuildPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	p2 := params(t)
	p2.BounceDomain = "bounce.example.com,bounce.acme.co.tz"
	b, err := BuildPlan(p2)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("order or spelling changed the digest: %s vs %s", a.Digest, b.Digest)
	}
	files := map[string]string{}
	for _, f := range a.Files {
		files[f.Path] = string(f.Content)
	}
	if !strings.Contains(files["/etc/postfix/main.cf"], "relay_domains = bounce.acme.co.tz, bounce.example.com\n") {
		t.Fatalf("relay_domains: %s", files["/etc/postfix/main.cf"])
	}
	for path, content := range files {
		if strings.HasSuffix(path, "/transport") {
			for _, d := range []string{"bounce.acme.co.tz smtp:[127.0.0.1]:2525", "bounce.example.com smtp:[127.0.0.1]:2525"} {
				if !strings.Contains(content, d) {
					t.Errorf("transport map lacks %q:\n%s", d, content)
				}
			}
		}
		if strings.HasSuffix(path, "/senders") {
			for _, d := range []string{"bounce.acme.co.tz OK", "bounce.example.com OK"} {
				if !strings.Contains(content, d) {
					t.Errorf("sender map lacks %q:\n%s", d, content)
				}
			}
		}
	}
	p3 := params(t)
	p3.BounceDomain = "bad domain.example"
	if _, err := BuildPlan(p3); err == nil {
		t.Fatal("an invalid bounce domain was accepted")
	}
}

// A listener on the Docker bridge address cannot bind at boot until Docker has created the network. The plan must order
// Postfix after Docker and wait for the address; a loopback listener needs neither. (QEMU lab: Postfix was down after
// every reboot with "bind 172.30.0.1 port 10587: Cannot assign requested address".)
func TestPostfixBootOrderingWhenSubmissionIsOnTheDockerBridge(t *testing.T) {
	p := params(t)
	p.SubmissionBind = "172.30.0.1:10587"
	pl, err := BuildPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	var drop *File
	for i := range pl.Files {
		if pl.Files[i].Path == postfixBootDropIn {
			drop = &pl.Files[i]
		}
	}
	if drop == nil {
		t.Fatal("no boot-ordering drop-in for a bridge address")
	}
	c := string(drop.Content)
	for _, want := range []string{"After=docker.service", "Wants=docker.service", "inet 172\\.30\\.0\\.1/", "$$(seq 1 90)", "TimeoutStartSec=180"} {
		if !strings.Contains(c, want) {
			t.Fatalf("drop-in lacks %q:\n%s", want, c)
		}
	}
	if !strings.HasPrefix(c, Marker) {
		t.Fatal("drop-in must carry the installer marker")
	}
	loop, err := BuildPlan(params(t)) // 127.0.0.1
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range loop.Files {
		if f.Path == postfixBootDropIn {
			t.Fatal("a loopback listener needs no boot ordering")
		}
	}
	// It is applied, backed up and rolled back like every other managed file.
	run := &fakeRun{}
	rc, err := Apply(p, pl.Digest, run, now)
	if err != nil || rc.Result != "ok" {
		t.Fatalf("%v %+v", err, rc)
	}
	if !strings.Contains(read(t, p, postfixBootDropIn), "After=docker.service") {
		t.Fatal("drop-in not written")
	}
	if _, err := Rollback(p, rc.ID, run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.path(postfixBootDropIn)); !os.IsNotExist(err) {
		t.Fatal("rollback must remove the drop-in")
	}
}

func snapshotTree(t *testing.T, root string) string {
	var b strings.Builder
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			c, _ := os.ReadFile(p)
			fmt.Fprintf(&b, "%s %o %x\n", strings.TrimPrefix(p, root), info.Mode().Perm(), sha256.Sum256(c))
		}
		return nil
	})
	return b.String()
}

// Production protection: a host whose mail service is in use is never
// changed, whatever the flags. -adopt only replaces an unused configuration.
func TestProductionMailServerIsNeverReplaced(t *testing.T) {
	cases := map[string]func(t *testing.T, root string){
		"mailcow": func(t *testing.T, root string) {
			writeFileAtomic(filepath.Join(root, "/opt/mailcow-dockerized/mailcow.conf"), []byte("MAILCOW_HOSTNAME=mail.example.com\nDBPASS=x\n"), 0o600)
		},
		"postfix in use": func(t *testing.T, root string) {
			writeFileAtomic(filepath.Join(root, "/etc/postfix/main.cf"), []byte("myhostname = mx.example.org\nvirtual_mailbox_domains = example.org\n"), 0o644)
		},
		"exim": func(t *testing.T, root string) {
			writeFileAtomic(filepath.Join(root, "/usr/sbin/exim4"), nil, 0o755)
		},
		"opendkim signing for another server": func(t *testing.T, root string) {
			writeFileAtomic(filepath.Join(root, "/etc/opendkim.conf"), []byte("KeyTable /etc/opendkim/key.table\nSigningTable refile:/etc/opendkim/signing.table\n"), 0o644)
		},
		"unknown program on port 25": func(t *testing.T, root string) {
			fakeListener(t, root, "smtp-sink", 25)
		},
		"submission port taken": func(t *testing.T, root string) {
			fakeListener(t, root, "python3", 10587)
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			p := params(t)
			prepare(t, p.Root)
			before := snapshotTree(t, p.Root)
			pl, err := BuildPlan(p)
			if err != nil {
				t.Fatal(err)
			}
			if len(pl.Blockers) == 0 || !strings.Contains(pl.Summary(), "BLOCKED") {
				t.Fatalf("not blocked: %+v", pl)
			}
			for _, adopt := range []bool{false, true} {
				p.Adopt = adopt
				pl, _ := BuildPlan(p)
				run := &fakeRun{}
				if _, err := Apply(p, pl.Digest, run, now); !errors.Is(err, ErrBlocked) {
					t.Fatalf("adopt=%v: %v", adopt, err)
				}
				if len(run.calls) != 0 {
					t.Fatalf("commands ran on a blocked host: %v", run.calls)
				}
			}
			if snapshotTree(t, p.Root) != before {
				t.Fatal("a blocked host was changed")
			}
		})
	}
}

// fakeListener makes a process own a listening socket in the host's /proc.
func fakeListener(t *testing.T, root, comm string, port int) {
	t.Helper()
	pid := filepath.Join(root, "proc", "4242")
	writeFileAtomic(filepath.Join(pid, "comm"), []byte(comm+"\n"), 0o644)
	writeFileAtomic(filepath.Join(pid, "cmdline"), []byte(comm+"\x00"), 0o644)
	os.MkdirAll(filepath.Join(pid, "fd"), 0o755)
	if err := os.Symlink("socket:[777]", filepath.Join(pid, "fd", "3")); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf("   0: 0100007F:%04X 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 777 1 0 100 0 0 10 0", port)
	writeFileAtomic(filepath.Join(root, "proc/net/tcp"), []byte("  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"+line+"\n"), 0o644)
}

// The installer installs missing packages on Debian/Ubuntu as an approved
// step, never removes a package to do so, and rollback can remove exactly
// what it installed.
func TestPackagesAreInstalledSafely(t *testing.T) {
	p := params(t)
	os.Remove(filepath.Join(p.Root, "/usr/sbin/postfix"))
	os.Remove(filepath.Join(p.Root, "/usr/sbin/opendkim"))
	writeFileAtomic(filepath.Join(p.Root, "/etc/os-release"), []byte("ID=ubuntu\nVERSION_ID=\"24.04\"\n"), 0o644)
	pl, _ := BuildPlan(p)
	if strings.Join(pl.MissingPackages, " ") != "opendkim postfix" || pl.PackageManager != "apt" || !strings.Contains(pl.Summary(), "install   packages opendkim postfix") {
		t.Fatalf("%+v\n%s", pl, pl.Summary())
	}
	// apt would remove another package: refused, rolled back, nothing installed.
	run := &fakeRun{out: map[string]string{"apt-get -s install": "Inst postfix (3.8.6)\nRemv exim4-daemon-light [4.97]\n"}}
	rc, err := Apply(p, pl.Digest, run, now)
	if err == nil || rc.Result != "failed_rolled_back" || !strings.Contains(err.Error(), "exim4-daemon-light") {
		t.Fatalf("%v %+v", err, rc)
	}
	for _, c := range run.calls {
		if strings.Contains(c, "DEBIAN_FRONTEND=noninteractive apt-get install") {
			t.Fatal("packages installed although apt would remove one")
		}
	}
	// Clean install.
	run = &fakeRun{}
	rc, err = Apply(p, pl.Digest, run, now)
	if err != nil || strings.Join(rc.InstalledPackages, " ") != "opendkim postfix" {
		t.Fatalf("%v %+v", err, rc)
	}
	joined := strings.Join(run.calls, "\n")
	if !strings.Contains(joined, "debconf-set-selections") || !strings.Contains(joined, "env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends opendkim postfix") {
		t.Fatalf("%s", joined)
	}
	// Rollback without -remove-packages keeps them; with it, purges exactly them.
	p.RemovePackages = true
	run = &fakeRun{}
	if _, err := Rollback(p, "", run); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(run.calls, "\n"), "apt-get purge -y opendkim postfix") {
		t.Fatalf("%v", run.calls)
	}
}

// Scenario 6: a failed setup is resumed by running the same plan again; the
// retry neither duplicates files, keys, certificates nor firewall rules, and
// a further plan has nothing left to do.
func TestFailedSetupResumesWithoutDuplicates(t *testing.T) {
	p := params(t)
	pl, _ := BuildPlan(p)
	if rc, err := Apply(p, pl.Digest, &fakeRun{fail: "opendkim -n"}, now); err == nil || rc.Result != "failed_rolled_back" {
		t.Fatalf("%v", err)
	}
	pl, _ = BuildPlan(p)
	rc, err := Apply(p, pl.Digest, &fakeRun{}, now.Add(time.Minute))
	if err != nil || rc.Result != "ok" {
		t.Fatal(err)
	}
	after := snapshotTree(t, filepath.Join(p.Root, "etc"))
	pl, _ = BuildPlan(p)
	for _, f := range pl.Files {
		if f.Change != "unchanged" {
			t.Fatalf("%s would change again", f.Path)
		}
	}
	for _, a := range pl.Actions {
		if a.Needed {
			t.Fatalf("%s would be generated again", a.Target)
		}
	}
	if _, err := Apply(p, pl.Digest, &fakeRun{}, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if snapshotTree(t, filepath.Join(p.Root, "etc")) != after {
		t.Fatal("a repeated apply changed the configuration")
	}
	fw := read(t, p, FirewallFile)
	if strings.Count(fw, "table inet njiwa_mail {") != 1 {
		t.Fatalf("firewall table duplicated:\n%s", fw)
	}
}

// T3.4: a user who cannot read the receipts is told to run as root, not
// that there is no receipt.
func TestReceiptPermissionErrorIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file; run as a normal user to exercise this")
	}
	p := params(t)
	dir := filepath.Join(p.Root, p.StateDir, "receipts")
	os.MkdirAll(dir, 0o700)
	os.Chmod(dir, 0)
	defer os.Chmod(dir, 0o700)
	if _, err := LoadReceipt(p, ""); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("%v", err)
	}
}

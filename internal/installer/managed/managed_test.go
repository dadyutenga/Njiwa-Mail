package managed

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRun struct {
	calls []string
	fail  string // command prefix that fails
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
	return "", nil
}

func params(t *testing.T) Params {
	return Params{Hostname: "MailOut.Example.com.", BounceDomain: "bounce.example.com", Domains: []string{"notify.example.com", "Notify.Example.com", "other.example.org"},
		Selector: "njiwa", SubmissionBind: "127.0.0.1:10587", InboundRelay: "127.0.0.1:2525", Root: t.TempDir(), StateDir: "/var/lib/njiwa-mail/installer"}
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
	for _, want := range []string{"smtpd_relay_restrictions = reject_unauth_destination", "mydestination =\n", "relay_domains = bounce.example.com",
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

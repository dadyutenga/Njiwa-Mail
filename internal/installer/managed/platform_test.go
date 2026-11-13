package managed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rhelParams(t *testing.T, osRelease string) Params {
	t.Helper()
	p := params(t)
	writeFileAtomic(filepath.Join(p.Root, "/etc/os-release"), []byte(osRelease), 0o644)
	return p
}

func TestDetectPlatform(t *testing.T) {
	for _, c := range []struct {
		release, family, mapType, compat, dropIn string
		epel                                     bool
	}{
		{"ID=ubuntu\nVERSION_ID=\"24.04\"\n", "debian", "hash", "3.6", postfixBootDropIn, false},
		{"ID=\"almalinux\"\nID_LIKE=\"rhel centos fedora\"\nVERSION_ID=\"9.6\"\n", "rhel", "hash", "2", "/etc/systemd/system/postfix.service.d/njiwa-mail.conf", true},
		{"ID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\nVERSION_ID=\"10.0\"\n", "rhel", "lmdb", "3.6", "/etc/systemd/system/postfix.service.d/njiwa-mail.conf", true},
		{"ID=fedora\nVERSION_ID=43\n", "rhel", "lmdb", "3.6", "/etc/systemd/system/postfix.service.d/njiwa-mail.conf", false},
	} {
		root := t.TempDir()
		writeFileAtomic(filepath.Join(root, "/etc/os-release"), []byte(c.release), 0o644)
		pl := DetectPlatform(root, nil)
		if pl.Family != c.family || pl.mapType() != c.mapType || pl.compatLevel() != c.compat || pl.postfixDropIn() != c.dropIn || pl.needsEPEL() != c.epel {
			t.Errorf("%q: %+v map=%s compat=%s dropin=%s epel=%v", c.release, pl, pl.mapType(), pl.compatLevel(), pl.postfixDropIn(), pl.needsEPEL())
		}
	}
	// Postfix facts from postconf override the defaults.
	pl := Platform{Family: "rhel", ID: "almalinux", Version: "9.6", PostfixVersion: "3.5.25", MapType: "lmdb"}
	if pl.compatLevel() != "2" || pl.mapType() != "lmdb" || pl.mapExt() != ".lmdb" {
		t.Fatalf("%+v", pl)
	}
	if (Platform{PostfixVersion: "3.10.1"}).compatLevel() != "3.6" {
		t.Fatal("3.10 must be >= 3.6")
	}
}

// AlmaLinux 9 host with nothing installed: the plan enables EPEL, installs
// with dnf, writes Postfix 3.5-compatible settings with hash tables, and the
// boot drop-in goes to postfix.service.
func TestPlanOnEnterpriseLinux9(t *testing.T) {
	p := rhelParams(t, "ID=\"almalinux\"\nID_LIKE=\"rhel centos fedora\"\nVERSION_ID=\"9.6\"\n")
	os.Remove(filepath.Join(p.Root, "/usr/sbin/postfix"))
	os.Remove(filepath.Join(p.Root, "/usr/sbin/opendkim"))
	p.SubmissionBind = "172.30.0.1:10587"
	pl, err := BuildPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	if pl.PackageManager != "dnf" || strings.Join(pl.MissingPackages, " ") != "epel-release opendkim postfix" {
		t.Fatalf("%s %v", pl.PackageManager, pl.MissingPackages)
	}
	var main string
	var dropIn bool
	for _, f := range pl.Files {
		if f.Path == "/etc/postfix/main.cf" {
			main = string(f.Content)
		}
		if f.Path == "/etc/systemd/system/postfix.service.d/njiwa-mail.conf" {
			dropIn = true
		}
	}
	if !strings.Contains(main, "compatibility_level = 2\n") || !strings.Contains(main, "relay_clientcerts = hash:") || !dropIn {
		t.Fatalf("dropIn=%v\n%s", dropIn, main)
	}
	if !strings.Contains(pl.Summary(), "install   packages epel-release opendkim postfix (dnf; nothing is removed)") {
		t.Fatal(pl.Summary())
	}
	// Apply: EPEL first, a dnf dry run, then the install; postmap with hash.
	run := &fakeRun{}
	rc, err := Apply(p, pl.Digest, run, now)
	if err != nil {
		t.Fatal(err)
	}
	calls := strings.Join(run.calls, "\n")
	for _, want := range []string{"dnf install -y epel-release", "dnf install --assumeno opendkim postfix", "dnf install -y opendkim postfix", "postmap hash:"} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing %q in\n%s", want, calls)
		}
	}
	if rc.PackageManager != "dnf" || strings.Join(rc.InstalledPackages, " ") != "epel-release opendkim postfix" {
		t.Fatalf("%+v", rc)
	}
	p.RemovePackages = true
	run = &fakeRun{}
	if _, err := Rollback(p, "", run); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(run.calls, "\n"), "dnf remove -y epel-release opendkim postfix") {
		t.Fatalf("%v", run.calls)
	}
}

// Fedora: LMDB tables (with the postfix-lmdb plugin), SELinux labelling and
// a firewalld notice; dnf refusing to remove anything.
func TestPlanOnFedoraWithSELinuxAndFirewalld(t *testing.T) {
	p := rhelParams(t, "ID=fedora\nVERSION_ID=43\n")
	writeFileAtomic(filepath.Join(p.Root, "/sys/fs/selinux/enforce"), []byte("1"), 0o644)
	writeFileAtomic(filepath.Join(p.Root, "/run/firewalld/firewalld.pid"), []byte("1"), 0o644)
	pl, _ := BuildPlan(p)
	if strings.Join(pl.MissingPackages, " ") != "postfix-lmdb" {
		t.Fatalf("%v", pl.MissingPackages)
	}
	sum := pl.Summary()
	for _, want := range []string{"selinux   tcp/10587 labelled smtp_port_t", "selinux   restorecon", "firewall-cmd --permanent --add-service=smtp"} {
		if !strings.Contains(sum, want) {
			t.Errorf("summary lacks %q:\n%s", want, sum)
		}
	}
	run := &fakeRun{out: map[string]string{"dnf install --assumeno": "Dependencies resolved.\nRemoving:\n sendmail   x86_64  8.18  @anaconda  2.9 M\n\nTransaction Summary\n"}}
	if _, err := Apply(p, pl.Digest, run, now); err == nil || !strings.Contains(err.Error(), "sendmail") {
		t.Fatalf("dnf removal not refused: %v", err)
	}
	run = &fakeRun{}
	rc, err := Apply(p, pl.Digest, run, now)
	if err != nil {
		t.Fatal(err)
	}
	calls := strings.Join(run.calls, "\n")
	for _, want := range []string{"semanage port -a -t smtp_port_t -p tcp 10587", "restorecon -R", "postmap lmdb:"} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing %q in\n%s", want, calls)
		}
	}
	if rc.SELinuxPort != "10587" {
		t.Fatalf("%+v", rc)
	}
	run = &fakeRun{}
	if _, err := Rollback(p, "", run); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(run.calls, "\n"), "semanage port -d -t smtp_port_t -p tcp 10587") {
		t.Fatalf("SELinux label not removed on rollback: %v", run.calls)
	}
}

// RHEL itself: EPEL is a subscription matter, so the plan explains how to
// enable it and apply refuses until it is.
func TestPlanOnRHELWithoutEPEL(t *testing.T) {
	p := rhelParams(t, "ID=\"rhel\"\nID_LIKE=\"fedora\"\nVERSION_ID=\"9.4\"\n")
	os.Remove(filepath.Join(p.Root, "/usr/sbin/opendkim"))
	pl, _ := BuildPlan(p)
	if pl.PackageManager != "" || !strings.Contains(pl.Summary(), "epel-release-latest-9.noarch.rpm") {
		t.Fatalf("%s\n%s", pl.PackageManager, pl.Summary())
	}
	if _, err := Apply(p, pl.Digest, &fakeRun{}, now); err == nil {
		t.Fatal("apply must refuse without EPEL")
	}
}

func TestDNFRemovals(t *testing.T) {
	out := "Dependencies resolved.\n=====\n Package Arch Version Repo Size\n=====\nInstalling:\n postfix x86_64 2:3.5.25 baseos 1.5 M\nRemoving:\n sendmail x86_64 8.16 @System 2.9 M\n esmtp x86_64 1.2 @System 100 k\n\nTransaction Summary\n"
	if got := strings.Join(dnfRemovals(out), " "); got != "sendmail esmtp" {
		t.Fatalf("%q", got)
	}
	if len(dnfRemovals("Installing:\n postfix x86_64 3.8 fedora 1 M\n\nTransaction Summary\n")) != 0 {
		t.Fatal("false positive")
	}
}

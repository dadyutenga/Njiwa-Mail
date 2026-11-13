package discover

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeHost builds a host tree under a temp dir: files, processes with
// listening sockets, interfaces and docker ps output.
type fakeHost struct {
	t      *testing.T
	root   string
	tcp    []string
	pid    int
	inode  int
	ifs    []Interface
	docker string
}

func newHost(t *testing.T, osID, version string) *fakeHost {
	h := &fakeHost{t: t, root: t.TempDir(), pid: 100, inode: 5000}
	h.file("/etc/os-release", fmt.Sprintf("ID=%s\nVERSION_ID=\"%s\"\nPRETTY_NAME=\"%s %s\"\n", osID, version, osID, version))
	h.file("/run/systemd/system/.keep", "")
	h.file("/proc/cpuinfo", "processor\t: 0\nprocessor\t: 1\n")
	h.file("/proc/meminfo", "MemTotal:        4000000 kB\nMemAvailable:    3000000 kB\n")
	h.ifs = []Interface{{Name: "lo", Addrs: []string{"127.0.0.1/8"}}, {Name: "eth0", Addrs: []string{"203.0.113.10/24"}}}
	return h
}

func (h *fakeHost) file(p, content string) {
	h.t.Helper()
	full := filepath.Join(h.root, p)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// listen adds a process that owns a listening socket on addr:port
// (addr as /proc hex: 00000000 = 0.0.0.0, 0100007F = 127.0.0.1).
func (h *fakeHost) listen(comm string, cmdline []string, hexAddr string, port int) {
	h.pid++
	h.inode++
	base := fmt.Sprintf("/proc/%d", h.pid)
	h.file(base+"/comm", comm+"\n")
	h.file(base+"/cmdline", strings.Join(cmdline, "\x00")+"\x00")
	fd := filepath.Join(h.root, base, "fd")
	os.MkdirAll(fd, 0o755)
	if err := os.Symlink(fmt.Sprintf("socket:[%d]", h.inode), filepath.Join(fd, "3")); err != nil {
		h.t.Fatal(err)
	}
	h.tcp = append(h.tcp, fmt.Sprintf("   %d: %s:%04X 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 %d 1 0000000000000000 100 0 0 10 0",
		len(h.tcp), hexAddr, port, h.inode))
}

func (h *fakeHost) proc(comm string, cmdline ...string) {
	h.pid++
	base := fmt.Sprintf("/proc/%d", h.pid)
	h.file(base+"/comm", comm+"\n")
	h.file(base+"/cmdline", strings.Join(cmdline, "\x00")+"\x00")
	os.MkdirAll(filepath.Join(h.root, base, "fd"), 0o755)
}

func (h *fakeHost) scan() *Report {
	h.file("/proc/net/tcp", "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"+strings.Join(h.tcp, "\n")+"\n")
	env := Env{Root: h.root, Interfaces: func() ([]Interface, error) { return h.ifs, nil },
		Statfs: func(string) (uint64, error) { return 50 << 30, nil }, Now: func() time.Time { return time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC) }}
	if h.docker != "" {
		env.Docker = func(...string) (string, error) { return h.docker, nil }
	}
	return Scan(env)
}

func codes(is []Issue) map[string]bool {
	m := map[string]bool{}
	for _, i := range is {
		m[i.Code] = true
	}
	return m
}

// Scenario 1: a fresh Ubuntu host (Postfix may be installed by the documented
// first step, unconfigured) -> managed mode is recommended, nothing blocks.
func TestFreshHostRecommendsManaged(t *testing.T) {
	h := newHost(t, "ubuntu", "24.04")
	h.file("/usr/sbin/postfix", "")
	h.file("/usr/bin/docker", "")
	h.listen("sshd", []string{"sshd"}, "00000000", 22)
	r := h.scan()
	rec := r.Recommendation
	if rec.Mode != ModeManaged || !rec.ManagedAllowed() || len(rec.Warnings) != 0 {
		t.Fatalf("%+v", rec)
	}
	if r.Host.CPUs != 2 || r.Host.MemAvailMB < 2000 || len(r.Host.PublicIPs) != 1 || len(r.Ports) != 1 || r.Ports[0].Process != "sshd" {
		t.Fatalf("host: %+v ports %+v", r.Host, r.Ports)
	}
}

// Scenario 2: Mailcow on the host -> existing SMTP, Mailcow offered with its
// hostname; the managed installer is blocked; no Mailcow secret is read.
func TestMailcowHostRecommendsExistingSMTP(t *testing.T) {
	h := newHost(t, "debian", "12")
	h.file("/opt/mailcow-dockerized/mailcow.conf", "MAILCOW_HOSTNAME=mail.example.co.tz\nDBPASS=supersecret\nDBROOT=rootsecret\n")
	for _, p := range []int{25, 465, 587, 993} {
		h.listen("docker-proxy", []string{"/usr/bin/docker-proxy", "-proto", "tcp", "-host-ip", "0.0.0.0", "-host-port", fmt.Sprint(p), "-container-ip", "172.22.1.253", "-container-port", fmt.Sprint(p)}, "00000000", p)
	}
	h.listen("nginx", []string{"nginx: master process"}, "00000000", 443)
	h.docker = `{"Names":"mailcowdockerized-postfix-mailcow-1","Image":"ghcr.io/mailcow/postfix:1.80","Labels":"com.docker.compose.project=mailcowdockerized","Ports":"0.0.0.0:25->25/tcp"}` + "\n" +
		`{"Names":"mailcowdockerized-nginx-mailcow-1","Image":"ghcr.io/mailcow/nginx:1.03","Labels":"com.docker.compose.project=mailcowdockerized","Ports":""}`
	h.ifs = append(h.ifs, Interface{Name: "br-abc", Addrs: []string{"172.22.1.1/24"}})
	r := h.scan()
	rec := r.Recommendation
	if rec.Mode != ModeExisting || rec.ManagedAllowed() || !codes(rec.ManagedBlockers)[CodeMailcowPresent] {
		t.Fatalf("%+v", rec)
	}
	if codes(rec.ManagedBlockers)[CodeMailPortInUse] {
		t.Fatalf("Mailcow's own ports must not be reported twice: %+v", rec.ManagedBlockers)
	}
	if len(rec.Candidates) != 1 || rec.Candidates[0].Host != "mail.example.co.tz" || rec.Candidates[0].Port != 587 {
		t.Fatalf("candidate: %+v", rec.Candidates)
	}
	if !r.Mailcow.Running || len(r.ComposeStacks) != 1 || r.ComposeStacks[0] != "mailcowdockerized" {
		t.Fatalf("mailcow: %+v %v", r.Mailcow, r.ComposeStacks)
	}
	if !codes(rec.Warnings)[CodeProxyPresent] {
		t.Fatalf("existing proxy not reported: %+v", rec.Warnings)
	}
	for _, p := range r.Ports {
		if p.Port == 25 && p.Container != "172.22.1.253:25" {
			t.Fatalf("docker-proxy target: %+v", p)
		}
	}
	if b := fmt.Sprintf("%+v", r); strings.Contains(b, "supersecret") || strings.Contains(b, "rootsecret") {
		t.Fatal("a Mailcow secret leaked into the report")
	}
}

// Scenario 3: a host running other applications, no mail server, but the
// provider has no public IP on the host (NAT): managed is still possible but
// warned; existing SMTP needs nothing from the host.
func TestBusyHostWithoutMailServer(t *testing.T) {
	h := newHost(t, "ubuntu", "22.04")
	h.ifs = []Interface{{Name: "eth0", Addrs: []string{"10.0.0.5/24"}}}
	h.listen("node", []string{"node", "server.js"}, "0100007F", 3000)
	h.listen("caddy", []string{"caddy", "run"}, "00000000", 443)
	r := h.scan()
	w := codes(r.Recommendation.Warnings)
	if !w[CodeNjiwaPortInUse] || !w[CodeProxyPresent] || !w[CodeNoPublicIP] || !w[CodeDockerMissing] {
		t.Fatalf("%+v", r.Recommendation.Warnings)
	}
	if r.Recommendation.Mode != ModeManaged {
		t.Fatalf("no mail server here, managed remains possible: %+v", r.Recommendation)
	}
	for _, i := range r.Recommendation.Warnings {
		if i.Code == CodeNjiwaPortInUse && !strings.Contains(i.Fix, "NJIWA_DASHBOARD_PORT") {
			t.Fatalf("fix must name the setting: %+v", i)
		}
	}
}

// Scenario 4: conflicting mail ports and a production Postfix -> managed is
// blocked with the reason, and the local server is offered for reuse.
func TestProductionPostfixBlocksManaged(t *testing.T) {
	h := newHost(t, "debian", "12")
	h.file("/usr/sbin/postfix", "")
	h.file("/etc/postfix/main.cf", "myhostname = mx.example.org\nvirtual_mailbox_domains = example.org\nsmtpd_sasl_auth_enable = yes\n")
	h.file("/var/spool/postfix/deferred/A/ABC123", "x")
	h.listen("master", []string{"/usr/lib/postfix/sbin/master", "-w"}, "00000000", 25)
	h.listen("master", []string{"/usr/lib/postfix/sbin/master", "-w"}, "00000000", 587)
	h.listen("python3", []string{"python3", "-m", "smtpd"}, "0100007F", 10587)
	rec := h.scan().Recommendation
	b := codes(rec.ManagedBlockers)
	if rec.Mode != ModeExisting || !b[CodePostfixInUse] || !b[CodeInstallerPort] || b[CodeMailPortInUse] {
		t.Fatalf("%+v", rec.ManagedBlockers)
	}
	if len(rec.Candidates) != 1 || rec.Candidates[0].Host != "mx.example.org" || rec.Candidates[0].Port != 587 {
		t.Fatalf("%+v", rec.Candidates)
	}
	for _, i := range rec.ManagedBlockers {
		if i.Code == CodePostfixInUse && !strings.Contains(i.Detail, "1 message(s) in the Postfix queue") {
			t.Fatalf("evidence missing: %+v", i)
		}
	}
}

// Something unknown on port 25 (no MTA package) blocks too.
func TestUnknownProgramOnPort25(t *testing.T) {
	h := newHost(t, "ubuntu", "24.04")
	h.listen("smtp-sink", []string{"smtp-sink"}, "00000000", 25)
	rec := h.scan().Recommendation
	if !codes(rec.ManagedBlockers)[CodeMailPortInUse] || rec.Mode != ModeExisting {
		t.Fatalf("%+v", rec)
	}
}

// Other MTAs, OpenDKIM in use, unsupported OS, no systemd, low resources.
func TestOtherBlockers(t *testing.T) {
	h := newHost(t, "centos", "9")
	os.RemoveAll(filepath.Join(h.root, "run/systemd"))
	h.file("/usr/sbin/exim4", "")
	h.file("/usr/sbin/opendkim", "")
	h.file("/etc/opendkim.conf", "Syslog yes\nKeyTable /etc/opendkim/key.table\n")
	h.file("/proc/meminfo", "MemTotal: 900000 kB\nMemAvailable: 300000 kB\n")
	rec := h.scan().Recommendation
	b := codes(rec.ManagedBlockers)
	for _, c := range []string{CodeOtherMTA, CodeOpenDKIMInUse, CodeOSUnsupported, CodeNoSystemd, CodeMemoryLow} {
		if !b[c] {
			t.Errorf("missing %s: %+v", c, rec.ManagedBlockers)
		}
	}
	if !codes(rec.Warnings)[CodeNjiwaResourcesLow] {
		t.Errorf("low memory for Njiwa itself: %+v", rec.Warnings)
	}
}

// The Postfix and OpenDKIM the Njiwa installer configured are ours: a re-run
// (retry, upgrade, renewal) is never blocked by its own services.
func TestOwnManagedInstallIsNotAConflict(t *testing.T) {
	h := newHost(t, "debian", "12")
	h.file("/usr/sbin/postfix", "")
	h.file("/usr/sbin/opendkim", "")
	h.file("/etc/postfix/main.cf", Marker+" Manual changes are replaced on the next apply.\nmyhostname = mailout.example.com\n")
	h.file("/etc/opendkim.conf", Marker+"\nKeyTable refile:/etc/opendkim/njiwa/key.table\n")
	h.listen("master", []string{"/usr/lib/postfix/sbin/master", "-w"}, "00000000", 25)
	h.listen("master", []string{"/usr/lib/postfix/sbin/master", "-w"}, "01001EAC", 10587) // 172.30.0.1
	h.listen("opendkim", []string{"/usr/sbin/opendkim"}, "0100007F", 8891)
	h.listen("docker-proxy", []string{"docker-proxy", "-host-port", "8080", "-container-ip", "172.30.0.4", "-container-port", "8080"}, "0100007F", 8080)
	h.ifs = append(h.ifs, Interface{Name: "br-njiwa", Addrs: []string{"172.30.0.1/24"}})
	rec := h.scan().Recommendation
	if !rec.ManagedAllowed() || len(rec.Warnings) != 0 || rec.Mode != ModeManaged {
		t.Fatalf("own install reported as a conflict: %+v", rec)
	}
}

// A Docker network overlapping Njiwa's subnet is reported with the fix.
func TestSubnetConflict(t *testing.T) {
	h := newHost(t, "ubuntu", "24.04")
	h.ifs = append(h.ifs, Interface{Name: "br-other", Addrs: []string{"172.30.0.1/16"}})
	if !codes(h.scan().Recommendation.Warnings)[CodeSubnetConflict] {
		t.Fatal("overlapping network not reported")
	}
}

// Discovery never writes: the tree is byte-identical after a scan.
func TestScanIsReadOnly(t *testing.T) {
	h := newHost(t, "debian", "12")
	h.file("/etc/postfix/main.cf", "relayhost = [smtp.example.net]:587\n")
	h.file("/usr/sbin/postfix", "")
	before := snapshot(t, h.root)
	h.scan()
	h.scan()
	after := snapshot(t, h.root)
	delete(after, "/proc/net/tcp") // written by the helper, not by Scan
	delete(before, "/proc/net/tcp")
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatal("scan changed the host tree")
	}
}

func snapshot(t *testing.T, root string) map[string]string {
	m := map[string]string{}
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			b, _ := os.ReadFile(p)
			m[strings.TrimPrefix(p, root)] = info.Mode().String() + string(b)
		}
		return nil
	})
	return m
}

// The real host: the scan runs and produces a recommendation (smoke test).
func TestRealHostScanRuns(t *testing.T) {
	r := Scan(HostEnv(false))
	if r.Recommendation == nil || (r.Recommendation.Mode != ModeManaged && r.Recommendation.Mode != ModeExisting) {
		t.Fatalf("%+v", r)
	}
}

// A Postfix installed with the package's default configuration and no
// traffic is not a production server: managed mode stays possible (the
// installer replaces the configuration only with -adopt, after a backup).
func TestDefaultPostfixIsAdoptableNotBlocking(t *testing.T) {
	h := newHost(t, "ubuntu", "24.04")
	h.file("/usr/sbin/postfix", "")
	h.file("/etc/postfix/main.cf", "myhostname = vps.example.net\nmydestination = $myhostname, localhost\nrelayhost =\n")
	h.file("/var/log/mail.log", "Nov 13 01:00:01 vps postfix/local[11]: ABC: to=<root@vps>, relay=local, status=sent (delivered to mailbox)\n")
	h.listen("master", []string{"/usr/lib/postfix/sbin/master", "-w"}, "00000000", 25)
	r := h.scan()
	if !r.Recommendation.ManagedAllowed() {
		t.Fatalf("default Postfix blocked managed mode: %+v", r.Recommendation.ManagedBlockers)
	}
	var pf Service
	for _, s := range r.MailServices {
		if s.Name == "postfix" {
			pf = s
		}
	}
	if !pf.Running || pf.InUse || pf.Ours {
		t.Fatalf("%+v", pf)
	}
	// The same Postfix after it relayed real mail is in use.
	h.file("/var/log/mail.log", "Nov 13 01:00:01 vps postfix/smtp[11]: ABC: to=<a@gmail.com>, relay=gmail-smtp-in.l.google.com[1.2.3.4]:25, status=sent (250 OK)\n")
	if codes(h.scan().Recommendation.ManagedBlockers)[CodePostfixInUse] != true {
		t.Fatal("a Postfix that relays mail must block")
	}
}

// Regression (found on a real host): Postfix's workers are named "smtpd" and
// share the listening sockets with master. A running Njiwa-managed Postfix
// must not look like OpenSMTPD or like a foreign program on its ports.
func TestRunningPostfixWorkersAreNotOpenSMTPD(t *testing.T) {
	h := newHost(t, "debian", "12")
	h.file("/usr/sbin/postfix", "")
	h.file("/etc/postfix/main.cf", Marker+" Manual changes are replaced on the next apply.\n")
	h.listen("master", []string{"/usr/lib/postfix/sbin/master", "-w"}, "00000000", 25)
	h.listen("master", []string{"/usr/lib/postfix/sbin/master", "-w"}, "0100007F", 10587)
	// The smtpd workers hold the same sockets (same inodes), with higher pids.
	for _, inode := range []int{h.inode - 1, h.inode} {
		h.pid++
		base := fmt.Sprintf("/proc/%d", h.pid)
		h.file(base+"/comm", "smtpd\n")
		h.file(base+"/cmdline", "smtpd\x00-n\x00smtp\x00-t\x00inet\x00")
		os.MkdirAll(filepath.Join(h.root, base, "fd"), 0o755)
		os.Symlink(fmt.Sprintf("socket:[%d]", inode), filepath.Join(h.root, base, "fd", "6"))
	}
	r := h.scan()
	if !r.Recommendation.ManagedAllowed() {
		t.Fatalf("own running Postfix blocked managed mode: %+v", r.Recommendation.ManagedBlockers)
	}
	for _, s := range r.MailServices {
		if s.Name == "opensmtpd" {
			t.Fatalf("Postfix workers reported as OpenSMTPD: %+v", s)
		}
	}
	for _, p := range r.Ports {
		if p.Process != "master" {
			t.Fatalf("port %d attributed to %q, not Postfix's master", p.Port, p.Process)
		}
	}
	// OpenSMTPD itself (its binary installed) is still detected.
	h.file("/usr/sbin/smtpd", "")
	if !codes(h.scan().Recommendation.ManagedBlockers)[CodeOtherMTA] {
		t.Fatal("an installed OpenSMTPD was not detected")
	}
}

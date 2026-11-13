// Package discover inspects a host before Njiwa-Mail is installed or a
// delivery mode is chosen. It is strictly read-only: it reads files under
// Root (normally "/"), the kernel's /proc tables, interface addresses and,
// when allowed, read-only "docker ps" output. It never starts, stops or
// reconfigures anything, and it never records secrets: from Mailcow's
// mailcow.conf only MAILCOW_HOSTNAME is read, and process command lines are
// used only to find which port a docker-proxy publishes.
package discover

import (
	"bufio"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Marker is the first line of every file the managed installer writes.
const Marker = "# Managed by the Njiwa-Mail installer."

// Env is everything a scan touches. Tests point Root at a fake host.
type Env struct {
	Root string
	// Interfaces lists network interfaces with their addresses (CIDR form).
	Interfaces func() ([]Interface, error)
	// Statfs returns free bytes for a path (already joined with Root).
	Statfs func(path string) (uint64, error)
	// Docker runs a read-only docker command and returns stdout; nil skips
	// Docker (the scan then relies on docker-proxy processes and files).
	Docker func(args ...string) (string, error)
	Now    func() time.Time
}

type Interface struct {
	Name  string   `json:"name"`
	Addrs []string `json:"addrs"` // CIDR
}

// Report is the result of a scan. Everything in it is safe to store and show.
type Report struct {
	ScannedAt      time.Time       `json:"scanned_at"`
	Host           Host            `json:"host"`
	Ports          []Port          `json:"ports"`
	MailServices   []Service       `json:"mail_services"`
	Mailcow        *Mailcow        `json:"mailcow,omitempty"`
	Containers     []Container     `json:"containers,omitempty"`
	ComposeStacks  []string        `json:"compose_stacks,omitempty"`
	DockerNetworks []Network       `json:"docker_networks,omitempty"`
	ReverseProxies []string        `json:"reverse_proxies,omitempty"`
	Certificates   []Certificate   `json:"certificates,omitempty"`
	Limits         []string        `json:"limits,omitempty"` // what could not be inspected, and why
	Recommendation *Recommendation `json:"recommendation,omitempty"`
}

type Host struct {
	OS            string   `json:"os"`
	OSID          string   `json:"os_id"`
	OSVersion     string   `json:"os_version"`
	Systemd       bool     `json:"systemd"`
	CPUs          int      `json:"cpus"`
	MemTotalMB    int      `json:"mem_total_mb"`
	MemAvailMB    int      `json:"mem_available_mb"`
	DiskFreeMB    int      `json:"disk_free_mb"` // under /var
	PublicIPs     []string `json:"public_ips"`
	PrivateIPs    []string `json:"private_ips"`
	DockerPresent bool     `json:"docker_present"`
}

// Port is one listening TCP socket.
type Port struct {
	Port      int    `json:"port"`
	Address   string `json:"address"`
	Process   string `json:"process,omitempty"`   // "" when unknown (needs root)
	Container string `json:"container,omitempty"` // container address a docker-proxy forwards to
	Public    bool   `json:"public"`              // bound to all interfaces or a public address
}

// Service is a mail-related program found on the host.
type Service struct {
	Name      string   `json:"name"` // postfix, exim, sendmail, opensmtpd, opendkim, dovecot
	Installed bool     `json:"installed"`
	Running   bool     `json:"running"`
	Ours      bool     `json:"ours"`     // configured by the Njiwa-Mail installer
	InUse     bool     `json:"in_use"`   // shows signs of serving real mail
	Evidence  []string `json:"evidence"` // why InUse (no secrets)
	Hostname  string   `json:"hostname,omitempty"`
}

type Mailcow struct {
	Path     string `json:"path"`
	Hostname string `json:"hostname"`
	Running  bool   `json:"running"`
}

type Container struct {
	Name    string `json:"name"`
	Image   string `json:"image"`
	Project string `json:"project,omitempty"`
	Ports   string `json:"ports,omitempty"`
}

type Network struct {
	Interface string `json:"interface"`
	Subnet    string `json:"subnet"`
}

type Certificate struct {
	Path     string    `json:"path"`
	Names    []string  `json:"names"`
	NotAfter time.Time `json:"not_after"`
	Owner    string    `json:"owner"` // letsencrypt, mailcow, postfix, njiwa
}

// HostEnv is the real host.
func HostEnv(useDocker bool) Env {
	e := Env{
		Root: "/",
		Interfaces: func() ([]Interface, error) {
			ifs, err := net.Interfaces()
			if err != nil {
				return nil, err
			}
			var out []Interface
			for _, i := range ifs {
				as, _ := i.Addrs()
				it := Interface{Name: i.Name}
				for _, a := range as {
					it.Addrs = append(it.Addrs, a.String())
				}
				out = append(out, it)
			}
			return out, nil
		},
		Statfs: func(path string) (uint64, error) {
			var st syscall.Statfs_t
			if err := syscall.Statfs(path, &st); err != nil {
				return 0, err
			}
			return st.Bavail * uint64(st.Bsize), nil
		},
		Now: time.Now,
	}
	if useDocker {
		e.Docker = dockerCLI
	}
	return e
}

func (e Env) path(p string) string { return filepath.Join(e.Root, p) }

func (e Env) read(p string) ([]byte, error) { return os.ReadFile(e.path(p)) }

func (e Env) exists(p string) bool {
	_, err := os.Stat(e.path(p))
	return err == nil
}

// Scan inspects the host. It never fails as a whole: anything it cannot read
// is listed in Limits.
func Scan(e Env) *Report {
	if e.Now == nil {
		e.Now = time.Now
	}
	r := &Report{ScannedAt: e.Now().UTC()}
	r.Host = scanHost(e, r)
	procs, complete := scanProcesses(e)
	if !complete {
		r.Limits = append(r.Limits, "some processes could not be inspected (run as root to see which program owns each port)")
	}
	r.Ports = scanPorts(e, procs, r)
	r.Containers, r.ComposeStacks = scanDocker(e, r)
	r.Host.DockerPresent = len(r.Containers) > 0 || e.exists("/usr/bin/docker") || e.exists("/var/run/docker.sock") || hasProc(procs, "dockerd") || hasProc(procs, "docker-proxy")
	r.Mailcow = scanMailcow(e, r.Containers)
	r.MailServices = scanMailServices(e, procs, r)
	r.DockerNetworks = scanNetworks(e)
	r.ReverseProxies = scanProxies(procs, r.Containers)
	r.Certificates = scanCertificates(e)
	rec := Recommend(r, DefaultNeeds())
	r.Recommendation = &rec
	return r
}

// ---------------------------------------------------------------- host

func scanHost(e Env, r *Report) Host {
	h := Host{}
	if b, err := e.read("/etc/os-release"); err == nil {
		kv := parseKV(string(b))
		h.OS, h.OSID, h.OSVersion = kv["PRETTY_NAME"], kv["ID"], kv["VERSION_ID"]
	} else {
		r.Limits = append(r.Limits, "/etc/os-release could not be read")
	}
	h.Systemd = e.exists("/run/systemd/system")
	if b, err := e.read("/proc/cpuinfo"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "processor") {
				h.CPUs++
			}
		}
	}
	if b, err := e.read("/proc/meminfo"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			f := strings.Fields(l)
			if len(f) < 2 {
				continue
			}
			kb, _ := strconv.Atoi(f[1])
			switch f[0] {
			case "MemTotal:":
				h.MemTotalMB = kb / 1024
			case "MemAvailable:":
				h.MemAvailMB = kb / 1024
			}
		}
	} else {
		r.Limits = append(r.Limits, "memory could not be read")
	}
	if e.Statfs != nil {
		if free, err := e.Statfs(e.path("/var")); err == nil {
			h.DiskFreeMB = int(free / (1 << 20))
		}
	}
	if e.Interfaces != nil {
		ifs, err := e.Interfaces()
		if err != nil {
			r.Limits = append(r.Limits, "network interfaces could not be listed")
		}
		for _, i := range ifs {
			for _, a := range i.Addrs {
				ip, _, err := net.ParseCIDR(a)
				if err != nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
					continue
				}
				if isDockerIface(i.Name) {
					continue
				}
				if ip.IsPrivate() || isCGNAT(ip) {
					h.PrivateIPs = append(h.PrivateIPs, ip.String())
				} else {
					h.PublicIPs = append(h.PublicIPs, ip.String())
				}
			}
		}
	}
	return h
}

func isDockerIface(n string) bool {
	return n == "docker0" || strings.HasPrefix(n, "br-") || strings.HasPrefix(n, "veth")
}

func isCGNAT(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1]&0xC0 == 64
}

func parseKV(s string) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(l), "=")
		if ok && !strings.HasPrefix(k, "#") {
			m[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return m
}

// ---------------------------------------------------------------- processes

type process struct {
	pid     int
	ppid    int
	comm    string
	exe     string // resolved executable path ("" when not readable)
	cmdline []string
	sockets map[string]bool // socket inode numbers
}

// isPostfix reports a process that belongs to Postfix (master, smtpd and
// the other daemons), by executable path or command line, not by its name:
// "master" and "smtpd" are common names.
func (p process) isPostfix() bool {
	return strings.Contains(p.exe, "/postfix/") || strings.Contains(strings.Join(p.cmdline, " "), "postfix")
}

func scanProcesses(e Env) ([]process, bool) {
	ents, err := os.ReadDir(e.path("/proc"))
	if err != nil {
		return nil, false
	}
	complete := true
	var out []process
	for _, ent := range ents {
		pid, err := strconv.Atoi(ent.Name())
		if err != nil {
			continue
		}
		base := filepath.Join("/proc", ent.Name())
		comm, err := e.read(base + "/comm")
		if err != nil {
			continue
		}
		p := process{pid: pid, comm: strings.TrimSpace(string(comm)), sockets: map[string]bool{}}
		if cl, err := e.read(base + "/cmdline"); err == nil {
			p.cmdline = strings.Split(strings.TrimRight(string(cl), "\x00"), "\x00")
		}
		if st, err := e.read(base + "/stat"); err == nil {
			// "pid (comm) state ppid ...": comm may contain spaces and ")".
			if i := strings.LastIndexByte(string(st), ')'); i > 0 {
				if f := strings.Fields(string(st)[i+1:]); len(f) > 1 {
					p.ppid, _ = strconv.Atoi(f[1])
				}
			}
		}
		if x, err := os.Readlink(e.path(base + "/exe")); err == nil {
			p.exe = x
		}
		fds, err := os.ReadDir(e.path(base + "/fd"))
		if err != nil {
			complete = false
		}
		for _, fd := range fds {
			l, err := os.Readlink(e.path(base + "/fd/" + fd.Name()))
			if err == nil && strings.HasPrefix(l, "socket:[") {
				p.sockets[strings.TrimSuffix(strings.TrimPrefix(l, "socket:["), "]")] = true
			}
		}
		out = append(out, p)
	}
	return out, complete
}

func hasProc(ps []process, comm string) bool {
	for _, p := range ps {
		if p.comm == comm {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- ports

func scanPorts(e Env, procs []process, r *Report) []Port {
	// A listening socket is shared by a daemon and its workers (Postfix's
	// master and its smtpd processes). It belongs to the holder whose parent
	// does not hold it too: the daemon that opened it. PIDs wrap around, so
	// the lowest PID is only the fallback when the parents are unknown.
	holders := map[string][]process{}
	for _, p := range procs {
		for s := range p.sockets {
			holders[s] = append(holders[s], p)
		}
	}
	byInode := map[string]process{}
	for inode, hs := range holders {
		pids := map[int]bool{}
		for _, h := range hs {
			pids[h.pid] = true
		}
		var root, lowest *process
		for i := range hs {
			h := &hs[i]
			if lowest == nil || h.pid < lowest.pid {
				lowest = h
			}
			if h.ppid != 0 && !pids[h.ppid] && (root == nil || h.pid < root.pid) {
				root = h
			}
		}
		if root == nil {
			root = lowest
		}
		byInode[inode] = *root
	}
	// docker-proxy publishes a container port: -host-port 25 -container-ip 172.22.1.253 -container-port 25
	proxyTarget := map[int]string{}
	for _, p := range procs {
		if p.comm != "docker-proxy" {
			continue
		}
		var hp, cip, cport string
		for i := 0; i+1 < len(p.cmdline); i++ {
			switch p.cmdline[i] {
			case "-host-port":
				hp = p.cmdline[i+1]
			case "-container-ip":
				cip = p.cmdline[i+1]
			case "-container-port":
				cport = p.cmdline[i+1]
			}
		}
		if n, err := strconv.Atoi(hp); err == nil && cip != "" {
			proxyTarget[n] = net.JoinHostPort(cip, cport)
		}
	}
	seen := map[string]bool{}
	var out []Port
	read := false
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := e.read(f)
		if err != nil {
			continue
		}
		read = true
		for _, line := range strings.Split(string(b), "\n")[1:] {
			fs := strings.Fields(line)
			if len(fs) < 10 || fs[3] != "0A" {
				continue
			}
			ip, port, ok := parseProcAddr(fs[1])
			if !ok {
				continue
			}
			key := ip.String() + ":" + strconv.Itoa(port)
			if seen[key] {
				continue
			}
			seen[key] = true
			pt := Port{Port: port, Address: ip.String(), Public: ip.IsUnspecified() || (!ip.IsLoopback() && !ip.IsPrivate())}
			if p, ok := byInode[fs[9]]; ok {
				pt.Process = p.comm
				if p.comm == "master" && !p.isPostfix() {
					pt.Process = "master (not Postfix)"
				}
				if p.comm == "docker-proxy" {
					pt.Container = proxyTarget[port]
				}
			}
			out = append(out, pt)
		}
	}
	if !read {
		r.Limits = append(r.Limits, "listening ports could not be read")
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].Address < out[j].Address
	})
	return out
}

// parseProcAddr decodes "0100007F:1F90" or the 32-hex IPv6 form.
func parseProcAddr(s string) (net.IP, int, bool) {
	h, p, ok := strings.Cut(s, ":")
	if !ok {
		return nil, 0, false
	}
	port, err := strconv.ParseUint(p, 16, 16)
	if err != nil {
		return nil, 0, false
	}
	raw, err := hex.DecodeString(h)
	if err != nil || (len(raw) != 4 && len(raw) != 16) {
		return nil, 0, false
	}
	ip := make(net.IP, len(raw))
	for i := 0; i < len(raw); i += 4 {
		ip[i], ip[i+1], ip[i+2], ip[i+3] = raw[i+3], raw[i+2], raw[i+1], raw[i]
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip, int(port), true
}

// PortsIn returns the listeners on one port.
func (r *Report) PortsIn(port int) []Port {
	var out []Port
	for _, p := range r.Ports {
		if p.Port == port {
			out = append(out, p)
		}
	}
	return out
}

// ---------------------------------------------------------------- docker

func dockerCLI(args ...string) (string, error) {
	if _, err := os.Stat("/var/run/docker.sock"); err != nil {
		return "", err
	}
	return runCommand("docker", args...)
}

func scanDocker(e Env, r *Report) ([]Container, []string) {
	if e.Docker == nil {
		return nil, nil
	}
	out, err := e.Docker("ps", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		r.Limits = append(r.Limits, "running containers could not be listed (docker ps not available to this user)")
		return nil, nil
	}
	var cs []Container
	stacks := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var row struct{ Names, Image, Labels, Ports string }
		if json.Unmarshal(sc.Bytes(), &row) != nil {
			continue
		}
		c := Container{Name: row.Names, Image: row.Image, Ports: row.Ports}
		for _, l := range strings.Split(row.Labels, ",") {
			if k, v, ok := strings.Cut(l, "="); ok && k == "com.docker.compose.project" {
				c.Project = v
				stacks[v] = true
			}
		}
		cs = append(cs, c)
	}
	var names []string
	for s := range stacks {
		names = append(names, s)
	}
	sort.Strings(names)
	return cs, names
}

// ---------------------------------------------------------------- mail

var mailcowDirs = []string{"/opt/mailcow-dockerized", "/srv/mailcow-dockerized", "/root/mailcow-dockerized"}

func scanMailcow(e Env, cs []Container) *Mailcow {
	var m *Mailcow
	for _, d := range mailcowDirs {
		b, err := e.read(d + "/mailcow.conf")
		if err != nil {
			continue
		}
		// Only the hostname: mailcow.conf also holds database passwords.
		m = &Mailcow{Path: d}
		for _, l := range strings.Split(string(b), "\n") {
			if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok && k == "MAILCOW_HOSTNAME" {
				m.Hostname = strings.Trim(strings.TrimSpace(v), `"'`)
			}
		}
		break
	}
	for _, c := range cs {
		if c.Project == "mailcowdockerized" || strings.HasPrefix(c.Name, "mailcowdockerized") {
			if m == nil {
				m = &Mailcow{}
			}
			m.Running = true
		}
	}
	return m
}

type mtaDef struct {
	name  string
	bins  []string
	comms []string
}

var mtaDefs = []mtaDef{
	{"postfix", []string{"/usr/sbin/postfix"}, []string{"master"}},
	{"exim", []string{"/usr/sbin/exim4", "/usr/sbin/exim"}, []string{"exim4", "exim"}},
	{"sendmail", []string{"/usr/sbin/sendmail.sendmail", "/usr/lib/sendmail.d"}, []string{"sendmail"}},
	{"opensmtpd", []string{"/usr/sbin/smtpd"}, []string{"smtpd"}},
	{"opendkim", []string{"/usr/sbin/opendkim"}, []string{"opendkim"}},
	{"dovecot", []string{"/usr/sbin/dovecot"}, []string{"dovecot"}},
}

func scanMailServices(e Env, procs []process, r *Report) []Service {
	var out []Service
	for _, d := range mtaDefs {
		s := Service{Name: d.name}
		for _, b := range d.bins {
			if e.exists(b) {
				s.Installed = true
			}
		}
		for _, p := range procs {
			for _, c := range d.comms {
				switch {
				case p.comm != c:
				case d.name == "postfix" && !p.isPostfix():
					// "master" is a common name: only Postfix's counts.
				case d.name == "opensmtpd" && (p.isPostfix() || (!s.Installed && p.exe != "/usr/sbin/smtpd")):
					// Postfix's workers are also called "smtpd".
				default:
					s.Running = true
				}
			}
		}
		if !s.Installed && !s.Running {
			continue
		}
		switch d.name {
		case "postfix":
			postfixUse(e, &s, r)
		case "opendkim":
			if b, err := e.read("/etc/opendkim.conf"); err == nil {
				if strings.HasPrefix(string(b), Marker) {
					s.Ours = true
				} else if signing := OpenDKIMSigning(string(b)); len(signing) > 0 {
					s.InUse = true
					s.Evidence = append(s.Evidence, "/etc/opendkim.conf signs mail ("+strings.Join(signing, ", ")+")")
				}
			}
		default:
			// Any other MTA or IMAP server that is installed is someone's.
			s.InUse = true
			if s.Running {
				s.Evidence = append(s.Evidence, d.name+" is running")
			} else {
				s.Evidence = append(s.Evidence, d.name+" is installed")
			}
		}
		out = append(out, s)
	}
	return out
}

// OpenDKIMSigning lists the signing settings an opendkim.conf contains.
// None means the package default, which signs nothing; the managed installer
// uses the same rule for what -adopt may replace.
func OpenDKIMSigning(conf string) []string {
	// Mode without "s" only verifies (Fedora's package default lists a key
	// file but runs "Mode v"). OpenDKIM's own default mode is "sv".
	if mode := activeValue(conf, "Mode"); mode != "" && !strings.Contains(mode, "s") {
		return nil
	}
	return activeKeys(conf, "KeyTable", "SigningTable", "KeyFile", "Domain", "Selector")
}

func activeValue(conf, key string) string {
	for _, l := range strings.Split(conf, "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && strings.EqualFold(f[0], key) {
			return f[1]
		}
	}
	return ""
}

func activeKeys(conf string, keys ...string) []string {
	var found []string
	for _, l := range strings.Split(conf, "\n") {
		f := strings.Fields(l)
		if len(f) < 2 || strings.HasPrefix(f[0], "#") {
			continue
		}
		for _, k := range keys {
			if strings.EqualFold(f[0], k) {
				found = append(found, k)
			}
		}
	}
	return found
}

// postfixUse decides whether an installed Postfix is someone's production
// mail server. A Postfix the Njiwa installer configured is ours; one installed
// by "apt install postfix" with "No configuration" (no main.cf) is not in use.
func postfixUse(e Env, s *Service, r *Report) {
	b, err := e.read("/etc/postfix/main.cf")
	if err != nil {
		return
	}
	cf := string(b)
	if strings.HasPrefix(cf, Marker) {
		s.Ours = true
		return
	}
	kv := parsePostfixConf(cf)
	s.Hostname = kv["myhostname"]
	s.Evidence = append(s.Evidence, postfixConfigEvidence(e, kv)...)
	if ev := masterServices(e); ev != "" {
		s.Evidence = append(s.Evidence, ev)
	}
	if n, ok := countQueue(e); ok && n > 0 {
		s.Evidence = append(s.Evidence, fmt.Sprintf("%d message(s) in the Postfix queue", n))
	} else if !ok {
		// Unknown is treated as in use: never adopt what could not be checked.
		s.Evidence = append(s.Evidence, "the Postfix queue could not be read (run as root)")
	}
	if sentRemote(e) {
		s.Evidence = append(s.Evidence, "/var/log/mail.log shows mail sent to other servers")
	}
	// Conservative: a configuration Njiwa did not write is in use unless it is
	// exactly what the distribution package writes (only the keys and values
	// below). Running alone is not evidence: "apt install postfix" (Internet
	// Site) starts it with the package configuration, which the installer may
	// replace with -adopt after backing it up.
	s.InUse = len(s.Evidence) > 0
}

// packageDefaultKeys are the main.cf settings the Debian/Ubuntu postfix
// package writes for "Internet Site" or "Local only". Any other key with a
// value is site configuration, so the server is treated as in use.
var packageDefaultKeys = map[string]bool{
	"smtpd_banner": true, "biff": true, "append_dot_mydomain": true, "readme_directory": true, "compatibility_level": true,
	"smtpd_tls_cert_file": true, "smtpd_tls_key_file": true, "smtpd_tls_security_level": true, "smtpd_use_tls": true,
	"smtpd_tls_session_cache_database": true, "smtp_tls_CApath": true, "smtp_tls_CAfile": true, "smtp_tls_security_level": true,
	"smtp_tls_session_cache_database": true, "smtpd_relay_restrictions": true, "myhostname": true, "alias_maps": true,
	"alias_database": true, "myorigin": true, "mydestination": true, "relayhost": true, "mynetworks": true,
	"mailbox_size_limit": true, "recipient_delimiter": true, "inet_interfaces": true, "inet_protocols": true,
	"delay_warning_time": true, "mailbox_command": true, "maillog_file": true,
	// The RHEL family's postfix package (AlmaLinux, Rocky Linux, RHEL, Fedora).
	"queue_directory": true, "command_directory": true, "daemon_directory": true, "data_directory": true, "mail_owner": true,
	"unknown_local_recipient_reject_code": true, "debug_peer_level": true, "debugger_command": true, "sendmail_path": true,
	"newaliases_path": true, "mailq_path": true, "setgid_group": true, "html_directory": true, "manpage_directory": true,
	"sample_directory": true, "meta_directory": true, "shlib_directory": true,
	"smtpd_tls_CAfile": true, "smtpd_tls_CApath": true, "tls_random_source": true, "smtpd_tls_loglevel": true,
	"smtp_tls_loglevel": true, "smtpd_tls_received_header": true, "smtpd_tls_mandatory_protocols": true,
	"smtpd_tls_protocols": true, "smtp_tls_mandatory_protocols": true, "smtp_tls_protocols": true,
	"default_database_type": true,
}

func parsePostfixConf(cf string) map[string]string {
	kv := map[string]string{}
	key := ""
	for _, l := range strings.Split(cf, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if (l[0] == ' ' || l[0] == '\t') && key != "" { // continuation line
			kv[key] += " " + t
			continue
		}
		if k, v, ok := strings.Cut(l, "="); ok {
			key = strings.TrimSpace(k)
			kv[key] = strings.TrimSpace(v)
		}
	}
	return kv
}

func postfixConfigEvidence(e Env, kv map[string]string) []string {
	var ev []string
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if kv[k] != "" && !packageDefaultKeys[k] {
			ev = append(ev, k+" is configured in /etc/postfix/main.cf")
		}
	}
	if v := kv["relayhost"]; v != "" {
		ev = append(ev, "relayhost is set in /etc/postfix/main.cf")
	}
	for _, n := range listItems(kv["mynetworks"]) {
		if !loopbackNet(n) {
			ev = append(ev, "Postfix relays mail for "+n+" (mynetworks)")
			break
		}
	}
	local := map[string]bool{"$myhostname": true, "localhost": true, "localhost.localdomain": true, "localhost.$mydomain": true}
	local[strings.ToLower(kv["myhostname"])] = true
	if h, err := e.read("/etc/hostname"); err == nil {
		name := strings.ToLower(strings.TrimSpace(string(h)))
		local[name] = true
		if short, _, ok := strings.Cut(name, "."); ok {
			local[short] = true
		}
	}
	for _, d := range listItems(kv["mydestination"]) {
		if !local[strings.ToLower(d)] {
			ev = append(ev, "Postfix receives mail for "+d+" (mydestination)")
			break
		}
	}
	// The packages' own placeholder certificates: Debian/Ubuntu's snakeoil
	// and the RHEL family's /etc/pki/tls/certs/postfix.pem.
	if c := kv["smtpd_tls_cert_file"]; c != "" && !strings.Contains(c, "snakeoil") && c != "/etc/pki/tls/certs/postfix.pem" {
		ev = append(ev, "Postfix uses its own TLS certificate ("+c+")")
	}
	return ev
}

func listItems(v string) []string {
	return strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
}

func loopbackNet(n string) bool {
	n = strings.Trim(n, "[]")
	switch n {
	case "127.0.0.0/8", "::1/128", "::ffff:127.0.0.0/104", "[::ffff:127.0.0.0]/104", "[::1]/128", "127.0.0.1", "::1":
		return true
	}
	return strings.HasPrefix(n, "::ffff:127.0.0.0]/") || strings.HasPrefix(n, "::1]/")
}

// masterServices reports a submission or smtps service enabled in
// master.cf: the server takes mail from users.
func masterServices(e Env) string {
	b, err := e.read("/etc/postfix/master.cf")
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && (f[0] == "submission" || f[0] == "smtps" || f[0] == "submissions" || f[0] == "465" || f[0] == "587") && f[1] == "inet" {
			return "master.cf enables the " + f[0] + " service (users submit mail here)"
		}
	}
	return ""
}

// sentRemote reads the end of the mail log for mail relayed to another
// server (local deliveries, such as cron mail, do not count).
func sentRemote(e Env) bool {
	f, err := os.Open(e.path("/var/log/mail.log"))
	if err != nil {
		return false
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > 1<<20 {
		_, _ = f.Seek(st.Size()-1<<20, 0)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		l := sc.Text()
		if strings.Contains(l, "status=sent") && strings.Contains(l, "relay=") && !strings.Contains(l, "relay=local") {
			return true
		}
	}
	return false
}

func countQueue(e Env) (int, bool) {
	n, ok := 0, true
	for _, q := range []string{"incoming", "active", "deferred", "hold", "maildrop"} {
		root := e.path("/var/spool/postfix/" + q)
		if _, err := os.Stat(root); err != nil {
			continue
		}
		err := filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
			if err != nil {
				ok = false
				return filepath.SkipDir
			}
			if !d.IsDir() {
				n++
			}
			return nil
		})
		if err != nil {
			ok = false
		}
	}
	return n, ok
}

// ---------------------------------------------------------------- networks, proxies, certificates

func scanNetworks(e Env) []Network {
	if e.Interfaces == nil {
		return nil
	}
	ifs, _ := e.Interfaces()
	var out []Network
	for _, i := range ifs {
		if i.Name != "docker0" && !strings.HasPrefix(i.Name, "br-") {
			continue
		}
		for _, a := range i.Addrs {
			if _, n, err := net.ParseCIDR(a); err == nil && n.IP.To4() != nil {
				out = append(out, Network{Interface: i.Name, Subnet: n.String()})
			}
		}
	}
	return out
}

var proxyComms = map[string]string{"nginx": "nginx", "apache2": "Apache", "httpd": "Apache", "caddy": "Caddy", "traefik": "Traefik", "haproxy": "HAProxy", "envoy": "Envoy"}

func scanProxies(procs []process, cs []Container) []string {
	set := map[string]bool{}
	for _, p := range procs {
		if n, ok := proxyComms[p.comm]; ok {
			set[n] = true
		}
	}
	for _, c := range cs {
		for comm, n := range proxyComms {
			if strings.Contains(strings.ToLower(c.Image), comm) {
				set[n+" (container "+c.Name+")"] = true
			}
		}
	}
	var out []string
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func scanCertificates(e Env) []Certificate {
	var out []Certificate
	add := func(path, owner string) {
		b, err := e.read(path)
		if err != nil {
			return
		}
		blk, _ := pem.Decode(b)
		if blk == nil {
			return
		}
		c, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			return
		}
		names := c.DNSNames
		if len(names) == 0 && c.Subject.CommonName != "" {
			names = []string{c.Subject.CommonName}
		}
		out = append(out, Certificate{Path: path, Names: names, NotAfter: c.NotAfter.UTC(), Owner: owner})
	}
	if ents, err := os.ReadDir(e.path("/etc/letsencrypt/live")); err == nil {
		for _, d := range ents {
			if d.IsDir() {
				add("/etc/letsencrypt/live/"+d.Name()+"/cert.pem", "letsencrypt")
			}
		}
	}
	for _, d := range mailcowDirs {
		add(d+"/data/assets/ssl/cert.pem", "mailcow")
	}
	add("/etc/postfix/njiwa/server.crt", "njiwa")
	return out
}

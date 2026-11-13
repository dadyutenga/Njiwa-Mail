// Package preflight runs the read-only host checks for managed mode (backlog
// E01): can this VPS send mail directly? It changes nothing on the host. When
// a check cannot be confirmed it says so ("cannot_confirm") instead of
// reporting green. Every dependency is injectable so each predicate is tested
// without a real host.
package preflight

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/njiwa-tz/mail/internal/installer/discover"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Status string

const (
	Ready                 Status = "ready"
	NeedsAction           Status = "needs_action"
	BlockedExternalAction Status = "blocked_external_action"
	CannotConfirm         Status = "cannot_confirm"
)

// Stable problem codes for failed checks.
const (
	CodeOSUnsupported     = "HOST_OS_UNSUPPORTED"
	CodeNoSystemd         = "HOST_NO_SYSTEMD"
	CodeDiskLow           = "HOST_DISK_LOW"
	CodeMemoryLow         = "HOST_MEMORY_LOW"
	CodeClockUnsynced     = "HOST_CLOCK_NOT_SYNCED"
	CodeIPNotLocal        = "HOST_IP_NOT_LOCAL"
	CodePTRMissing        = "PTR_MISSING"
	CodePTRMismatch       = "PTR_MISMATCH"
	CodeFCrDNSFailed      = "FCRDNS_FAILED"
	CodeHostnameNoA       = "HOSTNAME_NO_ADDRESS"
	CodeEgressBlocked     = "SMTP_EGRESS_BLOCKED"
	CodeMTAConflict       = "MTA_CONFLICT"
	CodeMissingParameters = "PREFLIGHT_PARAMETERS_MISSING"
	CodePrivatePortPublic = "PRIVATE_PORT_PUBLIC"
)

type Check struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Status   Status `json:"status"`
	Required bool   `json:"required"`
	Code     string `json:"code,omitempty"`
	Detail   string `json:"detail"`
	Fix      string `json:"fix,omitempty"`
}

// Supported lists the distributions the installer is tested on. Anything else
// is refused rather than half-supported.
// One list for the preflight and discovery (see discover.Supported).
var Supported = discover.Supported

type Resolver interface {
	LookupAddr(ctx context.Context, addr string) ([]string, error)
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
}

// Env is everything the checks touch on the host and the network.
type Env struct {
	ReadFile     func(string) ([]byte, error)
	Stat         func(string) (os.FileInfo, error)
	Statfs       func(path string) (freeBytes, freeInodes uint64, err error)
	InterfaceIPs func() ([]net.IP, error)
	Resolver     Resolver
	// DialBanner connects and returns the first line the server sends.
	DialBanner func(ctx context.Context, addr string) (string, error)
}

// Params are what the operator intends: the public mail hostname (PTR and
// HELO), the sending IP, where the spool lives, and which MX to probe port 25.
type Params struct {
	Hostname string
	IP       string
	SpoolDir string
	ProbeMX  string // domain whose MX is used for the port 25 probe (default gmail.com)
}

const (
	minFreeBytes  = 2 << 30
	minFreeInodes = 100_000
	minMemKB      = 512 * 1024
)

// Run executes every check. Order matters only for readability.
func Run(ctx context.Context, env Env, p Params) []Check {
	p.Hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(p.Hostname), "."))
	if p.ProbeMX == "" {
		p.ProbeMX = "gmail.com"
	}
	if p.SpoolDir == "" {
		p.SpoolDir = "/var"
	}
	out := []Check{checkOS(env), checkSystemd(env), checkDisk(env, p.SpoolDir), checkMemory(env), checkClock(env)}
	ip := net.ParseIP(strings.TrimSpace(p.IP))
	if p.Hostname == "" || ip == nil {
		out = append(out, Check{Key: "parameters", Title: "Mail hostname and sending IP", Status: NeedsAction, Required: true,
			Code: CodeMissingParameters, Detail: "the mail hostname and the public sending IP are needed for the DNS checks",
			Fix: "Set MAIL_HOSTNAME and OUTBOUND_IP (or pass -hostname and -ip)."})
	} else {
		out = append(out, checkLocalIP(env, ip), checkHostnameA(ctx, env, p.Hostname, ip), checkPTR(ctx, env, p.Hostname, ip))
	}
	out = append(out, checkEgress(ctx, env, p.ProbeMX), checkConflicts(ctx, env), checkPrivatePorts(env))
	return out
}

// Summary is the overall result: ready only when every required check is.
func Summary(cs []Check) (ok bool, first *Check) {
	ok = true
	for i := range cs {
		if cs[i].Required && cs[i].Status != Ready {
			if ok {
				first = &cs[i]
			}
			ok = false
		}
	}
	return ok, first
}

func osRelease(env Env) (id, version, pretty string) {
	b, err := env.ReadFile("/etc/os-release")
	if err != nil {
		return "", "", ""
	}
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "ID":
			id = v
		case "VERSION_ID":
			version = v
		case "PRETTY_NAME":
			pretty = v
		}
	}
	return
}

func checkOS(env Env) Check {
	c := Check{Key: "os", Title: "Supported operating system", Required: true}
	id, ver, pretty := osRelease(env)
	if id == "" {
		c.Status, c.Code, c.Detail = CannotConfirm, CodeOSUnsupported, "/etc/os-release could not be read"
		c.Fix = "Run the preflight on the host itself, not inside a container."
		return c
	}
	if discover.OSSupported(id, ver) {
		c.Status, c.Detail = Ready, pretty
		return c
	}
	c.Status, c.Code, c.Detail = NeedsAction, CodeOSUnsupported, pretty+" is not a tested distribution"
	c.Fix = "Use " + discover.SupportedText + ", or connect an existing mail server instead."
	return c
}

func checkSystemd(env Env) Check {
	c := Check{Key: "service_manager", Title: "systemd service manager", Required: true}
	if _, err := env.Stat("/run/systemd/system"); err == nil {
		c.Status, c.Detail = Ready, "systemd is running"
		return c
	}
	c.Status, c.Code, c.Detail, c.Fix = NeedsAction, CodeNoSystemd, "systemd is not running", "The installer manages Postfix with systemd."
	return c
}

func checkDisk(env Env, dir string) Check {
	c := Check{Key: "disk", Title: "Disk space for the mail queue", Required: true}
	free, inodes, err := env.Statfs(dir)
	if err != nil {
		c.Status, c.Detail = CannotConfirm, "could not read free space of "+dir
		return c
	}
	c.Detail = fmt.Sprintf("%.1f GiB and %d inodes free on %s", float64(free)/(1<<30), inodes, dir)
	if free < minFreeBytes || inodes < minFreeInodes {
		c.Status, c.Code, c.Fix = NeedsAction, CodeDiskLow, "Free at least 2 GiB and 100,000 inodes for the queue and logs."
		return c
	}
	c.Status = Ready
	return c
}

func checkMemory(env Env) Check {
	c := Check{Key: "memory", Title: "Available memory", Required: true}
	b, err := env.ReadFile("/proc/meminfo")
	if err != nil {
		c.Status, c.Detail = CannotConfirm, "/proc/meminfo could not be read"
		return c
	}
	for _, line := range strings.Split(string(b), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "MemAvailable:" {
			kb, _ := strconv.Atoi(f[1])
			c.Detail = fmt.Sprintf("%d MiB available", kb/1024)
			if kb < minMemKB {
				c.Status, c.Code, c.Fix = NeedsAction, CodeMemoryLow, "Provide at least 512 MiB of free memory."
				return c
			}
			c.Status = Ready
			return c
		}
	}
	c.Status, c.Detail = CannotConfirm, "MemAvailable not reported"
	return c
}

func checkClock(env Env) Check {
	c := Check{Key: "clock", Title: "Clock synchronised", Required: true}
	if _, err := env.Stat("/run/systemd/timesync/synchronized"); err == nil {
		c.Status, c.Detail = Ready, "systemd-timesyncd reports the clock is synchronised"
		return c
	}
	c.Status, c.Code, c.Detail = CannotConfirm, CodeClockUnsynced, "no time synchronisation was detected"
	c.Fix = "Enable NTP (timedatectl set-ntp true). TLS and DKIM signatures need a correct clock."
	return c
}

func checkLocalIP(env Env, ip net.IP) Check {
	c := Check{Key: "sending_ip", Title: "Sending IP is on this host", Required: true}
	ips, err := env.InterfaceIPs()
	if err != nil {
		c.Status, c.Detail = CannotConfirm, "could not list network interfaces"
		return c
	}
	for _, a := range ips {
		if a.Equal(ip) {
			c.Status, c.Detail = Ready, ip.String()+" is bound to a network interface"
			return c
		}
	}
	// Behind 1:1 NAT (some clouds) the public IP is not on an interface. That is
	// not proof of a problem, so it is reported as unconfirmed, not failed.
	c.Status, c.Code = CannotConfirm, CodeIPNotLocal
	c.Detail = ip.String() + " is not bound to any interface on this host (it may be NAT'ed by the provider)"
	c.Fix = "Check OUTBOUND_IP. If the provider uses 1:1 NAT, confirm with them that mail leaves from this address."
	return c
}

// onlyLoopback reports whether a lookup returned addresses and every one is a loopback address.
func onlyLoopback(addrs []net.IPAddr) bool {
	if len(addrs) == 0 {
		return false
	}
	for _, a := range addrs {
		if !a.IP.IsLoopback() {
			return false
		}
	}
	return true
}

func isNotFound(err error) bool {
	var de *net.DNSError
	return errors.As(err, &de) && de.IsNotFound
}

func checkHostnameA(ctx context.Context, env Env, host string, ip net.IP) Check {
	c := Check{Key: "hostname_a", Title: "Mail hostname points at the sending IP", Required: true}
	addrs, err := env.Resolver.LookupIPAddr(ctx, host)
	if err != nil && !isNotFound(err) {
		c.Status, c.Detail = CannotConfirm, "the DNS lookup for "+host+" failed"
		return c
	}
	var seen []string
	for _, a := range addrs {
		seen = append(seen, a.IP.String())
		if a.IP.Equal(ip) {
			c.Status, c.Detail = Ready, host+" resolves to "+ip.String()
			return c
		}
	}
	if onlyLoopback(addrs) {
		// Debian and Ubuntu map the machine's own name to 127.0.1.1 in /etc/hosts, which hides public DNS from this
		// host. Telling the operator to add a record that may already exist would be wrong, so say what is unknown.
		c.Status, c.Code = CannotConfirm, CodeHostnameNoA
		c.Detail = host + " resolves to " + strings.Join(seen, ", ") + " on this host, most likely from /etc/hosts, so public DNS could not be seen"
		c.Fix = "From another machine run: dig +short A " + host + " (it must print " + ip.String() + "). Or remove the name from the 127.0.1.1 line of /etc/hosts and run the preflight again."
		return c
	}
	c.Status, c.Code = NeedsAction, CodeHostnameNoA
	c.Detail = host + " resolves to [" + strings.Join(seen, ", ") + "]"
	c.Fix = "At your DNS provider, add an A record (AAAA for IPv6): " + host + " → " + ip.String() + "."
	return c
}

func checkPTR(ctx context.Context, env Env, host string, ip net.IP) Check {
	c := Check{Key: "ptr", Title: "Reverse DNS (PTR) matches, forward-confirmed", Required: true}
	names, err := env.Resolver.LookupAddr(ctx, ip.String())
	if err != nil && !isNotFound(err) {
		c.Status, c.Detail = CannotConfirm, "the PTR lookup for "+ip.String()+" failed"
		return c
	}
	fix := "In your VPS provider's panel, set the reverse DNS (PTR) of " + ip.String() + " to " + host + "."
	if len(names) == 0 {
		c.Status, c.Code, c.Detail, c.Fix = BlockedExternalAction, CodePTRMissing, ip.String()+" has no PTR record", fix
		return c
	}
	for _, n := range names {
		n = strings.ToLower(strings.TrimSuffix(n, "."))
		if n != host {
			continue
		}
		addrs, _ := env.Resolver.LookupIPAddr(ctx, n)
		for _, a := range addrs {
			if a.IP.Equal(ip) {
				c.Status, c.Detail = Ready, ip.String()+" → "+host+" → "+ip.String()
				return c
			}
		}
		if onlyLoopback(addrs) {
			c.Status, c.Code = CannotConfirm, CodeFCrDNSFailed
			c.Detail = "the PTR names " + host + ", but this host resolves " + host + " to a loopback address (probably /etc/hosts), so the forward lookup could not be confirmed"
			c.Fix = "From another machine run: dig +short A " + host + " (it must print " + ip.String() + ")."
			return c
		}
		c.Status, c.Code = NeedsAction, CodeFCrDNSFailed
		c.Detail = "the PTR names " + host + ", but " + host + " does not resolve back to " + ip.String()
		c.Fix = "Add or correct the A record " + host + " → " + ip.String() + "."
		return c
	}
	c.Status, c.Code, c.Detail, c.Fix = BlockedExternalAction, CodePTRMismatch, "the PTR of "+ip.String()+" is "+strings.Join(names, ", "), fix
	return c
}

// checkEgress connects to a large provider's MX on port 25 and reads the
// banner. No message is sent. Port 587 says nothing about direct delivery.
func checkEgress(ctx context.Context, env Env, probe string) Check {
	c := Check{Key: "outbound_25", Title: "Outbound connections on port 25", Required: true}
	mxs, err := env.Resolver.LookupMX(ctx, probe)
	if err != nil || len(mxs) == 0 {
		c.Status, c.Detail = CannotConfirm, "could not look up the MX of "+probe+" to test port 25"
		return c
	}
	target := strings.TrimSuffix(mxs[0].Host, ".")
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	banner, err := env.DialBanner(dctx, net.JoinHostPort(target, "25"))
	if err == nil && strings.HasPrefix(banner, "220") {
		c.Status, c.Detail = Ready, "connected to "+target+":25 and received a greeting"
		return c
	}
	c.Status, c.Code = BlockedExternalAction, CodeEgressBlocked
	c.Detail = "could not reach " + target + " on port 25"
	if err != nil {
		c.Detail += ": " + err.Error()
	}
	c.Fix = "Many VPS providers block outbound port 25. Ask your provider to open it, or connect an existing mail server instead."
	return c
}

// ManagedMarker is the first line of every Postfix main.cf the installer writes
// (internal/installer/managed.Marker; a test keeps the two in step).
const ManagedMarker = "# Managed by the Njiwa-Mail installer."

// checkConflicts looks for a mail server already on this host. The installer
// never overwrites one blindly.
func checkConflicts(ctx context.Context, env Env) Check {
	c := Check{Key: "mta_conflict", Title: "No other mail server on this host", Required: true}
	var found []string
	// Postfix is the one MTA the installer itself installs ("apt install postfix", answer "No configuration"),
	// so its binary alone is not a conflict. A configuration that Njiwa-Mail did not write is; one it wrote is ours.
	ours := false
	for _, p := range []string{"/usr/sbin/postfix", "/usr/sbin/exim4", "/usr/sbin/exim", "/usr/sbin/sendmail.sendmail", "/opt/mailcow-dockerized"} {
		if _, err := env.Stat(p); err != nil {
			continue
		}
		if p == "/usr/sbin/postfix" {
			b, rerr := env.ReadFile("/etc/postfix/main.cf")
			switch {
			case rerr != nil: // installed but not configured
				continue
			case strings.HasPrefix(string(b), ManagedMarker):
				ours = true
				continue
			}
			p = "/usr/sbin/postfix with a configuration Njiwa-Mail did not write (/etc/postfix/main.cf)"
		}
		found = append(found, p)
	}
	dctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if banner, err := env.DialBanner(dctx, "127.0.0.1:25"); err == nil && !ours {
		found = append(found, "something answers on port 25 ("+strings.TrimSpace(banner)+")")
	}
	if len(found) == 0 {
		c.Status, c.Detail = Ready, "no other mail server was found"
		if ours {
			c.Detail = "the only mail server is the Postfix that the Njiwa-Mail installer configured"
		}
		return c
	}
	c.Status, c.Code = NeedsAction, CodeMTAConflict
	c.Detail = "found: " + strings.Join(found, "; ")
	c.Fix = "Njiwa-Mail will not overwrite an existing mail server. Remove it, or connect it as an existing mail server instead."
	return c
}

// HostEnv is the real host.
func HostEnv() Env {
	return Env{
		ReadFile: os.ReadFile,
		Stat:     os.Stat,
		Statfs: func(path string) (uint64, uint64, error) {
			var st syscall.Statfs_t
			if err := syscall.Statfs(path, &st); err != nil {
				return 0, 0, err
			}
			return st.Bavail * uint64(st.Bsize), st.Ffree, nil
		},
		InterfaceIPs: func() ([]net.IP, error) {
			as, err := net.InterfaceAddrs()
			var out []net.IP
			for _, a := range as {
				if n, ok := a.(*net.IPNet); ok {
					out = append(out, n.IP)
				}
			}
			return out, err
		},
		Resolver: net.DefaultResolver,
		DialBanner: func(ctx context.Context, addr string) (string, error) {
			var d net.Dialer
			conn, err := d.DialContext(ctx, "tcp", addr)
			if err != nil {
				return "", err
			}
			defer conn.Close()
			if dl, ok := ctx.Deadline(); ok {
				_ = conn.SetDeadline(dl)
			}
			line, err := bufio.NewReader(conn).ReadString('\n')
			return strings.TrimSpace(line), err
		},
	}
}

// privatePorts must listen on loopback (or a private Docker gateway) only:
// the inbound listener, PostgreSQL, the API, the dashboard, the DKIM milter.
var privatePorts = map[int]string{2525: "Njiwa inbound listener", 5432: "PostgreSQL", 8080: "Njiwa API", 3000: "Njiwa dashboard", 8891: "DKIM signer"}

// checkPrivatePorts reads the kernel's listening sockets and reports any
// private port bound to all interfaces or a public address. The HTTPS proxy
// is the only way the API and dashboard should be reached from outside.
func checkPrivatePorts(env Env) Check {
	c := Check{Key: "private_ports", Title: "Private ports are not public", Required: true}
	var found []string
	read := false
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := env.ReadFile(f)
		if err != nil {
			continue
		}
		read = true
		for _, line := range strings.Split(string(b), "\n")[1:] {
			fs := strings.Fields(line)
			if len(fs) < 4 || fs[3] != "0A" { // 0A = LISTEN
				continue
			}
			ip, port, ok := parseProcAddr(fs[1])
			if !ok {
				continue
			}
			name, private := privatePorts[port]
			if !private || ip.IsLoopback() || (ip.IsPrivate() && !ip.IsUnspecified()) {
				continue
			}
			found = append(found, fmt.Sprintf("%s (port %d) listens on %s", name, port, ip))
		}
	}
	if !read {
		c.Status, c.Detail = CannotConfirm, "the listening sockets could not be read"
		return c
	}
	if len(found) == 0 {
		c.Status, c.Detail = Ready, "no private port listens on a public interface"
		return c
	}
	c.Status, c.Code, c.Detail = NeedsAction, CodePrivatePortPublic, strings.Join(found, "; ")
	c.Fix = "Bind these to 127.0.0.1 (the provided compose.yaml does), and reach the API and dashboard only through the HTTPS proxy. The installer's firewall table also drops them from outside."
	return c
}

// parseProcAddr decodes "0100007F:1F90" (little-endian IPv4) or the 32-hex
// IPv6 form from /proc/net/tcp*.
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
	for i := 0; i < len(raw); i += 4 { // each 32-bit word is little-endian
		ip[i], ip[i+1], ip[i+2], ip[i+3] = raw[i+3], raw[i+2], raw[i+1], raw[i]
	}
	return ip, int(port), true
}

package discover

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"
)

// Problem codes. Managed blockers stop "Send from my VPS" on this host (they
// can never be overridden: the installer does not replace a mail server that
// is in use); warnings apply to the Njiwa-Mail stack in either mode.
const (
	CodeMailcowPresent    = "MAILCOW_PRESENT"
	CodeOtherMTA          = "OTHER_MTA_PRESENT"
	CodePostfixInUse      = "POSTFIX_IN_USE"
	CodeOpenDKIMInUse     = "OPENDKIM_IN_USE"
	CodeMailPortInUse     = "MAIL_PORT_IN_USE"
	CodeInstallerPort     = "INSTALLER_PORT_IN_USE"
	CodeOSUnsupported     = "HOST_OS_UNSUPPORTED"
	CodeNoSystemd         = "HOST_NO_SYSTEMD"
	CodeMemoryLow         = "HOST_MEMORY_LOW"
	CodeDiskLow           = "HOST_DISK_LOW"
	CodeIMAPPresent       = "MAILBOX_SERVER_PRESENT"
	CodeNjiwaPortInUse    = "NJIWA_PORT_IN_USE"
	CodeSubnetConflict    = "NJIWA_SUBNET_CONFLICT"
	CodeProxyPresent      = "REVERSE_PROXY_PRESENT"
	CodeNoPublicIP        = "PUBLIC_IP_NOT_ON_HOST"
	CodeNjiwaResourcesLow = "NJIWA_RESOURCES_LOW"
	CodeDockerMissing     = "DOCKER_NOT_FOUND"
)

const (
	ModeManaged  = "managed_mta"
	ModeExisting = "existing_smtp"
)

// Supported distributions for the managed installer (same as preflight).
var Supported = map[string][]string{"debian": {"12"}, "ubuntu": {"22.04", "24.04"}}

// Needs are the host resources Njiwa-Mail itself uses.
type Needs struct {
	Subnet         string // NJIWA_SUBNET
	APIPort        int    // NJIWA_API_PORT
	DashboardPort  int    // NJIWA_DASHBOARD_PORT
	InboundPort    int    // NJIWA_INBOUND_PORT
	SubmissionPort int    // managed: worker submission listener
	MilterPort     int    // managed: OpenDKIM
}

func DefaultNeeds() Needs {
	return Needs{Subnet: "172.30.0.0/24", APIPort: 8080, DashboardPort: 3000, InboundPort: 2525, SubmissionPort: 10587, MilterPort: 8891}
}

type Issue struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
	Fix    string `json:"fix"`
}

// Candidate is an existing mail server Njiwa-Mail can connect to (Mode A).
// Credentials are never discovered: the operator enters a mailbox login.
type Candidate struct {
	Kind    string `json:"kind"` // mailcow | local_mta
	Label   string `json:"label"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	TLSMode string `json:"tls_mode"`
	Note    string `json:"note"`
}

type Recommendation struct {
	Mode            string      `json:"mode"`
	Reasons         []string    `json:"reasons"`
	ManagedBlockers []Issue     `json:"managed_blockers"`
	Warnings        []Issue     `json:"warnings"`
	Candidates      []Candidate `json:"candidates"`
}

// ManagedAllowed reports whether the managed installer may run on this host.
func (r Recommendation) ManagedAllowed() bool { return len(r.ManagedBlockers) == 0 }

// Recommend is a pure function of a report: what to choose, and why.
func Recommend(r *Report, n Needs) Recommendation {
	rec := Recommendation{ManagedBlockers: []Issue{}, Warnings: []Issue{}, Candidates: []Candidate{}}
	block := func(code, detail, fix string) {
		rec.ManagedBlockers = append(rec.ManagedBlockers, Issue{code, detail, fix})
	}
	warn := func(code, detail, fix string) { rec.Warnings = append(rec.Warnings, Issue{code, detail, fix}) }
	offerExisting := "Choose \"Use my existing mail server\": Njiwa-Mail connects to it over SMTP and changes nothing on it."

	// Existing mail infrastructure: never replaced, offered for reuse.
	if m := r.Mailcow; m != nil {
		where := "Mailcow"
		if m.Path != "" {
			where += " in " + m.Path
		}
		block(CodeMailcowPresent, where+" runs the mail service on this host.", offerExisting)
		host := m.Hostname
		if host == "" {
			host = "<your Mailcow hostname>"
		}
		rec.Candidates = append(rec.Candidates, Candidate{Kind: "mailcow", Label: "Mailcow on this server", Host: host, Port: 587, TLSMode: "starttls",
			Note: "Create a mailbox for Njiwa-Mail in Mailcow (for example noreply@your-domain) and enter its login. The Mailcow API key is optional."})
	}
	for _, s := range r.MailServices {
		switch {
		case s.Ours:
			continue
		case s.Name == "postfix" && s.InUse:
			block(CodePostfixInUse, "Postfix on this host already handles mail: "+strings.Join(s.Evidence, "; ")+".", offerExisting)
			rec.Candidates = append(rec.Candidates, localCandidate(r, s, "Postfix on this server"))
		case s.Name == "postfix":
			// Installed but unconfigured, or only the package default: the
			// installer may configure it (it backs up any file first).
		case s.Name == "opendkim" && s.InUse:
			block(CodeOpenDKIMInUse, "OpenDKIM on this host already signs mail: "+strings.Join(s.Evidence, "; ")+".", offerExisting)
		case s.Name == "dovecot":
			block(CodeIMAPPresent, "A mailbox server (Dovecot) runs on this host, so this is a production mail server.", offerExisting)
		case s.Name == "exim" || s.Name == "sendmail" || s.Name == "opensmtpd":
			block(CodeOtherMTA, strings.Join(s.Evidence, "; ")+". Installing Postfix next to it would conflict with it.", offerExisting)
			rec.Candidates = append(rec.Candidates, localCandidate(r, s, s.Name+" on this server"))
		}
	}
	// Ports: mail ports owned by anything but the Njiwa-managed Postfix.
	// A port owned by a mail service already reported above is not repeated.
	ourPostfix := false
	explained := map[string]bool{}
	for _, s := range r.MailServices {
		if s.Name == "postfix" && s.Ours {
			ourPostfix = true
		}
		// An unused Postfix (package default, adoptable) owns its ports
		// legitimately: the installer replaces its configuration with -adopt.
		if s.InUse || (s.Name == "postfix" && !s.Ours) {
			for _, d := range mtaDefs {
				if d.name == s.Name {
					for _, c := range d.comms {
						explained[c] = true
					}
				}
			}
		}
	}
	for _, port := range []int{25, 465, 587, 993, 995} {
		for _, p := range r.PortsIn(port) {
			if ourPostfix && p.Process == "master" {
				continue
			}
			if (mailcowCovers(rec) && p.Process == "docker-proxy") || explained[p.Process] {
				continue // already explained above
			}
			block(CodeMailPortInUse, fmt.Sprintf("Port %d is already used by %s.", port, owner(p)),
				"Njiwa-Mail never takes over a port another program owns. "+offerExisting)
		}
	}
	for _, port := range []int{n.SubmissionPort, n.MilterPort} {
		for _, p := range r.PortsIn(port) {
			if (ourPostfix || explained["master"]) && (p.Process == "master" || p.Process == "opendkim") {
				continue
			}
			block(CodeInstallerPort, fmt.Sprintf("Port %d, which the managed installer needs, is used by %s.", port, owner(p)),
				fmt.Sprintf("Pass another port to the installer (-submission 172.30.0.1:<port>), or stop the program on port %d if it is not needed.", port))
		}
	}
	// Host suitability for running Postfix.
	if vs, ok := Supported[r.Host.OSID]; !ok || !contains(vs, r.Host.OSVersion) {
		os := r.Host.OS
		if os == "" {
			os = "This operating system"
		}
		block(CodeOSUnsupported, os+" is not one the managed installer is tested on (Debian 12, Ubuntu 22.04, Ubuntu 24.04).", offerExisting)
	}
	if !r.Host.Systemd {
		block(CodeNoSystemd, "systemd is not running; the managed installer runs Postfix and OpenDKIM as systemd services.", offerExisting)
	}
	if r.Host.MemAvailMB > 0 && r.Host.MemAvailMB < 512 {
		block(CodeMemoryLow, fmt.Sprintf("Only %d MiB of memory is available; the mail service needs at least 512 MiB.", r.Host.MemAvailMB), "Free memory or add more, or "+strings.ToLower(offerExisting[:1])+offerExisting[1:])
	}
	if r.Host.DiskFreeMB > 0 && r.Host.DiskFreeMB < 2048 {
		block(CodeDiskLow, fmt.Sprintf("Only %d MiB is free under /var; the mail queue needs at least 2 GiB.", r.Host.DiskFreeMB), "Free disk space under /var.")
	}

	// Njiwa-Mail's own stack, in either mode.
	for _, np := range []struct {
		port      int
		what, env string
	}{{n.APIPort, "the Njiwa-Mail API", "NJIWA_API_PORT"}, {n.DashboardPort, "the dashboard", "NJIWA_DASHBOARD_PORT"}, {n.InboundPort, "the inbound listener", "NJIWA_INBOUND_PORT"}} {
		for _, p := range r.PortsIn(np.port) {
			if isNjiwa(p, n) {
				continue
			}
			warn(CodeNjiwaPortInUse, fmt.Sprintf("Port %d (%s) is used by %s.", np.port, np.what, owner(p)),
				fmt.Sprintf("Set %s in .env to a free port before docker compose up; nothing else needs to change.", np.env))
		}
	}
	if _, want, err := net.ParseCIDR(n.Subnet); err == nil {
		for _, net0 := range r.DockerNetworks {
			if _, have, err := net.ParseCIDR(net0.Subnet); err == nil && overlaps(want, have) && !isNjiwaNet(net0, n) {
				warn(CodeSubnetConflict, fmt.Sprintf("The Docker network on %s uses %s, which overlaps Njiwa-Mail's network %s.", net0.Interface, net0.Subnet, n.Subnet),
					"Set NJIWA_SUBNET in .env to a free /24 (for example 172.31.250.0/24).")
			}
		}
	}
	if len(r.ReverseProxies) > 0 {
		warn(CodeProxyPresent, "A reverse proxy is already running ("+strings.Join(r.ReverseProxies, ", ")+").",
			"Keep it: add the locations from deploy/nginx.conf (or the equivalent) to your existing site. Njiwa-Mail does not replace its certificates.")
	}
	if len(r.Host.PublicIPs) == 0 {
		warn(CodeNoPublicIP, "No public IP address is configured on this host (it may be behind NAT).",
			"Managed mode needs a public IP with a PTR record and outbound port 25; the preflight checks them. Existing SMTP mode does not.")
	}
	if r.Host.MemTotalMB > 0 && r.Host.MemTotalMB < 1024 {
		warn(CodeNjiwaResourcesLow, fmt.Sprintf("The host has %d MiB of memory; Njiwa-Mail (PostgreSQL, API, worker) needs about 1 GiB.", r.Host.MemTotalMB), "Use a host with at least 1 GiB of memory.")
	}
	if !r.Host.DockerPresent {
		warn(CodeDockerMissing, "Docker was not found.", "Install Docker Engine and the Compose plugin to run Njiwa-Mail (docs/INSTALL.md).")
	}

	if len(rec.ManagedBlockers) > 0 {
		rec.Mode = ModeExisting
		mailInUse := false
		for _, b := range rec.ManagedBlockers {
			switch b.Code {
			case CodeMailcowPresent, CodeOtherMTA, CodePostfixInUse, CodeOpenDKIMInUse, CodeMailPortInUse, CodeIMAPPresent:
				mailInUse = true
			}
		}
		if mailInUse {
			rec.Reasons = append(rec.Reasons, "This host already runs a mail service. Njiwa-Mail will not replace or reconfigure it: use it (or another mail server) as an existing mail server.")
		} else {
			rec.Reasons = append(rec.Reasons, "This host cannot run the Njiwa-managed mail service (see below). Njiwa-Mail itself runs here fine: connect it to an existing mail server or SMTP provider.")
		}
		if len(rec.Candidates) > 0 {
			rec.Reasons = append(rec.Reasons, "A mail server was found that Njiwa-Mail can connect to: "+rec.Candidates[0].Label+".")
		} else {
			rec.Reasons = append(rec.Reasons, "Connect any SMTP server or provider you have (host, port, a login allowed to send as your domain).")
		}
	} else {
		rec.Mode = ModeManaged
		rec.Reasons = append(rec.Reasons, "No mail server is installed or in use here, so Njiwa-Mail can set up its own (Postfix and DKIM signing) on this host.",
			"Sending directly also needs a public IP with a PTR record and outbound port 25; the preflight checks them. If your provider blocks port 25, use an existing mail server or SMTP provider instead.")
	}
	return rec
}

func localCandidate(r *Report, s Service, label string) Candidate {
	port, tls := 25, "starttls"
	if len(r.PortsIn(587)) > 0 {
		port = 587
	} else if len(r.PortsIn(465)) > 0 {
		port, tls = 465, "implicit"
	}
	host := s.Hostname
	if host == "" {
		host = "<this server's mail hostname>"
	}
	return Candidate{Kind: "local_mta", Label: label, Host: host, Port: port, TLSMode: tls,
		Note: "Use a login this server accepts for authenticated submission, allowed to send as your domain. Use the hostname on the server's TLS certificate."}
}

func mailcowCovers(rec Recommendation) bool {
	for _, b := range rec.ManagedBlockers {
		if b.Code == CodeMailcowPresent {
			return true
		}
	}
	return false
}

func owner(p Port) string {
	switch {
	case p.Process == "docker-proxy" && p.Container != "":
		return "a Docker container (" + p.Container + ")"
	case p.Process == "docker-proxy":
		return "a Docker container"
	case p.Process == "master":
		return "Postfix (not configured by Njiwa-Mail)"
	case p.Process != "":
		return p.Process
	}
	return "another program (run discovery as root to see which)"
}

// isNjiwa: a listener published for a container on Njiwa's own network.
func isNjiwa(p Port, n Needs) bool {
	if p.Process != "docker-proxy" || p.Container == "" {
		return false
	}
	host, _, _ := net.SplitHostPort(p.Container)
	_, sub, err := net.ParseCIDR(n.Subnet)
	return err == nil && sub.Contains(net.ParseIP(host))
}

func isNjiwaNet(nw Network, n Needs) bool {
	_, have, err1 := net.ParseCIDR(nw.Subnet)
	_, want, err2 := net.ParseCIDR(n.Subnet)
	return err1 == nil && err2 == nil && have.String() == want.String()
}

func overlaps(a, b *net.IPNet) bool { return a.Contains(b.IP) || b.Contains(a.IP) }

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func runCommand(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}

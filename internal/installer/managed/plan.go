// Package managed installs and maintains the managed-mode mail delivery
// service on the operator's own host (backlog E02-E04, E09): Postfix for
// direct-to-MX delivery and a bounce route, OpenDKIM for signing.
//
// It is an operator-run, root-only CLI, outside the API trust boundary: the
// Njiwa web process never touches host configuration. Every change goes
// plan -> approve exact digest -> apply (with backup) -> validate ->
// automatic rollback on failure, and leaves a receipt. It refuses to replace a
// mail server it did not install.
package managed

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/installer/discover"
)

// Marker is the first line of every file the installer owns.
const Marker = "# Managed by the Njiwa-Mail installer. Manual changes are replaced on the next apply."

type Params struct {
	Hostname       string   // public mail hostname: HELO, PTR, certificate name
	BounceDomain   string   // domain of BOUNCE_ADDRESS, and any per-domain return-path domains (D04), comma-separated; DSNs to them are relayed to Njiwa
	Domains        []string // sending domains to sign (and allow as envelope senders)
	Selector       string   // DKIM selector
	SubmissionBind string   // private listener for the Njiwa worker, host:port
	InboundRelay   string   // Njiwa inbound listener that receives DSNs, host:port
	Root           string   // filesystem root (tests use a temp dir)
	StateDir       string   // receipts and backups
	// Adopt allows replacing an existing Postfix configuration that the
	// installer did not write (it is backed up first).
	Adopt bool
	// NoFirewall skips the host firewall table (G02). By default the
	// installer adds an nftables table that only drops Njiwa's private ports
	// from outside; its policy is accept, so SSH and other services are never
	// affected.
	NoFirewall bool
	// ComposeSubnet is the Docker network the worker connects from when the
	// submission listener is on its gateway (default 172.30.0.0/24).
	ComposeSubnet string
	// RenewCerts reissues the server and client certificates now, even when
	// they are not yet due (they are reissued automatically within
	// RenewBefore of expiry).
	RenewCerts bool
	// RemovePackages (rollback only) also removes the packages that apply
	// installed.
	RemovePackages bool `json:"-"`
	// Platform describes the distribution (detected when empty).
	Platform Platform `json:"platform"`
	// Now is the planning time (tests); zero means the current time.
	Now time.Time
}

// bounceDomains lists the normalised bounce domains (empty when bounces are not received).
func (p Params) bounceDomains() []string {
	var out []string
	for _, d := range strings.Split(p.BounceDomain, ",") {
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}

func (p *Params) normalise() error {
	p.Hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(p.Hostname), "."))
	{
		// One or several bounce domains (D04), canonical and sorted so the plan digest is stable.
		seenB, list := map[string]bool{}, []string{}
		for _, d := range strings.Split(p.BounceDomain, ",") {
			d = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
			if d != "" && !seenB[d] {
				seenB[d] = true
				list = append(list, d)
			}
		}
		sort.Strings(list)
		p.BounceDomain = strings.Join(list, ",")
	}
	if p.Selector == "" {
		p.Selector = "dkim"
	}
	if p.SubmissionBind == "" {
		p.SubmissionBind = "127.0.0.1:10587"
	}
	if p.Root == "" {
		p.Root = "/"
	}
	if p.StateDir == "" {
		p.StateDir = "/var/lib/njiwa-mail/installer"
	}
	if p.ComposeSubnet == "" {
		p.ComposeSubnet = "172.30.0.0/24"
	}
	if _, _, err := net.ParseCIDR(p.ComposeSubnet); err != nil {
		return errors.New("-compose-subnet must be a CIDR such as 172.30.0.0/24")
	}
	if !hostRe(p.Hostname) {
		return errors.New("a valid public mail hostname is required (-hostname)")
	}
	for _, d := range p.bounceDomains() {
		if !hostRe(d) {
			return errors.New("the bounce domain " + d + " is not a valid domain name")
		}
	}
	if p.BounceDomain != "" && p.InboundRelay == "" {
		return errors.New("bounces need the Njiwa inbound listener address (-inbound host:port)")
	}
	if h, port, err := net.SplitHostPort(p.SubmissionBind); err != nil || net.ParseIP(h) == nil || port == "" {
		return errors.New("-submission must be ip:port, for example 127.0.0.1:10587 or the Docker bridge address")
	} else if ip := net.ParseIP(h); ip.IsUnspecified() {
		return errors.New("-submission must not listen on all interfaces")
	}
	if p.InboundRelay != "" {
		if _, _, err := net.SplitHostPort(p.InboundRelay); err != nil {
			return errors.New("-inbound must be host:port")
		}
	}
	if !selRe(p.Selector) {
		return errors.New("the DKIM selector may contain only letters, digits and hyphens")
	}
	seen := map[string]bool{}
	var ds []string
	for _, d := range p.Domains {
		d = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
		if d == "" || seen[d] {
			continue
		}
		if !hostRe(d) {
			return fmt.Errorf("%q is not a valid domain name", d)
		}
		seen[d] = true
		ds = append(ds, d)
	}
	sort.Strings(ds)
	p.Domains = ds
	if len(ds) == 0 {
		return errors.New("at least one sending domain is required (-domains, or the project domains in the database)")
	}
	return nil
}

func hostRe(s string) bool {
	if len(s) == 0 || len(s) > 253 || !strings.Contains(s, ".") {
		return false
	}
	for _, l := range strings.Split(s, ".") {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, r := range l {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

func selRe(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return s != ""
}

// File is one file the installer writes.
type File struct {
	Path    string      `json:"path"` // absolute path on the host
	Mode    os.FileMode `json:"mode"`
	Content []byte      `json:"-"`
	Change  string      `json:"change"` // create | update | unchanged
}

// Action is a generated secret created only when missing (keys, certificates).
type Action struct {
	Kind   string `json:"kind"` // generate_pki | generate_dkim_key
	Target string `json:"target"`
	Needed bool   `json:"needed"`
}

type Plan struct {
	Params  Params   `json:"params"`
	Files   []File   `json:"files"`
	Actions []Action `json:"actions"`
	Digest  string   `json:"digest"`
	// Conflict is set when the host has a mail server the installer did not
	// install; Apply refuses unless Params.Adopt.
	Conflict string `json:"conflict,omitempty"`
	// Blockers are mail services in use or ports owned by another program.
	// Apply always refuses while there are any; -adopt does not override them.
	Blockers []discover.Issue `json:"blockers,omitempty"`
	// MissingPackages are installed by apply (Debian/Ubuntu) as an approved
	// step; PackageManager is "apt" when that is possible.
	MissingPackages []string `json:"missing_packages,omitempty"`
	PackageManager  string   `json:"package_manager,omitempty"`
	// Warnings need the operator's attention but do not block apply.
	Warnings []string `json:"warnings,omitempty"`
}

// Paths of everything the installer owns.
const (
	pfDir      = "/etc/postfix"
	njDir      = "/etc/postfix/njiwa"
	dkimDir    = "/etc/opendkim"
	dkimConf   = "/etc/opendkim.conf"
	dkimTables = "/etc/opendkim/njiwa"
	keyDir     = "/etc/opendkim/keys"
	clientDir  = "/etc/njiwa-mail/submission"
	// FirewallFile is the nftables ruleset the installer owns.
	FirewallFile = "/etc/njiwa-mail/firewall.nft"
	firewallUnit = "/etc/systemd/system/njiwa-mail-firewall.service"
	// postfixBootDropIn orders Postfix after Docker when its submission listener is on the Docker bridge address.
	postfixBootDropIn = "/etc/systemd/system/postfix@.service.d/njiwa-mail.conf"
	milterSocket      = "inet:127.0.0.1:8891"
)

func (p Params) path(abs string) string { return filepath.Join(p.Root, abs) }

func dkimKeyPath(d, sel string) string { return filepath.Join(keyDir, d, sel+".private") }

// BuildPlan computes the desired configuration and compares it with the host.
// It reads but never writes.
func BuildPlan(p Params) (*Plan, error) {
	if err := p.normalise(); err != nil {
		return nil, err
	}
	if p.Platform.Family == "" && p.Platform.ID == "" {
		p.Platform = DetectPlatform(p.Root, nil)
	}
	pl := &Plan{Params: p}
	// Production protection: the same read-only discovery as "njiwa-mail
	// discover". A mail service in use, or a port another program owns, is a
	// blocker that nothing overrides. An unused configuration that Njiwa did
	// not write (the package default) is replaced only with -adopt, after a
	// backup.
	pl.Blockers = installBlockers(p)
	for _, f := range []string{pfDir + "/main.cf", dkimConf} {
		if b, err := os.ReadFile(p.path(f)); err == nil && !strings.HasPrefix(string(b), Marker) && !isPackageDefault(f, b) {
			pl.Conflict = "an existing configuration (" + f + ") was not written by Njiwa-Mail; it shows no sign of use"
			break
		}
	}
	pl.MissingPackages, pl.PackageManager = missingPackages(p)
	if pl.PackageManager == "dnf" && p.Platform.needsEPEL() && contains(pl.MissingPackages, "opendkim") {
		// OpenDKIM is in EPEL on Enterprise Linux: enabling it is its own,
		// visible step (epel-release is then removed again by -remove-packages).
		pl.MissingPackages = append([]string{"epel-release"}, pl.MissingPackages...)
	}
	if p.Platform.SELinux {
		// SELinux: Postfix may listen only on ports labelled smtp_port_t, and
		// files written by the installer get their policy labels.
		if _, port, err := net.SplitHostPort(p.SubmissionBind); err == nil && port != "25" && port != "465" && port != "587" {
			pl.Actions = append(pl.Actions, Action{Kind: "selinux_port", Target: "tcp/" + port + " labelled smtp_port_t (semanage port)", Needed: true})
		}
		pl.Actions = append(pl.Actions, Action{Kind: "selinux_relabel", Target: "restorecon on /etc/postfix /etc/opendkim /etc/opendkim.conf /etc/njiwa-mail", Needed: true})
	}
	if p.Platform.Firewalld && p.BounceDomain != "" {
		pl.Warnings = append(pl.Warnings, "firewalld is running and its default zone does not accept port 25, where bounce reports arrive. "+
			"Njiwa-Mail does not change firewalld; open it yourself: firewall-cmd --permanent --add-service=smtp && firewall-cmd --reload")
	}
	if len(pl.MissingPackages) > 0 && pl.PackageManager == "" && p.Platform.ID == "rhel" {
		pl.Warnings = append(pl.Warnings, "OpenDKIM comes from EPEL. On RHEL enable it first: subscription-manager repos --enable codeready-builder-for-rhel-"+
			strconv.Itoa(p.Platform.major())+"-x86_64-rpms && dnf install https://dl.fedoraproject.org/pub/epel/epel-release-latest-"+strconv.Itoa(p.Platform.major())+".noarch.rpm")
	}

	host, port, _ := net.SplitHostPort(p.SubmissionBind)
	want := []File{
		{Path: pfDir + "/main.cf", Mode: 0644, Content: mainCF(p)},
		{Path: pfDir + "/master.cf", Mode: 0644, Content: masterCF(p)},
		{Path: njDir + "/senders", Mode: 0644, Content: sendersMap(p)},
		{Path: njDir + "/transport", Mode: 0644, Content: transportMap(p)},
		{Path: dkimConf, Mode: 0644, Content: openDKIMConf(p)},
		{Path: dkimTables + "/key.table", Mode: 0640, Content: keyTable(p)},
		{Path: dkimTables + "/signing.table", Mode: 0640, Content: signingTable(p)},
		{Path: dkimTables + "/trusted.hosts", Mode: 0640, Content: trustedHosts(p, host)},
		{Path: clientDir + "/endpoint", Mode: 0644, Content: []byte(Marker + "\nhost=" + host + "\nport=" + port + "\nserver_name=" + p.Hostname + "\n")},
	}
	if !p.NoFirewall {
		want = append(want,
			File{Path: FirewallFile, Mode: 0644, Content: firewallRules(p, host, port)},
			File{Path: firewallUnit, Mode: 0644, Content: firewallService()})
	}
	// With the worker in Docker the listener is on the bridge address (for example 172.30.0.1). That address only exists once
	// Docker has created the Compose network, so at boot Postfix failed with "bind: Cannot assign requested address" and
	// stayed down until someone restarted it (found on a clean Ubuntu 24.04 VM after the first reboot).
	if ip := net.ParseIP(host); ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
		want = append(want, File{Path: p.Platform.postfixDropIn(), Mode: 0644, Content: postfixBootOrdering(host)})
	}
	for i := range want {
		cur, err := os.ReadFile(p.path(want[i].Path))
		switch {
		case err != nil:
			want[i].Change = "create"
		case string(cur) == string(want[i].Content):
			want[i].Change = "unchanged"
		default:
			want[i].Change = "update"
		}
	}
	pl.Files = want
	_, caErr := os.Stat(p.path(njDir + "/ca.crt"))
	pl.Actions = append(pl.Actions, Action{Kind: "generate_pki", Target: njDir + " and " + clientDir, Needed: caErr != nil})
	if caErr == nil {
		now := p.now()
		for _, c := range []struct{ kind, path string }{{"renew_server_certificate", njDir + "/server.crt"}, {"renew_client_certificate", clientDir + "/client.crt"}} {
			if certDue(p.path(c.path), now, p.RenewCerts) {
				pl.Actions = append(pl.Actions, Action{Kind: c.kind, Target: c.path, Needed: true})
			}
		}
		if exp, err := CertExpiry(p.path(njDir + "/ca.crt")); err == nil && exp.Sub(now) < CAWarnBefore {
			pl.Warnings = append(pl.Warnings, fmt.Sprintf("the local submission CA expires on %s; it is not renewed automatically (see OPERATIONS.md)", exp.Format("2006-01-02")))
		}
	}
	for _, d := range p.Domains {
		_, err := os.Stat(p.path(dkimKeyPath(d, p.Selector)))
		pl.Actions = append(pl.Actions, Action{Kind: "generate_dkim_key", Target: dkimKeyPath(d, p.Selector), Needed: err != nil})
	}

	h := sha256.New()
	fmt.Fprintf(h, "conflict=%s\nadopt=%v\npackages=%v %s\n", pl.Conflict, p.Adopt, pl.MissingPackages, pl.PackageManager)
	for _, b := range pl.Blockers {
		fmt.Fprintf(h, "blocker=%s %s\n", b.Code, b.Detail)
	}
	for _, f := range pl.Files {
		fmt.Fprintf(h, "%s %o %d\n", f.Path, f.Mode, len(f.Content))
		h.Write(f.Content)
	}
	for _, a := range pl.Actions {
		fmt.Fprintf(h, "%s %s %v\n", a.Kind, a.Target, a.Needed)
	}
	pl.Digest = hex.EncodeToString(h.Sum(nil))[:24]
	return pl, nil
}

// Summary is a human-readable description of the plan.
func (pl *Plan) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Mail hostname:     %s\nSigning domains:   %s (selector %s)\nBounce domain:     %s\nWorker submission: %s (mutual TLS)\n\n",
		pl.Params.Hostname, strings.Join(pl.Params.Domains, ", "), pl.Params.Selector, orNone(strings.ReplaceAll(pl.Params.BounceDomain, ",", ", ")), pl.Params.SubmissionBind)
	for _, f := range pl.Files {
		fmt.Fprintf(&b, "  %-9s %s\n", f.Change, f.Path)
	}
	for _, a := range pl.Actions {
		if a.Needed {
			verb := "generate"
			switch {
			case strings.HasPrefix(a.Kind, "renew_"):
				verb = "renew"
			case strings.HasPrefix(a.Kind, "selinux_"):
				verb = "selinux"
			}
			fmt.Fprintf(&b, "  %-9s %s\n", verb, a.Target)
		}
	}
	if len(pl.MissingPackages) > 0 {
		switch pl.PackageManager {
		case "apt":
			fmt.Fprintf(&b, "  %-9s packages %s (apt-get, Postfix with \"No configuration\"; nothing is removed)\n", "install", strings.Join(pl.MissingPackages, " "))
		case "dnf":
			fmt.Fprintf(&b, "  %-9s packages %s (dnf; nothing is removed)\n", "install", strings.Join(pl.MissingPackages, " "))
		default:
			fmt.Fprintf(&b, "\nMISSING PACKAGES: install %s with your package manager first.\n", strings.Join(pl.MissingPackages, " "))
		}
	}
	if pl.Conflict != "" {
		fmt.Fprintf(&b, "\nCONFLICT: %s. Apply replaces it only with -adopt (backed up first).\n", pl.Conflict)
	}
	if len(pl.Blockers) > 0 {
		b.WriteString("\nBLOCKED: Njiwa-Mail will not install its mail service on this host, and nothing will be changed:\n")
		for _, x := range pl.Blockers {
			fmt.Fprintf(&b, "  [%s] %s\n      %s\n", x.Code, x.Detail, x.Fix)
		}
	}
	for _, w := range pl.Warnings {
		fmt.Fprintf(&b, "\nWARNING: %s.\n", w)
	}
	fmt.Fprintf(&b, "\nPlan digest: %s\n", pl.Digest)
	return b.String()
}

func orNone(s string) string {
	if s == "" {
		return "(none: bounces are not received)"
	}
	return s
}

func mainCF(p Params) []byte {
	relay := ""
	transport := ""
	if p.BounceDomain != "" {
		relay = strings.Join(p.bounceDomains(), ", ")
		transport = p.Platform.mapType() + ":" + njDir + "/transport"
	}
	return []byte(Marker + `
compatibility_level = ` + p.Platform.compatLevel() + `
myhostname = ` + p.Hostname + `
myorigin = $myhostname
smtp_helo_name = $myhostname
smtpd_banner = $myhostname ESMTP
biff = no
append_dot_mydomain = no
readme_directory = no
inet_interfaces = all
inet_protocols = all

# This server delivers nothing locally and is no one's relay. mynetworks only
# covers the host itself for the local pickup daemon; relaying is granted by
# the worker's client certificate on the private submission listener.
mydestination =
# Signed return paths (bounces+<id>.<tag>@bounce-domain) are relayed like the plain address.
recipient_delimiter = +
mynetworks = 127.0.0.0/8 [::1]/128
local_transport = error:local delivery is disabled
alias_maps =
alias_database =

# Inbound on port 25: only DSNs and feedback for the bounce domain, relayed to
# the Njiwa inbound listener. Everything else is refused.
relay_domains = ` + relay + `
transport_maps = ` + transport + `
smtpd_relay_restrictions = reject_unauth_destination
smtpd_recipient_restrictions = reject_unauth_destination
smtpd_helo_required = yes
disable_vrfy_command = yes
message_size_limit = 26214400

# TLS. Outbound to recipient MX servers is opportunistic, as is common; the
# submission listener requires a verified client certificate.
smtp_tls_security_level = may
smtp_tls_loglevel = 1
smtpd_tls_security_level = may
smtpd_tls_cert_file = ` + njDir + `/server.crt
smtpd_tls_key_file = ` + njDir + `/server.key
smtpd_tls_CAfile = ` + njDir + `/ca.crt
smtpd_tls_fingerprint_digest = sha256
smtpd_tls_loglevel = 1
relay_clientcerts = ` + p.Platform.mapType() + `:` + njDir + `/relay_clientcerts

# Used only by the submission service (see master.cf).
njiwa_submission_sender_restrictions = check_sender_access ` + p.Platform.mapType() + `:` + njDir + `/senders, reject
njiwa_submission_relay_restrictions = permit_tls_clientcerts, reject
`)
}

// masterCF is the standard Debian/Ubuntu service list (chroot off, so DNS
// works without copying files into the spool) plus the private submission
// listener. Only that listener passes mail to the DKIM signer, and a signer
// outage makes it answer 4xx (tempfail) instead of sending unsigned mail.
func masterCF(p Params) []byte {
	return []byte(Marker + `
# service type  private unpriv  chroot  wakeup  maxproc command + args
smtp      inet  n       -       n       -       -       smtpd
` + p.SubmissionBind + ` inet n -       n       -       -       smtpd
  -o syslog_name=postfix/njiwa-submission
  -o smtpd_tls_security_level=encrypt
  -o smtpd_tls_req_ccert=yes
  -o smtpd_relay_restrictions=$njiwa_submission_relay_restrictions
  -o smtpd_recipient_restrictions=$njiwa_submission_relay_restrictions
  -o smtpd_sender_restrictions=$njiwa_submission_sender_restrictions
  -o smtpd_milters=` + milterSocket + `
  -o milter_default_action=tempfail
  -o milter_macro_daemon_name=ORIGINATING
pickup    unix  n       -       n       60      1       pickup
cleanup   unix  n       -       n       -       0       cleanup
qmgr      unix  n       -       n       300     1       qmgr
tlsmgr    unix  -       -       n       1000?   1       tlsmgr
rewrite   unix  -       -       n       -       -       trivial-rewrite
bounce    unix  -       -       n       -       0       bounce
defer     unix  -       -       n       -       0       bounce
trace     unix  -       -       n       -       0       bounce
verify    unix  -       -       n       -       1       verify
flush     unix  n       -       n       1000?   0       flush
proxymap  unix  -       -       n       -       -       proxymap
proxywrite unix -       -       n       -       1       proxymap
smtp      unix  -       -       n       -       -       smtp
relay     unix  -       -       n       -       -       smtp
showq     unix  n       -       n       -       -       showq
error     unix  -       -       n       -       -       error
retry     unix  -       -       n       -       -       error
discard   unix  -       -       n       -       -       discard
local     unix  -       n       n       -       -       local
virtual   unix  -       n       n       -       -       virtual
lmtp      unix  -       -       n       -       -       lmtp
anvil     unix  -       -       n       -       1       anvil
scache    unix  -       -       n       -       1       scache
postlog   unix-dgram n  -       n       -       1       postlogd
`)
}

// sendersMap lists the envelope-sender domains the worker may use: the
// signing domains and the bounce domain. Anything else is rejected.
func sendersMap(p Params) []byte {
	var b strings.Builder
	b.WriteString(Marker + "\n")
	ds := append([]string{}, p.Domains...)
	ds = append(ds, p.bounceDomains()...)
	sort.Strings(ds)
	last := ""
	for _, d := range ds {
		if d != last {
			b.WriteString(d + " OK\n")
		}
		last = d
	}
	return []byte(b.String())
}

func transportMap(p Params) []byte {
	s := Marker + "\n"
	h, port, _ := net.SplitHostPort(p.InboundRelay)
	for _, d := range p.bounceDomains() {
		s += d + " smtp:[" + h + "]:" + port + "\n"
	}
	return []byte(s)
}

func openDKIMConf(p Params) []byte {
	return []byte(Marker + `
Syslog                  yes
UMask                   007
Mode                    s
Canonicalization        relaxed/simple
OversignHeaders         From
SubDomains              no
Socket                  ` + strings.Replace(milterSocket, "inet:127.0.0.1:", "inet:", 1) + `@127.0.0.1
PidFile                 /run/opendkim/opendkim.pid
UserID                  opendkim
KeyTable                refile:` + dkimTables + `/key.table
SigningTable            refile:` + dkimTables + `/signing.table
ExternalIgnoreList      refile:` + dkimTables + `/trusted.hosts
InternalHosts           refile:` + dkimTables + `/trusted.hosts
MTA                     ORIGINATING
`)
}

func keyTable(p Params) []byte {
	var b strings.Builder
	b.WriteString(Marker + "\n")
	for _, d := range p.Domains {
		fmt.Fprintf(&b, "%s._domainkey.%s %s:%s:%s\n", p.Selector, d, d, p.Selector, dkimKeyPath(d, p.Selector))
	}
	return []byte(b.String())
}

func signingTable(p Params) []byte {
	var b strings.Builder
	b.WriteString(Marker + "\n")
	for _, d := range p.Domains {
		fmt.Fprintf(&b, "*@%s %s._domainkey.%s\n", d, p.Selector, d)
	}
	return []byte(b.String())
}

func trustedHosts(p Params, submissionHost string) []byte {
	hosts := []string{"127.0.0.1", "::1"}
	if submissionHost != "127.0.0.1" && submissionHost != "::1" {
		hosts = append(hosts, submissionHost)
	}
	return []byte(Marker + "\n" + strings.Join(hosts, "\n") + "\n")
}

// SubmissionPort is the worker's port from the bind address.
func SubmissionPort(bind string) int {
	_, p, _ := net.SplitHostPort(bind)
	n, _ := strconv.Atoi(p)
	return n
}

// privatePorts are Njiwa ports that must never be reachable from outside the
// host: the inbound listener, PostgreSQL, the API and dashboard (served via
// the HTTPS proxy), and the DKIM milter.
var privatePorts = []string{"2525", "5432", "8080", "3000", "8891"}

// firewallRules is a self-contained nftables table. It replaces itself
// atomically on every load, keeps policy accept (it never blocks SSH or other
// services) and only drops Njiwa's private ports from outside the host. The
// worker's submission port is open to the host and, when it listens on the
// Docker gateway, to the Compose network only. Port 25 is left open: it is
// the public MTA port (bounces) and refuses relaying by itself.
func firewallRules(p Params, submissionHost, submissionPort string) []byte {
	allow := ""
	if ip := net.ParseIP(submissionHost); ip != nil && !ip.IsLoopback() {
		fam := "ip"
		if ip.To4() == nil {
			fam = "ip6"
		}
		_, n, _ := net.ParseCIDR(p.ComposeSubnet)
		allow = "\t\ttcp dport " + submissionPort + " " + fam + " saddr " + n.String() + " accept\n"
	}
	return []byte(Marker + `
# Loaded by njiwa-mail-firewall.service. Inspect with: nft list table inet njiwa_mail
table inet njiwa_mail
delete table inet njiwa_mail
table inet njiwa_mail {
	chain input {
		type filter hook input priority filter - 1; policy accept;
		iif "lo" accept
` + allow + `		tcp dport { ` + submissionPort + `, ` + strings.Join(privatePorts, ", ") + ` } counter drop
	}
}
`)
}

// postfixBootOrdering makes Postfix start after Docker and wait (up to 90 s) for the submission address to exist.
// "$$" is systemd's escape for a literal "$".
func postfixBootOrdering(host string) []byte {
	return []byte(Marker + `
[Unit]
After=docker.service
Wants=docker.service

[Service]
TimeoutStartSec=180
ExecStartPre=/bin/sh -c 'for i in $$(seq 1 90); do ip -4 -o addr show | grep -q "inet ` + strings.ReplaceAll(host, ".", `\.`) + `/" && exit 0; sleep 1; done; echo "submission address ` + host + ` is not up after 90 s; Postfix will fail to bind it" >&2; exit 0'
`)
}

func firewallService() []byte {
	return []byte(Marker + `
[Unit]
Description=Njiwa Mail host firewall table (private ports)
Before=network-pre.target postfix.service
Wants=network-pre.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/sbin/nft -f ` + FirewallFile + `
ExecStop=/usr/sbin/nft delete table inet njiwa_mail

[Install]
WantedBy=multi-user.target
`)
}

// installBlockers runs the read-only discovery against the host the plan is
// for and keeps the findings that make a managed install unsafe. Host
// suitability (OS, systemd, memory) is the preflight's job.
func installBlockers(p Params) []discover.Issue {
	r := discover.Scan(discover.Env{Root: p.Root})
	n := discover.DefaultNeeds()
	if _, port, err := net.SplitHostPort(p.SubmissionBind); err == nil {
		n.SubmissionPort, _ = strconv.Atoi(port)
	}
	rec := discover.Recommend(r, n)
	var out []discover.Issue
	for _, b := range rec.ManagedBlockers {
		switch b.Code {
		case discover.CodeMailcowPresent, discover.CodeOtherMTA, discover.CodePostfixInUse, discover.CodeOpenDKIMInUse,
			discover.CodeMailPortInUse, discover.CodeInstallerPort, discover.CodeIMAPPresent:
			out = append(out, b)
		}
	}
	return out
}

// isPackageDefault reports a configuration file as shipped by the
// distribution package, which carries no site configuration to protect.
func isPackageDefault(path string, b []byte) bool {
	return path == dkimConf && len(discover.OpenDKIMSigning(string(b))) == 0
}

// missingPackages lists the programs the installer needs that are not
// installed, and whether apt can install them.
func missingPackages(p Params) ([]string, string) {
	var miss []string
	for pkg, bin := range map[string]string{"postfix": "/usr/sbin/postfix", "opendkim": "/usr/sbin/opendkim"} {
		if _, err := os.Stat(p.path(bin)); err != nil {
			miss = append(miss, pkg)
		}
	}
	// LMDB tables are a plugin package on the RHEL family (postfix-lmdb):
	// needed when Postfix itself does not list lmdb.
	if p.Platform.Family == "rhel" && p.Platform.mapType() == "lmdb" && p.Platform.MapType == "" {
		if _, err := os.Stat(p.path("/usr/lib64/postfix/postfix-lmdb.so")); err != nil {
			miss = append(miss, "postfix-lmdb")
		}
	}
	sort.Strings(miss)
	if len(miss) == 0 {
		return nil, ""
	}
	pm := p.Platform.packageManager()
	// RHEL itself has no epel-release package in its own repositories; the
	// operator enables EPEL (a subscription matter) first.
	if pm == "dnf" && p.Platform.ID == "rhel" && p.Platform.needsEPEL() && contains(miss, "opendkim") {
		return miss, ""
	}
	return miss, pm
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

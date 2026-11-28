package service

import (
	"context"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	installHostRe     = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	installSelectorRe = regexp.MustCompile(`^[A-Za-z0-9-]{1,63}$`)
)

// InstallerFacts is what the installation knows about a managed install; the
// operator supplies only what it cannot know.
type InstallerFacts struct {
	Hostname, BounceDomain, Selector            string
	SubmissionBind, InboundRelay, ComposeSubnet string
	Domains                                     []string
}

// InstallerFacts fills in the install parameters. hostname, bounce and
// selector override what the server knows when given.
func (s *Service) InstallerFacts(ctx context.Context, hostname, bounce, selector string) (*InstallerFacts, error) {
	f := &InstallerFacts{InboundRelay: "127.0.0.1:2525", ComposeSubnet: s.Opts.NjiwaSubnet}
	if f.ComposeSubnet == "" {
		f.ComposeSubnet = "172.30.0.0/24"
	}
	f.Hostname = strings.ToLower(strings.TrimSpace(hostname))
	if f.Hostname == "" {
		f.Hostname = strings.ToLower(s.Opts.MailHost)
	}
	if !installHostRe.MatchString(f.Hostname) {
		return nil, badRequest("enter the public mail hostname, for example mail.yourdomain")
	}
	var err error
	if f.Domains, err = s.ClaimedDomains(ctx); err != nil {
		return nil, err
	}
	if len(f.Domains) == 0 {
		return nil, &Error{Status: 409, Code: "no_verified_domain", Message: "verify a sending domain in a project first"}
	}
	sel, _, host, port := s.managedKeys(ctx)
	for _, v := range []string{selector, sel, s.Opts.DKIMSelector, "njiwa"} {
		if v = strings.TrimSpace(v); v != "" {
			f.Selector = v
			break
		}
	}
	if !installSelectorRe.MatchString(f.Selector) {
		return nil, badRequest("the DKIM selector may contain only letters, digits and hyphens")
	}
	f.SubmissionBind = "172.30.0.1:10587"
	if _, n, err := net.ParseCIDR(f.ComposeSubnet); err == nil && n.IP.To4() != nil {
		gw := append(net.IP{}, n.IP.To4()...)
		gw[3]++
		f.SubmissionBind = net.JoinHostPort(gw.String(), "10587")
	}
	if host != "" && port != "" {
		f.SubmissionBind = net.JoinHostPort(host, port)
	}
	var domains []string
	for _, d := range strings.Split(bounce, ",") {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
			if !installHostRe.MatchString(d) {
				return nil, badRequest("the bounce domain is not a valid domain name")
			}
			domains = append(domains, d)
		}
	}
	if len(domains) == 0 {
		for d := range s.bounceDomains(ctx) {
			domains = append(domains, d)
		}
	}
	sort.Strings(domains)
	f.BounceDomain = strings.Join(domains, ",")
	return f, nil
}

// InstallApprover is who approved the plan with this digest in the dashboard.
func (s *Service) InstallApprover(ctx context.Context, digest string) string {
	var who string
	_ = s.Pool.QueryRow(ctx, `SELECT actor FROM audit_log WHERE action='installer.apply_requested' AND target_id=$1
ORDER BY id DESC LIMIT 1`, digest).Scan(&who)
	return who
}

// RecordInstallOnce stores a finished install unless it is already recorded.
// The host keeps its last job, so a receipt seen before stays ignored even
// after it was rolled back.
func (s *Service) RecordInstallOnce(ctx context.Context, a Actor, in ManagedInstall, submissionBind string) error {
	var have bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM installer_receipts WHERE plan_digest=$1 AND action='apply'
  AND result_code='ok' AND rollback_state <> 'rolled_back')
  OR EXISTS (SELECT 1 FROM audit_log WHERE action='installer.apply' AND details->>'receipt'=$2)`, in.PlanDigest, in.ReceiptID).Scan(&have); err != nil {
		return unavailable("managed_install", err)
	}
	if have {
		return nil
	}
	host, port, err := net.SplitHostPort(submissionBind)
	if err != nil {
		return badRequest("the installer result is incomplete")
	}
	in.SubmissionHost = host
	in.SubmissionPort, _ = strconv.Atoi(port)
	return s.RecordManagedInstall(ctx, a, in)
}

// RecordRollbackOnce marks the receipts of a plan rolled back, once per host
// job: a job seen before must not undo a later installation.
func (s *Service) RecordRollbackOnce(ctx context.Context, a Actor, digest, job string) error {
	var seen, open bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM audit_log WHERE action='installer.rollback_recorded' AND details->>'job'=$2),
  EXISTS (SELECT 1 FROM installer_receipts WHERE plan_digest=$1 AND rollback_state <> 'rolled_back')`, digest, job).Scan(&seen, &open); err != nil {
		return unavailable("managed_rollback", err)
	}
	if seen || !open {
		return nil
	}
	if err := s.RecordManagedRollback(ctx, a, digest); err != nil {
		return err
	}
	return s.Audit(ctx, a, "installer.rollback_recorded", "installer", digest, map[string]any{"job": job})
}

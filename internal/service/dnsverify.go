package service

import (
	"context"
	"net"
	"strings"
	"time"
)

// Verification after apply. A successful provider response is never readiness:
// each record is judged from what public DNS answers.

const (
	VerifyVerified    = "verified"
	VerifyPending     = "pending"
	VerifyFailed      = "failed"
	VerifyNeedsAction = "needs_action"
)

const propagationWindow = 24 * time.Hour

type VerifyItem struct {
	Key    string `json:"key"`
	Type   string `json:"type"`
	Name   string `json:"name"`
	State  string `json:"state"`
	Detail string `json:"detail"`
	Found  string `json:"found,omitempty"`
}

func notVisible(found string) bool {
	return found == "" || found == "(no record)" || found == "(lookup failed)" || found == "(not checked yet)"
}

// fcrdns checks reverse DNS of ip and that the PTR name resolves back to it
// (and, when host is set, that it is that host).
func fcrdns(ctx context.Context, r Resolver, ip net.IP, host string) (bool, []string, error) {
	names, err := r.LookupAddr(ctx, ip.String())
	pass := false
	var vals []string
	for _, n := range names {
		n = fqdn(n)
		vals = append(vals, n)
		if host != "" && n != host {
			continue
		}
		if addrs, aerr := r.LookupIPAddr(ctx, n); aerr == nil {
			for _, a := range addrs {
				pass = pass || a.IP.Equal(ip)
			}
		}
	}
	return pass, vals, err
}

func (s *Service) port25(ctx context.Context) error {
	if s.Opts.Port25Probe != nil {
		return s.Opts.Port25Probe(ctx)
	}
	d := net.Dialer{Timeout: 6 * time.Second}
	c, err := d.DialContext(ctx, "tcp", "gmail-smtp-in.l.google.com:25")
	if err != nil {
		return err
	}
	return c.Close()
}

// VerifyDNSSetup re-checks DNS now and reports each record and the domain.
func (s *Service) VerifyDNSSetup(ctx context.Context, sc *Scope, a Actor, id string, r Resolver) (map[string]any, error) {
	row, err := s.loadSetup(ctx, sc.ProjectID, id)
	if err != nil {
		return nil, err
	}
	if err := s.dnsLimit(ctx, "dns:verify:domain:"+row.Domain, time.Hour, 120); err != nil {
		return nil, err
	}
	if r == nil {
		r = net.DefaultResolver
	}
	recs, err := s.CheckDomain(ctx, row.DomainID, r)
	if err != nil {
		return nil, err
	}
	changes, err := s.changesOf(ctx, sc.ProjectID, id)
	if err != nil {
		return nil, err
	}
	ours := map[string]bool{}
	var since time.Time
	for _, c := range changes {
		if c.Status == "applied" {
			ours[c.ItemKey] = true
		}
	}
	_ = s.Pool.QueryRow(ctx, `SELECT COALESCE(max(applied_at), now()) FROM dns_changes WHERE setup_id=$1 AND status='applied'`, id).Scan(&since)
	recent := time.Since(since) < propagationWindow

	items := []VerifyItem{}
	for _, rec := range recs {
		it := VerifyItem{Key: rec.Key, Type: rec.Type, Name: rec.Host, Found: rec.Found}
		switch {
		case rec.Pass:
			it.State, it.Detail = VerifyVerified, "Seen in public DNS and valid."
		case rec.Placeholder:
			it.State, it.Detail = VerifyNeedsAction, "Njiwa-Mail does not know this value yet. "+rec.Hint
		case notVisible(rec.Found) && ours[rec.Key] && recent:
			it.State, it.Detail = VerifyPending, "Published, but not visible in public DNS yet. This can take a while; the check repeats automatically."
		case notVisible(rec.Found):
			it.State, it.Detail = VerifyNeedsAction, "Not published. "+rec.Hint
		default:
			it.State, it.Detail = VerifyFailed, rec.Hint
		}
		items = append(items, it)
	}

	// Managed delivery: the sending address, its reverse DNS and outbound port 25.
	kind := s.senderKind(ctx)
	if kind == senderManaged {
		ip := net.ParseIP(strings.TrimSpace(s.Opts.OutboundIP))
		host := fqdn(s.Opts.MailHost)
		switch {
		case ip == nil:
			items = append(items, VerifyItem{Key: "public_ip", Type: "IP", Name: "OUTBOUND_IP", State: VerifyNeedsAction, Detail: "Set OUTBOUND_IP to this server's public address."})
		case !PublicIP(ip):
			items = append(items, VerifyItem{Key: "public_ip", Type: "IP", Name: ip.String(), State: VerifyNeedsAction, Detail: "This is not a public address, so receivers cannot connect back to it."})
		default:
			items = append(items, VerifyItem{Key: "public_ip", Type: "IP", Name: ip.String(), State: VerifyVerified, Detail: "A public address."})
			pass, vals, perr := fcrdns(ctx, r, ip, host)
			p := VerifyItem{Key: "ptr", Type: "PTR", Name: ip.String(), Found: observed(vals, perr)}
			switch {
			case pass:
				p.State, p.Detail = VerifyVerified, "Reverse DNS names this host and the name resolves back to the same address (forward-confirmed)."
			case len(vals) == 0:
				p.State, p.Detail = VerifyNeedsAction, "No reverse DNS (PTR) is set. Ask your hosting provider to set it to "+host+"; many receivers distrust mail without it."
			default:
				p.State, p.Detail = VerifyFailed, "The PTR does not name "+host+" and resolve back to "+ip.String()+"."
			}
			items = append(items, p)
		}
		pr := VerifyItem{Key: "port25", Type: "TCP", Name: "outbound port 25"}
		if perr := s.port25(ctx); perr != nil {
			pr.State, pr.Detail = VerifyNeedsAction, "This server could not open an outbound connection on port 25. Many hosting providers block it by default: ask yours to open it, or send through a relay."
		} else {
			pr.State, pr.Detail = VerifyVerified, "Outbound connections on port 25 work from this server."
		}
		items = append(items, pr)
	}

	var verifiedAt *time.Time
	var spf, dkim, dmarc, mx bool
	var purpose string
	var signed *time.Time
	if err := s.Pool.QueryRow(ctx, `SELECT verified_at, spf_ok, dkim_ok, dmarc_ok, mx_ok, purpose, dkim_verified_at FROM domains WHERE id=$1 AND project_id=$2`, row.DomainID, sc.ProjectID).
		Scan(&verifiedAt, &spf, &dkim, &dmarc, &mx, &purpose, &signed); err != nil {
		return nil, notFound(err)
	}
	ready := verifiedAt != nil
	if sendingPurpose(purpose) {
		ready = ready && spf && dkim && dmarc
	}
	if receivingPurpose(purpose) {
		ready = ready && mx
	}
	failed, pending := false, false
	for _, it := range items {
		switch it.State {
		case VerifyFailed:
			failed, ready = true, false
		case VerifyPending:
			pending, ready = true, false
		case VerifyNeedsAction:
			if it.Key != "return_path" && it.Key != "return_path_spf" {
				ready = false
			}
		}
	}
	overall, msg := VerifyNeedsAction, "Some records still need your attention."
	switch {
	case ready:
		overall, msg = VerifyVerified, "DNS checks pass for this domain. That does not guarantee inbox delivery: send a test message and confirm it arrives signed before sending real mail."
	case failed:
		overall, msg = VerifyFailed, "A record is published but wrong. See the details below."
	case pending:
		overall, msg = VerifyPending, "Waiting for DNS to propagate. Njiwa-Mail keeps checking."
	}
	_ = s.Audit(ctx, a, "dns.verify", "dns_setup", id, map[string]any{"project": sc.Project, "domain": row.Domain, "state": overall})
	return map[string]any{"setup_id": id, "domain": row.Domain, "state": overall, "ready": ready, "message": msg, "items": items,
		"signing_proven": signed != nil, "checked_at": time.Now().UTC()}, nil
}

// RecheckDNSSetups re-checks domains whose DNS setup was applied recently and
// is not ready yet, at most every 10 minutes each, for at most 48 hours.
func (s *Service) RecheckDNSSetups(ctx context.Context, r Resolver) (int, error) {
	rows, err := s.Pool.Query(ctx, `SELECT DISTINCT s.domain_id::text FROM dns_setups s JOIN domains d ON d.id=s.domain_id
WHERE s.status IN ('applied','partial') AND s.updated_at > now() - interval '48 hours'
  AND (d.last_checked_at IS NULL OR d.last_checked_at < now() - interval '10 minutes')
  AND NOT (d.verified_at IS NOT NULL AND d.spf_ok AND d.dkim_ok AND d.dmarc_ok)
LIMIT 50`)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if _, err := s.CheckDomain(ctx, id, r); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

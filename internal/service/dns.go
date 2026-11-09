package service

import (
	"context"
	"net"
	"sort"
	"strings"
	"time"
)

// The DNS health checker is read-only. It records what each record should be
// and what was observed; the operator fixes DNS by hand.

// Resolver is the subset of net.Resolver the checker needs (replaceable in tests).
type Resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupAddr(ctx context.Context, addr string) ([]string, error)
}

type DNSResult struct {
	Domain   string `json:"domain"`
	Type     string `json:"record_type"`
	Expected string `json:"expected"`
	Observed string `json:"observed"`
	Pass     bool   `json:"pass"`
}

func fqdn(h string) string { return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), ".")) }

func observed(vals []string, err error) string {
	if len(vals) == 0 {
		if err != nil {
			if de, ok := err.(*net.DNSError); ok && de.IsNotFound {
				return "(no record)"
			}
			return "(lookup failed)"
		}
		return "(no record)"
	}
	s := strings.Join(vals, " | ")
	if len(s) > 1000 {
		s = s[:1000]
	}
	return s
}

func pick(vals []string, prefix string) []string {
	var out []string
	for _, v := range vals {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), prefix) {
			out = append(out, v)
		}
	}
	return out
}

// sendingDomains returns the distinct domains of live sender identities.
func (s *Service) sendingDomains(ctx context.Context) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `SELECT DISTINCT split_part(si.email,'@',2) FROM sender_identities si
JOIN environments e ON e.id=si.environment_id WHERE e.mode='live' ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// RunDNSChecks checks SPF, DKIM, DMARC and MX for every sending domain (and
// the bounce address domain), MX for the inbound domain and reverse DNS of the
// outbound IP, then stores the results.
func (s *Service) RunDNSChecks(ctx context.Context, r Resolver) ([]DNSResult, error) {
	if r == nil {
		r = net.DefaultResolver
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	domains, err := s.sendingDomains(ctx)
	if err != nil {
		return nil, unavailable("dns_domains", err)
	}
	if _, d, ok := splitAddr(s.Opts.BounceAddress); ok {
		domains = append(domains, d)
	}
	sort.Strings(domains)
	host, ip := fqdn(s.Opts.MailHost), strings.TrimSpace(s.Opts.OutboundIP)
	var out []DNSResult
	seen := map[string]bool{}

	mx := func(domain, why string) {
		recs, err := r.LookupMX(ctx, domain)
		var vals []string
		pass := false
		for _, m := range recs {
			vals = append(vals, fqdn(m.Host))
			pass = pass || (host != "" && fqdn(m.Host) == host)
		}
		exp := "MX pointing at the Mailcow host (set MAIL_HOSTNAME to check the exact value)"
		if host != "" {
			exp = "MX " + host + " (" + why + ")"
		} else {
			pass = len(recs) > 0
		}
		out = append(out, DNSResult{domain, "MX", exp, observed(vals, err), pass})
	}

	for _, d := range domains {
		if seen[d] {
			continue
		}
		seen[d] = true

		txt, err := r.LookupTXT(ctx, d)
		spf := pick(txt, "v=spf1")
		exp := "v=spf1 mx -all (must authorise the Mailcow IP)"
		pass := len(spf) == 1
		if ip != "" {
			exp = "v=spf1 ip4:" + ip + " -all (or another mechanism that covers " + ip + ")"
			if strings.Contains(ip, ":") {
				exp = "v=spf1 ip6:" + ip + " -all (or another mechanism that covers " + ip + ")"
			}
		}
		if pass {
			rec := " " + strings.ToLower(spf[0]) + " "
			covers := strings.Contains(rec, " mx ") || strings.Contains(rec, " a ") || strings.Contains(rec, "include:") ||
				(host != "" && strings.Contains(rec, "a:"+host)) || (ip != "" && strings.Contains(rec, ":"+strings.ToLower(ip)))
			pass = covers && !strings.Contains(rec, "+all")
		}
		out = append(out, DNSResult{d, "SPF", exp, observed(spf, err), pass})

		name := s.Opts.DKIMSelector + "._domainkey." + d
		txt, err = r.LookupTXT(ctx, name)
		joined := strings.ReplaceAll(strings.Join(txt, ""), " ", "")
		pass = strings.Contains(joined, "p=") && !strings.Contains(joined, "p=;") && !strings.HasSuffix(joined, "p=")
		out = append(out, DNSResult{d, "DKIM", name + " TXT v=DKIM1; k=rsa; p=<public key shown in Mailcow>", observed(txt, err), pass})

		// DMARC: the domain's own record, else the parent domain's (organisational fallback).
		txt, err = r.LookupTXT(ctx, "_dmarc."+d)
		dm := pick(txt, "v=dmarc1")
		if len(dm) == 0 {
			if i := strings.IndexByte(d, '.'); i > 0 && strings.Contains(d[i+1:], ".") {
				ptxt, perr := r.LookupTXT(ctx, "_dmarc."+d[i+1:])
				if p := pick(ptxt, "v=dmarc1"); len(p) > 0 {
					dm, err = p, perr
				}
			}
		}
		pass = len(dm) == 1 && strings.Contains(strings.ToLower(strings.ReplaceAll(dm[0], " ", "")), ";p=")
		out = append(out, DNSResult{d, "DMARC", "_dmarc." + d + " TXT v=DMARC1; p=none (then quarantine once reports look clean)", observed(dm, err), pass})

		mx(d, "bounces and replies return here")
	}

	if in := s.Opts.InboundDomain; in != "" && !seen[in] {
		mx(in, "inbound mail is relayed by Mailcow")
	}

	if ip != "" {
		names, err := r.LookupAddr(ctx, ip)
		pass := false
		var vals []string
		for _, n := range names {
			vals = append(vals, fqdn(n))
			pass = pass || (host != "" && fqdn(n) == host)
		}
		exp := "PTR for " + ip + " equal to the Mailcow hostname (set MAIL_HOSTNAME to check the exact value)"
		if host != "" {
			exp = "PTR " + host
		} else {
			pass = len(names) > 0
		}
		out = append(out, DNSResult{ip, "PTR", exp, observed(vals, err), pass})
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	for _, c := range out {
		if _, err := tx.Exec(ctx, `INSERT INTO dns_checks(domain, record_type, expected, observed, pass) VALUES ($1,$2,$3,$4,$5)`,
			c.Domain, c.Type, c.Expected, c.Observed, c.Pass); err != nil {
			return nil, unavailable("dns_store", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, unavailable("commit", err)
	}
	if out == nil {
		out = []DNSResult{}
	}
	return out, nil
}

// LatestDNSChecks returns the most recent result per domain and record type.
func (s *Service) LatestDNSChecks(ctx context.Context) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT DISTINCT ON (domain, record_type) domain, record_type, expected, observed, pass, checked_at
FROM dns_checks ORDER BY domain, record_type, checked_at DESC`)
}

// DNSCheckDue reports whether the newest stored check is older than maxAge.
func (s *Service) DNSCheckDue(ctx context.Context, maxAge time.Duration) (bool, error) {
	var due bool
	err := s.Pool.QueryRow(ctx, `SELECT COALESCE(max(checked_at) < now() - make_interval(secs => $1::float8), true) FROM dns_checks`, maxAge.Seconds()).Scan(&due)
	return due, err
}

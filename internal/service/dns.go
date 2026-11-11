package service

import (
	"context"
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/dnsauth"
)

// The DNS health checker is read-only. It records what each record should be
// and what was observed; the operator fixes DNS by hand.

// Resolver is the subset of net.Resolver the checker needs (replaceable in tests).
type Resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupAddr(ctx context.Context, addr string) ([]string, error)
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// testNetIP is TEST-NET-1 (RFC 5737): never a real sender. Evaluating SPF for
// it checks that a record is valid and within limits when the real sending
// address is not known to Njiwa-Mail.
var testNetIP = net.ParseIP("192.0.2.0")

// senderIP is the address mail really leaves from, when Njiwa-Mail knows it:
// the managed MTA host, or the Mailcow server OUTBOUND_IP describes. For a
// generic SMTP server it is unknown (nil).
func (s *Service) senderIP(kind string) net.IP {
	if kind == senderGeneric {
		return nil
	}
	return net.ParseIP(strings.TrimSpace(s.Opts.OutboundIP))
}

// evalSPF checks domain's SPF. With a known sending IP it must evaluate to
// pass for that IP; otherwise the record must be valid (no permerror or
// temperror, no +all), and the hint says only a test message can prove it.
func evalSPF(ctx context.Context, r Resolver, domain string, ip net.IP) (bool, string, []string) {
	txt, _ := r.LookupTXT(ctx, domain)
	recs := dnsauth.FindSPF(txt)
	switch {
	case len(recs) == 0:
		return false, "No SPF record was found. Publish one TXT record that starts with v=spf1.", recs
	case len(recs) > 1:
		return false, "Two SPF records were found. Merge them into one record: receivers treat two as an error.", recs
	}
	if p := dnsauth.Problems(recs[0]); len(p) > 0 {
		return false, p[0] + ".", recs
	}
	if ip != nil {
		o := dnsauth.CheckHost(ctx, r, ip, domain)
		if o.Result == dnsauth.Pass {
			return true, "", recs
		}
		why := "SPF evaluates to " + string(o.Result) + " for " + ip.String() + ", the address this mail is sent from"
		if o.Reason != "" {
			why += ": " + o.Reason
		}
		if o.Result == dnsauth.Fail || o.Result == dnsauth.SoftFail || o.Result == dnsauth.Neutral {
			why += ". Add a mechanism that covers it, such as ip4:" + ip.String() + "."
		}
		return false, why, recs
	}
	o := dnsauth.CheckHost(ctx, r, testNetIP, domain)
	if o.Result == dnsauth.PermError || o.Result == dnsauth.TempError || o.Result == dnsauth.None {
		return false, "The SPF record cannot be evaluated: " + o.Reason + ".", recs
	}
	return true, "The record is valid. Njiwa-Mail cannot see the address your mail server sends from, so only a test message shows it is authorised.", recs
}

// evalDKIM validates the public key published at host. When expected is a
// known record (from the signing server), the published key must be the same
// key. Publishing a key proves only that it is published: whether mail is
// actually signed with it needs a received test message.
func evalDKIM(txt []string, err error, expected string) (bool, string) {
	switch {
	case len(txt) == 0:
		if err != nil && !isNotFoundErr(err) {
			return false, "The DNS lookup failed; check again in a moment."
		}
		return false, "No DKIM record was found at this host. Publish the public key your signing server gives you."
	case len(txt) > 1:
		return false, "More than one TXT record was found at this host. Publish exactly one DKIM record per selector."
	}
	k, perr := dnsauth.ParseDKIMKey(txt[0])
	switch {
	case perr != nil:
		return false, "The DKIM record is not valid: " + perr.Error() + "."
	case k.Revoked:
		return false, "p= is empty, which means the key is revoked. Publish the current public key."
	}
	if expected != "" {
		if want, err := dnsauth.ParseDKIMKey(expected); err == nil && want.Fingerprint != "" && want.Fingerprint != k.Fingerprint {
			return false, "The published key is not the key your signing server uses, or the value was cut off when pasted."
		}
	}
	hint := "The key is published and valid. Only a received test message shows mail is actually signed with it."
	if k.KeyType == "rsa" && k.Bits < 2048 {
		hint = "The key is valid but only " + strconv.Itoa(k.Bits) + " bits; use 2048. " + hint
	}
	if k.TestingMode {
		hint = "t=y marks the key as testing: receivers may ignore its signatures. Remove t=y when ready. " + hint
	}
	return true, hint
}

func isNotFoundErr(err error) bool {
	var de *net.DNSError
	return errors.As(err, &de) && de.IsNotFound
}

// evalDMARC finds and validates the domain's DMARC policy and checks that at
// least one authenticated identifier can align with the From domain: DKIM
// (signed with d=domain) or SPF on the envelope domain (the bounce address).
func evalDMARC(ctx context.Context, r Resolver, domain, envDomain string, dkimOK, envSPFOK bool) (bool, string, string) {
	d, err := dnsauth.LookupDMARC(ctx, r, domain)
	if errors.Is(err, dnsauth.ErrNoDMARC) {
		return false, "(no record)", "No DMARC record was found at _dmarc." + domain + " or its organisational domain."
	}
	found := d.Record
	if d.FoundAt != "" && d.FoundAt != domain {
		found += " (from _dmarc." + d.FoundAt + ")"
	}
	if err != nil {
		if found == "" {
			found = "(invalid)"
		}
		return false, found, "The DMARC record is not valid: " + err.Error() + "."
	}
	spfAligned := envSPFOK && dnsauth.Aligned(d.ASPF, domain, envDomain)
	mode := map[string]string{"r": "relaxed", "s": "strict"}
	switch {
	case dkimOK || spfAligned:
		hint := ""
		if !spfAligned {
			hint = "DMARC will pass through DKIM only: SPF authenticates " + envDomain + ", which is not aligned with " + domain + " (aspf=" + mode[d.ASPF] + "). Keep DKIM signing with d=" + domain + "."
		}
		if d.Policy == "none" {
			hint = strings.TrimSpace(hint + " p=none only monitors; move to quarantine once reports look clean.")
		}
		return true, found, hint
	default:
		why := "DMARC cannot pass: DKIM is not passing yet"
		if !dnsauth.Aligned(d.ASPF, domain, envDomain) {
			why += ", and SPF authenticates " + envDomain + ", which is not aligned with " + domain + " (aspf=" + mode[d.ASPF] + ")"
		} else if !envSPFOK {
			why += ", and SPF does not pass for " + envDomain
		}
		return false, found, why + "."
	}
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
	sip := s.senderIP(s.senderKind(ctx))
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

		_, err := r.LookupTXT(ctx, d)
		exp := "one valid v=spf1 record that authorises the server your mail is sent from"
		if sip != nil {
			exp = "v=spf1 record that evaluates to pass for " + sip.String()
		}
		pass, why, spf := evalSPF(ctx, r, d, sip)
		if why != "" && !pass {
			exp += " (" + why + ")"
		}
		out = append(out, DNSResult{d, "SPF", exp, observed(spf, err), pass})

		name := s.Opts.DKIMSelector + "._domainkey." + d
		txt, err := r.LookupTXT(ctx, name)
		dk := pick(txt, "v=dkim1")
		if len(dk) == 0 {
			dk = txt
		}
		pass, why = evalDKIM(dk, err, "")
		dkexp := name + " TXT v=DKIM1; k=rsa; p=<public key of the server that signs your mail>"
		if !pass {
			dkexp += " (" + why + ")"
		}
		out = append(out, DNSResult{d, "DKIM", dkexp, observed(txt, err), pass})

		// DMARC: the domain's own record, else the organisational domain's;
		// validated, not just present.
		dm, derr := dnsauth.LookupDMARC(ctx, r, d)
		dexp := "_dmarc." + d + " TXT v=DMARC1; p=none (then quarantine once reports look clean)"
		dobs := dm.Record
		if derr != nil {
			dexp += " (" + derr.Error() + ")"
			if dobs == "" {
				dobs = "(no record)"
			}
		}
		out = append(out, DNSResult{d, "DMARC", dexp, dobs, derr == nil})

		mx(d, "bounces and replies return here")
	}

	if in := s.Opts.InboundDomain; in != "" && !seen[in] {
		mx(in, "inbound mail is relayed by Mailcow")
	}

	// Reverse DNS of the sending address, forward-confirmed (FCrDNS): the PTR
	// name must resolve back to the same address. Only when Njiwa-Mail knows
	// the sender; for a generic SMTP server its operator owns this check.
	if sip != nil {
		names, err := r.LookupAddr(ctx, ip)
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
					pass = pass || a.IP.Equal(sip)
				}
			}
		}
		exp := "PTR for " + ip + " naming the mail host, whose A/AAAA record points back to " + ip + " (set MAIL_HOSTNAME to check the exact name)"
		if host != "" {
			exp = "PTR " + host + ", and " + host + " resolving back to " + ip
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

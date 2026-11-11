// Package dnsauth evaluates email authentication DNS records: SPF (RFC 7208)
// and DMARC (RFC 7489). It is read-only and bounded: every lookup counts
// against the RFC limits and runs under the caller's context deadline. It
// replaces "a v=spf1 record exists" heuristics with real evaluation.
package dnsauth

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
)

// Resolver is the DNS surface the evaluator needs. *net.Resolver satisfies it.
type Resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// Result is an SPF check_host() result.
type Result string

const (
	Pass      Result = "pass"
	Fail      Result = "fail"
	SoftFail  Result = "softfail"
	Neutral   Result = "neutral"
	None      Result = "none"
	PermError Result = "permerror"
	TempError Result = "temperror"
)

const (
	maxLookups     = 10 // RFC 7208 4.6.4: DNS-querying terms
	maxVoidLookups = 2  // RFC 7208 4.6.4: lookups returning no answer
	maxMXNames     = 10
)

// SPFOutcome is the evaluation of one IP against one domain's SPF policy.
type SPFOutcome struct {
	Result  Result `json:"result"`
	Record  string `json:"record,omitempty"`
	Matched string `json:"matched,omitempty"` // the term that decided the result
	Lookups int    `json:"lookups"`
	Reason  string `json:"reason,omitempty"`
}

// FindSPF returns the domain's SPF records (the TXT strings starting with
// "v=spf1" followed by a space or the end). More than one is a permerror.
func FindSPF(txts []string) []string {
	var out []string
	for _, t := range txts {
		l := strings.ToLower(strings.TrimSpace(t))
		if l == "v=spf1" || strings.HasPrefix(l, "v=spf1 ") {
			out = append(out, strings.TrimSpace(t))
		}
	}
	return out
}

type evaluator struct {
	r      Resolver
	ip     net.IP
	n      int // DNS-querying terms used
	voids  int
	stack  map[string]bool // include/redirect loop protection
	lookup error           // first temporary DNS failure
}

// CheckHost evaluates whether ip may send mail for domain, following
// includes and redirects within the RFC lookup limits.
func CheckHost(ctx context.Context, r Resolver, ip net.IP, domain string) SPFOutcome {
	e := &evaluator{r: r, ip: ip, stack: map[string]bool{}}
	o := e.check(ctx, strings.ToLower(strings.TrimSuffix(domain, ".")))
	o.Lookups = e.n
	return o
}

func isNotFound(err error) bool {
	var de *net.DNSError
	return errors.As(err, &de) && de.IsNotFound
}

func (e *evaluator) txt(ctx context.Context, name string) ([]string, Result, string) {
	v, err := e.r.LookupTXT(ctx, name)
	if err != nil && !isNotFound(err) {
		return nil, TempError, "DNS lookup for " + name + " failed"
	}
	return v, "", ""
}

func (e *evaluator) check(ctx context.Context, domain string) SPFOutcome {
	if e.stack[domain] {
		return SPFOutcome{Result: PermError, Reason: "include or redirect loop at " + domain}
	}
	e.stack[domain] = true
	defer delete(e.stack, domain)

	txts, res, why := e.txt(ctx, domain)
	if res != "" {
		return SPFOutcome{Result: res, Reason: why}
	}
	recs := FindSPF(txts)
	switch len(recs) {
	case 0:
		return SPFOutcome{Result: None, Reason: "no SPF record at " + domain}
	case 1:
	default:
		return SPFOutcome{Result: PermError, Reason: "more than one SPF record at " + domain + "; merge them into one"}
	}
	rec := recs[0]
	terms := strings.Fields(rec)[1:]
	var redirect string
	for _, t := range terms {
		lt := strings.ToLower(t)
		if strings.HasPrefix(lt, "redirect=") {
			if redirect != "" {
				return SPFOutcome{Result: PermError, Record: rec, Reason: "more than one redirect= modifier"}
			}
			redirect = t[len("redirect="):]
			continue
		}
		if strings.HasPrefix(lt, "exp=") {
			continue // explanation only
		}
		if i := strings.IndexByte(lt, '='); i > 0 && !strings.ContainsAny(lt[:i], ":/") {
			continue // unknown modifier: ignored (RFC 7208 6)
		}
		q := Pass
		switch t[0] {
		case '+':
			t = t[1:]
		case '-':
			q, t = Fail, t[1:]
		case '~':
			q, t = SoftFail, t[1:]
		case '?':
			q, t = Neutral, t[1:]
		}
		match, res, why := e.mechanism(ctx, domain, t)
		if res != "" {
			return SPFOutcome{Result: res, Record: rec, Matched: t, Reason: why}
		}
		if match {
			return SPFOutcome{Result: q, Record: rec, Matched: t}
		}
	}
	if redirect != "" {
		if strings.Contains(redirect, "%") {
			return SPFOutcome{Result: PermError, Record: rec, Reason: "macros in redirect= are not supported by this checker"}
		}
		if e.n++; e.n > maxLookups {
			return SPFOutcome{Result: PermError, Record: rec, Reason: "more than 10 DNS lookups"}
		}
		o := e.check(ctx, strings.ToLower(redirect))
		if o.Result == None {
			o.Result, o.Reason = PermError, "redirect target "+redirect+" has no SPF record"
		}
		return o
	}
	return SPFOutcome{Result: Neutral, Record: rec, Reason: "no mechanism matched and there is no all"}
}

func splitCIDR(spec string) (string, int, int, error) {
	c4, c6 := 32, 128
	if i := strings.Index(spec, "//"); i >= 0 {
		n, err := strconv.Atoi(spec[i+2:])
		if err != nil || n < 0 || n > 128 {
			return "", 0, 0, errors.New("bad ip6 prefix")
		}
		c6, spec = n, spec[:i]
	}
	if i := strings.IndexByte(spec, '/'); i >= 0 {
		n, err := strconv.Atoi(spec[i+1:])
		if err != nil || n < 0 || n > 32 {
			return "", 0, 0, errors.New("bad ip4 prefix")
		}
		c4, spec = n, spec[:i]
	}
	return spec, c4, c6, nil
}

func (e *evaluator) inNet(ip net.IP, c4, c6 int) bool {
	if v4 := ip.To4(); v4 != nil {
		if t := e.ip.To4(); t != nil {
			return v4.Mask(net.CIDRMask(c4, 32)).Equal(t.Mask(net.CIDRMask(c4, 32)))
		}
		return false
	}
	if e.ip.To4() != nil {
		return false
	}
	return ip.Mask(net.CIDRMask(c6, 128)).Equal(e.ip.Mask(net.CIDRMask(c6, 128)))
}

func (e *evaluator) count() (Result, string) {
	e.n++
	if e.n > maxLookups {
		return PermError, "more than 10 DNS lookups (include, a, mx, ptr, exists, redirect); flatten the record"
	}
	return "", ""
}

func (e *evaluator) void(n int, name string) (Result, string) {
	if n == 0 {
		e.voids++
		if e.voids > maxVoidLookups {
			return PermError, "more than 2 lookups returned nothing (last: " + name + ")"
		}
	}
	return "", ""
}

func (e *evaluator) addrs(ctx context.Context, host string) ([]net.IP, Result, string) {
	as, err := e.r.LookupIPAddr(ctx, host)
	if err != nil && !isNotFound(err) {
		return nil, TempError, "DNS lookup for " + host + " failed"
	}
	out := make([]net.IP, 0, len(as))
	for _, a := range as {
		out = append(out, a.IP)
	}
	return out, "", ""
}

// mechanism reports whether one mechanism matches. A non-empty Result is an
// error that ends evaluation.
func (e *evaluator) mechanism(ctx context.Context, domain, t string) (bool, Result, string) {
	lt := strings.ToLower(t)
	name, arg := lt, ""
	if i := strings.IndexAny(lt, ":/"); i >= 0 {
		name, arg = lt[:i], lt[i:]
	}
	target := func(def string) (string, int, int, Result, string) {
		spec := strings.TrimPrefix(arg, ":")
		host, c4, c6, err := splitCIDR(spec)
		if err != nil {
			return "", 0, 0, PermError, "invalid prefix length in " + t
		}
		if host == "" {
			host = def
		}
		if strings.Contains(host, "%") {
			return "", 0, 0, PermError, "macros (" + t + ") are not supported by this checker"
		}
		return host, c4, c6, "", ""
	}
	switch name {
	case "all":
		if arg != "" {
			return false, PermError, "all takes no argument"
		}
		return true, "", ""
	case "ip4", "ip6":
		spec := strings.TrimPrefix(arg, ":")
		if !strings.Contains(spec, "/") {
			if name == "ip4" {
				spec += "/32"
			} else {
				spec += "/128"
			}
		}
		_, n, err := net.ParseCIDR(spec)
		if err != nil || (name == "ip4") != (n.IP.To4() != nil) {
			return false, PermError, "invalid " + name + " address in " + t
		}
		return n.Contains(e.ip), "", ""
	case "a":
		host, c4, c6, res, why := target(domain)
		if res != "" {
			return false, res, why
		}
		if res, why := e.count(); res != "" {
			return false, res, why
		}
		ips, res, why := e.addrs(ctx, host)
		if res != "" {
			return false, res, why
		}
		if res, why := e.void(len(ips), host); res != "" {
			return false, res, why
		}
		for _, ip := range ips {
			if e.inNet(ip, c4, c6) {
				return true, "", ""
			}
		}
		return false, "", ""
	case "mx":
		host, c4, c6, res, why := target(domain)
		if res != "" {
			return false, res, why
		}
		if res, why := e.count(); res != "" {
			return false, res, why
		}
		mxs, err := e.r.LookupMX(ctx, host)
		if err != nil && !isNotFound(err) {
			return false, TempError, "DNS lookup for MX " + host + " failed"
		}
		if res, why := e.void(len(mxs), host); res != "" {
			return false, res, why
		}
		if len(mxs) > maxMXNames {
			return false, PermError, "more than 10 MX names for " + host
		}
		for _, m := range mxs {
			ips, res, why := e.addrs(ctx, strings.TrimSuffix(m.Host, "."))
			if res != "" {
				return false, res, why
			}
			for _, ip := range ips {
				if e.inNet(ip, c4, c6) {
					return true, "", ""
				}
			}
		}
		return false, "", ""
	case "include":
		host := strings.TrimPrefix(arg, ":")
		if host == "" || strings.Contains(host, "%") {
			return false, PermError, "include needs a domain (macros are not supported by this checker)"
		}
		if res, why := e.count(); res != "" {
			return false, res, why
		}
		o := e.check(ctx, host)
		switch o.Result {
		case Pass:
			return true, "", ""
		case Fail, SoftFail, Neutral:
			return false, "", ""
		case TempError:
			return false, TempError, o.Reason
		default:
			return false, PermError, "include:" + host + ": " + o.Reason
		}
	case "exists":
		host := strings.TrimPrefix(arg, ":")
		if host == "" || strings.Contains(host, "%") {
			return false, PermError, "exists needs a domain (macros are not supported by this checker)"
		}
		if res, why := e.count(); res != "" {
			return false, res, why
		}
		ips, res, why := e.addrs(ctx, host)
		if res != "" {
			return false, res, why
		}
		return len(ips) > 0, "", ""
	case "ptr":
		// Deprecated (RFC 7208 5.5) and unreliable; counted, never matched here.
		if res, why := e.count(); res != "" {
			return false, res, why
		}
		return false, "", ""
	}
	return false, PermError, "unknown mechanism " + t
}

// Problems are static issues worth reporting even when evaluation passes.
func Problems(rec string) []string {
	var p []string
	l := " " + strings.ToLower(rec) + " "
	if strings.Contains(l, " +all ") || strings.Contains(l, " all ") { // a bare "all" means +all
		p = append(p, "+all lets anyone send as this domain; end with -all or ~all")
	}
	if strings.Contains(l, " ptr") {
		p = append(p, "the ptr mechanism is deprecated and slow; remove it")
	}
	return p
}

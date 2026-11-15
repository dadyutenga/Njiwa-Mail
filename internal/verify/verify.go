// Package verify checks an email address before it is sent to: syntax, the
// domain's mail servers (MX, with the A/AAAA fallback and RFC 7505 null MX),
// disposable-address providers and role accounts. It never connects to the
// recipient's mail server (an SMTP "probe" is unreliable and gets the sending
// IP blocklisted), so a "deliverable" result means the domain accepts mail,
// not that the mailbox exists.
package verify

import (
	"bufio"
	"context"
	_ "embed"
	"errors"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Result values.
const (
	Deliverable   = "deliverable"   // valid syntax, the domain accepts mail
	Risky         = "risky"         // deliverable, but disposable or a role account
	Undeliverable = "undeliverable" // bad syntax, no mail server for the domain, or a null MX
	Unknown       = "unknown"       // DNS did not answer in time; try again
)

// Check is one address's verdict. Reasons are stable codes.
type Check struct {
	Email      string   `json:"email"`
	Result     string   `json:"result"`
	Reasons    []string `json:"reasons"`
	Syntax     bool     `json:"syntax_valid"`
	Domain     string   `json:"domain,omitempty"`
	MX         []string `json:"mx,omitempty"`
	Disposable bool     `json:"disposable"`
	Role       bool     `json:"role_account"`
}

// Resolver is the part of net.Resolver used.
type Resolver interface {
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}

//go:embed disposable.txt
var disposableList string

var roleLocals = map[string]bool{}

func init() {
	for _, r := range strings.Fields(`abuse admin administrator billing compliance contact postmaster hostmaster
		webmaster info hello help support sales marketing office team noreply no-reply donotreply do-not-reply
		mailer-daemon root security privacy legal jobs careers hr press media newsletter accounts accounting
		enquiries inquiries feedback service customerservice orders all everyone staff`) {
		roleLocals[r] = true
	}
}

// Verifier caches domain lookups for a short time: a list of addresses at
// the same domain costs one DNS query.
type Verifier struct {
	Resolver   Resolver
	Timeout    time.Duration
	TTL        time.Duration
	Normalize  func(string) (string, bool)
	disposable map[string]bool

	mu    sync.Mutex
	cache map[string]domainInfo
}

type domainInfo struct {
	at      time.Time
	mx      []string
	reason  string // "" ok; no_mx_records | domain_not_found | null_mx | dns_timeout
	unknown bool
}

// New builds a verifier. extraFile may name a file of further disposable
// domains, one per line ("#" comments); "" uses the built-in list only.
func New(r Resolver, normalize func(string) (string, bool), extraFile string) (*Verifier, error) {
	v := &Verifier{Resolver: r, Normalize: normalize, Timeout: 4 * time.Second, TTL: 10 * time.Minute,
		disposable: map[string]bool{}, cache: map[string]domainInfo{}}
	add := func(text string) {
		sc := bufio.NewScanner(strings.NewReader(text))
		for sc.Scan() {
			l := strings.ToLower(strings.TrimSpace(sc.Text()))
			if l != "" && !strings.HasPrefix(l, "#") {
				v.disposable[l] = true
			}
		}
	}
	add(disposableList)
	if extraFile != "" {
		b, err := os.ReadFile(extraFile)
		if err != nil {
			return nil, err
		}
		add(string(b))
	}
	return v, nil
}

// IsDisposable reports whether domain or a parent domain is a known
// disposable-address provider.
func (v *Verifier) IsDisposable(domain string) bool {
	for d := domain; d != ""; {
		if v.disposable[d] {
			return true
		}
		i := strings.IndexByte(d, '.')
		if i < 0 {
			break
		}
		d = d[i+1:]
	}
	return false
}

// IsRole reports whether local names a function, not a person (info@, support@).
func IsRole(local string) bool {
	if i := strings.IndexByte(local, '+'); i >= 0 {
		local = local[:i]
	}
	return roleLocals[local]
}

// Verify checks one address.
func (v *Verifier) Verify(ctx context.Context, raw string) Check {
	c := Check{Email: strings.TrimSpace(raw), Reasons: []string{}}
	addr, ok := v.Normalize(c.Email)
	if !ok {
		c.Result = Undeliverable
		c.Reasons = append(c.Reasons, "invalid_syntax")
		return c
	}
	c.Email, c.Syntax = addr, true
	at := strings.LastIndexByte(addr, '@')
	local, domain := addr[:at], addr[at+1:]
	c.Domain = domain
	c.Disposable, c.Role = v.IsDisposable(domain), IsRole(local)
	info := v.domain(ctx, domain)
	c.MX = info.mx
	switch {
	case info.unknown:
		c.Result = Unknown
		c.Reasons = append(c.Reasons, info.reason)
	case info.reason != "":
		c.Result = Undeliverable
		c.Reasons = append(c.Reasons, info.reason)
	default:
		c.Result = Deliverable
	}
	if c.Disposable {
		c.Reasons = append(c.Reasons, "disposable_domain")
	}
	if c.Role {
		c.Reasons = append(c.Reasons, "role_account")
	}
	if c.Result == Deliverable && (c.Disposable || c.Role) {
		c.Result = Risky
	}
	return c
}

func (v *Verifier) domain(ctx context.Context, domain string) domainInfo {
	v.mu.Lock()
	if d, ok := v.cache[domain]; ok && time.Since(d.at) < v.TTL {
		v.mu.Unlock()
		return d
	}
	v.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, v.Timeout)
	defer cancel()
	d := domainInfo{at: time.Now()}
	mxs, err := v.Resolver.LookupMX(ctx, domain)
	switch {
	case err == nil && len(mxs) == 1 && (mxs[0].Host == "." || mxs[0].Host == ""):
		d.reason = "null_mx" // RFC 7505: this domain accepts no mail
	case err == nil && len(mxs) > 0:
		for _, m := range mxs {
			d.mx = append(d.mx, strings.TrimSuffix(m.Host, "."))
		}
		sort.Strings(d.mx)
	case err != nil && !notFound(err):
		d.reason, d.unknown = "dns_timeout", true
	default:
		// No MX (or no such domain; Go's resolver does not tell them apart):
		// RFC 5321 falls back to the domain's own address.
		if _, herr := v.Resolver.LookupHost(ctx, domain); herr == nil {
			d.mx = []string{domain}
		} else if notFound(herr) {
			d.reason = "no_mail_server"
		} else {
			d.reason, d.unknown = "dns_timeout", true
		}
	}
	if !d.unknown {
		v.mu.Lock()
		if len(v.cache) > 10000 {
			v.cache = map[string]domainInfo{}
		}
		v.cache[domain] = d
		v.mu.Unlock()
	}
	return d
}

// notFound: the name has no such records (NXDOMAIN or an empty answer), as
// opposed to a timeout or a server failure.
func notFound(err error) bool {
	var de *net.DNSError
	return errors.As(err, &de) && de.IsNotFound
}

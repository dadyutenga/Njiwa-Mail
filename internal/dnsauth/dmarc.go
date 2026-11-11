package dnsauth

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// DMARC is a parsed DMARC policy record (RFC 7489 6.3).
type DMARC struct {
	Record string   `json:"record"`
	Policy string   `json:"p"`
	SubPol string   `json:"sp,omitempty"`
	ADKIM  string   `json:"adkim"` // r or s
	ASPF   string   `json:"aspf"`  // r or s
	Pct    int      `json:"pct"`
	RUA    []string `json:"rua,omitempty"`
	RUF    []string `json:"ruf,omitempty"`
	// FoundAt is the name the record was published at (the From domain or,
	// by fallback, its organisational domain).
	FoundAt string `json:"found_at"`
}

func validPolicy(p string) bool { return p == "none" || p == "quarantine" || p == "reject" }

// ParseDMARC validates one record. Unknown tags are ignored as the RFC requires.
func ParseDMARC(rec string) (DMARC, error) {
	d := DMARC{Record: strings.TrimSpace(rec), ADKIM: "r", ASPF: "r", Pct: 100}
	parts := strings.Split(d.Record, ";")
	seen := map[string]bool{}
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return d, errors.New("tag without a value: " + part)
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		if i == 0 {
			if k != "v" || v != "DMARC1" {
				return d, errors.New("the record must start with v=DMARC1")
			}
			continue
		}
		if seen[k] {
			return d, errors.New("tag " + k + " appears twice")
		}
		seen[k] = true
		lv := strings.ToLower(v)
		switch k {
		case "p":
			if !validPolicy(lv) {
				return d, errors.New("p must be none, quarantine or reject")
			}
			d.Policy = lv
		case "sp":
			if !validPolicy(lv) {
				return d, errors.New("sp must be none, quarantine or reject")
			}
			d.SubPol = lv
		case "adkim", "aspf":
			if lv != "r" && lv != "s" {
				return d, errors.New(k + " must be r (relaxed) or s (strict)")
			}
			if k == "adkim" {
				d.ADKIM = lv
			} else {
				d.ASPF = lv
			}
		case "pct":
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 || n > 100 {
				return d, errors.New("pct must be a number from 0 to 100")
			}
			d.Pct = n
		case "rua", "ruf":
			var uris []string
			for _, u := range strings.Split(v, ",") {
				u = strings.TrimSpace(u)
				if !strings.HasPrefix(strings.ToLower(u), "mailto:") || !strings.Contains(u, "@") {
					return d, errors.New(k + " must list mailto: addresses")
				}
				uris = append(uris, u)
			}
			if k == "rua" {
				d.RUA = uris
			} else {
				d.RUF = uris
			}
		}
	}
	if d.Policy == "" {
		return d, errors.New("the p= tag (none, quarantine or reject) is required")
	}
	return d, nil
}

// OrgDomain is the organisational domain (registrable domain under the public
// suffix list); the input itself when it cannot be determined.
func OrgDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	if o, err := publicsuffix.EffectiveTLDPlusOne(domain); err == nil {
		return o
	}
	return domain
}

// Aligned reports DMARC identifier alignment between the From domain and an
// authenticated domain: strict needs an exact match, relaxed the same
// organisational domain.
func Aligned(mode, from, authenticated string) bool {
	from, authenticated = strings.ToLower(strings.TrimSuffix(from, ".")), strings.ToLower(strings.TrimSuffix(authenticated, "."))
	if mode == "s" {
		return from == authenticated
	}
	return OrgDomain(from) == OrgDomain(authenticated)
}

// TXTResolver is the part of Resolver DMARC discovery needs.
type TXTResolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// ErrNoDMARC means neither the domain nor its organisational domain publishes a record.
var ErrNoDMARC = errors.New("no DMARC record")

// LookupDMARC finds the policy for fromDomain: _dmarc.<domain>, else
// _dmarc.<organisational domain> (RFC 7489 6.6.3). More than one record at
// the chosen name is an error.
func LookupDMARC(ctx context.Context, r TXTResolver, fromDomain string) (DMARC, error) {
	fromDomain = strings.ToLower(strings.TrimSuffix(fromDomain, "."))
	names := []string{fromDomain}
	if org := OrgDomain(fromDomain); org != fromDomain {
		names = append(names, org)
	}
	for _, n := range names {
		txts, err := r.LookupTXT(ctx, "_dmarc."+n)
		if err != nil && !isNotFound(err) {
			return DMARC{FoundAt: n}, errors.New("DNS lookup for _dmarc." + n + " failed")
		}
		var recs []string
		for _, t := range txts {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(t)), "v=dmarc1") {
				recs = append(recs, t)
			}
		}
		switch len(recs) {
		case 0:
			continue
		case 1:
			d, err := ParseDMARC(recs[0])
			d.FoundAt = n
			return d, err
		default:
			return DMARC{FoundAt: n}, errors.New("more than one DMARC record at _dmarc." + n)
		}
	}
	return DMARC{}, ErrNoDMARC
}

package service

import (
	"context"
	"encoding/json"
	"net"
	"strings"

	"github.com/njiwa-tz/mail/internal/dnsauth"
)

// SendingFacts describe where an existing SMTP server sends from (C07).
type SendingFacts struct {
	EgressIPs    []string          `json:"egress_ips,omitempty"`    // IPs or CIDR ranges
	SPFInclude   string            `json:"spf_include,omitempty"`   // e.g. spf.provider.example
	DKIMSelector string            `json:"dkim_selector,omitempty"` // the server's selector
	DKIMKeys     map[string]string `json:"dkim_keys,omitempty"`     // domain -> DKIM TXT value
}

// SendingFactsInput updates facts. Fields left nil keep their value;
// DKIMKeys entries are merged, and an empty value removes that domain's key.
type SendingFactsInput struct {
	EgressIPs    *[]string         `json:"egress_ips"`
	SPFInclude   *string           `json:"spf_include"`
	DKIMSelector *string           `json:"dkim_selector"`
	DKIMKeys     map[string]string `json:"dkim_keys"`
}

func (f *SendingFacts) ips() []net.IP {
	if f == nil {
		return nil
	}
	var out []net.IP
	for _, v := range f.EgressIPs {
		if ip := net.ParseIP(v); ip != nil {
			out = append(out, ip)
		} else if _, n, err := net.ParseCIDR(v); err == nil {
			out = append(out, n.IP)
		}
	}
	return out
}

// sendingFacts returns the facts of the active existing-SMTP profile.
func (s *Service) sendingFacts(ctx context.Context) *SendingFacts {
	var raw []byte
	if err := s.Pool.QueryRow(ctx, `SELECT sending_facts FROM transport_profiles WHERE is_active AND mode='existing_smtp'`).Scan(&raw); err != nil {
		return nil
	}
	var f SendingFacts
	if json.Unmarshal(raw, &f) != nil {
		return nil
	}
	if len(f.EgressIPs) == 0 && f.SPFInclude == "" && f.DKIMSelector == "" && len(f.DKIMKeys) == 0 {
		return nil
	}
	return &f
}

// dkimSelector is the selector to publish and check: the existing server's
// own selector when the operator stated it, else DKIM_SELECTOR.
func (s *Service) dkimSelector(f *SendingFacts) string {
	if f != nil && f.DKIMSelector != "" {
		return f.DKIMSelector
	}
	return s.Opts.DKIMSelector
}

// SetSendingFacts validates and stores sending facts on an existing-SMTP
// profile. Audited. It does not change the profile's revision.
func (s *Service) SetSendingFacts(ctx context.Context, a Actor, profileID string, in SendingFactsInput) (SendingFacts, error) {
	var f SendingFacts
	if !isUUID(profileID) {
		return f, newErr(404, "not_found")
	}
	var raw []byte
	var mode string
	if err := s.Pool.QueryRow(ctx, `SELECT mode, sending_facts FROM transport_profiles WHERE id=$1::uuid AND status <> 'retired'`, profileID).Scan(&mode, &raw); err != nil {
		return f, notFound(err)
	}
	if mode != "existing_smtp" {
		return f, &Error{Status: 409, Code: "profile_not_smtp", Message: "sending facts describe an existing SMTP server; managed mode knows its own"}
	}
	_ = json.Unmarshal(raw, &f)
	if in.EgressIPs != nil {
		var ips []string
		for _, v := range *in.EgressIPs {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			if ip := net.ParseIP(v); ip != nil {
				if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
					return f, badRequest(v + " is not a public sending address")
				}
				ips = append(ips, ip.String())
				continue
			}
			if _, n, err := net.ParseCIDR(v); err == nil {
				if ones, bits := n.Mask.Size(); (bits == 32 && ones < 16) || (bits == 128 && ones < 32) {
					return f, badRequest(v + " is too wide a range for SPF")
				}
				ips = append(ips, n.String())
				continue
			}
			return f, badRequest(v + " is not an IP address or range")
		}
		if len(ips) > 20 {
			return f, badRequest("at most 20 addresses or ranges")
		}
		f.EgressIPs = ips
	}
	if in.SPFInclude != nil {
		v := strings.TrimSpace(*in.SPFInclude)
		v = strings.TrimPrefix(strings.ToLower(v), "include:")
		if v != "" {
			n, ok := NormalizeDomain(v)
			if !ok {
				return f, badRequest("the SPF include must be a domain, for example spf.provider.example")
			}
			v = n
		}
		f.SPFInclude = v
	}
	if in.DKIMSelector != nil {
		v := strings.TrimSpace(*in.DKIMSelector)
		for _, r := range v {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '.' || r == '_') {
				return f, badRequest("the DKIM selector may contain only letters, digits, '-', '_' and '.'")
			}
		}
		f.DKIMSelector = v
	}
	for d, v := range in.DKIMKeys {
		dn, ok := NormalizeDomain(d)
		if !ok {
			return f, badRequest(d + " is not a domain")
		}
		v = strings.TrimSpace(v)
		if v == "" {
			delete(f.DKIMKeys, dn)
			continue
		}
		k, err := dnsauth.ParseDKIMKey(v)
		if err != nil {
			return f, badRequest("the DKIM key for " + dn + " is not valid: " + err.Error())
		}
		if k.Revoked {
			return f, badRequest("the DKIM key for " + dn + " is empty (revoked)")
		}
		if f.DKIMKeys == nil {
			f.DKIMKeys = map[string]string{}
		}
		f.DKIMKeys[dn] = v
	}
	fj, _ := json.Marshal(f)
	if _, err := s.Pool.Exec(ctx, `UPDATE transport_profiles SET sending_facts=$2::jsonb, updated_at=updated_at WHERE id=$1::uuid`, profileID, fj); err != nil {
		return f, unavailable("sending_facts", err)
	}
	_ = s.Audit(ctx, a, "transport.sending_facts", "transport_profile", profileID, map[string]any{"ips": len(f.EgressIPs),
		"include": f.SPFInclude != "", "selector": f.DKIMSelector, "dkim_domains": len(f.DKIMKeys)})
	return f, nil
}

// evalSPFFor checks SPF with everything known about the sender: each known
// sending address must be authorised; failing that, the provider's include
// must be referenced; failing that, the record must at least be valid.
func evalSPFFor(ctx context.Context, r Resolver, domain string, ips []net.IP, include string) (bool, string, []string) {
	if len(ips) > 0 {
		var recs []string
		for _, ip := range ips {
			ok, why, rr := evalSPF(ctx, r, domain, ip)
			recs = rr
			if !ok {
				return false, why, recs
			}
		}
		return true, "", recs
	}
	ok, why, recs := evalSPF(ctx, r, domain, nil)
	if !ok || include == "" {
		return ok, why, recs
	}
	for _, t := range strings.Fields(strings.ToLower(recs[0])) {
		t = strings.TrimLeft(t, "+")
		if t == "include:"+include || t == "redirect="+include {
			return true, "The record includes " + include + ", as your mail server's operator documents.", recs
		}
	}
	return false, "The SPF record does not include " + include + ", which authorises your mail server. Add include:" + include + " before the final -all.", recs
}

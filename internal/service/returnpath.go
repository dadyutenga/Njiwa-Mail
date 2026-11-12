package service

import (
	"context"
	"strings"
)

// D04: a sending domain's own return-path (bounce) domain.
//
// DMARC counts SPF only when the envelope sender's domain is aligned with the
// From domain. With one installation-wide bounce domain that holds only for
// sending domains under the same organisational domain. A domain from another
// organisation can therefore name its own return-path domain, which must be the
// domain itself or a subdomain of it, so ownership proven for the domain covers
// it (nobody can point their bounces at a name that is not theirs).

// SetDomainReturnPath sets (or, with an empty name, clears) the return-path
// domain of one domain of the project. The previous SPF and DMARC results no
// longer describe the new requirements, so the domain must be checked again
// before Live mail is allowed from it.
func (s *Service) SetDomainReturnPath(ctx context.Context, sc *Scope, id, bounceDomain string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	var name string
	if err := s.Pool.QueryRow(ctx, `SELECT name FROM domains WHERE id=$1 AND project_id=$2`, id, sc.ProjectID).Scan(&name); err != nil {
		return notFound(err)
	}
	bd := strings.TrimSpace(bounceDomain)
	if bd != "" {
		n, ok := NormalizeDomain(bd)
		if !ok {
			return badRequest("the return-path domain is not a valid domain name")
		}
		bd = n
		if bd != name && !strings.HasSuffix(bd, "."+name) {
			return &Error{Status: 422, Code: "return_path_not_under_domain", Message: "the return-path domain must be " + name + " or a subdomain of it, for example bounce." + name}
		}
		if _, def, ok := splitAddr(s.Opts.BounceAddress); ok && bd == def {
			return &Error{Status: 422, Code: "return_path_is_default", Message: "that is already the installation's bounce domain; clear the field to use it"}
		}
		if s.Opts.InboundDomain != "" && (bd == s.Opts.InboundDomain || strings.HasSuffix(s.Opts.InboundDomain, "."+bd) && bd != name) {
			return &Error{Status: 422, Code: "return_path_conflicts_inbound", Message: "that name is used for inbound mail"}
		}
	}
	if _, _, ok := splitAddr(s.Opts.BounceAddress); !ok && bd != "" {
		return &Error{Status: 409, Code: "bounces_not_configured", Message: "set BOUNCE_ADDRESS first: it provides the mailbox name used at the return-path domain"}
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE domains SET bounce_domain=$3, spf_ok=false, dmarc_ok=false, last_checked_at=NULL WHERE id=$1 AND project_id=$2`, id, sc.ProjectID, bd); err != nil {
		return unavailable("domain_return_path", err)
	}
	s.forgetBounceDomains()
	return nil
}

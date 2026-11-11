package service

import (
	"context"
	"errors"
	"strings"

	"github.com/njiwa-tz/mail/internal/db"
)

// Domain claims (backlog D01-D03). A domain row belongs to one project and
// carries that project's ownership token and DNS results. The claim says which
// single project in the installation may actually use the name. Live mail and
// Live senders need both: a verified row in the project AND the active claim.

// NormalizeDomain returns the canonical form used for every comparison:
// lowercase ASCII (IDNA/punycode), no surrounding space, no trailing dot.
func NormalizeDomain(name string) (string, bool) {
	name = strings.TrimSuffix(strings.TrimSpace(name), ".")
	if name == "" || strings.ContainsAny(name, "@/: ") {
		return "", false
	}
	e, ok := NormalizeEmail("a@" + name)
	if !ok {
		return "", false
	}
	return e[2:], true
}

// claimForVerifiedDomain records the active claim for a project whose DNS
// ownership proof just passed. It reports whether this project holds the
// claim. The first verified project wins; later ones need an operator transfer.
func claimForVerifiedDomain(ctx context.Context, tx *db.Tx, name, projectID string) (bool, string, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO domain_claims(name, project_id, state) VALUES ($1,$2::uuid,'active') ON CONFLICT (name) DO NOTHING`, name, projectID); err != nil {
		return false, "", err
	}
	var owner, state string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(project_id::text,''), state FROM domain_claims WHERE name=$1`, name).Scan(&owner, &state); err != nil {
		return false, "", err
	}
	return state == "active" && owner == projectID, state, nil
}

// releaseClaimIfUnused drops the project's claim when it no longer has any
// row for the name. Called after a domain is deleted.
func (s *Service) releaseClaimIfUnused(ctx context.Context, name, projectID string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM domain_claims c WHERE c.name=$1 AND c.project_id=$2::uuid
AND NOT EXISTS (SELECT 1 FROM domains d WHERE d.name=c.name AND d.project_id=c.project_id)`, name, projectID)
	return err
}

// requireLiveDomain is the single sending-domain gate for Live mail and Live
// sender identities: the domain must be registered in this project for
// sending, ownership-verified, hold the installation-wide claim, and (when
// checkDNS) pass SPF, DKIM and DMARC. There is no grandfathering of
// unregistered domains.
func (s *Service) requireLiveDomain(ctx context.Context, q queryer, projectID, domain string, checkDNS bool) error {
	var verified, spf, dkim, dmarc, claimed, otherClaim bool
	err := q.QueryRow(ctx, `SELECT d.verified_at IS NOT NULL, d.spf_ok, d.dkim_ok, d.dmarc_ok,
  EXISTS (SELECT 1 FROM domain_claims c WHERE c.name=d.name AND c.project_id=d.project_id AND c.state='active'),
  EXISTS (SELECT 1 FROM domain_claims c WHERE c.name=d.name AND (c.project_id IS DISTINCT FROM d.project_id))
FROM domains d WHERE d.project_id=$1::uuid AND d.name=$2 AND d.purpose IN ('sending','both')
ORDER BY d.verified_at IS NULL LIMIT 1`, projectID, domain).Scan(&verified, &spf, &dkim, &dmarc, &claimed, &otherClaim)
	if errors.Is(err, db.ErrNoRows) {
		if s.Opts.AllowUnregisteredLiveDomains {
			// Upgrade escape hatch: only for names nobody has claimed, so it
			// can never be used to send as another project's domain.
			var claimedAnywhere bool
			if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM domain_claims WHERE name=$1)`, domain).Scan(&claimedAnywhere); err != nil {
				return unavailable("domain_lookup", err)
			}
			if !claimedAnywhere {
				return nil
			}
			return &Error{Status: 403, Code: "domain_claimed_elsewhere",
				Message: domain + " belongs to another project in this installation, or is awaiting operator review. An operator must transfer it first."}
		}
		return &Error{Status: 403, Code: "domain_not_registered",
			Message: domain + " is not a sending domain of this project. Add it on the Domains page and verify it before sending Live mail."}
	}
	if err != nil {
		return unavailable("domain_lookup", err)
	}
	if otherClaim && !claimed {
		return &Error{Status: 403, Code: "domain_claimed_elsewhere",
			Message: domain + " belongs to another project in this installation, or is awaiting operator review. An operator must transfer it first."}
	}
	if !verified || !claimed {
		return &Error{Status: 403, Code: "domain_not_ready", Message: "verify ownership of " + domain + " on the Domains page first."}
	}
	if checkDNS && (!spf || !dkim || !dmarc) {
		return &Error{Status: 403, Code: "domain_not_ready", Message: domain + " does not pass SPF, DKIM and DMARC yet."}
	}
	return nil
}

// DomainClaims lists every claim in the installation (operator view).
func (s *Service) DomainClaims(ctx context.Context) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT c.name, c.state, p.name AS project, c.claimed_at, c.updated_at,
  (SELECT array_agg(DISTINCT p2.name ORDER BY p2.name) FROM domains d JOIN projects p2 ON p2.id=d.project_id WHERE d.name=c.name) AS projects_with_domain
FROM domain_claims c LEFT JOIN projects p ON p.id=c.project_id ORDER BY c.state DESC, c.name`)
}

// TransferDomainClaim gives the claim on name to project. The target must
// already have added the domain. Every project's ownership proof for the name
// is reset, so the new owner must re-verify with its own token and the old
// owner's senders stop counting as verified immediately. Audited.
func (s *Service) TransferDomainClaim(ctx context.Context, a Actor, name, project string) error {
	name, ok := NormalizeDomain(name)
	if !ok {
		return badRequest("enter a domain name such as notify.example.com")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("claim_transfer", err)
	}
	defer tx.Rollback(ctx)
	var to string
	if err := tx.QueryRow(ctx, `SELECT p.id::text FROM projects p WHERE p.name=$1`, project).Scan(&to); err != nil {
		return notFound(err)
	}
	var has bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM domains WHERE name=$1 AND project_id=$2::uuid)`, name, to).Scan(&has); err != nil {
		return unavailable("claim_transfer", err)
	}
	if !has {
		return &Error{Status: 409, Code: "domain_not_added", Message: "add " + name + " to project " + project + " before transferring it"}
	}
	var from string
	_ = tx.QueryRow(ctx, `SELECT COALESCE(project_id::text,'') FROM domain_claims WHERE name=$1 FOR UPDATE`, name).Scan(&from)
	if _, err := tx.Exec(ctx, `INSERT INTO domain_claims(name, project_id, state) VALUES ($1,$2::uuid,'active')
ON CONFLICT (name) DO UPDATE SET project_id=EXCLUDED.project_id, state='active', claimed_at=now(), updated_at=now()`, name, to); err != nil {
		return unavailable("claim_transfer", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE domains SET verified_at=NULL WHERE name=$1`, name); err != nil {
		return unavailable("claim_transfer", err)
	}
	if err := audit(ctx, tx, a, "domain.claim_transfer", "domain", name, map[string]any{"from_project_id": from, "to_project_id": to}); err != nil {
		return unavailable("audit", err)
	}
	return tx.Commit(ctx)
}

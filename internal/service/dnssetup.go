package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/dnsprovider"
	"github.com/njiwa-tz/mail/internal/secure"
)

// DNS setup lifecycle: analyze → approve → apply → verify (→ rollback).
// See docs/DNS_SETUP.md for the rules the plan engine enforces.

const approvalLife = 30 * time.Minute

// NSResolver is the extra lookup the analysis needs; net.Resolver provides it.
type NSResolver interface {
	LookupNS(ctx context.Context, name string) ([]*net.NS, error)
}

type setupRow struct {
	ID, ProjectID, DomainID, Domain, ConnID, ZoneID, ZoneName, Status, Digest, ApprovedBy string
	Plan                                                                                  DNSPlan
	ApprovedAt                                                                            *time.Time
	Acks                                                                                  []string
}

func (s *Service) sleeper() func(context.Context, time.Duration) error {
	if s.Opts.DNSSleep != nil {
		return s.Opts.DNSSleep
	}
	return dnsprovider.Sleep
}

// readRetry retries an idempotent read on transient failures and rate limits.
func (s *Service) readRetry(ctx context.Context, fn func() error) error {
	return dnsprovider.Retry(ctx, 4, 500*time.Millisecond, 20*time.Second, s.sleeper(), fn)
}

func (s *Service) loadSetup(ctx context.Context, projectID, id string) (*setupRow, error) {
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	r := &setupRow{}
	var plan, acks []byte
	var conn *string
	err := s.Pool.QueryRow(ctx, `SELECT id::text, project_id::text, domain_id::text, domain, connection_id::text, zone_id, zone_name, status, plan, plan_digest,
  approved_by, approved_at, acknowledged FROM dns_setups WHERE id=$1 AND project_id=$2`, id, projectID).
		Scan(&r.ID, &r.ProjectID, &r.DomainID, &r.Domain, &conn, &r.ZoneID, &r.ZoneName, &r.Status, &plan, &r.Digest, &r.ApprovedBy, &r.ApprovedAt, &acks)
	if err != nil {
		return nil, notFound(err)
	}
	if conn != nil {
		r.ConnID = *conn
	}
	_ = json.Unmarshal(plan, &r.Plan)
	_ = json.Unmarshal(acks, &r.Acks)
	return r, nil
}

// domainRequirements returns the records the domain needs for the active
// delivery mode. It is provider independent.
func (s *Service) domainRequirements(ctx context.Context, projectID, domainID string) (name, purpose string, recs []DomainRecord, err error) {
	if !uuidRe.MatchString(domainID) {
		return "", "", nil, newErr(404, "not_found")
	}
	var token, ownBounce string
	if err := s.Pool.QueryRow(ctx, `SELECT name, verify_token, purpose, bounce_domain FROM domains WHERE id=$1 AND project_id=$2`, domainID, projectID).
		Scan(&name, &token, &purpose, &ownBounce); err != nil {
		return "", "", nil, notFound(err)
	}
	kind := s.senderKind(ctx)
	var facts *SendingFacts
	if kind == senderGeneric {
		facts = s.sendingFacts(ctx)
	}
	dkim := ""
	if kind == senderManaged {
		dkim = s.managedDKIM(ctx, name)
	} else if facts != nil && facts.DKIMKeys[name] != "" {
		dkim = facts.DKIMKeys[name]
	} else if pk, err := s.mailcowDKIM(ctx, name); err == nil && pk != "" {
		dkim = pk
	}
	return name, purpose, s.expectedRecords(kind, facts, s.selectorFor(ctx, kind, facts), name, purpose, token, dkim, ownBounce), nil
}

// ManualDNSRecords is the manual-setup fallback: the exact records this
// domain needs, ready to copy into any DNS provider's panel.
func (s *Service) ManualDNSRecords(ctx context.Context, sc *Scope, domainID string) (map[string]any, error) {
	name, _, recs, err := s.domainRequirements(ctx, sc.ProjectID, domainID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"domain": name, "records": recs, "note": "Publish these at your DNS provider, then run a check. Mail hostnames must be DNS only (not proxied)."}, nil
}

func (s *Service) nsLookup(ctx context.Context, r Resolver, zone string) ([]string, bool) {
	var ns NSResolver
	if r == nil {
		ns = net.DefaultResolver
	} else if v, ok := r.(NSResolver); ok {
		ns = v
	} else {
		return nil, true
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	recs, err := ns.LookupNS(cctx, zone)
	if err != nil && len(recs) == 0 {
		return nil, true
	}
	var out []string
	for _, n := range recs {
		out = append(out, lower(n.Host))
	}
	return out, false
}

// analyze reads the zone and builds a plan. It writes nothing.
func (s *Service) analyze(ctx context.Context, p dnsprovider.Provider, c *dnsConn, projectID, domainID, zoneID string, r Resolver) (DNSPlan, dnsprovider.Zone, error) {
	name, purpose, req, err := s.domainRequirements(ctx, projectID, domainID)
	if err != nil {
		return DNSPlan{}, dnsprovider.Zone{}, err
	}
	var zone dnsprovider.Zone
	if err := s.readRetry(ctx, func() (e error) { zone, e = p.Zone(ctx, zoneID); return }); err != nil {
		return DNSPlan{}, zone, s.dnsCall(ctx, c, err)
	}
	var recs []dnsprovider.Record
	if err := s.readRetry(ctx, func() (e error) { recs, e = p.Records(ctx, zoneID); return }); err != nil {
		return DNSPlan{}, zone, s.dnsCall(ctx, c, err)
	}
	var dnssec string
	_ = s.readRetry(ctx, func() (e error) { dnssec, e = p.DNSSEC(ctx, zoneID); return })
	obs, failed := s.nsLookup(ctx, r, zone.Name)
	plan := BuildDNSPlan(PlanInput{Domain: name, Purpose: purpose, ZoneID: zone.ID, ZoneName: zone.Name, ZoneNS: zone.NameServers, ObservedNS: obs, NSLookupFail: failed,
		DNSSEC: dnssec, Required: req, Existing: recs, MailHost: s.Opts.MailHost})
	return plan, zone, nil
}

func (s *Service) openSetupFor(ctx context.Context, sc *Scope, domainID string) (string, error) {
	var id string
	err := s.Pool.QueryRow(ctx, `SELECT id::text FROM dns_setups WHERE domain_id=$1 AND project_id=$2 AND status IN ('analyzed','blocked','approved','needs_review')
ORDER BY created_at DESC LIMIT 1`, domainID, sc.ProjectID).Scan(&id)
	if err != nil {
		return "", nil
	}
	return id, nil
}

// StartDNSSetup analyzes the domain against the chosen zone and saves the plan.
// It reuses an unfinished setup for the same domain so an interrupted run resumes.
func (s *Service) StartDNSSetup(ctx context.Context, sc *Scope, a Actor, domainID, connID, zoneID string, r Resolver) (map[string]any, error) {
	if !uuidRe.MatchString(domainID) {
		return nil, newErr(404, "not_found")
	}
	var name string
	if err := s.Pool.QueryRow(ctx, `SELECT name FROM domains WHERE id=$1 AND project_id=$2`, domainID, sc.ProjectID).Scan(&name); err != nil {
		return nil, notFound(err)
	}
	// Setting up DNS for a name another project claimed would help take it over.
	var other bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM domain_claims WHERE name=$1 AND project_id IS DISTINCT FROM $2::uuid)`, name, sc.ProjectID).Scan(&other); err != nil {
		return nil, unavailable("dns_claim", err)
	}
	if other {
		return nil, &Error{Status: 403, Code: "domain_claimed_elsewhere", Message: name + " belongs to another project in this installation or is awaiting operator review."}
	}
	if err := s.dnsLimit(ctx, "dns:analyze:user:"+a.UserID, time.Hour, 60); err != nil {
		return nil, err
	}
	if err := s.dnsLimit(ctx, "dns:analyze:domain:"+name, time.Hour, 40); err != nil {
		return nil, err
	}
	p, c, err := s.dnsProviderFor(ctx, sc.ProjectID, connID)
	if err != nil {
		return nil, err
	}
	plan, zone, err := s.analyze(ctx, p, c, sc.ProjectID, domainID, zoneID, r)
	if err != nil {
		return nil, err
	}
	pj, _ := json.Marshal(plan)
	status := "analyzed"
	if plan.Blocked {
		status = "blocked"
	}
	id, _ := s.openSetupFor(ctx, sc, domainID)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, unavailable("dns_setup", err)
	}
	defer tx.Rollback(ctx)
	if id != "" {
		if _, err := tx.Exec(ctx, `UPDATE dns_setups SET connection_id=$3, zone_id=$4, zone_name=$5, status=$6, plan=$7, plan_digest=$8, approved_by='', approved_at=NULL,
acknowledged='[]', updated_at=now() WHERE id=$1 AND project_id=$2`, id, sc.ProjectID, c.ID, zone.ID, zone.Name, status, pj, plan.Digest); err != nil {
			return nil, unavailable("dns_setup", err)
		}
	} else {
		id = secure.NewUUID()
		if _, err := tx.Exec(ctx, `INSERT INTO dns_setups(id, project_id, domain_id, domain, connection_id, zone_id, zone_name, status, plan, plan_digest, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, sc.ProjectID, domainID, name, c.ID, zone.ID, zone.Name, status, pj, plan.Digest, a.Name); err != nil {
			return nil, unavailable("dns_setup", err)
		}
	}
	if err := audit(ctx, tx, a, "dns.analyze", "dns_setup", id, map[string]any{"project": sc.Project, "domain": name, "zone": zone.Name, "digest": plan.Digest, "blocked": plan.Blocked, "changes": plan.Actionable()}); err != nil {
		return nil, unavailable("dns_setup", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, unavailable("dns_setup", err)
	}
	return s.SetupView(ctx, sc, id)
}

// ReanalyzeDNSSetup reads DNS again (after the user fixed a conflict by hand)
// and clears any approval: the user reviews the new plan.
func (s *Service) ReanalyzeDNSSetup(ctx context.Context, sc *Scope, a Actor, id string, r Resolver) (map[string]any, error) {
	row, err := s.loadSetup(ctx, sc.ProjectID, id)
	if err != nil {
		return nil, err
	}
	switch row.Status {
	case "analyzed", "blocked", "approved", "needs_review", "partial", "failed", "applied":
	default:
		return nil, &Error{Status: 409, Code: "setup_busy", Message: "This setup is " + row.Status + " and cannot be analyzed again now."}
	}
	if row.ConnID == "" {
		return nil, &Error{Status: 409, Code: "dns_connection_inactive", Message: "This setup has no Cloudflare connection. Connect again to continue."}
	}
	// Open setups are re-read in place; finished runs are history, so a fresh one starts.
	return s.StartDNSSetup(ctx, sc, a, row.DomainID, row.ConnID, row.ZoneID, r)
}

// ApproveDNSSetup records the user's approval of the exact plan they saw.
func (s *Service) ApproveDNSSetup(ctx context.Context, sc *Scope, a Actor, id, digest string, acks []string) (map[string]any, error) {
	row, err := s.loadSetup(ctx, sc.ProjectID, id)
	if err != nil {
		return nil, err
	}
	if row.Status == "blocked" || row.Plan.Blocked {
		return nil, &Error{Status: 409, Code: "setup_blocked", Message: "The analysis found a problem that must be fixed before anything can be applied."}
	}
	if row.Status != "analyzed" {
		return nil, &Error{Status: 409, Code: "setup_not_reviewable", Message: "This setup is " + row.Status + ". Analyze again to get a plan to approve."}
	}
	if row.Plan.Actionable() == 0 {
		return nil, &Error{Status: 409, Code: "nothing_to_apply", Message: "There is nothing to apply. Fix the conflicts by hand or verify the domain."}
	}
	if digest == "" || digest != row.Digest {
		return nil, &Error{Status: 409, Code: "plan_changed", Message: "The plan changed since you reviewed it. Review the new plan and approve again."}
	}
	have := map[string]bool{}
	for _, c := range acks {
		have[c] = true
	}
	var missing []string
	for _, need := range row.Plan.RequiredAcks {
		if !have[need] {
			missing = append(missing, need)
		}
	}
	if len(missing) > 0 {
		return nil, &Error{Status: 400, Code: "confirmation_required", Message: "These changes need an explicit confirmation: " + strings.Join(missing, ", ")}
	}
	aj, _ := json.Marshal(row.Plan.RequiredAcks)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, unavailable("dns_approve", err)
	}
	defer tx.Rollback(ctx)
	res, err := tx.Exec(ctx, `UPDATE dns_setups SET status='approved', approved_by=$3, approved_at=now(), acknowledged=$4, updated_at=now()
WHERE id=$1 AND project_id=$2 AND status='analyzed' AND plan_digest=$5`, id, sc.ProjectID, a.Name, aj, digest)
	if err != nil {
		return nil, unavailable("dns_approve", err)
	}
	if res.RowsAffected() != 1 {
		return nil, &Error{Status: 409, Code: "plan_changed", Message: "The plan changed since you reviewed it. Review the new plan and approve again."}
	}
	if err := audit(ctx, tx, a, "dns.approve", "dns_setup", id, map[string]any{"project": sc.Project, "domain": row.Domain, "digest": digest, "acknowledged": row.Plan.RequiredAcks}); err != nil {
		return nil, unavailable("dns_approve", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, unavailable("dns_approve", err)
	}
	return s.SetupView(ctx, sc, id)
}

var applyOrder = map[string]int{"ownership": 0, "dkim": 1, "spf": 2, "return_path_spf": 3, "dmarc": 4, "return_path": 5, "mx": 6}

type changeRec struct {
	ID, SetupID, Domain, ZoneID, ItemKey, Action, RecordID, Type, Name, Status, Note string
	Previous, New                                                                    *dnsprovider.Record
	Adopted                                                                          bool
}

func (s *Service) recordChange(ctx context.Context, row *setupRow, a Actor, it PlanItem, prev *dnsprovider.Record) (string, error) {
	id := secure.NewUUID()
	action := "create"
	if it.Action == PlanModify {
		action = "update"
	}
	nw, _ := json.Marshal(dnsprovider.Record{Type: it.Type, Name: it.Name, Content: it.Content, Priority: it.Priority, TTL: it.TTL})
	var pj []byte
	if prev != nil {
		pj, _ = json.Marshal(prev)
	}
	rid := ""
	if prev != nil {
		rid = prev.ID
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO dns_changes(id, setup_id, project_id, domain, zone_id, item_key, action, record_id, record_type, record_name, previous, new, status, actor)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'pending',$13)`, id, row.ID, row.ProjectID, row.Domain, row.ZoneID, it.Key, action, rid, it.Type, it.Name, nullBytes(pj), nw, a.Name)
	return id, err
}

func nullBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func (s *Service) finishChange(ctx context.Context, id, status, recordID string, final *dnsprovider.Record, adopted bool, errCode, note string) {
	var nj []byte
	if final != nil {
		nj, _ = json.Marshal(final)
	}
	if nj != nil {
		_, _ = s.Pool.Exec(ctx, `UPDATE dns_changes SET status=$2, record_id=CASE WHEN $3='' THEN record_id ELSE $3 END, new=$4, adopted=$5, error_code=$6, note=$7, applied_at=now() WHERE id=$1`,
			id, status, recordID, nj, adopted, errCode, note)
		return
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE dns_changes SET status=$2, adopted=$3, error_code=$4, note=$5, applied_at=now() WHERE id=$1`, id, status, adopted, errCode, note)
}

func findRecord(rs []dnsprovider.Record, typ, name, content string) *dnsprovider.Record {
	for i := range rs {
		if rs[i].Type == typ && rs[i].Name == name && rs[i].Content == content {
			return &rs[i]
		}
	}
	return nil
}

// createRecord creates one record idempotently. A rate limit means the request
// was not processed and is retried after waiting; an ambiguous failure (timeout,
// 5xx, lost response) is settled by looking at the zone, not by repeating blindly.
func (s *Service) createRecord(ctx context.Context, p dnsprovider.Provider, zoneID string, want dnsprovider.Record) (rec dnsprovider.Record, adopted bool, err error) {
	const attempts = 4
	wait := 500 * time.Millisecond
	var last error
	for i := 0; i < attempts; i++ {
		out, cerr := p.Create(ctx, zoneID, want)
		if cerr == nil {
			return out, false, nil
		}
		last = cerr
		var rl *dnsprovider.RateLimitError
		var tr *dnsprovider.TransientError
		switch {
		case errors.Is(cerr, dnsprovider.ErrConflict):
			// An identical record is already there: idempotent success, not ours to remove.
			var cur []dnsprovider.Record
			if rerr := s.readRetry(ctx, func() (e error) { cur, e = p.Records(ctx, zoneID); return }); rerr != nil {
				return dnsprovider.Record{}, false, rerr
			}
			if f := findRecord(cur, want.Type, want.Name, want.Content); f != nil {
				return *f, true, nil
			}
			return dnsprovider.Record{}, false, cerr
		case errors.As(cerr, &rl):
			d := rl.RetryAfter
			if d <= 0 {
				d = wait
			}
			if d > 30*time.Second {
				d = 30 * time.Second
			}
			if err := s.sleeper()(ctx, d); err != nil {
				return dnsprovider.Record{}, false, err
			}
		case errors.As(cerr, &tr):
			if err := s.sleeper()(ctx, wait); err != nil {
				return dnsprovider.Record{}, false, err
			}
			var cur []dnsprovider.Record
			if rerr := s.readRetry(ctx, func() (e error) { cur, e = p.Records(ctx, zoneID); return }); rerr == nil {
				if f := findRecord(cur, want.Type, want.Name, want.Content); f != nil {
					return *f, false, nil // it landed; the response was lost
				}
			}
		default:
			return dnsprovider.Record{}, false, cerr
		}
		wait *= 2
	}
	return dnsprovider.Record{}, false, last
}

// updateRecord changes one existing record once. It is never retried blindly:
// after an ambiguous failure the record is read back to see what happened.
func (s *Service) updateRecord(ctx context.Context, p dnsprovider.Provider, zoneID string, prev dnsprovider.Record, content string) (dnsprovider.Record, error) {
	want := prev
	want.Content = content
	out, err := p.Update(ctx, zoneID, prev.ID, want)
	if err == nil {
		return out, nil
	}
	var rl *dnsprovider.RateLimitError
	var tr *dnsprovider.TransientError
	if errors.As(err, &rl) || errors.As(err, &tr) {
		if errors.As(err, &rl) && rl.RetryAfter > 0 {
			d := rl.RetryAfter
			if d > 30*time.Second {
				d = 30 * time.Second
			}
			if serr := s.sleeper()(ctx, d); serr != nil {
				return dnsprovider.Record{}, serr
			}
		}
		var cur []dnsprovider.Record
		if rerr := s.readRetry(ctx, func() (e error) { cur, e = p.Records(ctx, zoneID); return }); rerr != nil {
			return dnsprovider.Record{}, err
		}
		for _, r := range cur {
			if r.ID != prev.ID {
				continue
			}
			if r.Content == content {
				return r, nil // it was applied
			}
			if r.Content == prev.Content && r.Modified == prev.Modified && errors.As(err, &rl) {
				// Rate limited and unchanged: the request was not processed; one more try is safe.
				return p.Update(ctx, zoneID, prev.ID, want)
			}
		}
	}
	return dnsprovider.Record{}, err
}

// ApplyDNSSetup writes the approved plan. It rebuilds the plan from a fresh read
// first and refuses if anything at the touched names changed since approval.
func (s *Service) ApplyDNSSetup(ctx context.Context, sc *Scope, a Actor, id string, r Resolver) (map[string]any, error) {
	row, err := s.loadSetup(ctx, sc.ProjectID, id)
	if err != nil {
		return nil, err
	}
	if row.Status != "approved" || row.ApprovedAt == nil {
		return nil, &Error{Status: 409, Code: "setup_not_approved", Message: "Approve the plan before applying it."}
	}
	if time.Since(*row.ApprovedAt) > approvalLife {
		_, _ = s.Pool.Exec(ctx, `UPDATE dns_setups SET status='needs_review', approved_at=NULL, approved_by='', updated_at=now() WHERE id=$1 AND project_id=$2 AND status='approved'`, id, sc.ProjectID)
		return nil, &Error{Status: 409, Code: "approval_expired", Message: "The approval expired after 30 minutes. Review the plan again and approve it."}
	}
	if err := s.dnsLimit(ctx, "dns:apply:user:"+a.UserID, time.Hour, 20); err != nil {
		return nil, err
	}
	if err := s.dnsLimit(ctx, "dns:apply:domain:"+row.Domain, time.Hour, 15); err != nil {
		return nil, err
	}
	p, c, err := s.dnsProviderFor(ctx, sc.ProjectID, row.ConnID)
	if err != nil {
		return nil, err
	}
	// Claim the run: two concurrent applies cannot both proceed.
	res, err := s.Pool.Exec(ctx, `UPDATE dns_setups SET status='applying', updated_at=now() WHERE id=$1 AND project_id=$2 AND status='approved'`, id, sc.ProjectID)
	if err != nil {
		return nil, unavailable("dns_apply", err)
	}
	if res.RowsAffected() != 1 {
		return nil, &Error{Status: 409, Code: "setup_busy", Message: "This setup is already being applied."}
	}
	revert := func(status string) {
		_, _ = s.Pool.Exec(ctx, `UPDATE dns_setups SET status=$3, updated_at=now() WHERE id=$1 AND project_id=$2 AND status='applying'`, id, sc.ProjectID, status)
	}
	fresh, _, err := s.analyze(ctx, p, c, sc.ProjectID, row.DomainID, row.ZoneID, r)
	if err != nil {
		revert("approved") // nothing was written; the approval still stands
		return nil, err
	}
	if fresh.Digest != row.Digest || fresh.Blocked {
		pj, _ := json.Marshal(fresh)
		_, _ = s.Pool.Exec(ctx, `UPDATE dns_setups SET status='needs_review', plan=$3, plan_digest=$4, approved_at=NULL, approved_by='', acknowledged='[]', updated_at=now() WHERE id=$1 AND project_id=$2`,
			id, sc.ProjectID, pj, fresh.Digest)
		_ = s.Audit(ctx, a, "dns.apply_refused", "dns_setup", id, map[string]any{"project": sc.Project, "domain": row.Domain, "reason": "dns_changed"})
		return nil, &Error{Status: 409, Code: "dns_changed_concurrently", Message: "DNS for this domain changed after you approved the plan, so nothing was written. Review the updated plan."}
	}
	items := append([]PlanItem(nil), fresh.Items...)
	sort.SliceStable(items, func(i, j int) bool { return applyOrder[items[i].Key] < applyOrder[items[j].Key] })
	applied, failed := 0, 0
	var firstErr error
	for _, it := range items {
		if it.Action != PlanCreate && it.Action != PlanModify {
			continue
		}
		chID, err := s.recordChange(ctx, row, a, it, it.Existing)
		if err != nil {
			firstErr, failed = unavailable("dns_apply", err), failed+1
			break
		}
		want := dnsprovider.Record{Type: it.Type, Name: it.Name, Content: it.Content, Priority: it.Priority, TTL: it.TTL}
		var out dnsprovider.Record
		var adopted bool
		if it.Action == PlanCreate {
			out, adopted, err = s.createRecord(ctx, p, row.ZoneID, want)
		} else {
			out, err = s.updateRecord(ctx, p, row.ZoneID, *it.Existing, it.Content)
		}
		if err != nil {
			code := "error"
			switch me := mapDNSErr(err).(type) {
			case *Error:
				code = me.Code
			}
			s.finishChange(ctx, chID, "failed", "", nil, false, code, "")
			firstErr, failed = s.dnsCall(ctx, c, err), failed+1
			break
		}
		note := ""
		if adopted {
			note = "already present; left as it was"
		}
		s.finishChange(ctx, chID, "applied", out.ID, &out, adopted, "", note)
		_ = s.Audit(ctx, a, "dns.record_"+map[string]string{PlanCreate: "create", PlanModify: "update"}[it.Action], "dns_change", chID, map[string]any{
			"project": sc.Project, "domain": row.Domain, "zone": row.ZoneName, "record_id": out.ID, "type": it.Type, "name": it.Name,
			"previous": prevContent(it.Existing), "new": it.Content, "adopted": adopted})
		applied++
	}
	status := "applied"
	switch {
	case failed > 0 && applied > 0:
		status = "partial"
	case failed > 0:
		status = "failed"
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE dns_setups SET status=$3, applied_at=CASE WHEN $3='applied' THEN now() ELSE applied_at END, updated_at=now() WHERE id=$1 AND project_id=$2`, id, sc.ProjectID, status)
	_ = s.Audit(ctx, a, "dns.apply", "dns_setup", id, map[string]any{"project": sc.Project, "domain": row.Domain, "applied": applied, "failed": failed, "status": status})
	if status == "applied" {
		// Look at DNS right away. Propagation may not be done: this is not readiness.
		_, _ = s.CheckDomain(ctx, row.DomainID, r)
	}
	if firstErr != nil {
		v, _ := s.SetupView(ctx, sc, id)
		return v, firstErr
	}
	return s.SetupView(ctx, sc, id)
}

func prevContent(r *dnsprovider.Record) string {
	if r == nil {
		return ""
	}
	return r.Content
}

// RollbackDNSSetup undoes what Njiwa-Mail changed, and only that, and only
// where nobody changed it since.
func (s *Service) RollbackDNSSetup(ctx context.Context, sc *Scope, a Actor, id string, r Resolver) (map[string]any, error) {
	row, err := s.loadSetup(ctx, sc.ProjectID, id)
	if err != nil {
		return nil, err
	}
	switch row.Status {
	case "applied", "partial", "failed", "rollback_partial":
	default:
		return nil, &Error{Status: 409, Code: "nothing_to_roll_back", Message: "There is nothing applied to roll back."}
	}
	p, c, err := s.dnsProviderFor(ctx, sc.ProjectID, row.ConnID)
	if err != nil {
		return nil, &Error{Status: 409, Code: "dns_connection_inactive", Message: "Rollback needs a working Cloudflare connection. Connect again, or remove the records listed in the change history by hand."}
	}
	if err := s.dnsLimit(ctx, "dns:apply:domain:"+row.Domain, time.Hour, 15); err != nil {
		return nil, err
	}
	changes, err := s.changesOf(ctx, sc.ProjectID, id)
	if err != nil {
		return nil, err
	}
	var cur []dnsprovider.Record
	if err := s.readRetry(ctx, func() (e error) { cur, e = p.Records(ctx, row.ZoneID); return }); err != nil {
		return nil, s.dnsCall(ctx, c, err)
	}
	byID := map[string]dnsprovider.Record{}
	for _, r := range cur {
		byID[r.ID] = r
	}
	rolled, skipped := 0, 0
	for i := len(changes) - 1; i >= 0; i-- {
		ch := changes[i]
		if ch.Status != "applied" || ch.Adopted {
			continue
		}
		now, exists := byID[ch.RecordID]
		skip := func(why string) {
			_, _ = s.Pool.Exec(ctx, `UPDATE dns_changes SET status='rollback_skipped', note=$2, rolled_back_at=now() WHERE id=$1`, ch.ID, why)
			skipped++
		}
		done := func() {
			_, _ = s.Pool.Exec(ctx, `UPDATE dns_changes SET status='rolled_back', rolled_back_at=now() WHERE id=$1`, ch.ID)
			rolled++
			_ = s.Audit(ctx, a, "dns.record_rollback", "dns_change", ch.ID, map[string]any{"project": sc.Project, "domain": row.Domain, "record_id": ch.RecordID, "type": ch.Type, "name": ch.Name})
		}
		switch ch.Action {
		case "create":
			switch {
			case !exists:
				skip("already removed by someone else")
			case now.Content != ch.New.Content || now.Type != ch.Type || now.Name != ch.Name:
				skip("changed since Njiwa-Mail created it; left alone. Review it in Cloudflare")
			default:
				if derr := p.Delete(ctx, row.ZoneID, ch.RecordID); derr != nil {
					if errors.Is(derr, dnsprovider.ErrNotFound) {
						skip("already removed by someone else")
					} else {
						skip("could not be removed (" + mapDNSErr(derr).(*Error).Code + "); remove it in Cloudflare")
					}
				} else {
					done()
				}
			}
		case "update":
			switch {
			case !exists:
				skip("the record no longer exists; nothing to restore")
			case now.Content != ch.New.Content:
				skip("changed again after Njiwa-Mail edited it; the newer change is kept. Review it in Cloudflare")
			default:
				prev := now
				prev.Content = ch.Previous.Content
				if _, uerr := p.Update(ctx, row.ZoneID, ch.RecordID, prev); uerr != nil {
					skip("could not be restored (" + mapDNSErr(uerr).(*Error).Code + "); restore it in Cloudflare")
				} else {
					done()
				}
			}
		}
	}
	status := "rolled_back"
	if skipped > 0 {
		status = "rollback_partial"
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE dns_setups SET status=$3, updated_at=now() WHERE id=$1 AND project_id=$2`, id, sc.ProjectID, status)
	_ = s.Audit(ctx, a, "dns.rollback", "dns_setup", id, map[string]any{"project": sc.Project, "domain": row.Domain, "rolled_back": rolled, "skipped": skipped})
	_, _ = s.CheckDomain(ctx, row.DomainID, r)
	return s.SetupView(ctx, sc, id)
}

func (s *Service) changesOf(ctx context.Context, projectID, setupID string) ([]changeRec, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id::text, setup_id::text, domain, zone_id, item_key, action, record_id, record_type, record_name, status, note, previous, new, adopted
FROM dns_changes WHERE setup_id=$1 AND project_id=$2 ORDER BY applied_at, id`, setupID, projectID)
	if err != nil {
		return nil, unavailable("dns_changes", err)
	}
	defer rows.Close()
	var out []changeRec
	for rows.Next() {
		var c changeRec
		var pj, nj []byte
		if err := rows.Scan(&c.ID, &c.SetupID, &c.Domain, &c.ZoneID, &c.ItemKey, &c.Action, &c.RecordID, &c.Type, &c.Name, &c.Status, &c.Note, &pj, &nj, &c.Adopted); err != nil {
			return nil, unavailable("dns_changes", err)
		}
		if len(pj) > 0 {
			c.Previous = &dnsprovider.Record{}
			_ = json.Unmarshal(pj, c.Previous)
		}
		c.New = &dnsprovider.Record{}
		_ = json.Unmarshal(nj, c.New)
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetupView is the wizard's read model: plan, changes and (when applied) verification.
func (s *Service) SetupView(ctx context.Context, sc *Scope, id string) (map[string]any, error) {
	row, err := s.loadSetup(ctx, sc.ProjectID, id)
	if err != nil {
		return nil, err
	}
	changes, err := s.changesOf(ctx, sc.ProjectID, id)
	if err != nil {
		return nil, err
	}
	var created, updated time.Time
	var applied *time.Time
	var connStatus *string
	_ = s.Pool.QueryRow(ctx, `SELECT s.created_at, s.updated_at, s.applied_at, c.status FROM dns_setups s LEFT JOIN dns_connections c ON c.id=s.connection_id WHERE s.id=$1`, id).Scan(&created, &updated, &applied, &connStatus)
	cs := ""
	if connStatus != nil {
		cs = *connStatus
	}
	ch := make([]map[string]any, 0, len(changes))
	for _, c := range changes {
		ch = append(ch, map[string]any{"id": c.ID, "key": c.ItemKey, "action": c.Action, "type": c.Type, "name": c.Name, "record_id": c.RecordID, "status": c.Status,
			"previous": c.Previous, "new": c.New, "adopted": c.Adopted, "note": c.Note})
	}
	return map[string]any{"id": row.ID, "domain_id": row.DomainID, "domain": row.Domain, "connection_id": row.ConnID, "connection_status": cs, "zone_id": row.ZoneID,
		"zone_name": row.ZoneName, "status": row.Status, "plan": row.Plan, "digest": row.Digest, "approved_by": row.ApprovedBy, "approved_at": row.ApprovedAt,
		"applied_at": applied, "created_at": created, "updated_at": updated, "changes": ch}, nil
}

// LatestDNSSetup lets the wizard resume an interrupted run.
func (s *Service) LatestDNSSetup(ctx context.Context, sc *Scope, domainID string) (map[string]any, error) {
	if !uuidRe.MatchString(domainID) {
		return nil, newErr(404, "not_found")
	}
	var id string
	err := s.Pool.QueryRow(ctx, `SELECT id::text FROM dns_setups WHERE domain_id=$1 AND project_id=$2 ORDER BY created_at DESC LIMIT 1`, domainID, sc.ProjectID).Scan(&id)
	if errors.Is(err, db.ErrNoRows) {
		return map[string]any{"setup": nil}, nil
	}
	if err != nil {
		return nil, unavailable("dns_setup", err)
	}
	v, err := s.SetupView(ctx, sc, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"setup": v}, nil
}

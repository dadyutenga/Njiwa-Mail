package service

import (
	"context"
	"errors"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/installation"
)

// The guided "set up this mail host" flow. Every step is derived from what is
// recorded, so it survives reloads and restarts and cannot claim more than
// happened.

// FlowStep is one step: done, current (do this next), todo or blocked.
type FlowStep struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

// HostFlow is the whole flow.
type HostFlow struct {
	Steps     []FlowStep `json:"steps"`
	ProfileID string     `json:"profile_id"`
}

// ManagedInstalled reports whether a verified installation is on record.
func (s *Service) ManagedInstalled(ctx context.Context) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM installer_receipts r JOIN transport_profiles p ON p.id=r.profile_id
WHERE p.mode='managed_mta' AND p.status <> 'retired' AND r.result_code='ok' AND r.verified_at IS NOT NULL AND r.rollback_state <> 'rolled_back')`).Scan(&ok)
	if err != nil {
		return false, unavailable("managed_installed", err)
	}
	return ok, nil
}

// HostSetupFlow answers where the setup of this mail host stands:
// scan, recommendation, install, DNS, connection test, activation.
func (s *Service) HostSetupFlow(ctx context.Context) (*HostFlow, error) {
	var scanned bool
	var recommended string
	var blockers int
	if err := s.Pool.QueryRow(ctx, `SELECT discovered_at IS NOT NULL, COALESCE(discovery->'recommendation'->>'mode',''),
  CASE WHEN jsonb_typeof(discovery->'recommendation'->'managed_blockers')='array' THEN jsonb_array_length(discovery->'recommendation'->'managed_blockers') ELSE 0 END
FROM installation_settings WHERE id=1`).Scan(&scanned, &recommended, &blockers); err != nil {
		return nil, unavailable("host_flow", err)
	}
	w, err := s.wizard(ctx, s.Pool)
	if err != nil {
		return nil, unavailable("host_flow", err)
	}
	f := &HostFlow{}
	var active bool
	var rev int64
	err = s.Pool.QueryRow(ctx, `SELECT id::text, is_active, config_revision FROM transport_profiles WHERE mode='managed_mta' AND status <> 'retired'
ORDER BY is_active DESC, created_at LIMIT 1`).Scan(&f.ProfileID, &active, &rev)
	if err != nil && !errors.Is(err, db.ErrNoRows) {
		return nil, unavailable("host_flow", err)
	}
	installed, err := s.ManagedInstalled(ctx)
	if err != nil {
		return nil, err
	}
	missing, err := s.DomainsMissingKey(ctx)
	if err != nil {
		return nil, err
	}
	// A DNS result counts only when it was checked after the latest keys were
	// recorded: an older pass says nothing about a new key.
	var claimed, passing int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE EXISTS (SELECT 1 FROM domains d WHERE d.name=c.name AND d.project_id=c.project_id
  AND d.spf_ok AND d.dkim_ok AND d.dmarc_ok AND d.last_checked_at >=
    (SELECT max(verified_at) FROM installer_receipts WHERE action='apply' AND result_code='ok' AND rollback_state <> 'rolled_back')))
FROM domain_claims c WHERE c.state='active'`).Scan(&claimed, &passing); err != nil {
		return nil, unavailable("host_flow", err)
	}
	var tested bool
	if f.ProfileID != "" {
		if err := s.Pool.QueryRow(ctx, `SELECT COALESCE((SELECT result='pass' FROM installation_checks WHERE profile_id=$1::uuid AND config_revision=$2
  AND check_key='connection' ORDER BY checked_at DESC LIMIT 1), false)`, f.ProfileID, rev).Scan(&tested); err != nil {
			return nil, unavailable("host_flow", err)
		}
	}
	// A host with a mail server on it is connected to, never installed over.
	blocked := !installed && recommended != "" && recommended != installation.ModeManaged && blockers > 0
	add := func(id string, done bool, detail string) {
		state := "todo"
		if done {
			state = "done"
		}
		f.Steps = append(f.Steps, FlowStep{ID: id, State: state, Detail: detail})
	}
	add("scan", scanned || installed, "Read-only: it finds mail servers, busy ports and certificates.")
	switch {
	case blocked:
		f.Steps = append(f.Steps, FlowStep{ID: "choose", State: "blocked",
			Detail: "A mail server is already on this host. Connect it as an existing mail server; Njiwa-Mail never replaces it."})
	default:
		add("choose", installed || w.SelectedMode == installation.ModeManaged, "Choose \"Send from my VPS\".")
	}
	add("install", installed, "Plan, approve the exact digest, install.")
	add("dns", installed && claimed > 0 && len(missing) == 0 && passing == claimed, "Publish SPF, DKIM and DMARC for every sending domain, then check each domain.")
	add("test", installed && tested, "Connect to the installed mail server.")
	add("activate", active, "Send Live mail through this host.")
	for i := range f.Steps {
		if f.Steps[i].State == "blocked" {
			for j := i + 1; j < len(f.Steps); j++ {
				f.Steps[j].State = "blocked"
			}
			break
		}
		if f.Steps[i].State == "todo" {
			f.Steps[i].State = "current"
			break
		}
	}
	return f, nil
}

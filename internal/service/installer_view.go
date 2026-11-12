package service

import (
	"context"
	"encoding/json"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Mail host screens (managed mode): what the installer did, whether delivery
// evidence is flowing, which signing domains still need a DKIM key, and where a
// staged DKIM rotation stands. All of it is read-only; the host changes only
// through the installer CLI.

// InstallerReceipts lists the installer's receipts, newest first.
func (s *Service) InstallerReceipts(ctx context.Context, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.maps(ctx, `SELECT r.id::text AS id, r.action, r.result_code, r.applied_version, r.approved_by, r.rollback_state,
  r.verified_at, r.created_at, left(r.plan_digest, 12) AS plan_digest, r.profile_id::text AS profile_id
FROM installer_receipts r ORDER BY r.created_at DESC LIMIT $1`, limit)
}

// EvidenceStatus says when delivery evidence last arrived. Silence is not proof
// that the log collector is down (no mail, no events), so it is reported as facts.
func (s *Service) EvidenceStatus(ctx context.Context) (map[string]any, error) {
	out := map[string]any{}
	for _, src := range []string{"smtp", "mta_log", "dsn"} {
		var last *time.Time
		var n24 int
		if err := s.Pool.QueryRow(ctx, `SELECT max(observed_at), count(*) FILTER (WHERE observed_at > now() - interval '24 hours') FROM delivery_events WHERE source=$1`, src).Scan(&last, &n24); err != nil {
			return nil, unavailable("evidence_status", err)
		}
		out[src] = map[string]any{"last_at": last, "events_24h": n24}
	}
	var live24 int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE state='submitted' AND submitted_at > now() - interval '24 hours' AND source <> 'transport_test'`).Scan(&live24)
	out["submitted_24h"] = live24
	return out, nil
}

// managedKeys returns the domains that have a DKIM record in the managed profile's last install record.
func (s *Service) managedKeys(ctx context.Context) (selector string, keys map[string]bool, host string, port string) {
	keys = map[string]bool{}
	var raw []byte
	if err := s.Pool.QueryRow(ctx, `SELECT config_json FROM transport_profiles WHERE mode='managed_mta' AND status <> 'retired' ORDER BY is_active DESC, created_at LIMIT 1`).Scan(&raw); err != nil {
		return "", keys, "", ""
	}
	var c struct {
		Selector string            `json:"selector"`
		Records  map[string]string `json:"dkim_records"`
		Host     string            `json:"submission_host"`
		Port     int               `json:"submission_port"`
	}
	_ = json.Unmarshal(raw, &c)
	for d := range c.Records {
		keys[d] = true
	}
	p := ""
	if c.Port > 0 {
		p = strconv.Itoa(c.Port)
	}
	return c.Selector, keys, c.Host, p
}

// DomainsMissingKey lists claimed sending domains the managed signer has no key for.
// After a new domain is verified the installer must run again to create it.
func (s *Service) DomainsMissingKey(ctx context.Context) ([]string, error) {
	claimed, err := s.ClaimedDomains(ctx)
	if err != nil {
		return nil, err
	}
	_, keys, _, _ := s.managedKeys(ctx)
	var missing []string
	for _, d := range claimed {
		if !keys[d] {
			missing = append(missing, d)
		}
	}
	return missing, nil
}

// SuggestedInstallerCommands builds the plan and apply commands for the current
// facts, so the operator can paste them (the digest from plan goes into apply).
func (s *Service) SuggestedInstallerCommands(ctx context.Context) []string {
	claimed, _ := s.ClaimedDomains(ctx)
	sel, _, host, port := s.managedKeys(ctx)
	if sel == "" {
		sel = s.Opts.DKIMSelector
	}
	if sel == "" {
		sel = "njiwa"
	}
	sub := "172.30.0.1:10587"
	if host != "" && port != "" {
		sub = host + ":" + port
	}
	flags := "-hostname " + s.Opts.MailHost + " -domains " + strings.Join(claimed, ",") + " -selector " + sel + " -submission " + sub
	bds := make([]string, 0, 4)
	for d := range s.bounceDomains(ctx) {
		bds = append(bds, d)
	}
	sort.Strings(bds)
	if len(bds) > 0 {
		flags += " -bounce-domain " + strings.Join(bds, ",")
	}
	return []string{"njiwa-mail installer plan " + flags, "njiwa-mail installer apply " + flags + " -approve <digest from the plan> -record"}
}

// RecordRotation stores the installer's view of a staged DKIM rotation.
func (s *Service) RecordRotation(ctx context.Context, a Actor, state map[string]any) error {
	raw, _ := json.Marshal(state)
	if _, err := s.Pool.Exec(ctx, `UPDATE installation_settings SET dkim_rotation=$1::jsonb, updated_at=now() WHERE id=1`, raw); err != nil {
		return unavailable("rotation_record", err)
	}
	return s.Audit(ctx, a, "installer.dkim_rotation", "installation", "", map[string]any{"state": state["state"], "from": state["from"], "to": state["to"]})
}

// RotationView returns the recorded rotation state. While a rotation is
// prepared it also checks, live, whether each new key is published in DNS.
func (s *Service) RotationView(ctx context.Context, r Resolver) (map[string]any, error) {
	var raw []byte
	if err := s.Pool.QueryRow(ctx, `SELECT dkim_rotation FROM installation_settings WHERE id=1`).Scan(&raw); err != nil {
		return nil, unavailable("rotation_view", err)
	}
	st := map[string]any{}
	_ = json.Unmarshal(raw, &st)
	if len(st) == 0 {
		return map[string]any{"state": "none"}, nil
	}
	if st["state"] == "prepared" {
		if r == nil {
			r = net.DefaultResolver
		}
		to, _ := st["to"].(string)
		recs, _ := st["records"].(map[string]any)
		var domains []string
		for d := range recs {
			domains = append(domains, d)
		}
		sort.Strings(domains)
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		var checks []map[string]any
		ready := len(domains) > 0
		for _, d := range domains {
			want, _ := recs[d].(string)
			txt, err := r.LookupTXT(cctx, to+"._domainkey."+d)
			ok, hint := evalDKIM(txt, err, want)
			ready = ready && ok
			checks = append(checks, map[string]any{"domain": d, "host": to + "._domainkey." + d, "published": ok, "hint": hint})
		}
		st["dns"], st["ready_to_activate"] = checks, ready
		delete(st, "records") // public values, but the page needs only the checks
	}
	return st, nil
}

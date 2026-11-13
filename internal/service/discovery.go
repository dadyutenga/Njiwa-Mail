package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/njiwa-tz/mail/internal/installer/discover"
)

// Host discovery (installation step 2). The scan runs on the host as a CLI
// command (the web process cannot and must not inspect the host); it records
// its report here so the setup wizard can show what was found and recommend
// a delivery mode.

// RecordDiscovery stores the latest discovery report.
func (s *Service) RecordDiscovery(ctx context.Context, a Actor, r *discover.Report) error {
	if r == nil || r.Recommendation == nil {
		return badRequest("the discovery report is incomplete")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return badRequest("the discovery report cannot be stored")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("discovery", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE installation_settings SET discovery=$1::jsonb, discovered_at=$2, updated_at=now() WHERE id=1`, raw, r.ScannedAt); err != nil {
		return unavailable("discovery", err)
	}
	if err := audit(ctx, tx, a, "installation.discovery", "installation", "1", map[string]any{
		"recommended_mode": r.Recommendation.Mode, "managed_blockers": len(r.Recommendation.ManagedBlockers),
		"warnings": len(r.Recommendation.Warnings)}); err != nil {
		return unavailable("audit", err)
	}
	return tx.Commit(ctx)
}

// Discovery returns the latest recorded report, or nil when discovery has not
// been recorded yet.
func (s *Service) Discovery(ctx context.Context) (map[string]any, error) {
	var raw []byte
	var at *time.Time
	if err := s.Pool.QueryRow(ctx, `SELECT discovery, discovered_at FROM installation_settings WHERE id=1`).Scan(&raw, &at); err != nil {
		return nil, unavailable("discovery", err)
	}
	if at == nil {
		return map[string]any{"report": nil, "recorded_at": nil}, nil
	}
	var rep map[string]any
	_ = json.Unmarshal(raw, &rep)
	return map[string]any{"report": rep, "recorded_at": at}, nil
}

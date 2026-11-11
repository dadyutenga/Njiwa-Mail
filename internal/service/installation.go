package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/installation"
	"github.com/njiwa-tz/mail/internal/secure"
)

// Installation wizard (backlog B01-B03). The operator's delivery-mode choice
// is persisted server-side in installation_settings.wizard, so the wizard
// resumes after a refresh, another browser or a redeploy. Choosing a mode
// never switches an active transport: that goes through the switch planner.

// Welcome states of the installation (UI screen 01).
const (
	WelcomeReadyToConfigure = "ready_to_configure" // nothing chosen yet
	WelcomeLegacyDetected   = "legacy_detected"    // settings migrated from an older install, not yet reviewed
	WelcomeInProgress       = "in_progress"        // mode chosen, required steps remain
	WelcomeDegraded         = "degraded"           // was ready, is not any more
	WelcomeCompleted        = "completed"          // transport ready; domains are a project matter
)

type WizardState struct {
	SelectedMode string     `json:"selected_mode,omitempty"`
	ConfirmedAt  *time.Time `json:"confirmed_at,omitempty"`
	ConfirmedBy  string     `json:"confirmed_by,omitempty"`
}

func (s *Service) wizard(ctx context.Context, q queryer) (WizardState, error) {
	var w WizardState
	var raw []byte
	if err := q.QueryRow(ctx, `SELECT wizard FROM installation_settings WHERE id=1`).Scan(&raw); err != nil {
		if errors.Is(err, db.ErrNoRows) {
			return w, nil
		}
		return w, err
	}
	_ = json.Unmarshal(raw, &w)
	return w, nil
}

// ChooseDeliveryMode records the operator's confirmed choice. With no active
// profile it simply selects the mode (and, for managed_mta, creates the draft
// profile the installer will complete). With an active profile of the same
// mode it confirms the existing installation ("continue existing setup").
// With an active profile of another mode it refuses: a switch must be planned.
func (s *Service) ChooseDeliveryMode(ctx context.Context, a Actor, mode string) (map[string]any, error) {
	if mode != installation.ModeManaged && mode != installation.ModeExisting {
		return nil, badRequest("mode must be managed_mta or existing_smtp")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, unavailable("installation_wizard", err)
	}
	defer tx.Rollback(ctx)
	var lv int64
	if err := tx.QueryRow(ctx, `SELECT lock_version FROM installation_settings WHERE id=1 FOR UPDATE`).Scan(&lv); err != nil {
		return nil, unavailable("installation_wizard", err)
	}
	var activeMode string
	_ = tx.QueryRow(ctx, `SELECT COALESCE((SELECT mode FROM transport_profiles WHERE is_active),'')`).Scan(&activeMode)
	if activeMode != "" && activeMode != mode {
		return nil, &Error{Status: 409, Code: "mode_switch_requires_plan",
			Message: "mail is already going out through the other delivery mode. Create a profile for the new mode, verify it, then use the switch planner."}
	}
	if mode == installation.ModeManaged && activeMode == "" {
		if _, err := tx.Exec(ctx, `INSERT INTO transport_profiles(mode, label, status)
SELECT 'managed_mta','Send from this server','draft'
WHERE NOT EXISTS (SELECT 1 FROM transport_profiles WHERE mode='managed_mta' AND status <> 'retired')`); err != nil {
			return nil, unavailable("installation_wizard", err)
		}
	}
	now := time.Now().UTC()
	wj, _ := json.Marshal(WizardState{SelectedMode: mode, ConfirmedAt: &now, ConfirmedBy: a.Name})
	if _, err := tx.Exec(ctx, `UPDATE installation_settings SET wizard=$1::jsonb, lock_version=lock_version+1, updated_at=now() WHERE id=1`, wj); err != nil {
		return nil, unavailable("installation_wizard", err)
	}
	if err := audit(ctx, tx, a, "installation.mode_selected", "installation", "1", map[string]any{"mode": mode, "active_mode": activeMode}); err != nil {
		return nil, unavailable("audit", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, unavailable("installation_wizard", err)
	}
	return s.InstallationState(ctx)
}

// InstallationState is installation readiness, the wizard state and the
// profile it is computed for. Project onboarding is a separate scope and
// never feeds into this.
func (s *Service) InstallationState(ctx context.Context) (map[string]any, error) {
	w, err := s.wizard(ctx, s.Pool)
	if err != nil {
		return nil, unavailable("installation_state", err)
	}
	var id, source string
	err = s.Pool.QueryRow(ctx, `SELECT id::text, source FROM transport_profiles WHERE is_active`).Scan(&id, &source)
	if err != nil && !errors.Is(err, db.ErrNoRows) {
		return nil, unavailable("installation_state", err)
	}
	out := map[string]any{"wizard": w, "active_profile": nil, "profile": nil}
	var rd installation.Readiness
	profileID := id
	switch {
	case id != "":
		// The active profile always decides.
	case w.SelectedMode == installation.ModeManaged:
		// Not active yet: show the draft the installer will complete.
		_ = s.Pool.QueryRow(ctx, `SELECT COALESCE((SELECT id::text FROM transport_profiles WHERE mode='managed_mta' AND status <> 'retired' ORDER BY created_at LIMIT 1),'')`).Scan(&profileID)
	case w.SelectedMode == installation.ModeExisting:
		// The Mail server page creates the profile on first save.
		_ = s.Pool.QueryRow(ctx, `SELECT COALESCE((SELECT id::text FROM transport_profiles WHERE mode='existing_smtp' AND status <> 'retired' ORDER BY created_at LIMIT 1),'')`).Scan(&profileID)
	}
	if profileID != "" {
		if rd, err = s.profileReadiness(ctx, profileID); err != nil {
			return nil, unavailable("installation_state", err)
		}
		p, err := s.TransportProfile(ctx, profileID)
		if err != nil {
			return nil, err
		}
		out["profile"] = p
		if id != "" {
			out["active_profile"] = p
		}
	} else {
		rd = installation.Compute(installation.Facts{Now: time.Now(), Mode: w.SelectedMode})
	}
	out["readiness"] = rd

	var everPassed bool
	if id != "" {
		_ = s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM installation_checks WHERE profile_id=$1::uuid AND result='pass')`, id).Scan(&everPassed)
	}
	switch {
	case id != "" && w.ConfirmedAt == nil && (source == "legacy_mail_server" || source == "environment"):
		out["welcome_state"] = WelcomeLegacyDetected
	case id == "" && w.SelectedMode == "":
		out["welcome_state"] = WelcomeReadyToConfigure
	case id != "" && rd.Status == installation.Ready:
		out["welcome_state"] = WelcomeCompleted
	case everPassed:
		out["welcome_state"] = WelcomeDegraded
	default:
		out["welcome_state"] = WelcomeInProgress
	}
	return out, nil
}

// InstallationSummary is the safe subset of InstallationState shown to every
// workspace: no profile configuration, only status and progress.
func (s *Service) InstallationSummary(ctx context.Context) (map[string]any, error) {
	st, err := s.InstallationState(ctx)
	if err != nil {
		return nil, err
	}
	rd := st["readiness"].(installation.Readiness)
	return map[string]any{"scope": "installation", "status": rd.Status, "mode": rd.Mode, "progress": rd.Progress,
		"welcome_state": st["welcome_state"]}, nil
}

// QueueTransportTest sends one controlled Live message through the normal
// accept pipeline, queue and worker (backlog C04). Every Live rule applies:
// approved sender, domain gate, suppressions, quotas. When the worker finishes
// it, the outcome is recorded as the test_send check of the profile revision
// that sent it, so a pass proves the real path, not just an SMTP handshake.
// from defaults to the environment's default (else first) sender.
func (s *Service) QueueTransportTest(ctx context.Context, sc *Scope, user, from, to string) (*Accepted, error) {
	if sc.Mode != "live" {
		return nil, &Error{Status: 409, Code: "live_only", Message: "a transport test uses the Live environment; Sandbox never reaches a mail server"}
	}
	if from == "" {
		if err := s.Pool.QueryRow(ctx, `SELECT email FROM sender_identities WHERE environment_id=$1 ORDER BY is_default DESC, created_at LIMIT 1`, sc.EnvID).Scan(&from); err != nil {
			return nil, &Error{Status: 409, Code: "no_default_sender", Message: "add a verified Live sender to this project first"}
		}
	}
	in := ComposeInput{From: from, To: to, Subject: "Njiwa-Mail delivery test",
		Text: "This is a controlled test from Njiwa-Mail. It went through the normal queue and worker.\n\n" +
			"If it arrived, check the headers: SPF, DKIM and DMARC should pass, with DKIM signed by your sending domain.",
		transport: true}
	return s.Compose(ctx, sc, user, "transport-test-"+secure.NewUUID(), in)
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/installation"
	"github.com/njiwa-tz/mail/internal/mailer"
)

// Transport profiles (backlog A05-A08). A profile is one way of getting mail
// out; exactly one is active. Checks are bound to a profile and to its config
// revision, so a pass for another profile or an older revision never counts.
//
// Secrets keep the legacy AAD so a profile migrated byte for byte from the
// mail_server row opens unchanged, and so an older binary can still read what
// the dual write keeps in mail_server.

const (
	smtpPasswordAAD   = "mail_server:smtp_password"
	mailcowAPIKeyAAD  = "mail_server:mailcow_api_key"
	transportCheckTTL = installation.DefaultCheckTTL
)

// smtpProfileConfig is the non-secret configuration stored in config_json.
func smtpProfileConfig(in MailServerInput) map[string]any {
	return map[string]any{"host": in.SMTPHost, "port": in.SMTPPort, "tls_mode": in.TLSMode, "username": strings.TrimSpace(in.Username),
		"test_sender": in.TestSender, "inbound_listener": in.InboundListener, "mailcow_api_url": in.MailcowAPIURL}
}

// syncLegacyProfile mirrors a Mail server page save into the registry. It
// updates the existing_smtp profile (preferring the active one) and bumps its
// revision only when the configuration or a secret really changed, so
// re-saving identical settings does not invalidate a good check.
func syncLegacyProfile(ctx context.Context, tx *db.Tx, cfg map[string]any, pw, key []byte, clearKey bool, provider *string, hasMailcowURL bool) error {
	cj, _ := json.Marshal(cfg)
	newProvider := "generic"
	if hasMailcowURL {
		newProvider = "mailcow"
	}
	if provider != nil {
		newProvider = *provider
	}
	var id string
	err := tx.QueryRow(ctx, `SELECT id::text FROM transport_profiles WHERE mode='existing_smtp' AND status <> 'retired' ORDER BY is_active DESC, created_at LIMIT 1 FOR UPDATE`).Scan(&id)
	if errors.Is(err, db.ErrNoRows) {
		if cfg["host"] == "" {
			return nil
		}
		var anyActive bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM transport_profiles WHERE is_active)`).Scan(&anyActive); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO transport_profiles(mode, provider_type, label, config_json, encrypted_secret, encrypted_provider_secret, status, is_active, source)
VALUES ('existing_smtp',$1,'Existing SMTP',$2::jsonb,$3,$4,'needs_action',$5,'operator')`, newProvider, cj, pw, key, !anyActive)
		return err
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE transport_profiles SET
  config_revision = config_revision + CASE WHEN config_json IS DISTINCT FROM $2::jsonb OR $3::bool OR ($6::bool AND encrypted_provider_secret IS NOT NULL) THEN 1 ELSE 0 END,
  status = CASE WHEN config_json IS DISTINCT FROM $2::jsonb OR $3::bool OR ($6::bool AND encrypted_provider_secret IS NOT NULL) THEN 'needs_action' ELSE status END,
  config_json = $2::jsonb,
  provider_type = COALESCE($4::text, CASE WHEN $8::bool THEN 'mailcow' ELSE provider_type END),
  encrypted_secret = COALESCE($5, encrypted_secret),
  encrypted_provider_secret = CASE WHEN $6::bool THEN NULL ELSE COALESCE($7, encrypted_provider_secret) END,
  updated_at = now()
WHERE id = $1::uuid`, id, cj, pw != nil || key != nil, provider, pw, clearKey, key, hasMailcowURL)
	return err
}

// AdoptEnvSMTP records an environment-provided SMTP configuration as a profile
// when the installation has none, so it shows up in the registry and the
// wizard. The environment still wins at runtime (unchanged precedence); this
// only makes the setting visible and migratable. It never overwrites a profile.
func (s *Service) AdoptEnvSMTP(ctx context.Context, cfg mailer.SMTPConfig, sender string) error {
	if cfg.Host == "" {
		return nil
	}
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM transport_profiles`).Scan(&n); err != nil || n > 0 {
		return err
	}
	mode := "starttls"
	if cfg.Implicit {
		mode = "implicit"
	}
	var pw []byte
	if cfg.Password != "" {
		var err error
		if pw, err = s.Box.Seal([]byte(cfg.Password), []byte(smtpPasswordAAD)); err != nil {
			return err
		}
	}
	cj, _ := json.Marshal(map[string]any{"host": cfg.Host, "port": cfg.Port, "tls_mode": mode, "username": cfg.Username,
		"test_sender": sender, "inbound_listener": "", "mailcow_api_url": ""})
	_, err := s.Pool.Exec(ctx, `INSERT INTO transport_profiles(mode, label, config_json, encrypted_secret, status, is_active, source)
SELECT 'existing_smtp','Existing SMTP (from environment)',$1::jsonb,$2,'needs_action',true,'environment'
WHERE NOT EXISTS (SELECT 1 FROM transport_profiles)`, cj, pw)
	return err
}

// ---------------------------------------------------------------- checks

func problemCodeFor(kind string, detail any) string {
	switch d := detail.(type) {
	case []mailer.Step:
		for _, st := range d {
			if !st.OK && !st.Skipped {
				if st.Code != "" {
					return st.Code
				}
				return "SMTP_" + strings.ToUpper(st.Key) + "_FAILED"
			}
		}
	case map[string]any:
		if r, _ := d["reason"].(string); r != "" {
			return "SMTP_" + strings.TrimPrefix(strings.ToUpper(r), "SMTP_")
		}
		return "SMTP_SEND_FAILED"
	}
	return installation.CodeCheckFailed
}

// RecordInstallationCheck stores one check result for a profile at a config
// revision. safe may only hold values that are safe to show an operator.
func (s *Service) RecordInstallationCheck(ctx context.Context, profileID string, rev int64, key string, ok bool, problem string, safe map[string]any, external bool) error {
	result := "pass"
	if !ok {
		result = "fail"
	} else {
		problem = ""
	}
	if safe == nil {
		safe = map[string]any{}
	}
	if external {
		safe["external"] = true
	}
	sj, _ := json.Marshal(safe)
	if _, err := s.Pool.Exec(ctx, `INSERT INTO installation_checks(profile_id, config_revision, check_key, result, problem_code, safe_details, expires_at)
VALUES ($1::uuid,$2,$3,$4,$5,$6::jsonb, now() + make_interval(secs => $7::float8))`, profileID, rev, key, result, problem, sj, transportCheckTTL.Seconds()); err != nil {
		return err
	}
	// Refresh the stored summary from the derived readiness of that profile.
	st := "needs_action"
	if rd, err := s.profileReadiness(ctx, profileID); err == nil && rd.Status == installation.Ready {
		st = "ready"
	}
	_, err := s.Pool.Exec(ctx, `UPDATE transport_profiles SET last_tested_at=now(), status=$2, updated_at=updated_at
WHERE id=$1::uuid AND config_revision=$3 AND status <> 'retired'`, profileID, st, rev)
	return err
}

func (s *Service) profileReadiness(ctx context.Context, id string) (installation.Readiness, error) {
	var mode string
	var rev int64
	var cj []byte
	var hasSecret bool
	err := s.Pool.QueryRow(ctx, `SELECT mode, config_revision, config_json, encrypted_secret IS NOT NULL FROM transport_profiles WHERE id=$1::uuid`, id).Scan(&mode, &rev, &cj, &hasSecret)
	if err != nil {
		return installation.Readiness{}, err
	}
	f := installation.Facts{Now: time.Now(), Mode: mode, ProfileRevision: rev, Checks: map[string]installation.Check{}}
	var c struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
	}
	_ = json.Unmarshal(cj, &c)
	f.ConfigComplete = c.Host != "" && c.Port > 0 && c.Username != "" && hasSecret
	rows, err := s.Pool.Query(ctx, `SELECT DISTINCT ON (check_key) check_key, result, config_revision, checked_at, expires_at, problem_code, COALESCE((safe_details->>'external')::bool, false)
FROM installation_checks WHERE profile_id=$1::uuid ORDER BY check_key, checked_at DESC`, id)
	if err != nil {
		return installation.Readiness{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var ck installation.Check
		if err := rows.Scan(&key, &ck.Result, &ck.Revision, &ck.CheckedAt, &ck.ExpiresAt, &ck.ProblemCode, &ck.External); err != nil {
			return installation.Readiness{}, err
		}
		f.Checks[key] = ck
	}
	if err := rows.Err(); err != nil {
		return installation.Readiness{}, err
	}
	_ = s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM installer_receipts WHERE profile_id=$1::uuid AND result_code='ok' AND verified_at IS NOT NULL AND rollback_state <> 'rolled_back')`, id).Scan(&f.ManagedInstalled)
	var active bool
	_ = s.Pool.QueryRow(ctx, `SELECT is_active FROM transport_profiles WHERE id=$1::uuid`, id).Scan(&active)
	if active {
		f.Delivery = s.deliveryHealth(ctx)
	}
	return installation.Compute(f), nil
}

// deliveryHealth looks at the last 15 minutes of Live send attempts.
func (s *Service) deliveryHealth(ctx context.Context) *installation.DeliveryHealth {
	d := &installation.DeliveryHealth{}
	var code *string
	err := s.Pool.QueryRow(ctx, `SELECT
  count(*) FILTER (WHERE ev.event_type='retry_scheduled' AND ev.reason_code = ANY($1)),
  max(ev.created_at) FILTER (WHERE ev.event_type='retry_scheduled' AND ev.reason_code = ANY($1)),
  (array_agg(ev.reason_code ORDER BY ev.created_at DESC) FILTER (WHERE ev.event_type='retry_scheduled' AND ev.reason_code = ANY($1)))[1],
  max(ev.created_at) FILTER (WHERE ev.event_type='submitted')
FROM message_events ev JOIN messages m ON m.id=ev.message_id JOIN environments e ON e.id=m.environment_id
WHERE e.mode='live' AND ev.created_at > now() - interval '15 minutes'`, transportDownReasons).Scan(&d.TransportFailures, &d.LastFailure, &code, &d.LastSubmitted)
	if err != nil {
		return nil
	}
	if code != nil {
		d.LastCode = *code
	}
	return d
}

// ---------------------------------------------------------------- registry

const profileCols = `id::text AS id, mode, provider_type, label, config_revision, config_json AS config, encrypted_secret IS NOT NULL AS secret_set,
  encrypted_provider_secret IS NOT NULL AS provider_secret_set, status, is_active, source, last_tested_at, created_at, updated_at`

func (s *Service) ListTransportProfiles(ctx context.Context) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT `+profileCols+` FROM transport_profiles WHERE status <> 'retired' ORDER BY is_active DESC, created_at`)
}

func (s *Service) TransportProfile(ctx context.Context, id string) (map[string]any, error) {
	if !isUUID(id) {
		return nil, newErr(404, "not_found")
	}
	return s.one(ctx, `SELECT `+profileCols+` FROM transport_profiles WHERE id=$1::uuid`, id)
}

type TransportProfileInput struct {
	Mode     string `json:"mode"`
	Label    string `json:"label"`
	Provider string `json:"provider_type"`
	SMTP     *struct {
		Host     string  `json:"host"`
		Port     int     `json:"port"`
		TLSMode  string  `json:"tls_mode"`
		Username string  `json:"username"`
		Password *string `json:"password"`
		Sender   string  `json:"test_sender"`
	} `json:"smtp"`
}

// CreateTransportProfile adds an inactive profile. It never activates it:
// activation goes through ActivateTransport so queue safety is enforced.
func (s *Service) CreateTransportProfile(ctx context.Context, a Actor, in TransportProfileInput) (map[string]any, error) {
	if in.Mode != "managed_mta" && in.Mode != "existing_smtp" {
		return nil, badRequest("mode must be managed_mta or existing_smtp")
	}
	in.Label = strings.TrimSpace(in.Label)
	if in.Label == "" || len(in.Label) > 80 {
		return nil, badRequest("give the profile a short label")
	}
	if in.Provider == "" {
		in.Provider = "generic"
	}
	if in.Provider != "generic" && in.Provider != "mailcow" {
		return nil, badRequest("provider_type must be generic or mailcow")
	}
	cfg := map[string]any{}
	var pw []byte
	switch in.Mode {
	case "existing_smtp":
		if in.SMTP == nil {
			return nil, badRequest("smtp settings are required for existing_smtp")
		}
		v := MailServerInput{SMTPHost: in.SMTP.Host, SMTPPort: in.SMTP.Port, TLSMode: in.SMTP.TLSMode, Username: in.SMTP.Username, TestSender: in.SMTP.Sender}
		if err := validateSMTPInput(&v); err != nil {
			return nil, err
		}
		if v.SMTPHost == "" {
			return nil, badRequest("SMTP host is required")
		}
		cfg = smtpProfileConfig(v)
		if in.SMTP.Password != nil && *in.SMTP.Password != "" {
			var err error
			if pw, err = s.Box.Seal([]byte(*in.SMTP.Password), []byte(smtpPasswordAAD)); err != nil {
				return nil, unavailable("seal", err)
			}
		}
	case "managed_mta":
		in.Provider = "generic"
	}
	cj, _ := json.Marshal(cfg)
	var id string
	err := s.Pool.QueryRow(ctx, `INSERT INTO transport_profiles(mode, provider_type, label, config_json, encrypted_secret, status)
VALUES ($1,$2,$3,$4::jsonb,$5,'draft') RETURNING id::text`, in.Mode, in.Provider, in.Label, cj, pw).Scan(&id)
	if err != nil {
		return nil, unavailable("transport_profile_create", err)
	}
	_ = s.Audit(ctx, a, "transport.profile_create", "transport_profile", id, map[string]any{"mode": in.Mode, "provider": in.Provider})
	return s.TransportProfile(ctx, id)
}

// ProfileSMTP returns the decrypted SMTP settings of any existing_smtp profile
// together with its revision, for diagnostics bound to that exact revision.
func (s *Service) ProfileSMTP(ctx context.Context, id string) (cfg mailer.SMTPConfig, sender string, rev int64, err error) {
	if !isUUID(id) {
		return cfg, "", 0, newErr(404, "not_found")
	}
	var mode string
	var cj, pwEnc []byte
	var updated time.Time
	if err = s.Pool.QueryRow(ctx, `SELECT mode, config_revision, config_json, encrypted_secret, updated_at FROM transport_profiles WHERE id=$1::uuid AND status <> 'retired'`, id).
		Scan(&mode, &rev, &cj, &pwEnc, &updated); err != nil {
		return cfg, "", 0, notFound(err)
	}
	if mode == "managed_mta" {
		cfg, ok, err := s.openManagedProfile(cj, pwEnc)
		if err != nil {
			return cfg, "", 0, unavailable("open_profile", err)
		}
		if !ok {
			return cfg, "", 0, &Error{Status: 409, Code: "managed_not_installed", Message: "run the host installer first: njiwa-mail installer apply"}
		}
		return cfg, "", rev, nil
	}
	cfg, sender, _, ok, err := s.openSMTPProfile(cj, pwEnc, updated)
	if err != nil {
		return cfg, "", 0, unavailable("open_profile", err)
	}
	if !ok {
		return cfg, "", 0, &Error{Status: 409, Code: "mail_server_not_configured", Message: "save the SMTP host, port, username and password first"}
	}
	return cfg, sender, rev, nil
}

// ---------------------------------------------------------------- switch planning (A08)

// SwitchPlan describes what changing the active profile would touch. Jobs that
// already left Njiwa with an ambiguous outcome (unknown) are never moved or
// replayed by a switch; they must be reconciled first.
type SwitchPlan struct {
	TargetID        string   `json:"target_id"`
	TargetMode      string   `json:"target_mode"`
	CurrentID       string   `json:"current_id,omitempty"`
	AlreadyActive   bool     `json:"already_active"`
	Queued          int      `json:"queued"`
	Scheduled       int      `json:"scheduled"`
	Submitting      int      `json:"submitting"`
	RetryWait       int      `json:"retry_wait"`
	Unknown         int      `json:"unknown"`
	Verified        bool     `json:"verified"`
	Blockers        []string `json:"blockers"`
	NeedsDecision   bool     `json:"needs_decision"` // pending jobs exist: operator must acknowledge
	RollbackProfile string   `json:"rollback_profile_id,omitempty"`
}

type queryer interface {
	QueryRow(ctx context.Context, q string, args ...any) db.Row
}

func (s *Service) planSwitch(ctx context.Context, q queryer, targetID string) (SwitchPlan, error) {
	p := SwitchPlan{TargetID: targetID, Blockers: []string{}}
	var rev int64
	var cj []byte
	var hasSecret bool
	var status string
	if err := q.QueryRow(ctx, `SELECT mode, config_revision, config_json, encrypted_secret IS NOT NULL, status, is_active FROM transport_profiles WHERE id=$1::uuid`, targetID).
		Scan(&p.TargetMode, &rev, &cj, &hasSecret, &status, &p.AlreadyActive); err != nil {
		return p, notFound(err)
	}
	if status == "retired" {
		return p, newErr(404, "not_found")
	}
	_ = q.QueryRow(ctx, `SELECT COALESCE((SELECT id::text FROM transport_profiles WHERE is_active),'')`).Scan(&p.CurrentID)
	_ = q.QueryRow(ctx, `SELECT COALESCE(previous_profile_id::text,'') FROM installation_settings WHERE id=1`).Scan(&p.RollbackProfile)
	// Only live mail uses a transport; sandbox messages are captured locally.
	if err := q.QueryRow(ctx, `SELECT
 count(*) FILTER (WHERE m.state='queued'), count(*) FILTER (WHERE m.state='scheduled'), count(*) FILTER (WHERE m.state='submitting'),
 count(*) FILTER (WHERE m.state='retry_wait'), count(*) FILTER (WHERE m.state='unknown')
FROM messages m JOIN environments e ON e.id=m.environment_id
WHERE e.mode='live' AND m.state IN ('queued','scheduled','submitting','retry_wait','unknown')`).Scan(&p.Queued, &p.Scheduled, &p.Submitting, &p.RetryWait, &p.Unknown); err != nil {
		return p, unavailable("switch_plan", err)
	}
	// The target must have a fresh passing connection check for its current revision.
	_ = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM installation_checks WHERE profile_id=$1::uuid AND check_key='connection' AND result='pass'
  AND config_revision=$2 AND (expires_at IS NULL OR expires_at > now()))`, targetID, rev).Scan(&p.Verified)
	if p.AlreadyActive {
		return p, nil
	}
	switch p.TargetMode {
	case "managed_mta":
		var installed bool
		_ = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM installer_receipts WHERE profile_id=$1::uuid AND result_code='ok' AND verified_at IS NOT NULL AND rollback_state <> 'rolled_back')`, targetID).Scan(&installed)
		if !installed {
			p.Blockers = append(p.Blockers, "managed_not_installed")
		}
	case "existing_smtp":
		var c struct {
			Host string `json:"host"`
		}
		_ = json.Unmarshal(cj, &c)
		if c.Host == "" || !hasSecret {
			p.Blockers = append(p.Blockers, "profile_incomplete")
		}
	}
	if !p.Verified {
		p.Blockers = append(p.Blockers, "profile_not_verified")
	}
	if p.Unknown > 0 {
		p.Blockers = append(p.Blockers, "unknown_jobs_require_reconciliation")
	}
	p.NeedsDecision = p.Queued+p.Scheduled+p.Submitting+p.RetryWait > 0
	return p, nil
}

func (s *Service) PlanTransportSwitch(ctx context.Context, targetID string) (SwitchPlan, error) {
	if !isUUID(targetID) {
		return SwitchPlan{}, newErr(404, "not_found")
	}
	return s.planSwitch(ctx, s.Pool, targetID)
}

// ActivateTransport makes target the one active profile, atomically and with
// an audit record. It refuses when the plan has blockers, and when pending
// jobs exist unless the operator explicitly acknowledged migrating them.
// Rolling back is the same call aimed at the previous profile, which is kept.
func (s *Service) ActivateTransport(ctx context.Context, a Actor, targetID string, migratePending bool) (SwitchPlan, error) {
	if !isUUID(targetID) {
		return SwitchPlan{}, newErr(404, "not_found")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return SwitchPlan{}, unavailable("transport_activate", err)
	}
	defer tx.Rollback(ctx)
	// Serialise switches; the plan is recomputed under the lock.
	var lv int64
	if err := tx.QueryRow(ctx, `SELECT lock_version FROM installation_settings WHERE id=1 FOR UPDATE`).Scan(&lv); err != nil {
		return SwitchPlan{}, unavailable("transport_activate", err)
	}
	plan, err := s.planSwitch(ctx, tx, targetID)
	if err != nil {
		return plan, err
	}
	if plan.AlreadyActive {
		return plan, nil
	}
	if len(plan.Blockers) > 0 {
		return plan, &Error{Status: 409, Code: plan.Blockers[0], Message: "this switch is blocked: " + strings.Join(plan.Blockers, ", ")}
	}
	if plan.NeedsDecision && !migratePending {
		return plan, &Error{Status: 409, Code: "pending_jobs_require_decision",
			Message: fmt.Sprintf("%d queued, %d scheduled, %d sending and %d waiting to retry. Let the queue drain, or confirm that these messages may be sent through the new profile.",
				plan.Queued, plan.Scheduled, plan.Submitting, plan.RetryWait)}
	}
	if _, err := tx.Exec(ctx, `UPDATE transport_profiles SET is_active=false, updated_at=now() WHERE is_active`); err != nil {
		return plan, unavailable("transport_activate", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE transport_profiles SET is_active=true, updated_at=now() WHERE id=$1::uuid`, targetID); err != nil {
		return plan, unavailable("transport_activate", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE installation_settings SET previous_profile_id=NULLIF($1,'')::uuid, lock_version=lock_version+1, updated_at=now() WHERE id=1`, plan.CurrentID); err != nil {
		return plan, unavailable("transport_activate", err)
	}
	if err := mirrorProfileToLegacy(ctx, tx, targetID); err != nil {
		return plan, unavailable("transport_activate", err)
	}
	if err := audit(ctx, tx, a, "transport.activate", "transport_profile", targetID, map[string]any{
		"from": plan.CurrentID, "to": targetID, "mode": plan.TargetMode, "queued": plan.Queued, "scheduled": plan.Scheduled, "submitting": plan.Submitting,
		"retry_wait": plan.RetryWait, "migrate_pending": migratePending}); err != nil {
		return plan, unavailable("audit", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return plan, unavailable("transport_activate", err)
	}
	plan.AlreadyActive = true
	return plan, nil
}

// mirrorProfileToLegacy copies an existing_smtp profile into the mail_server
// singleton so the Mail server page (and an older binary after a rollback)
// shows and uses the same settings as the active profile. The inbound
// listener is installation-level: it is kept when the profile has none.
// last_test is cleared because it described the previous settings.
func mirrorProfileToLegacy(ctx context.Context, tx *db.Tx, id string) error {
	_, err := tx.Exec(ctx, `INSERT INTO mail_server(id, smtp_host, smtp_port, smtp_tls_mode, smtp_username, smtp_password_enc, test_sender, inbound_listener, mailcow_api_url, mailcow_api_key_enc)
SELECT 1, COALESCE(config_json->>'host',''), COALESCE((config_json->>'port')::int, 587),
  CASE WHEN config_json->>'tls_mode'='implicit' THEN 'implicit' ELSE 'starttls' END,
  COALESCE(config_json->>'username',''), encrypted_secret, COALESCE(config_json->>'test_sender',''),
  COALESCE(config_json->>'inbound_listener',''), COALESCE(config_json->>'mailcow_api_url',''), encrypted_provider_secret
FROM transport_profiles WHERE id=$1::uuid AND mode='existing_smtp'
ON CONFLICT (id) DO UPDATE SET smtp_host=EXCLUDED.smtp_host, smtp_port=EXCLUDED.smtp_port, smtp_tls_mode=EXCLUDED.smtp_tls_mode,
  smtp_username=EXCLUDED.smtp_username, smtp_password_enc=EXCLUDED.smtp_password_enc, test_sender=EXCLUDED.test_sender,
  inbound_listener=CASE WHEN EXCLUDED.inbound_listener='' THEN mail_server.inbound_listener ELSE EXCLUDED.inbound_listener END,
  mailcow_api_url=EXCLUDED.mailcow_api_url, mailcow_api_key_enc=EXCLUDED.mailcow_api_key_enc,
  last_test=NULL, last_test_at=NULL, updated_at=now()`, id)
	return err
}

// UpdateTransportProfile edits an existing_smtp profile's label and SMTP
// settings. A blank password keeps the stored one. The revision is bumped only
// when settings or the secret really change, which makes earlier checks stale.
func (s *Service) UpdateTransportProfile(ctx context.Context, a Actor, id string, in TransportProfileInput) (map[string]any, error) {
	if !isUUID(id) {
		return nil, newErr(404, "not_found")
	}
	in.Label = strings.TrimSpace(in.Label)
	if in.Label == "" || len(in.Label) > 80 {
		return nil, badRequest("give the profile a short label")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, unavailable("transport_profile_update", err)
	}
	defer tx.Rollback(ctx)
	var mode string
	var active bool
	if err := tx.QueryRow(ctx, `SELECT mode, is_active FROM transport_profiles WHERE id=$1::uuid AND status <> 'retired' FOR UPDATE`, id).Scan(&mode, &active); err != nil {
		return nil, notFound(err)
	}
	if in.Mode != "" && in.Mode != mode {
		return nil, badRequest("a profile's mode cannot be changed; create a new profile")
	}
	patch := map[string]any{}
	var pw []byte
	if mode == "existing_smtp" {
		if in.SMTP == nil {
			return nil, badRequest("smtp settings are required for existing_smtp")
		}
		v := MailServerInput{SMTPHost: in.SMTP.Host, SMTPPort: in.SMTP.Port, TLSMode: in.SMTP.TLSMode, Username: in.SMTP.Username, TestSender: in.SMTP.Sender}
		if err := validateSMTPInput(&v); err != nil {
			return nil, err
		}
		if v.SMTPHost == "" {
			return nil, badRequest("SMTP host is required")
		}
		patch = map[string]any{"host": v.SMTPHost, "port": v.SMTPPort, "tls_mode": v.TLSMode, "username": strings.TrimSpace(v.Username), "test_sender": v.TestSender}
		if in.SMTP.Password != nil && *in.SMTP.Password != "" {
			if pw, err = s.Box.Seal([]byte(*in.SMTP.Password), []byte(smtpPasswordAAD)); err != nil {
				return nil, unavailable("seal", err)
			}
		}
	}
	pj, _ := json.Marshal(patch)
	var rev int64
	var changed bool
	if err := tx.QueryRow(ctx, `UPDATE transport_profiles p SET
  config_revision = p.config_revision + CASE WHEN (p.config_json || $2::jsonb) IS DISTINCT FROM p.config_json OR $3::bytea IS NOT NULL THEN 1 ELSE 0 END,
  status = CASE WHEN (p.config_json || $2::jsonb) IS DISTINCT FROM p.config_json OR $3::bytea IS NOT NULL THEN 'needs_action' ELSE p.status END,
  config_json = p.config_json || $2::jsonb,
  encrypted_secret = COALESCE($3::bytea, p.encrypted_secret),
  label = $4, updated_at = now()
FROM transport_profiles old WHERE p.id=$1::uuid AND old.id=p.id
RETURNING p.config_revision, p.config_revision <> old.config_revision`, id, pj, pw, in.Label).Scan(&rev, &changed); err != nil {
		return nil, unavailable("transport_profile_update", err)
	}
	if active && changed {
		if err := mirrorProfileToLegacy(ctx, tx, id); err != nil {
			return nil, unavailable("transport_profile_update", err)
		}
	}
	if err := audit(ctx, tx, a, "transport.profile_update", "transport_profile", id, map[string]any{"revision": rev, "config_changed": changed, "password_changed": pw != nil}); err != nil {
		return nil, unavailable("audit", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, unavailable("transport_profile_update", err)
	}
	return s.TransportProfile(ctx, id)
}

// RetireTransportProfile hides an inactive profile. The row is kept because
// messages and audit records refer to it; the active profile cannot be retired.
func (s *Service) RetireTransportProfile(ctx context.Context, a Actor, id string) error {
	if !isUUID(id) {
		return newErr(404, "not_found")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("transport_profile_retire", err)
	}
	defer tx.Rollback(ctx)
	var active bool
	if err := tx.QueryRow(ctx, `SELECT is_active FROM transport_profiles WHERE id=$1::uuid AND status <> 'retired' FOR UPDATE`, id).Scan(&active); err != nil {
		return notFound(err)
	}
	if active {
		return &Error{Status: 409, Code: "profile_active", Message: "activate another profile before retiring this one"}
	}
	if _, err := tx.Exec(ctx, `UPDATE transport_profiles SET status='retired', encrypted_secret=NULL, encrypted_provider_secret=NULL, updated_at=now() WHERE id=$1::uuid`, id); err != nil {
		return unavailable("transport_profile_retire", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE installation_settings SET previous_profile_id=NULL WHERE previous_profile_id=$1::uuid`, id); err != nil {
		return unavailable("transport_profile_retire", err)
	}
	if err := audit(ctx, tx, a, "transport.profile_retire", "transport_profile", id, nil); err != nil {
		return unavailable("audit", err)
	}
	return tx.Commit(ctx)
}

func isUUID(v string) bool {
	if len(v) != 36 {
		return false
	}
	for i, r := range v {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if r != '-' {
				return false
			}
		case !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F'):
			return false
		}
	}
	return true
}

// ProblemCode exposes the stable problem code for a diagnosis result.
func ProblemCode(kind string, detail any) string { return problemCodeFor(kind, detail) }

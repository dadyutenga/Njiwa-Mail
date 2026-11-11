package test

import (
	"context"
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/installation"
	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/service"
)

var okSteps = []mailer.Step{{Key: "dns", OK: true}, {Key: "tcp", OK: true}, {Key: "tls", OK: true}, {Key: "login", OK: true}, {Key: "sender", OK: true}}

func (e *env) saveSMTP(host, password string) {
	e.t.Helper()
	in := service.MailServerInput{SMTPHost: host, SMTPPort: 587, TLSMode: "starttls", Username: "njiwa@example.com", TestSender: "hello@notify.example.com"}
	if password != "" {
		in.Password = &password
	}
	e.must(e.svc.SaveMailServer(context.Background(), in))
}

func (e *env) activeProfile() map[string]any {
	e.t.Helper()
	l, err := e.svc.ListTransportProfiles(context.Background())
	e.must(err)
	for _, p := range l {
		if p["is_active"] == true {
			return p
		}
	}
	return nil
}

func (e *env) readiness() installation.Readiness {
	e.t.Helper()
	st, err := e.svc.InstallationState(context.Background())
	e.must(err)
	return st["readiness"].(installation.Readiness)
}

// A06: a working legacy singleton becomes the active profile; secrets survive;
// the old settings keep working; readiness is NOT inherited from the old test.
func TestLegacyMailServerMigratesToActiveProfile(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	sealed, err := e.box.Seal([]byte("legacy-pass"), []byte("mail_server:smtp_password"))
	e.must(err)
	// Simulate an installation that predates migration 0010.
	for _, q := range []string{
		`DROP TABLE installer_receipts, installation_checks, installation_settings, transport_profiles CASCADE`,
		`ALTER TABLE messages DROP COLUMN transport_profile_id, DROP COLUMN transport_revision`,
		`DELETE FROM schema_migrations WHERE version='0010_transport_profiles.sql'`,
	} {
		_, err := e.pool.Exec(ctx, q)
		e.must(err)
	}
	_, err = e.pool.Exec(ctx, `INSERT INTO mail_server(id, smtp_host, smtp_port, smtp_tls_mode, smtp_username, smtp_password_enc, test_sender, last_test, last_test_at)
VALUES (1,'mail.example.com',465,'implicit','njiwa@example.com',$1,'hello@notify.example.com','{"ok":true}',now())`, sealed)
	e.must(err)

	e.must(db.Migrate(ctx, e.pool))

	p := e.activeProfile()
	if p == nil || p["mode"] != "existing_smtp" || p["source"] != "legacy_mail_server" || p["secret_set"] != true {
		t.Fatalf("profile: %v", p)
	}
	cfg, sender, _, ok, err := e.svc.StoredSMTP(ctx)
	if err != nil || !ok || cfg.Host != "mail.example.com" || cfg.Port != 465 || !cfg.Implicit || cfg.Password != "legacy-pass" || sender != "hello@notify.example.com" {
		t.Fatalf("stored smtp after migration: %+v %v %v", cfg, ok, err)
	}
	if n := e.count(`SELECT count(*) FROM mail_server WHERE smtp_host='mail.example.com' AND smtp_password_enc IS NOT NULL`); n != 1 {
		t.Fatal("legacy row must be left intact for rollback")
	}
	if r := e.readiness(); r.Status == installation.Ready {
		t.Fatalf("legacy last_test must not make the new profile ready: %+v", r)
	}
	// Re-running migrations is a no-op.
	e.must(db.Migrate(ctx, e.pool))
	if n := e.count(`SELECT count(*) FROM transport_profiles`); n != 1 {
		t.Fatalf("profiles after rerun: %d", n)
	}
}

func TestEmptyLegacyRowCreatesNoProfile(t *testing.T) {
	e := setup(t)
	if n := e.count(`SELECT count(*) FROM transport_profiles`); n != 0 {
		t.Fatalf("fresh install must have no profile: %d", n)
	}
	if r := e.readiness(); r.Status != installation.NotStarted || r.Progress.Required != 1 {
		t.Fatalf("%+v", r)
	}
}

// A07: revision pinning. Checks belong to a revision; changing config makes the
// old green stale, re-saving identical config does not.
func TestStatusIsTiedToProfileRevision(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.saveSMTP("mail.example.com", "pw-one")
	p := e.activeProfile()
	if p == nil || p["config_revision"].(int64) != 1 {
		t.Fatalf("%v", p)
	}
	e.must(e.svc.RecordMailTest(ctx, "connection", true, okSteps))
	e.must(e.svc.RecordMailTest(ctx, "test_send", true, map[string]any{"state": "submitted"}))
	if r := e.readiness(); r.Status != installation.Ready || r.Progress.Complete != r.Progress.Required {
		t.Fatalf("want ready: %+v", r)
	}
	// Identical save (blank password keeps the stored one): still revision 1, still ready.
	e.saveSMTP("mail.example.com", "")
	if rev := e.activeProfile()["config_revision"].(int64); rev != 1 {
		t.Fatalf("identical save bumped revision to %d", rev)
	}
	if r := e.readiness(); r.Status != installation.Ready {
		t.Fatalf("identical save must not invalidate: %+v", r)
	}
	// Password rotation changes the revision and invalidates the old checks.
	e.saveSMTP("mail.example.com", "pw-two")
	if rev := e.activeProfile()["config_revision"].(int64); rev != 2 {
		t.Fatalf("rotation revision=%d", rev)
	}
	if r := e.readiness(); r.Status != installation.Stale {
		t.Fatalf("want stale after rotation: %+v", r)
	}
	cfg, _, _, _, _ := e.svc.StoredSMTP(ctx)
	if cfg.Password != "pw-two" {
		t.Fatal("rotated password not used")
	}
	// A host change likewise.
	e.must(e.svc.RecordMailTest(ctx, "connection", true, okSteps))
	e.must(e.svc.RecordMailTest(ctx, "test_send", true, map[string]any{"state": "submitted"}))
	e.saveSMTP("other.example.com", "")
	if r := e.readiness(); r.Status != installation.Stale {
		t.Fatalf("want stale after host change: %+v", r)
	}
	cfg, _, _, _, _ = e.svc.StoredSMTP(ctx)
	if cfg.Host != "other.example.com" || cfg.Password != "pw-two" {
		t.Fatalf("password must be retained across edits: %+v", cfg)
	}
	// A failing test is reported as needs_action with a stable code.
	e.must(e.svc.RecordMailTest(ctx, "connection", false, []mailer.Step{{Key: "tls", OK: false}}))
	r := e.readiness()
	var code string
	for _, s := range r.Steps {
		if s.ID == "verify_connection" {
			code = s.ProblemCode
		}
	}
	if r.Status == installation.Ready || code != "SMTP_TLS_FAILED" {
		t.Fatalf("code=%q %+v", code, r)
	}
}

func (e *env) newProfile(host string) string {
	e.t.Helper()
	owner := e.user("op-"+host+"@njiwa.co.tz", "owner")
	r := e.adm(owner, "POST", "/admin/v1/installation/transport-profiles", map[string]any{"mode": "existing_smtp", "label": host,
		"smtp": map[string]any{"host": host, "port": 587, "tls_mode": "starttls", "username": "u", "password": "api-secret-pw-xyz"}})
	if r.Code != 200 || strings.Contains(r.Raw, "api-secret-pw-xyz") || r.JSON["secret_set"] != true || r.JSON["is_active"] != false {
		e.t.Fatalf("create profile: %d %s", r.Code, r.Raw)
	}
	return str(r.JSON, "id")
}

// A07/A08: exactly one active profile, evidence is per profile, switching is
// planned and gated, audited, and rollback keeps the previous profile.
func TestProfileActivationPlanAndGates(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.saveSMTP("mail.example.com", "pw")
	a := str(e.activeProfile(), "id")
	b := e.newProfile("second.example.com")
	actor := service.Actor{Name: "test"}

	// A pass recorded for A must not verify B.
	_, _, revA, err := e.svc.ProfileSMTP(ctx, a)
	e.must(err)
	e.must(e.svc.RecordInstallationCheck(ctx, a, revA, "connection", true, "", nil, false))
	plan, err := e.svc.PlanTransportSwitch(ctx, b)
	e.must(err)
	if plan.Verified || !contains(plan.Blockers, "profile_not_verified") {
		t.Fatalf("B verified by A's check: %+v", plan)
	}
	if _, err := e.svc.ActivateTransport(ctx, actor, b, false); errCodeOf(err) != "profile_not_verified" {
		t.Fatalf("unverified activation allowed: %v", err)
	}
	// A pass for an old revision of B does not count either.
	_, _, revB, _ := e.svc.ProfileSMTP(ctx, b)
	e.must(e.svc.RecordInstallationCheck(ctx, b, revB-1, "connection", true, "", nil, false))
	if p, _ := e.svc.PlanTransportSwitch(ctx, b); p.Verified {
		t.Fatal("stale revision accepted as verification")
	}
	e.must(e.svc.RecordInstallationCheck(ctx, b, revB, "connection", true, "", nil, false))

	// The unique index makes a second active profile impossible, even by SQL.
	if _, err := e.pool.Exec(ctx, `UPDATE transport_profiles SET is_active=true WHERE id=$1::uuid`, b); err == nil {
		t.Fatal("two active profiles allowed")
	}

	// Pending live jobs need an explicit decision.
	k := e.provision("proj")
	r := e.post(k.live, idem("pend"), otpBody(testRcpt))
	if r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	plan, err = e.svc.PlanTransportSwitch(ctx, b)
	e.must(err)
	if plan.Queued != 1 || !plan.NeedsDecision || len(plan.Blockers) != 0 {
		t.Fatalf("plan: %+v", plan)
	}
	if _, err := e.svc.ActivateTransport(ctx, actor, b, false); errCodeOf(err) != "pending_jobs_require_decision" {
		t.Fatalf("silent re-route allowed: %v", err)
	}
	if str(e.activeProfile(), "id") != a {
		t.Fatal("failed activation changed the active profile")
	}
	// Unknown outcomes always block, even with the acknowledgement.
	id := str(r.JSON, "id")
	_, err = e.pool.Exec(ctx, `UPDATE messages SET state='unknown' WHERE id=$1`, id)
	e.must(err)
	if _, err := e.svc.ActivateTransport(ctx, actor, b, true); errCodeOf(err) != "unknown_jobs_require_reconciliation" {
		t.Fatalf("unknown job allowed through a switch: %v", err)
	}
	_, err = e.pool.Exec(ctx, `UPDATE messages SET state='queued' WHERE id=$1`, id)
	e.must(err)

	// Acknowledged switch succeeds, is audited and records the rollback target.
	if _, err := e.svc.ActivateTransport(ctx, actor, b, true); err != nil {
		t.Fatal(err)
	}
	if str(e.activeProfile(), "id") != b || e.count(`SELECT count(*) FROM transport_profiles WHERE is_active`) != 1 {
		t.Fatal("activation did not move the single active profile")
	}
	if e.count(`SELECT count(*) FROM audit_log WHERE action='transport.activate' AND target_id=$1 AND (details->>'migrate_pending')::bool`, b) != 1 {
		t.Fatal("activation not audited")
	}
	if e.count(`SELECT count(*) FROM installation_settings WHERE previous_profile_id=$1::uuid`, a) != 1 {
		t.Fatal("rollback target not recorded")
	}
	// Idempotent, and the API exposes plan/activate to operators only.
	if p, err := e.svc.ActivateTransport(ctx, actor, b, false); err != nil || !p.AlreadyActive {
		t.Fatalf("%v %+v", err, p)
	}
	if r := e.adm(admin, "GET", "/admin/v1/installation/transport-profiles/"+a+"/activation-plan", nil); r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	if r := e.do("GET", "/admin/v1/installation", "", "", nil); r.Code != 401 {
		t.Fatalf("installation state readable unauthenticated: %d", r.Code)
	}
	// Managed mode cannot be activated without an installer receipt.
	owner := e.user("own@njiwa.co.tz", "owner")
	mr := e.adm(owner, "POST", "/admin/v1/installation/transport-profiles", map[string]any{"mode": "managed_mta", "label": "VPS"})
	if mr.Code != 200 {
		t.Fatalf("%d %s", mr.Code, mr.Raw)
	}
	if _, err := e.svc.ActivateTransport(ctx, actor, str(mr.JSON, "id"), true); errCodeOf(err) != "managed_not_installed" {
		t.Fatalf("managed activation without installer: %v", err)
	}
}

// Old SMTP stays functional through the worker after migration, and each
// submitted message records the profile revision that accepted it.
func TestWorkerUsesActiveProfileAndPinsMessages(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.saveSMTP("mail.example.com", "pw")
	cfg, _, _, ok, err := e.svc.StoredSMTP(ctx)
	if err != nil || !ok || cfg.Host != "mail.example.com" {
		t.Fatalf("%v %v %+v", ok, err, cfg)
	}
	k := e.provision("proj")
	r := e.post(k.live, idem("pin"), otpBody(testRcpt))
	if r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	e.runWorker()
	id := str(r.JSON, "id")
	if e.state(id) != "submitted" {
		t.Fatalf("state %s", e.state(id))
	}
	if e.count(`SELECT count(*) FROM messages m JOIN transport_profiles p ON p.id=m.transport_profile_id
 WHERE m.id=$1 AND m.transport_revision=p.config_revision`, id) != 1 {
		t.Fatal("submitted message not pinned to the active profile revision")
	}
}

func TestEnvSMTPAdoptedOnceAndNeverOverwrites(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	env := mailer.SMTPConfig{Host: "smtp.env.example", Port: 587, Username: "envuser", Password: "env-secret"}
	e.must(e.svc.AdoptEnvSMTP(ctx, env, ""))
	e.must(e.svc.AdoptEnvSMTP(ctx, mailer.SMTPConfig{Host: "other.example", Port: 25}, ""))
	p := e.activeProfile()
	if p == nil || p["source"] != "environment" || e.count(`SELECT count(*) FROM transport_profiles`) != 1 {
		t.Fatalf("%v", p)
	}
	cfg, _, _, ok, err := e.svc.StoredSMTP(ctx)
	if err != nil || !ok || cfg.Host != "smtp.env.example" || cfg.Password != "env-secret" {
		t.Fatalf("%+v %v %v", cfg, ok, err)
	}
	if n := e.count(`SELECT count(*) FROM transport_profiles WHERE encrypted_secret::text LIKE '%env-secret%'`); n != 0 {
		t.Fatal("env password stored in clear")
	}
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func errCodeOf(err error) string {
	if se, ok := err.(*service.Error); ok {
		return se.Code
	}
	return ""
}

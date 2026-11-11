package test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/installation"
	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/service"
	"github.com/njiwa-tz/mail/internal/worker"
)

var okSteps = []mailer.Step{{Key: "dns", OK: true}, {Key: "tcp", OK: true}, {Key: "tls", OK: true}, {Key: "login", OK: true}, {Key: "sender", OK: true}}

func (e *env) saveSMTP(host, password string) { e.saveSMTPPort(host, 587, password) }

// pin is the active profile revision, as a test of its settings would name it.
func (e *env) pin() service.TransportPin {
	e.t.Helper()
	a, err := e.svc.LoadActiveSMTP(context.Background())
	e.must(err)
	return a.Pin()
}

func (e *env) saveSMTPPort(host string, port int, password string) {
	e.t.Helper()
	in := service.MailServerInput{SMTPHost: host, SMTPPort: port, TLSMode: "starttls", Username: "njiwa@example.com", TestSender: "hello@notify.example.com"}
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
	e.must(e.svc.RecordMailTest(ctx, e.pin(), "connection", true, okSteps))
	e.must(e.svc.RecordMailTest(ctx, e.pin(), "test_send", true, map[string]any{"state": "submitted"}))
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
	e.must(e.svc.RecordMailTest(ctx, e.pin(), "connection", true, okSteps))
	e.must(e.svc.RecordMailTest(ctx, e.pin(), "test_send", true, map[string]any{"state": "submitted"}))
	e.saveSMTP("other.example.com", "")
	if r := e.readiness(); r.Status != installation.Stale {
		t.Fatalf("want stale after host change: %+v", r)
	}
	cfg, _, _, _, _ = e.svc.StoredSMTP(ctx)
	if cfg.Host != "other.example.com" || cfg.Password != "pw-two" {
		t.Fatalf("password must be retained across edits: %+v", cfg)
	}
	// A failing test is reported as needs_action with a stable code.
	e.must(e.svc.RecordMailTest(ctx, e.pin(), "connection", false, []mailer.Step{{Key: "tls", OK: false}}))
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
	if c := service.ProblemCode("test_send", map[string]any{"reason": "smtp_auth_failed"}); c != "SMTP_AUTH_FAILED" {
		t.Fatalf("problem code %q", c)
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
	// Sandbox mail never touches a transport, so it is not part of the plan;
	// scheduled live mail is.
	if sr := e.post(k.sbx, idem("sbx"), otpBody(testRcpt)); sr.Code != 202 {
		t.Fatalf("%d %s", sr.Code, sr.Raw)
	}
	sched := otpBody(testRcpt)
	sched["send_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	sr := e.post(k.live, idem("sched"), sched)
	if sr.Code != 202 {
		t.Fatalf("%d %s", sr.Code, sr.Raw)
	}
	plan, err = e.svc.PlanTransportSwitch(ctx, b)
	e.must(err)
	if plan.Queued != 1 || plan.Scheduled != 1 || !plan.NeedsDecision || len(plan.Blockers) != 0 {
		t.Fatalf("plan: %+v", plan)
	}
	_, err = e.pool.Exec(ctx, `DELETE FROM messages WHERE id=$1`, str(sr.JSON, "id"))
	e.must(err)
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

// Old SMTP stays functional through the worker after the migration: the stored
// transport reads the active profile, really submits over verified TLS, and
// each submitted message records the profile revision that sent it.
func TestWorkerUsesActiveProfileAndPinsMessages(t *testing.T) {
	e := setup(t)
	srv, pool, port := startSMTP(t, scenario{})
	e.saveSMTPPort("127.0.0.1", port, "pw")
	e.w.SMTP = &worker.StoredTransport{Svc: e.svc, Timeout: 3 * time.Second, RootCAs: pool}
	k := e.provision("proj")
	r := e.post(k.live, idem("pin"), otpBody(testRcpt))
	if r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	e.runWorker()
	id := str(r.JSON, "id")
	if e.state(id) != "submitted" || !srv.saw("DATA") {
		t.Fatalf("state %s, data sent=%v", e.state(id), srv.saw("DATA"))
	}
	if e.count(`SELECT count(*) FROM messages m JOIN transport_profiles p ON p.id=m.transport_profile_id
 WHERE m.id=$1 AND p.is_active AND m.transport_revision=p.config_revision`, id) != 1 {
		t.Fatal("submitted message not pinned to the profile revision that sent it")
	}
	// A transport that cannot name a profile (environment SMTP, test fakes)
	// leaves the pin NULL instead of guessing the active profile.
	e.w.SMTP = e.tr
	r = e.post(k.live, idem("nopin"), otpBody(testRcpt))
	e.runWorker()
	if e.count(`SELECT count(*) FROM messages WHERE id=$1 AND state='submitted' AND transport_profile_id IS NULL`, str(r.JSON, "id")) != 1 {
		t.Fatal("unattributable submission must not be pinned")
	}
}

// An ambiguous outcome is the case reconciliation needs the pin for most.
func TestUnknownOutcomeIsPinnedAndNotResent(t *testing.T) {
	e := setup(t)
	srv, pool, port := startSMTP(t, scenario{dropAfterData: true})
	e.saveSMTPPort("127.0.0.1", port, "pw")
	e.w.SMTP = &worker.StoredTransport{Svc: e.svc, Timeout: 3 * time.Second, RootCAs: pool}
	k := e.provision("proj")
	r := e.post(k.live, idem("unk"), otpBody(testRcpt))
	e.runWorker()
	id := str(r.JSON, "id")
	if e.state(id) != "unknown" {
		t.Fatalf("state %s", e.state(id))
	}
	if e.count(`SELECT count(*) FROM messages m JOIN transport_profiles p ON p.id=m.transport_profile_id WHERE m.id=$1 AND m.transport_revision=p.config_revision`, id) != 1 {
		t.Fatal("unknown outcome not pinned")
	}
	e.runWorker()
	srv.mu.Lock()
	n := 0
	for _, c := range srv.cmds {
		if c == "DATA" {
			n++
		}
	}
	srv.mu.Unlock()
	if n != 1 {
		t.Fatalf("unknown outcome was resent: %d DATA commands", n)
	}
}

// A pass on environment SMTP must not vouch for different saved settings; when
// both are identical the result is attributed to the profile revision.
func TestEnvironmentTestBindsOnlyToIdenticalProfile(t *testing.T) {
	e := setup(t)
	admin := e.user("admin@njiwa.co.tz", "admin")
	_, pool, port := startSMTP(t, scenario{})
	envCfg := mailer.SMTPConfig{Host: "127.0.0.1", Port: port, Username: "njiwa@example.com", Password: "env-pw", RootCAs: pool, Timeout: 3 * time.Second}
	e.srv.EnvSMTP = &envCfg
	t.Cleanup(func() { e.srv.EnvSMTP = nil })

	e.saveSMTP("saved.example.com", "other-pw") // differs from the environment
	if r := e.admOK(admin, "POST", "/admin/v1/mail-server/test", nil); r.JSON["ok"] != true {
		t.Fatalf("env test: %s", r.Raw)
	}
	if n := e.count(`SELECT count(*) FROM installation_checks`); n != 0 {
		t.Fatalf("a pass on environment SMTP was attached to a different saved profile (%d checks)", n)
	}
	if r := e.readiness(); r.Status == installation.Ready {
		t.Fatalf("%+v", r)
	}
	st := e.admOK(admin, "GET", "/admin/v1/installation", nil)
	if st.JSON["environment_override"] != true {
		t.Fatalf("override not reported: %s", st.Raw)
	}

	e.saveSMTPPort("127.0.0.1", port, "env-pw") // now identical
	if r := e.admOK(admin, "POST", "/admin/v1/mail-server/test", nil); r.JSON["ok"] != true {
		t.Fatalf("env test: %s", r.Raw)
	}
	if n := e.count(`SELECT count(*) FROM installation_checks c JOIN transport_profiles p ON p.id=c.profile_id AND p.config_revision=c.config_revision WHERE c.result='pass'`); n != 1 {
		t.Fatalf("identical env/profile test not bound: %d", n)
	}
}

func stepByKey(cl map[string]any, key string) map[string]any {
	for _, it := range cl["steps"].([]any) {
		if m := it.(map[string]any); m["key"] == key {
			return m
		}
	}
	return nil
}

// B05/B06: the project checklist holds only this project's steps with a
// backend-computed count; installation readiness is reported beside it and
// follows the active profile's current revision.
func TestChecklistSeparatesProjectFromInstallation(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.must(e.svc.CreateProject(ctx, "alpha"))
	e.must(e.svc.CreateProject(ctx, "beta"))
	get := func(project string) map[string]any {
		return e.admOK(admin, "GET", "/admin/v1/w/"+project+"/live/checklist", nil).JSON
	}
	cl := get("alpha")
	if cl["scope"] != "project" || stepByKey(cl, "mail_server") != nil || stepByKey(cl, "project") != nil {
		t.Fatalf("installation or global steps in the project list: %v", cl["steps"])
	}
	prog := cl["progress"].(map[string]any)
	if prog["required"].(float64) != 5 || prog["complete"].(float64) != 0 {
		t.Fatalf("progress: %v", prog)
	}
	if m := stepByKey(cl, "mailbox"); m["required"] != false || m["optional"] != true {
		t.Fatalf("mailbox must be optional: %v", m)
	}
	inst := cl["installation"].(map[string]any)
	if inst["scope"] != "installation" || inst["status"] != "not_started" || inst["welcome_state"] != "ready_to_configure" {
		t.Fatalf("installation summary: %v", inst)
	}
	if _, leak := inst["active_profile"]; leak {
		t.Fatal("profile configuration leaked into the workspace checklist")
	}

	// Installation status follows the active revision, for every project alike.
	e.saveSMTP("mail.example.com", "pw")
	e.must(e.svc.RecordMailTest(ctx, e.pin(), "connection", true, okSteps))
	e.must(e.svc.RecordMailTest(ctx, e.pin(), "test_send", true, map[string]any{"state": "submitted"}))
	if st := get("beta")["installation"].(map[string]any)["status"]; st != "ready" {
		t.Fatalf("installation: %v", st)
	}
	e.saveSMTP("mail.example.com", "rotated")
	if st := get("alpha")["installation"].(map[string]any)["status"]; st != "stale" {
		t.Fatalf("installation after rotation: %v", st)
	}

	// Project B never inherits project A's progress.
	k := e.provision("gamma")
	r := e.post(k.sbx, idem("cap"), otpBody(testRcpt))
	if r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	e.runWorker()
	_, _, err := e.svc.CreateKey(ctx, "alpha", "live", []string{"emails:send"}, "k", 0)
	e.must(err)
	if stepByKey(get("alpha"), "api_key")["done"] != true || stepByKey(get("beta"), "api_key")["done"] != false {
		t.Fatal("api_key step leaked between projects")
	}
	if stepByKey(get("beta"), "test_email")["done"] != false {
		t.Fatal("another project's captured message completed this project's test step")
	}
}

// B01/B02: the delivery-mode choice is persisted server-side, survives a new
// service instance (a redeploy), and never switches an active transport.
func TestDeliveryModeWizardPersistsAndNeverSwitches(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	owner := e.user("owner@njiwa.co.tz", "owner")
	if r := e.adm(owner, "PUT", "/admin/v1/installation/wizard", map[string]any{"selected_mode": "postfix"}); r.Code != 400 {
		t.Fatalf("only the two modes are valid: %d", r.Code)
	}
	r := e.admOK(owner, "PUT", "/admin/v1/installation/wizard", map[string]any{"selected_mode": "managed_mta"})
	if r.JSON["welcome_state"] != "in_progress" || r.JSON["active_profile"] != nil {
		t.Fatalf("%s", r.Raw)
	}
	rd := r.JSON["readiness"].(map[string]any)
	if rd["mode"] != "managed_mta" || rd["status"] == "ready" {
		t.Fatalf("managed readiness: %v", rd)
	}
	ids := []string{}
	for _, st := range rd["steps"].([]any) {
		ids = append(ids, st.(map[string]any)["id"].(string))
	}
	if strings.Join(ids, ",") != "choose_mode,preflight,install,verify_transport" {
		t.Fatalf("managed steps: %v", ids)
	}
	// Choosing again is idempotent: one draft managed profile, never active.
	e.admOK(owner, "PUT", "/admin/v1/installation/wizard", map[string]any{"selected_mode": "managed_mta"})
	if e.count(`SELECT count(*) FROM transport_profiles WHERE mode='managed_mta' AND NOT is_active`) != 1 {
		t.Fatal("duplicate or active managed profile")
	}

	// Resume after a "redeploy": a fresh service on the same database.
	fresh := service.New(e.pool, e.box, service.Options{})
	st, err := fresh.InstallationState(ctx)
	e.must(err)
	if st["wizard"].(service.WizardState).SelectedMode != "managed_mta" {
		t.Fatalf("wizard not persisted: %v", st["wizard"])
	}

	// No active transport yet, so the operator may still change their mind.
	r = e.admOK(owner, "PUT", "/admin/v1/installation/wizard", map[string]any{"selected_mode": "existing_smtp"})
	if r.JSON["readiness"].(map[string]any)["mode"] != "existing_smtp" {
		t.Fatalf("%s", r.Raw)
	}
	// Once SMTP is active, choosing managed must go through the switch planner.
	e.saveSMTP("mail.example.com", "pw")
	if r := e.adm(owner, "PUT", "/admin/v1/installation/wizard", map[string]any{"selected_mode": "managed_mta"}); r.Code != 409 || errCode(r) != "mode_switch_requires_plan" {
		t.Fatalf("active transport switched by the wizard: %d %s", r.Code, r.Raw)
	}
	if e.activeProfile()["mode"] != "existing_smtp" {
		t.Fatal("active profile changed")
	}
	if e.count(`SELECT count(*) FROM audit_log WHERE action='installation.mode_selected'`) != 3 {
		t.Fatal("mode choices not audited")
	}
}

// An installation carried over from the singleton row is "legacy_detected"
// until the operator confirms it; confirming does not change any settings.
func TestLegacyInstallIsDetectedThenConfirmed(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	owner := e.user("owner@njiwa.co.tz", "owner")
	e.must(e.svc.AdoptEnvSMTP(ctx, mailer.SMTPConfig{Host: "smtp.env.example", Port: 587, Username: "u", Password: "p"}, ""))
	st := e.admOK(owner, "GET", "/admin/v1/installation", nil)
	if st.JSON["welcome_state"] != "legacy_detected" {
		t.Fatalf("%s", st.Raw)
	}
	rev := e.activeProfile()["config_revision"]
	r := e.admOK(owner, "PUT", "/admin/v1/installation/wizard", map[string]any{"selected_mode": "existing_smtp"})
	if r.JSON["welcome_state"] != "in_progress" || e.activeProfile()["config_revision"] != rev {
		t.Fatalf("confirming changed settings or state: %s", r.Raw)
	}
	e.must(e.svc.RecordMailTest(ctx, e.pin(), "connection", true, okSteps))
	e.must(e.svc.RecordMailTest(ctx, e.pin(), "test_send", true, map[string]any{"state": "submitted"}))
	if st, _ := e.svc.InstallationState(ctx); st["welcome_state"] != "completed" {
		t.Fatalf("%v", st["welcome_state"])
	}
	e.must(e.svc.RecordMailTest(ctx, e.pin(), "connection", false, []mailer.Step{{Key: "login"}}))
	if st, _ := e.svc.InstallationState(ctx); st["welcome_state"] != "degraded" {
		t.Fatalf("%v", st["welcome_state"])
	}
}

// B04/C05: generic is the default provider; Mailcow is opt-in and choosing
// generic drops any stored Mailcow API settings.
func TestProviderTypeIsOptIn(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	owner := e.user("owner@njiwa.co.tz", "owner")
	base := map[string]any{"smtp_host": "mail.example.com", "smtp_port": 587, "smtp_tls_mode": "starttls", "smtp_username": "u", "smtp_password": "pw"}
	r := e.admOK(owner, "PUT", "/admin/v1/mail-server", base)
	if r.JSON["provider_type"] != "generic" {
		t.Fatalf("default provider: %s", r.Raw)
	}
	// Mailcow chosen without an API key: SMTP still works, no API required.
	mc := map[string]any{}
	for k, v := range base {
		mc[k] = v
	}
	mc["provider_type"] = "mailcow"
	if r := e.admOK(owner, "PUT", "/admin/v1/mail-server", mc); r.JSON["provider_type"] != "mailcow" || r.JSON["mailcow_api_key_set"] != false {
		t.Fatalf("%s", r.Raw)
	}
	if cfg, _, _, ok, _ := e.svc.StoredSMTP(ctx); !ok || cfg.Host != "mail.example.com" {
		t.Fatal("Mailcow SMTP must work without the Mailcow API")
	}
	mc["mailcow_api_url"], mc["mailcow_api_key"] = "https://mail.example.com", "KEY-1"
	if r := e.admOK(owner, "PUT", "/admin/v1/mail-server", mc); r.JSON["mailcow_api_key_set"] != true {
		t.Fatalf("%s", r.Raw)
	}
	// Back to generic: the API settings are removed, not hidden.
	base["provider_type"] = "generic"
	r = e.admOK(owner, "PUT", "/admin/v1/mail-server", base)
	if r.JSON["provider_type"] != "generic" || r.JSON["mailcow_api_key_set"] != false || r.JSON["mailcow_api_url"] != "" {
		t.Fatalf("%s", r.Raw)
	}
	if e.count(`SELECT count(*) FROM transport_profiles WHERE encrypted_provider_secret IS NOT NULL`) != 0 {
		t.Fatal("Mailcow key kept on the profile")
	}
	if r := e.adm(owner, "PUT", "/admin/v1/mail-server", map[string]any{"smtp_host": "x.example.com", "provider_type": "sendgrid"}); r.Code != 400 {
		t.Fatalf("unknown provider accepted: %d", r.Code)
	}
}

// Editing and retiring profiles; activation keeps the Mail server page in step.
func TestProfileUpdateRetireAndLegacyMirror(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	owner := e.user("owner@njiwa.co.tz", "owner")
	actor := service.Actor{Name: "test"}
	in := service.MailServerInput{SMTPHost: "mail.example.com", SMTPPort: 587, TLSMode: "starttls", Username: "a", InboundListener: "njiwa-inbound:2525"}
	pw := "pw-a"
	in.Password = &pw
	e.must(e.svc.SaveMailServer(ctx, in))
	a := str(e.activeProfile(), "id")
	b := e.newProfile("second.example.com")
	base := "/admin/v1/installation/transport-profiles/"
	body := func(host string, password any) map[string]any {
		sm := map[string]any{"host": host, "port": 465, "tls_mode": "implicit", "username": "u"}
		if password != nil {
			sm["password"] = password
		}
		return map[string]any{"label": "Second", "smtp": sm}
	}
	r := e.admOK(owner, "PUT", base+b, body("second.example.com", nil))
	if r.JSON["config_revision"].(float64) != 2 || r.JSON["label"] != "Second" { // port and TLS mode changed
		t.Fatalf("update: %s", r.Raw)
	}
	r = e.admOK(owner, "PUT", base+b, body("second.example.com", nil))
	if r.JSON["config_revision"].(float64) != 2 {
		t.Fatalf("identical update bumped the revision: %s", r.Raw)
	}
	r = e.admOK(owner, "PUT", base+b, body("second.example.com", "rotated-secret-123"))
	if r.JSON["config_revision"].(float64) != 3 || strings.Contains(r.Raw, "rotated-secret-123") {
		t.Fatalf("rotation: %s", r.Raw)
	}
	if r := e.adm(owner, "PUT", base+b, map[string]any{"label": "x", "smtp": map[string]any{"host": "bad host", "port": 587}}); r.Code != 400 {
		t.Fatalf("invalid host accepted: %d", r.Code)
	}
	if r := e.adm(owner, "DELETE", base+a, nil); r.Code != 409 || errCode(r) != "profile_active" {
		t.Fatalf("active profile retired: %d %s", r.Code, r.Raw)
	}

	// Activate B: the legacy page and StoredSMTP now show B, with B's secret,
	// and the installation-level inbound listener survives.
	_, _, rev, err := e.svc.ProfileSMTP(ctx, b)
	e.must(err)
	e.must(e.svc.RecordInstallationCheck(ctx, b, rev, "connection", true, "", nil, false))
	if _, err := e.svc.ActivateTransport(ctx, actor, b, false); err != nil {
		t.Fatal(err)
	}
	ms, err := e.svc.GetMailServer(ctx)
	e.must(err)
	if ms["smtp_host"] != "second.example.com" || ms["smtp_tls_mode"] != "implicit" || ms["inbound_listener"] != "njiwa-inbound:2525" || ms["last_test"] != nil {
		t.Fatalf("legacy row not mirrored: %v", ms)
	}
	cfg, _, _, ok, err := e.svc.StoredSMTP(ctx)
	if err != nil || !ok || cfg.Host != "second.example.com" || cfg.Password != "rotated-secret-123" || !cfg.Implicit {
		t.Fatalf("%+v %v %v", cfg, ok, err)
	}
	// Saving the Mail server page now edits B (the active profile), not A.
	e.saveSMTP("third.example.com", "")
	if p := e.activeProfile(); str(p, "id") != b || p["config"].(map[string]any)["host"] != "third.example.com" {
		t.Fatalf("page save went to the wrong profile: %v", p)
	}

	// Retire A: hidden, cannot be activated, secrets dropped, rollback pointer cleared.
	e.admOK(owner, "DELETE", base+a, nil)
	l, _ := e.svc.ListTransportProfiles(ctx)
	if len(l) != 1 {
		t.Fatalf("retired profile still listed: %d", len(l))
	}
	if _, err := e.svc.ActivateTransport(ctx, actor, a, true); errCodeOf(err) != "not_found" {
		t.Fatalf("retired profile activated: %v", err)
	}
	if e.count(`SELECT count(*) FROM transport_profiles WHERE id=$1::uuid AND encrypted_secret IS NULL`, a) != 1 ||
		e.count(`SELECT count(*) FROM installation_settings WHERE previous_profile_id IS NOT NULL`) != 0 {
		t.Fatal("retire did not drop the secret or clear the rollback pointer")
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

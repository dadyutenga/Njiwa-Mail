package test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/installation"
	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/transport"
	"github.com/njiwa-tz/mail/internal/worker"
)

func readyStep(r installation.Readiness, id string) installation.Step {
	for _, s := range r.Steps {
		if s.ID == id {
			return s
		}
	}
	return installation.Step{}
}

// passChecks records fresh passing checks for the active profile.
func (e *env) passChecks() {
	e.t.Helper()
	ctx := context.Background()
	e.must(e.svc.RecordMailTest(ctx, e.pin(), "connection", true, okSteps))
	e.must(e.svc.RecordMailTest(ctx, e.pin(), "test_send", true, map[string]any{"state": "submitted"}))
}

// ageChecks moves every recorded check back in time, past its TTL when the age is over a day.
func (e *env) ageChecks(age time.Duration) {
	e.t.Helper()
	_, err := e.pool.Exec(context.Background(), `UPDATE installation_checks SET checked_at = checked_at - make_interval(secs => $1::float8),
 expires_at = expires_at - make_interval(secs => $1::float8)`, age.Seconds())
	e.must(err)
}

func (e *env) sendLive(k keys, key, to string) string {
	e.t.Helper()
	r := e.post(k.live, idem(key), otpBody(to))
	if r.Code != 202 {
		e.t.Fatalf("send: %d %s", r.Code, r.Raw)
	}
	e.runWorker()
	return str(r.JSON, "id")
}

// A check past its TTL stays Ready while live mail shows the same settings
// working, and goes stale, saying why, when nothing does.
func TestOldCheckIsConfirmedByLiveMail(t *testing.T) {
	e := setup(t)
	_, pool, port := startSMTP(t, scenario{})
	e.saveSMTPPort("127.0.0.1", port, "pw")
	e.w.SMTP = &worker.StoredTransport{Svc: e.svc, Timeout: 3 * time.Second, RootCAs: pool}
	k := e.provision("proj")
	e.passChecks()
	e.ageChecks(31 * time.Hour)

	r := e.readiness()
	s := readyStep(r, "verify_connection")
	if r.Status != installation.Stale || s.StaleReason != installation.StaleExpired || !strings.Contains(s.Message, "no recent live mail") {
		t.Fatalf("without evidence: %s %+v", r.Status, s)
	}

	id := e.sendLive(k, "evidence", testRcpt)
	if e.state(id) != "submitted" {
		t.Fatalf("state %s", e.state(id))
	}
	r = e.readiness()
	if r.Status != installation.Ready || r.LiveHandoverAt == nil {
		t.Fatalf("with live mail: %+v", r)
	}
	for _, step := range []string{"verify_connection", "verify_sender"} {
		s := readyStep(r, step)
		if s.Status != installation.Ready || s.ConfirmedAt == nil || !strings.HasPrefix(s.Message, "Confirmed by live mail handed over at ") {
			t.Fatalf("%s: %+v", step, s)
		}
	}
	sum, err := e.svc.InstallationSummary(context.Background())
	e.must(err)
	if sum["live_handover_at"] == nil {
		t.Fatalf("summary: %v", sum)
	}
	// Evidence ages out too.
	_, err = e.pool.Exec(context.Background(), `UPDATE delivery_events SET observed_at = now() - interval '25 hours'`)
	e.must(err)
	if r := e.readiness(); r.Status != installation.Stale || r.LiveHandoverAt != nil {
		t.Fatalf("old evidence: %+v", r)
	}
}

// Mail sent with the previous settings says nothing about the new ones.
func TestSettingsChangeIsStaleUntilTheNewSettingsAreShownWorking(t *testing.T) {
	e := setup(t)
	cert, pool := genCert(t)
	_, _, port := startSMTP(t, scenario{cert: &cert})
	_, _, port2 := startSMTP(t, scenario{cert: &cert})
	e.saveSMTPPort("127.0.0.1", port, "pw")
	e.w.SMTP = &worker.StoredTransport{Svc: e.svc, Timeout: 3 * time.Second, RootCAs: pool}
	k := e.provision("proj")
	e.passChecks()
	e.sendLive(k, "before", testRcpt)

	e.saveSMTPPort("127.0.0.1", port2, "pw")
	r := e.readiness()
	s := readyStep(r, "verify_sender")
	if r.Status != installation.Stale || s.StaleReason != installation.StaleSettingsChanged || s.ChangedAt == nil || !strings.HasPrefix(s.Message, "Settings changed at ") {
		t.Fatalf("after the edit: %s %+v", r.Status, s)
	}
	e.sendLive(k, "after", testRcpt)
	if r := e.readiness(); r.Status != installation.Ready {
		t.Fatalf("mail with the new settings: %+v", r)
	}
}

// The scheduler job refreshes the connection check before it expires, right
// after a settings change, and sooner after a failure. It sends no message.
func TestRecheckRefreshesTheConnectionCheck(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cert, pool := genCert(t)
	srv, _, port := startSMTP(t, scenario{cert: &cert})
	run := func(want bool, when string) {
		t.Helper()
		ran, err := e.svc.RecheckInstallation(ctx, pool)
		e.must(err)
		if ran != want {
			t.Fatalf("%s: ran=%v, want %v", when, ran, want)
		}
	}
	run(false, "nothing configured")
	e.saveSMTPPort("127.0.0.1", port, "pw")
	run(true, "after the first save")
	if s := readyStep(e.readiness(), "verify_connection"); s.Status != installation.Ready {
		t.Fatalf("%+v", s)
	}
	run(false, "fresh")
	e.ageChecks(5 * time.Hour)
	run(false, "five hours old")
	e.ageChecks(2 * time.Hour)
	run(true, "seven hours old, before it expires")
	if n := e.count(`SELECT count(*) FROM installation_checks WHERE check_key='connection' AND result='pass' AND expires_at > now() + interval '23 hours'`); n != 1 {
		t.Fatalf("refreshed checks: %d", n)
	}
	if srv.saw("DATA") {
		t.Fatal("the check sent a message")
	}
	// A change is checked at once, and a failure is retried after ten minutes.
	e.saveSMTPPort("127.0.0.1", 1, "pw")
	run(true, "after a change")
	if s := readyStep(e.readiness(), "verify_connection"); s.Status != installation.NeedsAction || s.ProblemCode == "" {
		t.Fatalf("%+v", s)
	}
	run(false, "just failed")
	e.ageChecks(11 * time.Minute)
	run(true, "retry after a failure")
}

// A 4xx or 5xx answer is kept on the message and as evidence, shown on the
// message, and a bad recipient does not make the installation unhealthy.
func TestRefusalsAreRecordedAndOneBadRecipientDoesNotDegrade(t *testing.T) {
	e := setup(t)
	cert, pool := genCert(t)
	srv, _, port := startSMTP(t, scenario{cert: &cert, rcpt: "550 5.1.1 <bad@example.org>: Recipient address rejected: User unknown"})
	e.saveSMTPPort("127.0.0.1", port, "pw")
	e.w.SMTP = &worker.StoredTransport{Svc: e.svc, Timeout: 3 * time.Second, RootCAs: pool}
	k := e.provision("sacas")
	e.passChecks()
	var id string
	for _, key := range []string{"r1", "r2", "r3"} {
		id = e.sendLive(k, key, "bad@example.org")
	}
	var code int
	var problem, reason string
	e.must(e.pool.QueryRow(context.Background(), `SELECT smtp_reject_code, smtp_reject_problem, smtp_reject_reason FROM messages WHERE id=$1`, id).Scan(&code, &problem, &reason))
	if e.state(id) != "failed" || code != 550 || problem != mailer.RejectRecipient || !strings.Contains(reason, "User unknown") || strings.Contains(reason, "bad@example.org") {
		t.Fatalf("state=%s code=%d problem=%s reason=%q", e.state(id), code, problem, reason)
	}
	if r := e.readiness(); r.Status != installation.Ready || readyStep(r, "live_delivery").ID != "" {
		t.Fatalf("a bad recipient degraded readiness: %+v", r)
	}
	admin := e.user("admin@njiwa.co.tz", "admin")
	d := e.admOK(admin, "GET", "/admin/v1/w/sacas/live/messages/"+id, nil)
	for _, want := range []string{`"event":"rejected"`, `"smtp_status":"550"`, mailer.RejectRecipient, "Your mail server refused it", `"smtp_reject_code":550`} {
		if !strings.Contains(d.Raw, want) {
			t.Errorf("message detail lacks %q: %s", want, d.Raw)
		}
	}
	list, err := e.svc.RecentRejections(context.Background())
	e.must(err)
	if len(list) != 1 || list[0]["problem_code"] != mailer.RejectRecipient || list[0]["count"] != int64(3) {
		t.Fatalf("recent refusals: %v", list)
	}
	// A 554 at the end of the message, for content.
	srv.sc.rcpt, srv.sc.final = "", "554 5.7.1 Message rejected as spam"
	id = e.sendLive(k, "spam", "spam@example.org")
	if n := e.count(`SELECT count(*) FROM delivery_events WHERE message_id=$1 AND event='rejected' AND smtp_status='554' AND problem_code=$2`, id, mailer.RejectPolicy); n != 1 {
		t.Fatalf("554 evidence: %d", n)
	}
}

// A server that refuses everyone degrades readiness and says what to fix.
func TestRepeatedSignInRefusalDegradesReadiness(t *testing.T) {
	e := setup(t)
	_, pool, port := startSMTP(t, scenario{authFail: true})
	e.saveSMTPPort("127.0.0.1", port, "pw")
	e.w.SMTP = &worker.StoredTransport{Svc: e.svc, Timeout: 3 * time.Second, RootCAs: pool}
	k := e.provision("proj")
	e.passChecks()
	for _, key := range []string{"a1", "a2", "a3"} {
		e.sendLive(k, key, testRcpt)
	}
	r := e.readiness()
	if s := readyStep(r, "live_delivery"); r.Status != installation.Error || s.ProblemCode != mailer.RejectAuth {
		t.Fatalf("%+v", r)
	}
}

// Mail sent with SMTP settings from the environment counts as evidence only
// when they are the settings of the active profile.
func TestEnvironmentSMTPIsAttributedOnlyWhenIdentical(t *testing.T) {
	e := setup(t)
	_, pool, port := startSMTP(t, scenario{})
	e.saveSMTPPort("127.0.0.1", port, "pw")
	cfg := mailer.SMTPConfig{Host: "127.0.0.1", Port: port, Username: "njiwa@example.com", Password: "pw", RootCAs: pool, Timeout: 3 * time.Second}
	use := func(c mailer.SMTPConfig) {
		e.w.SMTP = nil
		e.w.Transport = &worker.EnvTransport{ExistingSMTPTransport: &transport.ExistingSMTPTransport{SMTP: mailer.NewSMTP(c)}, Svc: e.svc, Cfg: c}
	}
	k := e.provision("proj")
	use(cfg)
	id := e.sendLive(k, "same", testRcpt)
	if e.count(`SELECT count(*) FROM messages WHERE id=$1 AND state='submitted' AND transport_profile_id IS NOT NULL`, id) != 1 {
		t.Fatalf("identical settings not attributed (state %s)", e.state(id))
	}
	other := cfg
	other.Password = "different"
	use(other)
	id = e.sendLive(k, "other", testRcpt)
	if e.count(`SELECT count(*) FROM messages WHERE id=$1 AND state='submitted' AND transport_profile_id IS NULL`, id) != 1 {
		t.Fatalf("different settings attributed (state %s)", e.state(id))
	}
}

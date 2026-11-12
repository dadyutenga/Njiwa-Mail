package test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/installation"
	"github.com/njiwa-tz/mail/internal/mailer"
)

// F03: when the mail server is down the worker pauses Live claims instead of
// burning every message's attempts, readiness says mail is not going out,
// nothing is lost or duplicated, and one success recovers both.
func TestTransportOutageBreakerAndDegradedReadiness(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.saveSMTP("mail.example.com", "pw") // an active profile, so readiness reflects delivery
	e.must(e.svc.RecordMailTest(ctx, e.pin(), "connection", true, okSteps))
	e.must(e.svc.RecordMailTest(ctx, e.pin(), "test_send", true, map[string]any{"state": "submitted"}))
	k := e.provision("down")
	down := true
	e.tr.fn = func(n int, _ mailer.Envelope) mailer.Result {
		if down {
			return mailer.Result{Outcome: mailer.TempFailure, Reason: "smtp_connect_failed"}
		}
		return mailer.Result{Outcome: mailer.Accepted, Reason: "smtp_250", Reply: "2.0.0 Ok: queued as AB12CD34"}
	}
	e.w.BreakerThreshold, e.w.BreakerBase, e.w.BreakerMax = 3, 300*time.Millisecond, time.Second
	ids := []string{}
	for i := 0; i < 5; i++ {
		r := e.post(k.live, idem("down"+string(rune('a'+i))), otpBody(testRcpt))
		if r.Code != 202 {
			t.Fatalf("%d %s", r.Code, r.Raw)
		}
		ids = append(ids, str(r.JSON, "id"))
	}
	e.runWorker()
	calls := e.tr.Calls()
	if calls != 5 || e.count(`SELECT count(*) FROM messages WHERE state='retry_wait'`) != 5 {
		t.Fatalf("first pass: %d calls", calls)
	}
	if !e.w.LivePaused() {
		t.Fatal("breaker did not open after repeated connection failures")
	}
	steps := e.readiness()
	if steps.Status != installation.Error || steps.Steps[len(steps.Steps)-1].ProblemCode != installation.CodeTransportFailing {
		t.Fatalf("readiness during the outage: %+v", steps)
	}
	// While paused, due messages are not claimed: no attempts are burnt.
	_, err := e.pool.Exec(ctx, `UPDATE messages SET next_attempt_at=now()`)
	e.must(err)
	e.runWorker()
	if e.tr.Calls() != calls {
		t.Fatalf("live mail claimed while the breaker was open: %d calls", e.tr.Calls())
	}
	// The server comes back; after the pause everything goes out exactly once.
	down = false
	time.Sleep(350 * time.Millisecond)
	e.runWorker()
	for _, id := range ids {
		if e.state(id) != "submitted" {
			t.Fatalf("message %s: %s", id, e.state(id))
		}
	}
	if e.tr.Calls() != calls+5 {
		t.Fatalf("duplicates or losses: %d calls", e.tr.Calls())
	}
	if r := e.readiness(); r.Status != installation.Ready {
		t.Fatalf("readiness after recovery: %+v", r)
	}
	if e.count(`SELECT count(*) FROM messages WHERE mta_queue_id='AB12CD34'`) != 5 {
		t.Fatal("queue id not recorded on submission")
	}
}

// F03: the Live backlog is bounded; a full queue answers a retryable 503 and
// stores nothing. Sandbox is not affected.
func TestLiveQueueIsBounded(t *testing.T) {
	e := setup(t)
	k := e.provision("bound")
	e.svc.Opts.LiveQueueMax = 2
	t.Cleanup(func() { e.svc.Opts.LiveQueueMax = 0 })
	for i := 0; i < 2; i++ {
		if r := e.post(k.live, idem("q"+string(rune('a'+i))), otpBody(testRcpt)); r.Code != 202 {
			t.Fatalf("%d %s", r.Code, r.Raw)
		}
	}
	r := e.post(k.live, idem("q-over"), otpBody(testRcpt))
	if r.Code != 503 || errCode(r) != "queue_full" || r.Hdr.Get("Retry-After") == "" {
		t.Fatalf("full queue: %d %s", r.Code, r.Raw)
	}
	if e.count(`SELECT count(*) FROM messages m JOIN environments en ON en.id=m.environment_id WHERE en.mode='live'`) != 2 {
		t.Fatal("a refused request was stored")
	}
	if r := e.post(k.sbx, idem("q-sbx"), otpBody(testRcpt)); r.Code != 202 {
		t.Fatalf("sandbox refused: %d", r.Code)
	}
}

// C08: readiness says whether bounce reports can come back, without making
// it a requirement for sending.
func TestBounceVisibilityStep(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.saveSMTP("mail.example.com", "pw")
	step := func() installation.Step {
		for _, s := range e.readiness().Steps {
			if s.ID == "bounce_reports" {
				return s
			}
		}
		t.Fatal("no bounce step")
		return installation.Step{}
	}
	if s := step(); s.ProblemCode != installation.CodeBounceListener || s.Required {
		t.Fatalf("bounce address set, no listener: %+v", s) // setup() sets BOUNCE_ADDRESS
	}
	e.must(e.svc.HeartbeatRole(ctx, "inbound", "t"))
	if s := step(); s.Status != installation.Ready || s.CheckedAt != nil {
		t.Fatalf("listener up: %+v", s)
	}
	e.svc.Opts.BounceAddress = ""
	t.Cleanup(func() { e.svc.Opts.BounceAddress = bounceAddr })
	if s := step(); s.ProblemCode != installation.CodeBouncesNotSetUp {
		t.Fatalf("no bounce address: %+v", s)
	}
}

// F08: /metrics reports transport health, signer refusals, the Live backlog
// against its limit and installation readiness, so alerts can fire on them.
func TestDeliveryHealthMetrics(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.saveSMTP("mail.example.com", "pw")
	e.must(e.svc.RecordMailTest(ctx, e.pin(), "connection", true, okSteps))
	e.must(e.svc.RecordMailTest(ctx, e.pin(), "test_send", true, map[string]any{"state": "submitted"}))
	k := e.provision("metrics")
	metrics := func() string {
		r := e.raw("GET", "/metrics", map[string]string{"Authorization": "Bearer metrics-token-0123456789"}, nil)
		if r.Code != 200 {
			t.Fatalf("%d %s", r.Code, r.Raw)
		}
		return r.Raw
	}
	has := func(m string, want ...string) {
		t.Helper()
		for _, w := range want {
			if !strings.Contains(m, w+"\n") {
				t.Errorf("metrics missing %q", w)
			}
		}
	}
	has(metrics(), `njiwa_mail_installation_ready{mode="existing_smtp"} 1`, "njiwa_mail_transport_failing 0",
		`njiwa_mail_transport_retries_15m{reason="smtp_454"} 0`, "njiwa_mail_live_backlog 0", `njiwa_mail_delivery_events_1h{event="deferred"} 0`)

	// The server refuses temporarily, as a managed Postfix does with its
	// signer down.
	e.tr.fn = func(int, mailer.Envelope) mailer.Result {
		return mailer.Result{Outcome: mailer.TempFailure, Reason: "smtp_454"}
	}
	for i := 0; i < 4; i++ {
		if r := e.post(k.live, idem("m"+string(rune('a'+i))), otpBody(testRcpt)); r.Code != 202 {
			t.Fatalf("%d %s", r.Code, r.Raw)
		}
	}
	e.runWorker()
	m := metrics()
	has(m, `njiwa_mail_installation_ready{mode="existing_smtp"} 0`, `njiwa_mail_installation_problem{code="TRANSPORT_FAILING"} 1`,
		"njiwa_mail_transport_failing 1", `njiwa_mail_transport_retries_15m{reason="smtp_454"} 4`, "njiwa_mail_live_backlog 4")
	if strings.Contains(m, "njiwa_mail_last_live_submission_age_seconds") {
		t.Error("no Live submission yet, so no age may be reported")
	}
	if strings.Contains(m, "njiwa_mail_client_cert_expiry_seconds") {
		t.Error("certificate expiry reported outside managed mode")
	}
}

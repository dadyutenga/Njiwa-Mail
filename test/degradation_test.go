package test

import (
	"context"
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

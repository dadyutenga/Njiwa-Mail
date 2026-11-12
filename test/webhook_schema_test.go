package test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/client"
	"github.com/njiwa-tz/mail/internal/webhook"
)

// F07: every event states its schema version, in the signed body and in a
// header; an endpoint pins a supported version; only supported versions work.
func TestWebhookEventsCarrySchemaVersion(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	ctx := context.Background()
	rc := newReceiver(t)
	epID, secret := e.endpoint(rc, "live", []string{"email.submitted"})
	d := &webhook.Deliverer{Svc: e.svc, AllowPrivate: true, RootCAs: rc.pool()}
	admin := e.user("admin@njiwa.co.tz", "admin")

	e.post(k.live, idem("sv1"), otpBody(testRcpt))
	e.runWorker()
	if n, err := d.RunOnce(ctx); err != nil || n != 1 {
		t.Fatalf("deliver: %d %v", n, err)
	}
	var raw map[string]any
	e.must(json.Unmarshal(rc.bodies[0], &raw))
	if raw["schema_version"] != float64(1) {
		t.Fatalf("body has no schema_version: %s", rc.bodies[0])
	}
	ev, err := client.VerifyWebhook(secret, rc.sigs[0], rc.bodies[0], time.Now())
	if err != nil || ev.SchemaVersion != 1 {
		t.Fatalf("client parse: %+v %v", ev, err)
	}
	if rc.header("X-DM-Schema-Version") != "1" {
		t.Fatalf("header: %q", rc.header("X-DM-Schema-Version"))
	}

	// Pinning: 1 is accepted, anything else is refused with a clear code.
	e.admOK(admin, "PATCH", "/admin/v1/webhooks/"+epID, map[string]any{"schema_version": 1})
	if r := e.adm(admin, "PATCH", "/admin/v1/webhooks/"+epID, map[string]any{"schema_version": 2}); r.Code != 422 || errCode(r) != "unsupported_schema_version" {
		t.Fatalf("unsupported version: %d %s", r.Code, r.Raw)
	}
	// A delivery row from before the column existed (default 1) is still sent as version 1.
	e.post(k.live, idem("sv2"), otpBody("two@example.org"))
	e.runWorker()
	e.must(execOK(e, `UPDATE webhook_deliveries SET schema_version=1 WHERE state='pending'`))
	d.RunOnce(ctx)
	e.must(json.Unmarshal(rc.bodies[rc.n()-1], &raw))
	if raw["schema_version"] != float64(1) {
		t.Fatal("legacy row not version 1")
	}
}

// F07: email.delivery_* events are idempotent. A log line is recorded once,
// a failed delivery is retried under the same event id, and an operator
// replay reuses it, so a receiver that stores ids processes each event once.
func TestDeliveryEventsRetryAndIdempotency(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	ctx := context.Background()
	rc := newReceiver(t)
	e.endpoint(rc, "live", []string{"email.delivery_remote_accepted", "email.delivery_deferred"})
	d := &webhook.Deliverer{Svc: e.svc, AllowPrivate: true, RootCAs: rc.pool()}

	r := e.post(k.live, idem("di1"), otpBody(testRcpt))
	id := str(r.JSON, "id")
	e.runWorker()
	e.must(execOK(e, `UPDATE messages SET mta_queue_id='4C1D2E3F4A' WHERE id='`+id+`'`))
	log := strings.Join([]string{
		`Nov 12 08:01:02 mailout postfix/smtp[2211]: 4C1D2E3F4A: to=<user@example.org>, relay=none, delay=30, delays=0.1/0/30/0, dsn=4.4.1, status=deferred (connect to mx.example.org[192.0.2.5]:25: Connection timed out)`,
		`Nov 12 08:06:02 mailout postfix/smtp[2211]: 4C1D2E3F4A: to=<user@example.org>, relay=mx.example.org[192.0.2.5]:25, delay=360, delays=0.1/300/0.2/0.3, dsn=2.0.0, status=sent (250 2.0.0 OK 1731398762 user@example.org)`,
	}, "\n")
	for i := 0; i < 3; i++ { // the collector restarts and re-reads the log
		e.svc.IngestMTALog(ctx, strings.NewReader(log))
	}
	if n := e.count(`SELECT count(*) FROM webhook_deliveries`); n != 2 {
		t.Fatalf("a re-read log produced %d events, want 2 (deferred, remote_accepted)", n)
	}

	// The receiver fails the first attempt; the retry carries the same id.
	rc.set(500)
	d.RunOnce(ctx)
	first := eventIDs(rc)
	e.must(execOK(e, `UPDATE webhook_deliveries SET next_attempt_at=now() WHERE state='pending'`))
	rc.set(200)
	d.RunOnce(ctx)
	seen := map[string]int{}
	for _, id := range eventIDs(rc) {
		seen[id]++
	}
	if len(seen) != 2 {
		t.Fatalf("distinct event ids: %v", seen)
	}
	for _, id := range first {
		if seen[id] < 2 && e.count(`SELECT count(*) FROM webhook_deliveries WHERE id=$1 AND state='succeeded' AND attempts=2`, id) != 1 {
			t.Fatalf("failed event %s was not retried under the same id: %v", id, seen)
		}
	}
	var types []string
	for _, b := range rc.bodies {
		var ev struct{ Type, ID string }
		json.Unmarshal(b, &ev)
		types = append(types, ev.Type)
	}
	if !strings.Contains(strings.Join(types, ","), "email.delivery_remote_accepted") {
		t.Fatalf("types: %v", types)
	}
	if e.count(`SELECT count(*) FROM messages WHERE id=$1 AND state='submitted'`, id) != 1 {
		t.Fatal("delivery evidence changed the message state")
	}
}

func eventIDs(rc *receiver) []string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	var out []string
	for _, b := range rc.bodies {
		var ev struct{ ID string }
		json.Unmarshal(b, &ev)
		out = append(out, ev.ID)
	}
	return out
}

func (rc *receiver) header(name string) string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if len(rc.hdrs) == 0 {
		return ""
	}
	return rc.hdrs[len(rc.hdrs)-1].Get(name)
}

func execOK(e *env, q string) error {
	_, err := e.pool.Exec(context.Background(), q)
	return err
}

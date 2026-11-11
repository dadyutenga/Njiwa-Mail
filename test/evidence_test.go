package test

import (
	"context"
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/service"
)

// Captured from Postfix 3.8.6 in this repository's managed-mode E2E run.
const realSent = `Nov 11 23:11:09 mailout postfix/smtp[11226]: EF8778C662: to=<dsn-check@bounce.example.test>, relay=127.0.0.1[127.0.0.1]:2525, delay=0.1, delays=0.09/0.01/0/0, dsn=2.0.0, status=sent (250 2.0.0 OK: queued)`

func TestParseMTALines(t *testing.T) {
	ev, ok := service.ParseMTALine(realSent)
	if !ok || ev.QueueID != "EF8778C662" || ev.Status != "sent" || ev.DSN != "2.0.0" || ev.Relay != "127.0.0.1" {
		t.Fatalf("%+v", ev)
	}
	def := `Nov 12 08:01:02 mailout postfix/smtp[2211]: 4C1D2E3F4A: to=<user@example.org>, relay=none, delay=30, delays=0.1/0/30/0, dsn=4.4.1, status=deferred (connect to mx.example.org[192.0.2.5]:25: Connection timed out)`
	if ev, ok := service.ParseMTALine(def); !ok || ev.Status != "deferred" || ev.DSN != "4.4.1" {
		t.Fatalf("%+v", ev)
	}
	exp := `Nov 17 08:01:02 mailout postfix/qmgr[812]: 4C1D2E3F4A: from=<bounces@notify.example.com>, status=expired, returned to sender`
	if ev, ok := service.ParseMTALine(exp); !ok || ev.Status != "expired" {
		t.Fatalf("%+v", ev)
	}
	for _, other := range []string{
		`Nov 12 08:00:00 mailout postfix/smtpd[100]: connect from unknown[192.0.2.9]`,
		`Nov 12 08:00:01 mailout postfix/cleanup[101]: 4C1D2E3F4A: message-id=<x@notify.example.com>`,
		`Nov 12 08:00:01 mailout postfix/qmgr[102]: 4C1D2E3F4A: from=<bounces@notify.example.com>, size=900, nrcpt=1 (queue active)`,
	} {
		if _, ok := service.ParseMTALine(other); ok {
			t.Fatalf("parsed a non-delivery line: %s", other)
		}
	}
}

// F01/E08: the worker records the submission with the MTA queue id; Postfix
// log lines and DSNs add evidence with their source; nothing ever says
// "delivered".
func TestDeliveryEvidenceTimeline(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	k := e.provision("ev")
	e.tr.fn = nil // the fake transport replies "2.0.0 Ok: queued as FAKE"
	r := e.post(k.live, idem("ev1"), otpBody(testRcpt))
	if r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	id := str(r.JSON, "id")
	e.runWorker()
	_, err := e.pool.Exec(ctx, `UPDATE messages SET mta_queue_id='4C1D2E3F4A' WHERE id=$1`, id) // as if the reply had carried it
	e.must(err)
	if e.count(`SELECT count(*) FROM delivery_events WHERE message_id=$1 AND event='submitted' AND source='smtp'`, id) != 1 {
		t.Fatal("submission evidence missing")
	}

	log := strings.Join([]string{
		`Nov 12 08:00:01 mailout postfix/cleanup[101]: 4C1D2E3F4A: message-id=<x@notify.example.com>`,
		`Nov 12 08:01:02 mailout postfix/smtp[2211]: 4C1D2E3F4A: to=<user@example.org>, relay=none, delay=30, delays=0.1/0/30/0, dsn=4.4.1, status=deferred (connect to mx.example.org[192.0.2.5]:25: Connection timed out)`,
		`Nov 12 08:06:02 mailout postfix/smtp[2211]: 4C1D2E3F4A: to=<user@example.org>, relay=mx.example.org[192.0.2.5]:25, delay=360, delays=0.1/300/0.2/0.3, dsn=2.0.0, status=sent (250 2.0.0 OK 1731398762 user@example.org)`,
		`Nov 12 08:06:03 mailout postfix/smtp[2299]: 0000AAAA11: to=<other@example.org>, relay=mx.example.org[192.0.2.5]:25, delay=1, delays=0/0/0/1, dsn=2.0.0, status=sent (250 OK)`,
	}, "\n")
	n, err := e.svc.IngestMTALog(ctx, strings.NewReader(log))
	if err != nil || n != 2 {
		t.Fatalf("ingested %d (%v), want 2: unrelated queue ids and lines are ignored", n, err)
	}
	if n, _ := e.svc.IngestMTALog(ctx, strings.NewReader(log)); n != 0 {
		t.Fatalf("replayed log recorded %d duplicates", n)
	}
	if e.count(`SELECT count(*) FROM delivery_events WHERE safe_reason LIKE '%@%' OR remote_host LIKE '%[%'`) != 0 {
		t.Fatal("an address or IP leaked into stored evidence")
	}
	m := e.admOK(admin, "GET", "/admin/v1/w/ev/live/messages/"+id, nil).JSON
	evs := m["delivery_evidence"].([]any)
	kinds := []string{}
	for _, x := range evs {
		kinds = append(kinds, x.(map[string]any)["event"].(string))
	}
	if strings.Join(kinds, ",") != "submitted,deferred,remote_accepted" {
		t.Fatalf("timeline: %v", kinds)
	}
	sum := str(m, "delivery_summary")
	if !strings.Contains(sum, "accepted it") || !strings.Contains(sum, "does not show whether it reached the inbox") || strings.Contains(strings.ToLower(sum), "delivered") {
		t.Fatalf("summary: %q", sum)
	}
	if m["state"] != "submitted" {
		t.Fatalf("message state changed by evidence: %v", m["state"])
	}

	// A permanent MTA bounce marks the message bounced, but never suppresses
	// the recipient: only a DSN correlated to our message's recipient does.
	bounce := `Nov 12 09:00:00 mailout postfix/smtp[3001]: 4C1D2E3F4A: to=<user@example.org>, relay=mx.example.org[192.0.2.5]:25, delay=1, delays=0/0/0/1, dsn=5.1.1, status=bounced (host mx.example.org[192.0.2.5] said: 550 5.1.1 <user@example.org>: User unknown (in reply to RCPT TO command))`
	if n, _ := e.svc.IngestMTALog(ctx, strings.NewReader(bounce)); n != 1 {
		t.Fatal("bounce line not recorded")
	}
	if e.count(`SELECT count(*) FROM messages WHERE id=$1 AND bounce_state='hard'`, id) != 1 || e.count(`SELECT count(*) FROM suppressions`) != 0 {
		t.Fatal("MTA bounce handling")
	}
	if !strings.Contains(str(e.admOK(admin, "GET", "/admin/v1/w/ev/live/messages/"+id, nil).JSON, "delivery_summary"), "bounced") {
		t.Fatal("summary after bounce")
	}
}

func TestQueueIDFromReply(t *testing.T) {
	for reply, want := range map[string]string{"2.0.0 Ok: queued as 92C4A8CB48": "92C4A8CB48", "2.0.0 OK: queued as 4Y2bXq0Lrkz9tF": "4Y2bXq0Lrkz9tF",
		"250 2.0.0 Ok": "", "queued as ab": ""} {
		if got := service.QueueIDFromReply(reply); got != want {
			t.Fatalf("%q: %q", reply, got)
		}
	}
}

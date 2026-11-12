package test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/service"
)

var verpRe = regexp.MustCompile(`^bounces\+[0-9a-f]{32}\.[0-9a-f]{16}@`)

// dsnTo is dsnMail without the message id header, so only the envelope can correlate it.
func dsnNoHeaderID(messageIDInHeader, rcpt string) string {
	m := dsnMail("00000000-0000-4000-8000-000000000000", rcpt, "5.1.1", "failed", "550 5.1.1 <"+rcpt+">: Recipient address rejected: User unknown")
	m = strings.Replace(m, "X-DM-Message-ID: 00000000-0000-4000-8000-000000000000", "X-DM-Message-ID: "+messageIDInHeader, 1)
	return m
}

// F06: with VERP on, the envelope sender is a signed per-message address;
// returned notifications are matched by it and a bad signature is never trusted.
func TestSignedReturnPathCorrelation(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	k := e.provision("verp")
	addr := e.listener("127.0.0.1")
	e.w.VERP = "on"

	send := func(n, to string) string {
		r := e.post(k.live, idem(n), otpBody(to))
		if r.Code != 202 {
			t.Fatalf("%s: %s", n, r.Raw)
		}
		e.runWorker()
		return str(r.JSON, "id")
	}
	a := send("a", "gone-a@example.org")
	if from := e.tr.last.From; !verpRe.MatchString(from) || from != e.svc.VerpAddress(a) {
		t.Fatalf("envelope sender %q, want the signed address %q", from, e.svc.VerpAddress(a))
	}
	// The signature is specific to the message.
	b := send("b", "gone-b@example.org")
	if e.svc.VerpAddress(a) == e.svc.VerpAddress(b) {
		t.Fatal("two messages share a return path")
	}
	// Off and auto (legacy transport) keep the plain bounce address.
	e.w.VERP = "off"
	send("c", "plain-c@example.org")
	if e.tr.last.From != bounceAddr {
		t.Fatalf("VERP off: %q", e.tr.last.From)
	}
	e.w.VERP = "auto"
	send("d", "plain-d@example.org")
	if e.tr.last.From != bounceAddr {
		t.Fatalf("VERP auto on a non-managed transport: %q", e.tr.last.From)
	}

	deliver := func(to, msg string) {
		t.Helper()
		e.must(smtpSend(addr, "", []string{to}, msg)) // the listener accepts signed addresses
		e.process()
	}
	by := func(id string) (string, string) {
		var how, st string
		e.must(e.pool.QueryRow(ctx, `SELECT coalesce((SELECT correlated_by FROM bounces WHERE message_id=$1 LIMIT 1),''), bounce_state FROM messages WHERE id=$1`, id).Scan(&how, &st))
		return how, st
	}

	// 1. Valid signed address, no message id anywhere in the headers: correlated and suppressed.
	deliver(e.svc.VerpAddress(a), dsnNoHeaderID("not-an-id", "gone-a@example.org"))
	if how, st := by(a); how != "verp" || st != "hard" {
		t.Fatalf("signed return path: correlated_by=%s bounce_state=%s", how, st)
	}
	if e.count(`SELECT count(*) FROM suppressions WHERE source='hard_bounce'`) != 1 {
		t.Fatal("hard bounce not suppressed")
	}
	// 2. The envelope beats a misleading header (the header names message b).
	e.must(execOK(e, `DELETE FROM suppressions`))
	e.must(execOK(e, `UPDATE messages SET bounce_state='none'`))
	e.must(execOK(e, `DELETE FROM bounces`))
	deliver(e.svc.VerpAddress(a), dsnNoHeaderID(b, "gone-a@example.org"))
	if how, st := by(a); how != "verp" || st != "hard" {
		t.Fatalf("a: %s %s", how, st)
	}
	if _, st := by(b); st != "none" {
		t.Fatal("the header was allowed to override the signed envelope")
	}
	// 3. A forged tag is never trusted, even with a perfect header.
	e.must(execOK(e, `DELETE FROM suppressions; DELETE FROM bounces; UPDATE messages SET bounce_state='none'`))
	forged := verpRe.ReplaceAllString(e.svc.VerpAddress(a), "bounces+"+strings.Repeat("0", 32)+"."+strings.Repeat("0", 16)+"@")
	if forged == e.svc.VerpAddress(a) {
		t.Fatal("test setup")
	}
	valid := e.svc.VerpAddress(a)
	at := strings.LastIndex(valid, "@")
	last := valid[at-1]
	flip := byte('0')
	if last == '0' {
		flip = 'f' // always differ from the real signature's last digit
	}
	badTag := valid[:at-1] + string(flip) + valid[at:]
	for i, to := range []string{forged, badTag} {
		// (a different notification each time, or the second would be a duplicate of the first)
		deliver(to, dsnMail([]string{a, b}[i], "gone-a@example.org", "5.1.1", "failed", "550 5.1.1 User unknown"))
		if how, st := by(a); how != "" || st != "none" {
			t.Fatalf("forged return path %q was trusted: %s %s", to, how, st)
		}
	}
	if e.count(`SELECT count(*) FROM bounces WHERE correlated_by='verp_invalid' AND message_id IS NULL`) != 2 || e.count(`SELECT count(*) FROM suppressions`) != 0 {
		t.Fatal("forged notifications must be recorded as uncorrelated and suppress nothing")
	}
	// 4. No tag at all: the header method still works (older messages, Mailcow without plus addressing).
	e.must(execOK(e, `DELETE FROM bounces; UPDATE messages SET bounce_state='none'`))
	deliver(bounceAddr, dsnMail(a, "gone-a@example.org", "5.1.1", "failed", "550 5.1.1 User unknown"))
	if how, st := by(a); how != "header" || st != "hard" {
		t.Fatalf("fallback: %s %s", how, st)
	}
	// 5. A valid signature for a different recipient than the one that bounced suppresses nobody.
	e.must(execOK(e, `DELETE FROM suppressions; DELETE FROM bounces; UPDATE messages SET bounce_state='none'`))
	deliver(e.svc.VerpAddress(a), dsnNoHeaderID("x", "someone-else@example.org"))
	if e.count(`SELECT count(*) FROM suppressions`) != 0 {
		t.Fatal("a signed return path let a report suppress an unrelated address")
	}

	// Routing: only the bounce mailbox's own extension is a bounce.
	for addr, want := range map[string]string{e.svc.VerpAddress(a): service.KindBounce, bounceAddr: service.KindBounce, "other+x@" + strings.SplitN(bounceAddr, "@", 2)[1]: ""} {
		got, err := e.svc.ResolveRecipient(ctx, addr)
		e.must(err)
		if got != want {
			t.Errorf("ResolveRecipient(%s) = %q, want %q", addr, got, want)
		}
	}
}

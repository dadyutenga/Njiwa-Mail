package worker

import (
	"context"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/transport"
)

type legacyPanic struct{}

func (legacyPanic) Send(context.Context, mailer.Envelope) mailer.Result { panic("legacy SMTP used") }

func TestSubmitUsesTransportContract(t *testing.T) {
	var got transport.Submission
	f := &transport.Fake{M: transport.ManagedMTA, Fn: func(_ int, s transport.Submission) transport.Receipt {
		got = s
		return transport.Receipt{Outcome: transport.Unknown, SafeReason: "no_reply_after_data"}
	}}
	w := &Worker{Transport: f, SMTP: legacyPanic{}}
	sub := transport.Submission{MessageID: "m@example.com", EnvelopeFrom: "bounce@example.com", Recipients: []string{"to@example.com"}, RFC5322: []byte("Subject: test\r\n\r\nbody")}
	r, pin := w.submit(context.Background(), sub)
	if r.Outcome != transport.Unknown || got.MessageID != sub.MessageID || got.EnvelopeFrom != sub.EnvelopeFrom || string(got.RFC5322) != string(sub.RFC5322) || pin.ProfileID != "" {
		t.Fatalf("submission changed: receipt=%+v pin=%+v got=%+v", r, pin, got)
	}
}

func TestBreakerUsesTransportOutcome(t *testing.T) {
	w := &Worker{BreakerThreshold: 1, BreakerBase: time.Minute, BreakerMax: time.Minute}
	w.breaker(transport.Receipt{Outcome: transport.Temporary, SafeReason: "smtp_connect_failed"})
	if !w.LivePaused() {
		t.Fatal("connection failure should pause live claims")
	}
	w.breaker(transport.Receipt{Outcome: transport.Accepted})
	if w.failStreak != 0 {
		t.Fatal("accepted submission should clear failure streak")
	}
}

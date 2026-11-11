package transport_test

import (
	"context"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/transport"
)

// Contract: every implementation reports its mode, accepts a submission and
// classifies the outcome with one of the four honest values.
func contract(t *testing.T, name string, tr transport.Transport, want transport.Mode, wantOutcome transport.Outcome) {
	t.Helper()
	if tr.Mode() != want {
		t.Fatalf("%s: mode %s", name, tr.Mode())
	}
	r := tr.Submit(context.Background(), transport.Submission{MessageID: "m1", EnvelopeFrom: "b@notify.example.com",
		Recipients: []string{"u@example.org"}, RFC5322: []byte("Subject: x\r\n\r\nbody\r\n")})
	if r.Outcome != wantOutcome {
		t.Fatalf("%s: outcome %s", name, r.Outcome)
	}
	if (r.Outcome == transport.Accepted) != (r.Acknowledged != nil) {
		t.Fatalf("%s: acknowledgement must be set exactly when accepted", name)
	}
}

func TestFakeSatisfiesContract(t *testing.T) {
	contract(t, "fake", &transport.Fake{}, transport.ExistingSMTP, transport.Accepted)
	contract(t, "fake-managed", &transport.Fake{M: transport.ManagedMTA}, transport.ManagedMTA, transport.Accepted)
	contract(t, "fake-unknown", &transport.Fake{Fn: func(int, transport.Submission) transport.Receipt {
		return transport.Receipt{Outcome: transport.Unknown, SafeReason: "no_reply_after_data"}
	}}, transport.ExistingSMTP, transport.Unknown)
}

func TestExistingSMTPUnreachableIsTemporaryNotUnknown(t *testing.T) {
	// Nothing listens on port 1: the failure is proven pre-DATA, so a retry
	// cannot duplicate and it must not be classified as unknown.
	tr := transport.NewExistingSMTP(mailer.SMTPConfig{Host: "127.0.0.1", Port: 1, Timeout: 2 * time.Second})
	contract(t, "smtp", tr, transport.ExistingSMTP, transport.Temporary)
	if _, err := tr.Check(context.Background()); err == nil {
		t.Fatal("check against a closed port must fail")
	}
}

func TestFromMailerMapping(t *testing.T) {
	now := time.Now()
	for in, want := range map[mailer.Outcome]transport.Outcome{mailer.Accepted: transport.Accepted, mailer.TempFailure: transport.Temporary,
		mailer.PermFailure: transport.Permanent, mailer.Unknown: transport.Unknown} {
		if got := transport.FromMailer(mailer.Result{Outcome: in}, now).Outcome; got != want {
			t.Fatalf("%v -> %s", in, got)
		}
	}
}

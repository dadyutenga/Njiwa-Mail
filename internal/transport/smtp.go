package transport

import (
	"context"
	"time"

	"github.com/njiwa-tz/mail/internal/mailer"
)

// ExistingSMTPTransport wraps the verified-TLS submission client. Protocol
// handling (TLS verification, retries semantics, unknown classification) stays
// in internal/mailer; nothing here is provider specific.
type ExistingSMTPTransport struct {
	SMTP *mailer.SMTP
}

func NewExistingSMTP(cfg mailer.SMTPConfig) *ExistingSMTPTransport {
	return &ExistingSMTPTransport{SMTP: mailer.NewSMTP(cfg)}
}

func (t *ExistingSMTPTransport) Mode() Mode { return ExistingSMTP }

// Check dials, negotiates TLS and authenticates; no message is sent. The
// egress IP and signing identity belong to the downstream server and are not
// discoverable from here, so the capability is intentionally empty.
func (t *ExistingSMTPTransport) Check(ctx context.Context) (Capability, error) {
	return Capability{}, t.SMTP.Check(ctx)
}

func (t *ExistingSMTPTransport) Submit(ctx context.Context, s Submission) Receipt {
	e := mailer.Envelope{From: s.EnvelopeFrom, Recipients: s.Recipients, Raw: s.RFC5322}
	if len(s.Recipients) > 0 {
		e.To = s.Recipients[0]
	}
	return FromMailer(t.SMTP.Send(ctx, e), time.Now().UTC())
}

package worker

import (
	"context"
	"crypto/x509"
	"fmt"
	"sync"
	"time"

	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/service"
	"github.com/njiwa-tz/mail/internal/transport"
)

// StoredTransport sends with the SMTP settings saved on the Mail server page.
// It is used only when the environment does not set SMTP_HOST. Settings are
// re-read at most every 30 seconds, so a change applies without a restart.
type StoredTransport struct {
	Svc     *service.Service
	Timeout time.Duration

	// RootCAs lets tests trust a private CA; nil uses the system roots.
	RootCAs *x509.CertPool

	mu      sync.Mutex
	checked time.Time
	updated time.Time
	smtp    *mailer.SMTP
	pin     service.TransportPin
	mode    transport.Mode
}

func (t *StoredTransport) load(ctx context.Context) (*mailer.SMTP, service.TransportPin) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if time.Since(t.checked) < 30*time.Second {
		return t.smtp, t.pin
	}
	t.checked = time.Now()
	a, err := t.Svc.LoadActiveSMTP(ctx)
	if err != nil {
		return t.smtp, t.pin // keep the last good settings through a database blip
	}
	if !a.OK {
		t.smtp, t.pin = nil, service.TransportPin{}
		return nil, t.pin
	}
	if t.smtp == nil || !a.Updated.Equal(t.updated) || a.Pin() != t.pin {
		a.Cfg.Timeout, a.Cfg.RootCAs = t.Timeout, t.RootCAs
		t.smtp, t.updated, t.pin = mailer.NewSMTP(a.Cfg), a.Updated, a.Pin()
	}
	t.mode = transport.Mode(a.Mode)
	return t.smtp, t.pin
}

func (t *StoredTransport) Ready(ctx context.Context) bool {
	s, _ := t.load(ctx)
	return s != nil
}

func (t *StoredTransport) Mode() transport.Mode {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.mode == "" {
		return transport.ExistingSMTP
	}
	return t.mode
}

func (t *StoredTransport) Check(ctx context.Context) (transport.Capability, error) {
	s, _ := t.load(ctx)
	if s == nil {
		return transport.Capability{}, fmt.Errorf("smtp_not_configured")
	}
	return transport.Capability{}, s.Check(ctx)
}

func (t *StoredTransport) Submit(ctx context.Context, sub transport.Submission) transport.Receipt {
	r, _ := t.SubmitPinned(ctx, sub)
	return r
}

func (t *StoredTransport) SubmitPinned(ctx context.Context, sub transport.Submission) (transport.Receipt, service.TransportPin) {
	e := mailer.Envelope{From: sub.EnvelopeFrom, Recipients: sub.Recipients, Raw: sub.RFC5322}
	if len(sub.Recipients) > 0 {
		e.To = sub.Recipients[0]
	}
	r, pin := t.SendPinned(ctx, e)
	return transport.FromMailer(r, time.Now().UTC()), pin
}

func (t *StoredTransport) Send(ctx context.Context, e mailer.Envelope) mailer.Result {
	r, _ := t.SendPinned(ctx, e)
	return r
}

// SendPinned sends and reports the profile revision whose settings were used,
// taken from the same snapshot as the client, so the record cannot name a
// profile that became active only after the attempt started.
func (t *StoredTransport) SendPinned(ctx context.Context, e mailer.Envelope) (mailer.Result, service.TransportPin) {
	s, pin := t.load(ctx)
	if s == nil {
		// Not claimed while unconfigured; reaching here means it was removed mid-batch.
		return mailer.Result{Outcome: mailer.TempFailure, Reason: "smtp_not_configured"}, service.TransportPin{}
	}
	return s.Send(ctx, e), pin
}

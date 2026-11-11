package worker

import (
	"context"
	"crypto/x509"
	"sync"
	"time"

	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/service"
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
	return t.smtp, t.pin
}

func (t *StoredTransport) Ready(ctx context.Context) bool {
	s, _ := t.load(ctx)
	return s != nil
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

package worker

import (
	"context"
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

	mu      sync.Mutex
	checked time.Time
	updated time.Time
	smtp    *mailer.SMTP
}

func (t *StoredTransport) load(ctx context.Context) *mailer.SMTP {
	t.mu.Lock()
	defer t.mu.Unlock()
	if time.Since(t.checked) < 30*time.Second {
		return t.smtp
	}
	t.checked = time.Now()
	cfg, _, updated, ok, err := t.Svc.StoredSMTP(ctx)
	if err != nil {
		return t.smtp // keep the last good settings through a database blip
	}
	if !ok {
		t.smtp = nil
		return nil
	}
	if t.smtp == nil || !updated.Equal(t.updated) {
		cfg.Timeout = t.Timeout
		t.smtp, t.updated = mailer.NewSMTP(cfg), updated
	}
	return t.smtp
}

func (t *StoredTransport) Ready(ctx context.Context) bool { return t.load(ctx) != nil }

func (t *StoredTransport) Send(ctx context.Context, e mailer.Envelope) mailer.Result {
	s := t.load(ctx)
	if s == nil {
		// Not claimed while unconfigured; reaching here means it was removed mid-batch.
		return mailer.Result{Outcome: mailer.TempFailure, Reason: "smtp_not_configured"}
	}
	return s.Send(ctx, e)
}

// Package worker claims queued messages and dispatches them.
package worker

import (
	"context"
	"errors"
	"fmt"
	"github.com/njiwa-tz/mail/internal/db"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/service"
	"github.com/njiwa-tz/mail/internal/transport"
)

type Worker struct {
	Svc               *service.Service
	SMTP              mailer.Transport    // nil => live messages are not claimed
	Transport         transport.Transport // production dispatch; SMTP remains for legacy callers
	ID                string
	Poll              time.Duration
	Batch             int
	Concurrency       int
	Lease             time.Duration
	SendTimeout       time.Duration
	RetentionInterval time.Duration
	Backoff           []time.Duration
	// BounceAddress, when set, is used as the SMTP envelope sender (MAIL FROM).
	BounceAddress string
	// VERP selects the signed per-message return path (F06): "on", "off", or
	// "auto"/"" = only when the active transport is the managed Postfix, whose
	// bounce domain is known to accept plus-addressed recipients.
	VERP string
	// BreakerThreshold consecutive connection-level failures pause Live claims
	// (0 = 5). BreakerBase is the first pause, doubled up to BreakerMax.
	BreakerThreshold        int
	BreakerBase, BreakerMax time.Duration

	once       sync.Once
	breakerMu  sync.Mutex
	failStreak int
	pauses     int
	pauseUntil time.Time
}

// breaker records one send outcome. After BreakerThreshold consecutive
// transport-level failures the worker stops claiming Live mail for a while,
// so queued messages wait (keeping their attempts and TTL) instead of each
// failing against a server that is down. One success closes it again.
func (w *Worker) breaker(res transport.Receipt) {
	w.breakerMu.Lock()
	defer w.breakerMu.Unlock()
	if res.Outcome == transport.Accepted {
		w.failStreak, w.pauses = 0, 0
		return
	}
	if res.Outcome != transport.Temporary || !service.TransportDown(res.SafeReason) {
		return
	}
	w.failStreak++
	if w.failStreak < w.BreakerThreshold {
		return
	}
	d := w.BreakerBase << w.pauses
	if d > w.BreakerMax || d <= 0 {
		d = w.BreakerMax
	}
	w.pauses++
	w.failStreak = 0
	w.pauseUntil = time.Now().Add(d)
	slog.Warn("mail server unreachable: pausing live sends", "reason", res.SafeReason, "pause", d.String())
}

// LivePaused reports whether the breaker is holding Live claims.
func (w *Worker) LivePaused() bool {
	w.breakerMu.Lock()
	defer w.breakerMu.Unlock()
	return time.Now().Before(w.pauseUntil)
}

func (w *Worker) defaults() { w.once.Do(w.setDefaults) }

// Readiness is implemented by a transport whose configuration can be absent
// (settings stored from the Mail server page). Live mail is not claimed
// while it is not ready.
type Readiness interface {
	Ready(ctx context.Context) bool
}

// PinnedSender is a transport that reports which profile revision it used.
type PinnedSender interface {
	SendPinned(ctx context.Context, e mailer.Envelope) (mailer.Result, service.TransportPin)
}

// PinnedTransport reports the exact profile revision used for a submission.
type PinnedTransport interface {
	SubmitPinned(ctx context.Context, s transport.Submission) (transport.Receipt, service.TransportPin)
}

func (w *Worker) submit(ctx context.Context, sub transport.Submission) (transport.Receipt, service.TransportPin) {
	if ps, ok := w.Transport.(PinnedTransport); ok {
		return ps.SubmitPinned(ctx, sub)
	}
	if w.Transport != nil {
		return w.Transport.Submit(ctx, sub), service.TransportPin{}
	}
	e := mailer.Envelope{From: sub.EnvelopeFrom, Recipients: sub.Recipients, Raw: sub.RFC5322}
	if len(sub.Recipients) > 0 {
		e.To = sub.Recipients[0]
	}
	if ps, ok := w.SMTP.(PinnedSender); ok {
		r, pin := ps.SendPinned(ctx, e)
		return transport.FromMailer(r, time.Now().UTC()), pin
	}
	return transport.FromMailer(w.SMTP.Send(ctx, e), time.Now().UTC()), service.TransportPin{}
}

func (w *Worker) liveReady(ctx context.Context) bool {
	if w.Transport != nil {
		if r, ok := w.Transport.(Readiness); ok {
			return r.Ready(ctx)
		}
		return true
	}
	if r, ok := w.SMTP.(Readiness); ok {
		return r.Ready(ctx)
	}
	return true
}

func (w *Worker) setDefaults() {
	if w.BreakerThreshold <= 0 {
		w.BreakerThreshold = 5
	}
	if w.BreakerBase <= 0 {
		w.BreakerBase = 15 * time.Second
	}
	if w.BreakerMax <= 0 {
		w.BreakerMax = 5 * time.Minute
	}
	if w.Poll <= 0 {
		w.Poll = time.Second
	}
	if w.Batch <= 0 {
		w.Batch = 10
	}
	if w.Concurrency <= 0 {
		w.Concurrency = 4
	}
	if w.Lease <= 0 {
		w.Lease = 90 * time.Second
	}
	if w.SendTimeout <= 0 {
		w.SendTimeout = 45 * time.Second
	}
	if len(w.Backoff) == 0 {
		w.Backoff = []time.Duration{15 * time.Second, 45 * time.Second, 120 * time.Second}
	}
	if w.ID == "" {
		w.ID = "worker"
	}
}

func (w *Worker) backoff(attempts int) time.Duration {
	i := attempts - 1
	if i < 0 {
		i = 0
	}
	if i >= len(w.Backoff) {
		i = len(w.Backoff) - 1
	}
	d := w.Backoff[i]
	return d + time.Duration((rand.Float64()*0.4-0.2)*float64(d)) // +/-20% jitter
}

// Run loops until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	w.defaults()
	slog.Info("worker started", "id", w.ID, "live_enabled", w.Transport != nil || w.SMTP != nil)
	lastRet := time.Time{}
	lastBeat := time.Time{}
	for ctx.Err() == nil {
		if time.Since(lastBeat) > 5*time.Second {
			if err := w.Svc.Heartbeat(ctx, w.ID); err != nil {
				slog.Error("heartbeat failed", "err", err)
			}
			lastBeat = time.Now()
		}
		if w.RetentionInterval > 0 && time.Since(lastRet) > w.RetentionInterval {
			if r, err := w.Svc.RunRetention(ctx); err != nil {
				slog.Error("retention failed", "err", err)
			} else {
				slog.Info("retention run", "captures", r.Captures, "messages", r.Messages, "payloads", r.Payloads, "buckets", r.Buckets)
			}
			lastRet = time.Now()
		}
		n, err := w.RunOnce(ctx)
		if err != nil {
			slog.Error("worker cycle failed", "err", err)
		}
		if n == 0 || err != nil {
			select {
			case <-ctx.Done():
			case <-time.After(w.Poll):
			}
		}
	}
	slog.Info("worker stopped", "id", w.ID)
}

// RunOnce performs one reap/expire/claim/process cycle synchronously.
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	w.defaults()
	if n, err := w.Svc.ReapLeases(ctx); err != nil {
		return 0, err
	} else if n > 0 {
		slog.Warn("leases expired; outcomes unknown", "count", n)
	}
	if _, err := w.Svc.ExpireDue(ctx); err != nil {
		return 0, err
	}
	if _, err := w.Svc.PromoteScheduled(ctx); err != nil {
		return 0, err
	}
	modes := []string{"sandbox"}
	if (w.Transport != nil || w.SMTP != nil) && !w.LivePaused() && w.liveReady(ctx) {
		modes = append(modes, "live")
	}
	claimed, err := w.Svc.Claim(ctx, w.Batch, w.Lease, modes)
	if err != nil {
		return 0, err
	}
	sem := make(chan struct{}, w.Concurrency)
	var wg sync.WaitGroup
	for _, c := range claimed {
		sem <- struct{}{}
		wg.Add(1)
		go func(c service.Claimed) {
			defer func() { <-sem; wg.Done() }()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("panic while processing; lease will expire to unknown", "id", c.ID, "panic", fmt.Sprint(r))
				}
			}()
			w.process(ctx, c)
		}(c)
	}
	wg.Wait()
	return len(claimed), nil
}

func (w *Worker) log(c service.Claimed, state, reason string) {
	slog.Info("message outcome", "id", c.ID, "state", state, "reason", reason, "attempt", c.Attempts)
}

func (w *Worker) lost(c service.Claimed, err error) {
	if err != nil {
		slog.Error("persisting outcome failed", "id", c.ID, "err", err)
	} else {
		slog.Warn("lease lost; outcome not recorded", "id", c.ID)
	}
}

func (w *Worker) process(ctx context.Context, c service.Claimed) {
	fail := func(reason string) {
		if ok, err := w.Svc.MarkFailed(ctx, c, reason); !ok {
			w.lost(c, err)
		}
		w.log(c, "failed", reason)
	}
	pl, err := w.Svc.OpenPayload(c)
	if err != nil {
		fail("payload_unreadable")
		return
	}
	if !time.Now().Before(c.ExpiresAt) { // never begin a submission after the deadline
		if ok, err := w.Svc.MarkExpired(ctx, c, "ttl_elapsed_before_submit"); !ok {
			w.lost(c, err)
		}
		w.log(c, "expired", "ttl_elapsed_before_submit")
		return
	}
	// Rendered from the template version pinned at accept time.
	subject, text, html, err := w.Svc.RenderPayload(ctx, pl)
	if err != nil {
		if errors.Is(err, db.ErrNoRows) {
			fail("template_missing")
		} else {
			fail("render_failed")
		}
		return
	}
	domain := pl.From[strings.LastIndexByte(pl.From, '@')+1:]
	msgID := c.ID + "@" + domain
	atts, err := w.Svc.LoadAttachments(ctx, c.ID)
	if err != nil {
		fail("attachment_unreadable")
		return
	}
	rcpts := append([]string{pl.To}, append(append([]string{}, pl.CC...), pl.BCC...)...)
	raw, err := mailer.Build(mailer.Message{FromName: pl.FromName, FromEmail: pl.From, To: pl.To, Subject: subject,
		Text: text, HTML: html, MessageID: msgID, DMMessageID: c.ID, Date: time.Now(),
		ReplyTo: pl.ReplyTo, CC: pl.CC, BCC: pl.BCC, Attachments: atts, Extra: pl.Headers, System: pl.System, Personal: pl.Personal})
	if err != nil {
		fail("build_failed")
		return
	}

	if c.Mode == "sandbox" {
		if pl.Confidential {
			// A recovery token must not become an admin-readable capture.
			if ok, err := w.Svc.MarkFailed(ctx, c, "confidential_not_captured"); !ok {
				w.lost(c, err)
			}
			w.log(c, "failed", "confidential_not_captured")
			return
		}
		if ok, err := w.Svc.MarkCaptured(ctx, c, raw, msgID); !ok {
			w.lost(c, err)
		}
		w.log(c, "captured", "sandbox")
		return
	}

	sctx, cancel := context.WithTimeout(ctx, w.SendTimeout)
	// The envelope sender is the bounce address so DSNs come back to the
	// inbound listener; the header From stays the approved sender.
	envFrom := pl.From
	if w.BounceAddress != "" {
		envFrom = w.BounceAddress
		if w.useVERP() {
			if v := w.Svc.VerpAddress(c.ID); v != "" {
				envFrom = v
			}
		}
	}
	sub := transport.Submission{MessageID: msgID, EnvelopeFrom: envFrom, Recipients: rcpts, RFC5322: raw}
	res, pin := w.submit(sctx, sub)
	c.Pin = pin
	cancel()
	w.breaker(res)

	switch res.Outcome {
	case transport.Accepted:
		var ok bool
		var perr error
		for i := 0; i < 5; i++ { // the message IS accepted; keep trying to record that
			if ok, perr = w.Svc.MarkSubmitted(context.WithoutCancel(ctx), c, res.SafeReason, res.TransportID, msgID); ok || (perr == nil && !ok) {
				break
			}
			time.Sleep(time.Duration(i+1) * 200 * time.Millisecond)
		}
		if !ok {
			w.lost(c, perr) // lease reaper will mark unknown; never resubmitted
		}
		w.log(c, "submitted", res.SafeReason)
	case transport.Temporary:
		st, err := w.Svc.ScheduleRetry(ctx, c, res.SafeReason, w.backoff(c.Attempts))
		if err != nil {
			w.lost(c, err)
		}
		w.log(c, st, res.SafeReason)
	case transport.Permanent:
		fail(res.SafeReason)
	default:
		if ok, err := w.Svc.MarkUnknown(ctx, c, res.SafeReason); !ok {
			w.lost(c, err)
		}
		w.log(c, "unknown", res.SafeReason)
	}
}

// useVERP reports whether messages go out with a signed per-message return path.
func (w *Worker) useVERP() bool {
	switch w.VERP {
	case "on":
		return true
	case "off":
		return false
	}
	return w.Transport != nil && w.Transport.Mode() == transport.ManagedMTA
}

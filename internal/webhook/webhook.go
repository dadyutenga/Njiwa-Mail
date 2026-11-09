// Package webhook delivers outbox events to project endpoints as signed
// HTTPS POSTs, with SSRF controls enforced at connect time.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/ditronics-tz/mail/internal/service"
)

const (
	SignatureHeader = "X-DM-Signature"
	maxResponse     = 1 << 20
)

// Sign returns the X-DM-Signature value: t=<unix>,v1=<hex HMAC-SHA256("t.body")>.
func Sign(secret string, t time.Time, body []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts))
	m.Write([]byte("."))
	m.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(m.Sum(nil))
}

var errBlocked = errors.New("destination address not allowed")

type Deliverer struct {
	Svc         *service.Service
	Batch       int
	Concurrency int
	Lease       time.Duration
	Timeout     time.Duration
	Poll        time.Duration

	// Test hooks. Production leaves both unset: private destinations are
	// refused and the system trust store is used.
	AllowPrivate bool
	RootCAs      *x509.CertPool

	once   sync.Once
	client *http.Client
}

func (d *Deliverer) init() {
	d.once.Do(func() {
		if d.Batch <= 0 {
			d.Batch = 20
		}
		if d.Concurrency <= 0 {
			d.Concurrency = 4
		}
		if d.Timeout <= 0 {
			d.Timeout = 10 * time.Second
		}
		if d.Lease <= 0 {
			d.Lease = 60 * time.Second
		}
		if d.Poll <= 0 {
			d.Poll = 2 * time.Second
		}
		dialer := &net.Dialer{Timeout: 5 * time.Second,
			// Control runs after DNS resolution, on the address actually being
			// connected to, so DNS rebinding cannot reach a private host.
			Control: func(network, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return errBlocked
				}
				if !d.AllowPrivate && !service.PublicIP(net.ParseIP(host)) {
					return errBlocked
				}
				return nil
			}}
		d.client = &http.Client{
			Timeout: d.Timeout,
			Transport: &http.Transport{
				Proxy:                  nil, // never route through an environment proxy
				DialContext:            dialer.DialContext,
				TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: d.RootCAs},
				TLSHandshakeTimeout:    5 * time.Second,
				ResponseHeaderTimeout:  d.Timeout,
				DisableKeepAlives:      true,
				MaxResponseHeaderBytes: 16 << 10,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	})
}

// Run delivers until ctx is cancelled.
func (d *Deliverer) Run(ctx context.Context) {
	d.init()
	for ctx.Err() == nil {
		n, err := d.RunOnce(ctx)
		if err != nil {
			slog.Error("webhook cycle failed", "err", err)
		}
		if n == 0 || err != nil {
			select {
			case <-ctx.Done():
			case <-time.After(d.Poll):
			}
		}
	}
}

// RunOnce claims and attempts one batch.
func (d *Deliverer) RunOnce(ctx context.Context) (int, error) {
	d.init()
	batch, err := d.Svc.ClaimDeliveries(ctx, d.Batch, d.Lease)
	if err != nil {
		return 0, err
	}
	sem := make(chan struct{}, d.Concurrency)
	var wg sync.WaitGroup
	for _, del := range batch {
		sem <- struct{}{}
		wg.Add(1)
		go func(del service.Delivery) {
			defer func() { <-sem; wg.Done() }()
			ok, code, reason := d.attempt(ctx, del)
			if err := d.Svc.FinishDelivery(context.WithoutCancel(ctx), del, ok, code, reason); err != nil {
				slog.Error("recording webhook outcome failed", "delivery", del.ID, "err", err)
			}
			slog.Info("webhook attempt", "delivery", del.ID, "event", del.EventType, "ok", ok, "status", code, "reason", reason, "attempt", del.Attempts)
		}(del)
	}
	wg.Wait()
	return len(batch), nil
}

// attempt posts one delivery. Only a 2xx response counts as success; a
// redirect is a failure because redirects are never followed.
func (d *Deliverer) attempt(ctx context.Context, del service.Delivery) (ok bool, code int, reason string) {
	u, err := url.Parse(del.URL)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return false, 0, "invalid_url"
	}
	body := del.Body()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, del.URL, bytes.NewReader(body))
	if err != nil {
		return false, 0, "invalid_url"
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ditronics-mail-webhook/1")
	req.Header.Set("X-DM-Event-ID", del.ID)
	req.Header.Set("X-DM-Event-Type", del.EventType)
	req.Header.Set(SignatureHeader, Sign(del.Secret, time.Now(), body))
	resp, err := d.client.Do(req)
	if err != nil {
		switch {
		case errors.Is(err, errBlocked):
			return false, 0, "destination_blocked"
		case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
			return false, 0, "timeout"
		default:
			var ce *tls.CertificateVerificationError
			if errors.As(err, &ce) {
				return false, 0, "tls_verification_failed"
			}
			return false, 0, "connection_failed"
		}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponse)) // read at most 1 MiB, then drop the connection
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, resp.StatusCode, ""
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return false, resp.StatusCode, "redirect_refused"
	}
	return false, resp.StatusCode, fmt.Sprintf("http_%d", resp.StatusCode)
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

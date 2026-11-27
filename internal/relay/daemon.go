package relay

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

// States a relay reports about itself.
const (
	StateStarting = "starting"
	StateRunning  = "running" // listening, with a usable certificate
	StateError    = "error"
)

// Daemon keeps the relay serving and says how it is doing. A missing or broken
// certificate, or a port in use, is reported and retried instead of ending the
// process, and a renewed certificate is picked up without a restart.
type Daemon struct {
	Server            Server // everything but TLS, which the daemon provides
	CertFile, KeyFile string
	// Report receives the current state; it is the relay's heartbeat.
	Report   func(ctx context.Context, state, detail string, info map[string]any)
	Interval time.Duration // between reports while running (default 15s)
	Retry    time.Duration // after a failure (default 10s)
}

// Run serves until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) {
	if d.Interval <= 0 {
		d.Interval = 15 * time.Second
	}
	if d.Retry <= 0 {
		d.Retry = 10 * time.Second
	}
	files := &certFiles{cert: d.CertFile, key: d.KeyFile}
	d.Report(ctx, StateStarting, "", nil)
	for ctx.Err() == nil {
		err := d.serve(ctx, files)
		if err == nil || ctx.Err() != nil {
			return
		}
		slog.Error("smtp relay is not serving; retrying", "err", err, "in", d.Retry.String())
		d.Report(ctx, StateError, err.Error(), nil)
		select {
		case <-ctx.Done():
		case <-time.After(d.Retry):
		}
	}
}

func (d *Daemon) serve(ctx context.Context, files *certFiles) error {
	if err := files.load(); err != nil {
		return err
	}
	l, err := net.Listen("tcp", d.Server.Addr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w", d.Server.Addr, err)
	}
	srv := d.Server
	srv.TLS = &tls.Config{GetCertificate: files.get, MinVersion: tls.VersionTLS12}
	sctx, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- srv.Serve(sctx, l) }()

	port := ""
	if a, ok := l.Addr().(*net.TCPAddr); ok {
		port = strconv.Itoa(a.Port)
	}
	t := time.NewTicker(d.Interval)
	defer t.Stop()
	for {
		detail := ""
		if err := files.load(); err != nil {
			detail = "still using the previous certificate: " + err.Error()
		}
		leaf := files.leaf()
		state := StateRunning
		if time.Now().After(leaf.NotAfter) {
			state, detail = StateError, "the TLS certificate expired on "+leaf.NotAfter.UTC().Format("2006-01-02")
		}
		d.Report(ctx, state, detail, map[string]any{"port": port, "implicit_tls": srv.Implicit,
			"cert_not_after": leaf.NotAfter.UTC().Format(time.RFC3339), "cert_names": leaf.DNSNames})
		select {
		case <-ctx.Done():
			<-done
			return nil
		case err := <-done:
			if err == nil {
				err = errors.New("the listener stopped")
			}
			return err
		case <-t.C:
		}
	}
}

// certFiles loads a certificate pair and reloads it when either file changes.
type certFiles struct {
	cert, key string

	mu      sync.Mutex
	stamp   string
	current *tls.Certificate
}

func (c *certFiles) load() error {
	if c.cert == "" || c.key == "" {
		return errors.New("set RELAY_TLS_CERT and RELAY_TLS_KEY: AUTH is only offered over TLS")
	}
	var stamp string
	for _, f := range []string{c.cert, c.key} {
		st, err := os.Stat(f)
		if err != nil {
			return fmt.Errorf("cannot read the TLS certificate: %w", err)
		}
		stamp += fmt.Sprintf("%d/%d;", st.ModTime().UnixNano(), st.Size())
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if stamp == c.stamp && c.current != nil {
		return nil
	}
	pair, err := tls.LoadX509KeyPair(c.cert, c.key)
	if err != nil {
		return fmt.Errorf("cannot load the TLS certificate: %w", err)
	}
	if pair.Leaf, err = x509.ParseCertificate(pair.Certificate[0]); err != nil {
		return fmt.Errorf("cannot parse the TLS certificate: %w", err)
	}
	c.current, c.stamp = &pair, stamp
	return nil
}

func (c *certFiles) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current, nil
}

func (c *certFiles) leaf() *x509.Certificate {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current.Leaf
}

package test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/relay"
)

func (e *env) relayState(admin session) map[string]any {
	return e.admOK(admin, "GET", "/admin/v1/relay", nil).JSON
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// namedPEM is a self-signed certificate that verifies for one DNS name.
func namedPEM(t *testing.T, name string) (certPEM, keyPEM []byte) {
	t.Helper()
	c := genCertFor(t, name)
	key, err := x509.MarshalPKCS8PrivateKey(c.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Certificate[0]}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})
}

func genCertFor(t *testing.T, name string) tls.Certificate {
	t.Helper()
	c, _ := genCertWith(t, func(x *x509.Certificate) { x.DNSNames = []string{name} })
	return c
}

// The dashboard states: not enabled, stale (configured, no heartbeat), and the
// real settings and host instead of placeholders.
func TestRelayStatusStates(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.svc.Opts.RelayAddr = "0.0.0.0:2587"
	st := e.relayState(admin)
	if st["state"] != "not_enabled" || st["running"] != false || st["host"] == "mail.example.com" || st["host"] == "" || st["host_source"] != "request" {
		t.Fatalf("%v", st)
	}
	e.svc.Opts.RelayEnabled, e.svc.Opts.RelayPublicHost, e.svc.Opts.RelayPublicPort = true, "smtp.example.com", "587"
	st = e.relayState(admin)
	if st["state"] != "stale" || st["host"] != "smtp.example.com" || st["port"] != "587" || st["host_source"] != "setting" {
		t.Fatalf("enabled without a heartbeat: %v", st)
	}
	e.must(e.svc.ReportRole(ctx, "relay", "h", "error", "cannot read the TLS certificate", nil))
	if st = e.relayState(admin); st["state"] != "error" || st["running"] != false || !strings.Contains(str(st, "detail"), "certificate") {
		t.Fatalf("error: %v", st)
	}
	e.must(e.svc.ReportRole(ctx, "relay", "h", "running", "", map[string]any{"port": "2587", "implicit_tls": true}))
	if st = e.relayState(admin); st["state"] != "running" || st["tls"] != "implicit" || st["port"] != "587" {
		t.Fatalf("running: %v", st)
	}
	_, err := e.pool.Exec(ctx, `UPDATE worker_heartbeats SET last_seen = now() - interval '2 minutes' WHERE role='relay'`)
	e.must(err)
	if st = e.relayState(admin); st["state"] != "stale" || st["detail"] != "" {
		t.Fatalf("stale: %v", st)
	}
}

// The relay survives a missing certificate, reports it, starts serving once the
// files appear, and the dashboard test sends a Sandbox message through it.
func TestRelayDaemonAndDashboardTest(t *testing.T) {
	e := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e.provision("sacas")
	admin := e.user("admin@njiwa.co.tz", "admin")
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "fullchain.pem"), filepath.Join(dir, "privkey.pem")
	port := freePort(t)
	addr := "127.0.0.1:" + strconv.Itoa(port)
	e.svc.Opts.RelayAddr, e.svc.Opts.RelayEnabled = addr, true
	e.svc.Opts.RelayPublicHost, e.svc.Opts.RelayProbeAddr = "relay.test", addr
	d := &relay.Daemon{Server: relay.Server{Svc: e.svc, Addr: addr, Hostname: "relay.test"}, CertFile: certFile, KeyFile: keyFile,
		Interval: 50 * time.Millisecond, Retry: 50 * time.Millisecond,
		Report: func(ctx context.Context, state, detail string, info map[string]any) {
			_ = e.svc.ReportRole(ctx, "relay", "daemon", state, detail, info)
		}}
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	waitFor(t, "the certificate error", func() bool { return e.relayState(admin)["state"] == "error" })

	certPEM, keyPEM := namedPEM(t, "relay.test")
	e.must(os.WriteFile(certFile, certPEM, 0o600))
	e.must(os.WriteFile(keyFile, keyPEM, 0o600))
	waitFor(t, "running", func() bool { return e.relayState(admin)["state"] == "running" })

	// An untrusted certificate is reported as the failing step.
	url := "/admin/v1/w/sacas/sandbox/relay-test"
	r := e.admOK(admin, "POST", url, nil)
	if r.JSON["ok"] != false || !strings.Contains(r.Raw, "certificate") {
		t.Fatalf("untrusted: %s", r.Raw)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)
	e.svc.Opts.RelayTestRoots = pool
	r = e.admOK(admin, "POST", url, nil)
	id := str(r.JSON, "message_id")
	if r.JSON["ok"] != true || id == "" || e.state(id) != "queued" {
		t.Fatalf("test through the relay: %s", r.Raw)
	}
	// The temporary key is revoked, and only Sandbox may be tested.
	if n := e.count(`SELECT count(*) FROM api_keys WHERE label='relay test (temporary)' AND revoked_at IS NULL`); n != 0 {
		t.Fatalf("temporary keys left usable: %d", n)
	}
	if r := e.adm(admin, "POST", "/admin/v1/w/sacas/live/relay-test", nil); r.Code != 409 || errCode(r) != "sandbox_only" {
		t.Fatalf("live: %d %s", r.Code, r.Raw)
	}
}

package test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/worker"
)

func failedCode(steps []mailer.Step) string {
	for _, s := range steps {
		if !s.OK && !s.Skipped {
			return s.Code
		}
	}
	return ""
}

// C03: every failed diagnosis step carries a stable, provider-neutral code.
func TestDiagnoseReportsTypedProblemCodes(t *testing.T) {
	ctx := context.Background()
	cfg := func(port int, implicit bool) mailer.SMTPConfig {
		return mailer.SMTPConfig{Host: "127.0.0.1", Port: port, Username: "notify@example.com", Password: passSecret, Implicit: implicit, Timeout: 3 * time.Second}
	}
	_, pool, port := startSMTP(t, scenario{})
	ok := cfg(port, false)
	ok.RootCAs = pool
	if steps := mailer.Diagnose(ctx, ok, liveFrom); failedCode(steps) != "" || !steps[4].OK {
		t.Fatalf("healthy server: %+v", steps)
	}
	cases := []struct {
		name string
		sc   scenario
		mod  func(*mailer.SMTPConfig)
		want string
	}{
		{"no STARTTLS", scenario{noSTARTTLS: true}, nil, mailer.CodeSTARTTLSMissing},
		{"auth rejected", scenario{authFail: true}, nil, mailer.CodeAuthRejected},
		{"sender rejected", scenario{rcpt: "550 5.7.1 sender not allowed"}, nil, mailer.CodeSenderRejected},
		{"untrusted certificate", scenario{}, func(c *mailer.SMTPConfig) { c.RootCAs = nil }, mailer.CodeTLSCertInvalid},
		{"implicit TLS on a STARTTLS port", scenario{}, func(c *mailer.SMTPConfig) { c.Implicit = true }, mailer.CodeTLSModeMismatch},
		{"no username", scenario{}, func(c *mailer.SMTPConfig) { c.Username = "" }, mailer.CodeAuthMissing},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, pool, port := startSMTP(t, c.sc)
			cf := cfg(port, false)
			cf.RootCAs = pool
			if c.mod != nil {
				c.mod(&cf)
			}
			if got := failedCode(mailer.Diagnose(ctx, cf, liveFrom)); got != c.want {
				t.Fatalf("want %s got %q", c.want, got)
			}
		})
	}
	// A closed port is refused, not a timeout.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	e := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if err != nil {
		t.Fatal(err)
	}
	if got := failedCode(mailer.Diagnose(ctx, cfg(e, false), "")); got != mailer.CodeTCPRefused {
		t.Fatalf("closed port: %q", got)
	}
	// The connection test never reaches link-local / metadata addresses.
	meta := cfg(80, false)
	meta.Host = "169.254.169.254"
	if steps := mailer.Diagnose(ctx, meta, ""); failedCode(steps) != mailer.CodeHostForbidden || steps[1].OK {
		t.Fatalf("metadata address reached: %+v", steps)
	}
}

// C04: a controlled test goes through the real queue and worker, and its
// outcome becomes the test_send check of the profile revision that sent it.
func TestQueuedTransportTestRecordsTheRealOutcome(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("proj")
	srv, pool, port := startSMTP(t, scenario{})
	e.saveSMTPPort("127.0.0.1", port, passSecret)
	_, err := e.pool.Exec(ctx, `UPDATE transport_profiles SET config_json = config_json || '{"username":"notify@example.com"}'`)
	e.must(err)
	e.w.SMTP = &worker.StoredTransport{Svc: e.svc, Timeout: 3 * time.Second, RootCAs: pool}

	if r := e.adm(admin, "POST", "/admin/v1/w/proj/sandbox/transport-test", map[string]any{"to": testRcpt}); r.Code != 409 || errCode(r) != "live_only" {
		t.Fatalf("sandbox: %d %s", r.Code, r.Raw)
	}
	r := e.admOK(admin, "POST", "/admin/v1/w/proj/live/transport-test", map[string]any{"to": testRcpt})
	id := str(r.JSON, "id")
	if e.count(`SELECT count(*) FROM installation_checks WHERE check_key='test_send'`) != 0 {
		t.Fatal("recorded before the worker ran")
	}
	e.runWorker()
	if e.state(id) != "submitted" || !srv.saw("DATA") {
		t.Fatalf("state %s", e.state(id))
	}
	if e.count(`SELECT count(*) FROM installation_checks c JOIN transport_profiles p ON p.id=c.profile_id AND p.config_revision=c.config_revision
 WHERE c.check_key='test_send' AND c.result='pass' AND c.safe_details->>'message_id'=$1 AND c.safe_details->>'via'='queue'`, id) != 1 {
		t.Fatal("queued test outcome not recorded for the sending revision")
	}

	// A rejected login is recorded as a failed test with the stable code.
	_, bad, badPort := startSMTP(t, scenario{authFail: true})
	e.saveSMTPPort("127.0.0.1", badPort, passSecret)
	e.w.SMTP = &worker.StoredTransport{Svc: e.svc, Timeout: 3 * time.Second, RootCAs: bad}
	r = e.admOK(admin, "POST", "/admin/v1/w/proj/live/transport-test", map[string]any{"to": testRcpt})
	e.runWorker()
	if e.state(str(r.JSON, "id")) != "failed" {
		t.Fatalf("state %s", e.state(str(r.JSON, "id")))
	}
	if e.count(`SELECT count(*) FROM installation_checks WHERE check_key='test_send' AND result='fail' AND problem_code='SMTP_AUTH_FAILED'`) != 1 {
		t.Fatal("failed queued test not recorded")
	}
	st := e.readiness()
	for _, s := range st.Steps {
		if s.ID == "verify_sender" && (s.Status == "ready" || s.ProblemCode != "SMTP_AUTH_FAILED") {
			t.Fatalf("readiness after failed queued test: %+v", s)
		}
	}

	// Ordinary API mail never becomes a transport test.
	keys := e.provision("third")
	if r := e.post(keys.sbx, idem("plain"), map[string]any{"from": liveFrom, "to": testRcpt, "template": "otp", "template_version": 1,
		"variables": map[string]any{"name": "x", "code": "123456", "expires_in_minutes": 5}, "transport_test": true}); r.Code == 202 {
		if e.count(`SELECT count(*) FROM messages WHERE id=$1 AND transport_test`, str(r.JSON, "id")) != 0 {
			t.Fatal("API input set transport_test")
		}
	}
}

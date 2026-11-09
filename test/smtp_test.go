package test

import (
	"context"
	"crypto/x509"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ditronics-tz/mail/internal/mailer"
	"github.com/ditronics-tz/mail/internal/tmpl"
)

func smtpClient(port int, pool *x509.CertPool) *mailer.SMTP {
	return mailer.NewSMTP(mailer.SMTPConfig{Host: "127.0.0.1", Port: port, Username: "notify@example.com", Password: passSecret,
		HeloName: "test.local", Timeout: 2 * time.Second, RootCAs: pool})
}

func env1() mailer.Envelope {
	raw, _ := mailer.Build(mailer.Message{FromName: "Ditronics", FromEmail: liveFrom, To: testRcpt, Subject: "Hi", Text: "hello", HTML: "<p>hello</p>", MessageID: "x@y.z", Date: time.Now()})
	return mailer.Envelope{From: liveFrom, To: testRcpt, Raw: raw}
}

func TestSMTPClassification(t *testing.T) {
	cases := []struct {
		name   string
		sc     scenario
		want   mailer.Outcome
		reason string
		data   bool // server should have received message data
	}{
		{"accepted", scenario{}, mailer.Accepted, "smtp_250", true},
		{"rcpt-4xx-temp", scenario{rcpt: "451 4.7.1 try later"}, mailer.TempFailure, "smtp_451", false},
		{"rcpt-5xx-perm", scenario{rcpt: "550 5.1.1 no such user"}, mailer.PermFailure, "smtp_550", false},
		{"final-4xx-not-accepted", scenario{final: "451 4.3.0 local error"}, mailer.TempFailure, "smtp_451", true},
		{"final-5xx-perm", scenario{final: "554 5.7.1 rejected"}, mailer.PermFailure, "smtp_554", true},
		{"drop-after-data-unknown", scenario{dropAfterData: true}, mailer.Unknown, "smtp_ack_lost", true},
		{"no-starttls", scenario{noSTARTTLS: true}, mailer.PermFailure, "smtp_starttls_unsupported", false},
		{"auth-failed", scenario{authFail: true}, mailer.PermFailure, "smtp_auth_failed", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, pool, port := startSMTP(t, c.sc)
			res := smtpClient(port, pool).Send(context.Background(), env1())
			if res.Outcome != c.want || res.Reason != c.reason {
				t.Fatalf("want %v/%s got %v/%s", c.want, c.reason, res.Outcome, res.Reason)
			}
			srv.mu.Lock()
			got := srv.data != ""
			srv.mu.Unlock()
			if got != c.data {
				t.Fatalf("data received=%v want %v", got, c.data)
			}
			if c.want == mailer.Accepted && !strings.Contains(res.Reply, "TEST123") {
				t.Fatalf("ack text not captured: %q", res.Reply)
			}
		})
	}
}

func TestSMTPVerifiesTLS(t *testing.T) {
	srv, _, port := startSMTP(t, scenario{})
	_, otherPool, _ := startSMTP(t, scenario{}) // trusts a different CA
	res := smtpClient(port, otherPool).Send(context.Background(), env1())
	if res.Outcome != mailer.PermFailure || res.Reason != "smtp_tls_verification_failed" {
		t.Fatalf("untrusted cert must fail verification: %v/%s", res.Outcome, res.Reason)
	}
	// no credentials or mail may be sent over an unverified channel
	if srv.saw("AUTH") || srv.saw("MAIL") || srv.saw("DATA") {
		t.Fatalf("client proceeded after failed verification: %v", srv.cmds)
	}
	// system roots (nil pool) do not trust the test cert either
	if r := smtpClient(port, nil).Send(context.Background(), env1()); r.Outcome == mailer.Accepted {
		t.Fatal("accepted with unverifiable certificate")
	}
}

func TestSMTPConnectAndTimeout(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if r := smtpClient(port, nil).Send(context.Background(), env1()); r.Outcome != mailer.TempFailure || r.Reason != "smtp_connect_failed" {
		t.Fatalf("closed port: %v/%s", r.Outcome, r.Reason)
	}
	_, pool, hport := startSMTP(t, scenario{hang: true})
	c := mailer.NewSMTP(mailer.SMTPConfig{Host: "127.0.0.1", Port: hport, Timeout: 300 * time.Millisecond, RootCAs: pool})
	start := time.Now()
	r := c.Send(context.Background(), env1())
	if r.Outcome != mailer.TempFailure || r.Reason != "smtp_timeout" || time.Since(start) > 3*time.Second {
		t.Fatalf("hang: %v/%s after %s", r.Outcome, r.Reason, time.Since(start))
	}
}

func TestSMTPCheck(t *testing.T) {
	_, pool, port := startSMTP(t, scenario{})
	if err := smtpClient(port, pool).Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, pool2, port2 := startSMTP(t, scenario{authFail: true})
	if err := smtpClient(port2, pool2).Check(context.Background()); err == nil {
		t.Fatal("check should fail on bad auth")
	}
}

func TestMIMEBuildSafe(t *testing.T) {
	if _, err := mailer.Build(mailer.Message{FromEmail: liveFrom, To: testRcpt, Subject: "a\r\nBcc: x@evil.net", MessageID: "a@b.c", Date: time.Now()}); err == nil {
		t.Fatal("subject header injection accepted")
	}
	if _, err := mailer.Build(mailer.Message{FromName: "x\nBcc: y", FromEmail: liveFrom, To: testRcpt, Subject: "s", MessageID: "a@b.c", Date: time.Now()}); err == nil {
		t.Fatal("display-name injection accepted")
	}
	raw, err := mailer.Build(mailer.Message{FromName: "Ditronics Séc", FromEmail: liveFrom, To: testRcpt, Subject: "Séance", Text: "héllo\nline2", HTML: "<p>héllo</p>", MessageID: "id1@notify.example.com", Date: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{"Message-ID: <id1@notify.example.com>", "multipart/alternative", "text/plain; charset=utf-8", "text/html; charset=utf-8", "MIME-Version: 1.0", "Auto-Submitted: auto-generated"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(strings.ReplaceAll(s, "\r\n", ""), "\n") {
		t.Error("bare LF in message")
	}
}

func TestTemplateRenderEscapes(t *testing.T) {
	tp := tmpl.Lookup("account-notice", 1)
	_, text, html, err := tp.Render(map[string]string{"name": `<script>alert(1)</script>`, "title": `T<b>`, "message": `<img src=http://evil/x onerror=1>`})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>") || strings.Contains(html, "<img") || strings.Contains(html, "<b>") {
		t.Fatalf("html not escaped: %s", html)
	}
	if strings.Contains(html, "src=\"http") || strings.Contains(html, "http://evil") && strings.Contains(html, "<img") {
		t.Fatal("remote asset reference")
	}
	if !strings.Contains(text, "<script>") { // plain text carries literal characters, harmless
		t.Fatal("plain text should be literal")
	}
	// pinned versions only
	if tmpl.Lookup("otp", 2) != nil || tmpl.Lookup("otp", 1) == nil {
		t.Fatal("version pinning")
	}
	for _, n := range []string{"otp", "password-reset", "account-notice"} {
		if tmpl.Lookup(n, 1) == nil {
			t.Fatalf("missing template %s", n)
		}
	}
}

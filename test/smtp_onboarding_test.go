package test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/service"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSMTPOnboardingReportsConnectionFailureWithoutSaving(t *testing.T) {
	e := setup(t)
	admin := e.user("admin@njiwa.co.tz", "admin")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	r := e.admOK(admin, "POST", "/admin/v1/mail-server/connect", map[string]any{"smtp_host": "127.0.0.1", "smtp_username": "mailbox@example.com", "smtp_password": passSecret, "smtp_port": port})
	if r.JSON["ok"] != false || r.JSON["saved"] != false || !strings.Contains(r.Raw, "SMTP_TCP_REFUSED") {
		t.Fatalf("%s", r.Raw)
	}
	if strings.Contains(r.Raw, passSecret) {
		t.Fatal("password leaked")
	}
	saved := e.admOK(admin, "GET", "/admin/v1/mail-server", nil)
	if saved.JSON["smtp_host"] != "" {
		t.Fatal("a failed connection replaced the saved configuration")
	}
}

func TestSMTPOnboardingOutboundOnly(t *testing.T) {
	e := setup(t)
	admin := e.user("admin@njiwa.co.tz", "admin")
	smtp, pool, port := startSMTP(t, scenario{})
	e.srv.SMTPRootCAs = pool
	r := e.admOK(admin, "POST", "/admin/v1/mail-server/connect", map[string]any{
		"smtp_host": "127.0.0.1", "smtp_username": "mailbox@example.com", "smtp_password": passSecret, "smtp_port": port, "provider_type": "mailcow",
	})
	if r.JSON["ok"] != true || r.JSON["saved"] != true {
		t.Fatalf("%s", r.Raw)
	}
	if smtp.saw("DATA") || !smtp.saw("RCPT TO:<MAILBOX@EXAMPLE.COM>") {
		t.Fatal("sender test must use the mailbox without sending a message")
	}
	saved := e.admOK(admin, "GET", "/admin/v1/mail-server", nil)
	if saved.JSON["test_sender"] != "mailbox@example.com" || saved.JSON["smtp_tls_mode"] != "starttls" || saved.JSON["smtp_password_set"] != true {
		t.Fatalf("%s", saved.Raw)
	}
	if strings.Contains(saved.Raw, passSecret) {
		t.Fatal("password leaked")
	}
	sent := e.admOK(admin, "POST", "/admin/v1/mail-server/test-send", map[string]any{"to": "recipient@example.com"})
	if sent.JSON["state"] != "submitted" || !smtp.saw("DATA") {
		t.Fatalf("%s", sent.Raw)
	}
	// A failed retry cannot overwrite an existing working profile.
	e.srv.SMTPRootCAs = x509.NewCertPool()
	failed := e.admOK(admin, "POST", "/admin/v1/mail-server/connect", map[string]any{"smtp_host": "127.0.0.1", "smtp_username": "other@example.com", "smtp_password": "wrong", "smtp_port": port})
	if failed.JSON["saved"] != false {
		t.Fatalf("%s", failed.Raw)
	}
	saved = e.admOK(admin, "GET", "/admin/v1/mail-server", nil)
	if saved.JSON["smtp_username"] != "mailbox@example.com" {
		t.Fatal("failed test replaced working mailbox")
	}
}

func TestSMTPOnboardingAuthenticationAndSenderFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		sc   scenario
		code string
	}{
		{"authentication", scenario{authFail: true}, mailer.CodeAuthRejected},
		{"sender", scenario{rcpt: "550 5.7.1 sender rejected"}, mailer.CodeSenderRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := setup(t)
			admin := e.user("admin@njiwa.co.tz", "admin")
			smtp, pool, port := startSMTP(t, tc.sc)
			e.srv.SMTPRootCAs = pool
			r := e.admOK(admin, "POST", "/admin/v1/mail-server/connect", map[string]any{"smtp_host": "127.0.0.1", "smtp_username": "mailbox@example.com", "smtp_password": passSecret, "smtp_port": port})
			if r.JSON["saved"] != false || !strings.Contains(r.Raw, tc.code) || smtp.saw("DATA") {
				t.Fatalf("%s", r.Raw)
			}
		})
	}
}

func TestSMTPCertificateDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, detail string
		change       func(*x509.Certificate)
	}{
		{"legacy Common Name", "SAN", func(c *x509.Certificate) { c.IPAddresses = nil; c.Subject.CommonName = "localhost" }},
		{"expired", "expired", func(c *x509.Certificate) { c.NotAfter = time.Now().Add(-time.Minute) }},
		{"wrong hostname", "hostname", func(c *x509.Certificate) { c.IPAddresses = nil; c.DNSNames = []string{"other.example.com"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cert, pool := genCertWith(t, tc.change)
			smtp, _, port := startSMTP(t, scenario{cert: &cert})
			cfg := mailer.SMTPConfig{Host: "127.0.0.1", Port: port, Username: "mailbox@example.com", Password: passSecret, RootCAs: pool, Timeout: 3 * time.Second}
			steps := mailer.Diagnose(context.Background(), cfg, "mailbox@example.com")
			if failedCode(steps) != mailer.CodeTLSCertInvalid || !strings.Contains(steps[2].Detail, tc.detail) {
				t.Fatalf("%+v", steps)
			}
			if smtp.saw("AUTH") {
				t.Fatal("credentials sent before certificate verification")
			}
		})
	}
}

func TestSMTPOnboardingDefaultsPreserveAdvancedSettings(t *testing.T) {
	e := setup(t)
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.admOK(admin, "PUT", "/admin/v1/mail-server", map[string]any{"smtp_host": "mail.example.com", "smtp_port": 465, "smtp_tls_mode": "implicit", "smtp_username": "mailbox@example.com", "smtp_password": passSecret, "provider_type": "mailcow", "inbound_listener": "inbound:2525", "mailcow_api_url": "https://mail.example.com"})
	in, cfg, err := e.svc.PrepareSMTPConnection(context.Background(), service.SMTPConnectInput{Host: "mail.example.com", Username: "mailbox@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 587 || cfg.Implicit || cfg.Password != passSecret || in.InboundListener != "inbound:2525" || in.MailcowAPIURL != "https://mail.example.com" {
		t.Fatal("defaults or advanced settings were lost")
	}
	cfg.Host = "does-not-exist.invalid"
	steps := mailer.Diagnose(context.Background(), cfg, in.TestSender)
	if failedCode(steps) != mailer.CodeDNSNotFound {
		t.Fatalf("%+v", steps)
	}
}

func TestLocalMailcowFactsCompleteDNSWithoutGuessingRemoteSMTP(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.saveSMTP("mail.example.com", "pw")
	e.must(e.svc.CreateProject(ctx, "localfacts"))
	d := e.admOK(admin, "POST", wsPath("localfacts", "/domains"), map[string]any{"name": "notify.example.com", "purpose": "both"})
	path := wsPath("localfacts", "/domains/"+str(d.JSON, "id"))
	key := dkimRecord(t)
	snapshot := map[string]any{"smtp_host": "mail.example.com", "updated_at": time.Now(), "mail_host": "mail.example.com", "egress_ips": []string{"93.184.216.34"}, "dkim_selector": "dkim", "dkim_keys": map[string]string{"notify.example.com": key}}
	file := filepath.Join(t.TempDir(), "facts.json")
	data, _ := json.Marshal(snapshot)
	e.must(os.WriteFile(file, data, 0600))
	e.svc.Opts.LocalMailcowFactsFile = file
	r := e.admOK(admin, "GET", path, nil)
	for _, k := range []string{"spf", "dkim", "mx"} {
		if recordByKey(r, k)["placeholder"] == true {
			t.Fatalf("%s still manual", k)
		}
	}
	if str(recordByKey(r, "spf"), "value") != "v=spf1 ip4:93.184.216.34 -all" || str(recordByKey(r, "dkim"), "value") != key || str(recordByKey(r, "mx"), "value") != "mail.example.com" {
		t.Fatalf("%s", r.Raw)
	}
	e.saveSMTP("remote.example.com", "pw")
	if recordByKey(e.admOK(admin, "GET", path, nil), "spf")["placeholder"] != true {
		t.Fatal("local IP authorized unrelated remote SMTP")
	}
}

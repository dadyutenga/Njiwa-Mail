//go:build managed_e2e

// End-to-end checks against a host where `njiwa-mail installer apply` has run
// with -domains notify.example.test -bounce-domain bounce.example.test
// -selector njiwa and the default -submission/-inbound. Run as root:
//
//	go test -tags managed_e2e ./internal/installer/managed -run E2E -v
//
// It needs nothing else on the network: the bounce domain is relayed to
// 127.0.0.1:2525, where this test listens as the Njiwa inbound listener.
package managed_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/smtp"
	"os"
	"strings"
	"testing"
	"time"

	gosmtp "github.com/emersion/go-smtp"
	"github.com/njiwa-tz/mail/internal/mailer"
)

type sink struct{ got chan []byte }

type sess struct{ s *sink }

func (s *sess) Reset()                                 {}
func (s *sess) Logout() error                          { return nil }
func (s *sess) Mail(string, *gosmtp.MailOptions) error { return nil }
func (s *sess) Rcpt(string, *gosmtp.RcptOptions) error { return nil }
func (s *sess) Data(r io.Reader) error {
	b, err := io.ReadAll(r)
	if err == nil {
		s.s.got <- b
	}
	return err
}

func startSink(t *testing.T) *sink {
	s := &sink{got: make(chan []byte, 4)}
	srv := gosmtp.NewServer(gosmtp.BackendFunc(func(*gosmtp.Conn) (gosmtp.Session, error) { return &sess{s}, nil }))
	srv.Domain = "sink.test" // must differ from the MTA's own name (loop detection)
	ln, err := net.Listen("tcp", "127.0.0.1:2525")
	if err != nil {
		t.Fatalf("the inbound address 127.0.0.1:2525 is busy: %v", err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return s
}

func managedConfig(t *testing.T, withCert bool) mailer.SMTPConfig {
	t.Helper()
	ca, err := os.ReadFile("/etc/njiwa-mail/submission/ca.crt")
	if err != nil {
		t.Skip("installer has not run on this host")
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca)
	cfg := mailer.SMTPConfig{Host: "127.0.0.1", Port: 10587, ServerName: "mailout.example.test", RootCAs: pool, HeloName: "njiwa-worker", Timeout: 15 * time.Second}
	if withCert {
		pair, err := tls.LoadX509KeyPair("/etc/njiwa-mail/submission/client.crt", "/etc/njiwa-mail/submission/client.key")
		if err != nil {
			t.Fatal(err)
		}
		cfg.ClientCert = &pair
	}
	return cfg
}

func message(from, to string) []byte {
	raw, _ := mailer.Build(mailer.Message{FromName: "Njiwa", FromEmail: from, To: to, Subject: "managed e2e",
		Text: "hello from the managed MTA test", MessageID: "e2e-" + time.Now().Format("150405.000") + "@notify.example.test", Date: time.Now()})
	return raw
}

func TestE2ENoOpenRelayOnPort25(t *testing.T) {
	c, err := smtp.Dial("127.0.0.1:25")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Hello("attacker.example"); err != nil {
		t.Fatal(err)
	}
	if err := c.Mail("spammer@attacker.example"); err != nil {
		t.Fatalf("MAIL refused early (fine, but unexpected): %v", err)
	}
	err = c.Rcpt("victim@example.org")
	if err == nil || !strings.HasPrefix(err.Error(), "554") {
		t.Fatalf("port 25 relayed to a third party: %v", err)
	}
	t.Logf("relay attempt refused: %v", err)
}

func TestE2ESubmissionNeedsTheClientCertificate(t *testing.T) {
	res := mailer.NewSMTP(managedConfig(t, false)).Send(context.Background(), mailer.Envelope{From: "bounces@bounce.example.test",
		To: "victim@example.org", Raw: message("hello@notify.example.test", "victim@example.org")})
	if res.Outcome == mailer.Accepted {
		t.Fatal("submission accepted a client without the certificate")
	}
	t.Logf("without certificate: %s %s", res.Outcome, res.Reason)
}

func TestE2EForeignEnvelopeSenderIsRejected(t *testing.T) {
	res := mailer.NewSMTP(managedConfig(t, true)).Send(context.Background(), mailer.Envelope{From: "x@attacker.example",
		To: "user@bounce.example.test", Raw: message("x@attacker.example", "user@bounce.example.test")})
	if res.Outcome != mailer.PermFailure {
		t.Fatalf("foreign envelope sender: %s %s", res.Outcome, res.Reason)
	}
}

func TestE2EAcceptedMailIsDKIMSigned(t *testing.T) {
	s := startSink(t)
	cfg := managedConfig(t, true)
	steps := mailer.Diagnose(context.Background(), cfg, "hello@notify.example.test")
	for _, st := range steps {
		if !st.OK {
			t.Fatalf("diagnosis: %+v", steps)
		}
	}
	res := mailer.NewSMTP(cfg).Send(context.Background(), mailer.Envelope{From: "bounces@bounce.example.test",
		To: "dsn-check@bounce.example.test", Raw: message("hello@notify.example.test", "dsn-check@bounce.example.test")})
	if res.Outcome != mailer.Accepted || !strings.Contains(res.Reply, "queued as") {
		t.Fatalf("submission: %s %s %s", res.Outcome, res.Reason, res.Reply)
	}
	select {
	case got := <-s.got:
		hdr, _, _ := bytes.Cut(got, []byte("\r\n\r\n"))
		h := string(hdr)
		if !strings.Contains(h, "DKIM-Signature:") || !strings.Contains(h, "d=notify.example.test") || !strings.Contains(h, "s=njiwa") {
			t.Fatalf("received message is not signed for notify.example.test/njiwa:\n%s", h)
		}
		t.Logf("received with a DKIM signature for d=notify.example.test s=njiwa (queue reply %q)", res.Reply)
		if path := os.Getenv("NJIWA_E2E_CAPTURE"); path != "" {
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the message did not arrive at the inbound listener")
	}
}

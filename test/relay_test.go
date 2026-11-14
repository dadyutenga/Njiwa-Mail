package test

import (
	"context"
	"crypto/tls"
	"net"
	"net/smtp"
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/relay"
	"github.com/njiwa-tz/mail/internal/service"
)

func startRelay(t *testing.T, e *env) string {
	t.Helper()
	certPEM, keyPEM := pemPair(t, "relay.test")
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	e.must(err)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	e.must(err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srv := &relay.Server{Svc: e.svc, Hostname: "relay.test", TLS: &tls.Config{Certificates: []tls.Certificate{cert}}}
	go func() { _ = srv.Serve(ctx, l) }()
	return l.Addr().String()
}

// relaySend talks SMTP like an application would: STARTTLS, AUTH PLAIN with
// the API key, one message to rcpts.
func relaySend(addr, key, from string, rcpts []string, msg string) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.StartTLS(&tls.Config{InsecureSkipVerify: true}); err != nil {
		return err
	}
	if err := c.Auth(smtp.PlainAuth("", "apikey", key, "127.0.0.1")); err != nil {
		return err
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, r := range rcpts {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(strings.ReplaceAll(msg, "\n", "\r\n"))); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// The relay: an application's SMTP client reaches the same accept path as the
// API. One message per envelope recipient, retries do not send twice, and the
// API's rules (scope, approved sender) still apply.
func TestSMTPRelay(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.provision("legacy")
	key, _, err := e.svc.CreateKey(ctx, "legacy", "sandbox", []string{"emails:send_raw", "emails:read"}, "relay", 0)
	e.must(err)
	addr := startRelay(t, e)
	msg := "From: Legacy App <" + liveFrom + ">\nTo: " + testRcpt + "\nSubject: Invoice 42\nMessage-ID: <inv-42@legacy.example>\n" +
		"X-Invoice: 42\nMIME-Version: 1.0\nContent-Type: text/plain; charset=utf-8\n\nYour invoice is ready.\n"
	rcpts := []string{testRcpt, "other@example.org"}
	if err := relaySend(addr, key, liveFrom, rcpts, msg); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		var n int
		e.must(e.pool.QueryRow(ctx, `SELECT count(*) FROM messages m JOIN projects p ON p.id=m.project_id WHERE p.name='legacy'`).Scan(&n))
		return n
	}
	if n := count(); n != 2 {
		t.Fatalf("messages after one relay send to two recipients: %d", n)
	}
	// The client retries the same message (a timeout on its side): no duplicate.
	if err := relaySend(addr, key, liveFrom, rcpts, msg); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 2 {
		t.Fatalf("retry created messages: %d", n)
	}
	// The message itself went through the accept path: payload sealed,
	// the custom header kept.
	pl := e.payloadOf("legacy", testRcpt)
	if pl.Subject != "Invoice 42" || pl.Headers["X-Invoice"] != "42" || !strings.Contains(pl.Text, "invoice is ready") {
		t.Fatalf("payload %+v", pl)
	}
	// A key without emails:send_raw cannot log in; a wrong key neither.
	tplKey, _, err := e.svc.CreateKey(ctx, "legacy", "sandbox", []string{"emails:send"}, "tpl", 0)
	e.must(err)
	for _, k := range []string{tplKey, "dmk_sbx_nope_nope", ""} {
		if err := relaySend(addr, k, liveFrom, rcpts, msg); err == nil || !strings.Contains(err.Error(), "535") {
			t.Fatalf("key %q: %v", k, err)
		}
	}
	// A sender the project has not approved is refused, permanently.
	bad := strings.Replace(msg, liveFrom, "someone@elsewhere.example", 1)
	bad = strings.Replace(bad, "inv-42", "inv-43", 1)
	if err := relaySend(addr, key, "someone@elsewhere.example", []string{testRcpt}, bad); err == nil || !strings.Contains(err.Error(), "550") {
		t.Fatalf("unapproved sender: %v", err)
	}
	// Without TLS there is no AUTH, so no mail.
	c, err := smtp.Dial(addr)
	e.must(err)
	defer c.Close()
	if ok, _ := c.Extension("AUTH"); ok {
		t.Fatal("AUTH offered without TLS")
	}
	if err := c.Mail(liveFrom); err == nil || !strings.Contains(err.Error(), "530") {
		t.Fatalf("MAIL without auth: %v", err)
	}
}

func TestRelayRequests(t *testing.T) {
	raw := []byte("From: a@example.com\r\nSubject: S\r\nReply-To: r@example.com\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/html\r\n\r\n<p>Hi</p>\r\n" +
		"--b\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"x.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERi0xLjQ=\r\n--b--\r\n")
	reqs, err := relay.Requests(raw, []string{"one@example.org", "two@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 2 || reqs[0].Key == reqs[1].Key || !strings.HasPrefix(reqs[0].Key, "relay:") {
		t.Fatalf("%+v", reqs)
	}
	b := string(reqs[0].Body)
	for _, want := range []string{`"to":"one@example.org"`, `"from":"a@example.com"`, `"reply_to":"r@example.com"`, `"html":"\u003cp\u003eHi\u003c/p\u003e"`, `"filename":"x.pdf"`, `"content_type":"application/pdf"`} {
		if !strings.Contains(b, want) {
			t.Errorf("missing %s in %s", want, b)
		}
	}
	if _, err := relay.Requests([]byte("Subject: no from\r\n\r\nx"), []string{"a@example.org"}); err == nil {
		t.Fatal("a message without From must be refused")
	}
}

// payloadOf opens the sealed payload of project's queued message to rcpt.
func (e *env) payloadOf(project, rcpt string) *service.Payload {
	e.t.Helper()
	var id string
	var enc []byte
	e.must(e.pool.QueryRow(context.Background(), `SELECT m.id::text, m.payload_enc FROM messages m JOIN projects p ON p.id=m.project_id
WHERE p.name=$1 AND m.recipient_hash=$2 LIMIT 1`, project, e.box.MAC("recipient", []byte(rcpt))).Scan(&id, &enc))
	pl, err := e.svc.OpenPayload(service.Claimed{ID: id, Payload: enc})
	e.must(err)
	return pl
}

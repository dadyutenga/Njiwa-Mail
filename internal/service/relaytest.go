package service

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/secure"
)

// RelayStep is one stage of a test through the relay.
type RelayStep struct {
	Key     string `json:"key"`
	Title   string `json:"title"`
	OK      bool   `json:"ok"`
	Skipped bool   `json:"skipped,omitempty"`
	Detail  string `json:"detail"`
}

// RelayTest is the result of sending one Sandbox message through the relay.
type RelayTest struct {
	OK        bool        `json:"ok"`
	Address   string      `json:"address"`    // where the API connected
	TLSName   string      `json:"tls_name"`   // the name the certificate had to match
	MessageID string      `json:"message_id"` // the queued message, when accepted
	Steps     []RelayStep `json:"steps"`
}

// TestRelay opens a real SMTP session to the relay, signs in with a key that
// lives two minutes and is revoked afterwards, and sends one Sandbox message
// from the project's default sender to itself. Sandbox mail is captured: no
// external mail is sent. It proves that the relay answers on the address the
// API uses, not that applications can reach its public address.
func (s *Service) TestRelay(ctx context.Context, sc *Scope, requestHost string) (*RelayTest, error) {
	if sc.Mode != "sandbox" {
		return nil, &Error{Status: 409, Code: "sandbox_only", Message: "the relay test sends a Sandbox message only"}
	}
	var project, from string
	if err := s.Pool.QueryRow(ctx, `SELECT p.name FROM environments e JOIN projects p ON p.id=e.project_id WHERE e.id=$1`, sc.EnvID).Scan(&project); err != nil {
		return nil, unavailable("relay_test", err)
	}
	if err := s.Pool.QueryRow(ctx, `SELECT email FROM sender_identities WHERE environment_id=$1 AND disabled_at IS NULL ORDER BY `+senderPick+` LIMIT 1`, sc.EnvID).Scan(&from); err != nil {
		return nil, &Error{Status: 409, Code: "no_default_sender", Message: "add a Sandbox sender to this project first"}
	}
	name, _, port := s.relayEndpoint(requestHost)
	addr := s.Opts.RelayProbeAddr
	if addr == "" {
		if _, p, err := net.SplitHostPort(s.Opts.RelayAddr); err == nil {
			port = p
		}
		addr = net.JoinHostPort("127.0.0.1", port)
	}
	key, prefix, err := s.CreateKey(ctx, project, "sandbox", []string{ScopeSendRaw}, "relay test (temporary)", 2*time.Minute)
	if err != nil {
		return nil, unavailable("relay_test", err)
	}
	defer func() { _ = s.RevokeKey(context.WithoutCancel(ctx), prefix) }()

	res := &RelayTest{Address: addr, TLSName: name, Steps: []RelayStep{
		{Key: "connect", Title: "Connected to the relay"},
		{Key: "tls", Title: "TLS certificate valid"},
		{Key: "auth", Title: "Signed in with a temporary API key"},
		{Key: "accepted", Title: "Message accepted and queued"},
	}}
	at := 0
	pass := func(detail string) { res.Steps[at].OK, res.Steps[at].Detail = true, detail; at++ }
	fail := func(err error) (*RelayTest, error) {
		var te *textproto.Error
		d := err.Error()
		if errors.As(err, &te) {
			d = mailer.SafeReplyText(te.Error())
		}
		res.Steps[at].Detail = d
		for i := at + 1; i < len(res.Steps); i++ {
			res.Steps[i].Skipped, res.Steps[i].Detail = true, "not run: an earlier step failed"
		}
		return res, nil
	}

	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(dctx, "tcp", addr)
	if err != nil {
		return fail(err)
	}
	defer conn.Close()
	if dl, ok := dctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	tc := &tls.Config{ServerName: name, MinVersion: tls.VersionTLS12, RootCAs: s.Opts.RelayTestRoots}
	var cl *smtp.Client
	if s.Opts.RelayImplicitTLS {
		pass("connected to " + addr)
		t := tls.Client(conn, tc)
		if err := t.HandshakeContext(dctx); err != nil {
			return fail(err)
		}
		if cl, err = smtp.NewClient(t, name); err != nil {
			return fail(err)
		}
		pass("TLS on connect, certificate matches " + name)
	} else {
		if cl, err = smtp.NewClient(conn, name); err != nil {
			return fail(err)
		}
		pass("connected to " + addr)
		if err := cl.StartTLS(tc); err != nil {
			return fail(err)
		}
		pass("STARTTLS, certificate matches " + name)
	}
	defer cl.Close()
	if err := cl.Auth(smtp.PlainAuth("", "apikey", key, name)); err != nil {
		return fail(err)
	}
	pass("AUTH PLAIN accepted")

	if err := cl.Mail(from); err != nil {
		return fail(err)
	}
	if err := cl.Rcpt(from); err != nil {
		return fail(err)
	}
	t := cl.Text
	id, err := t.Cmd("DATA")
	if err != nil {
		return fail(err)
	}
	t.StartResponse(id)
	_, _, err = t.ReadResponse(354)
	t.EndResponse(id)
	if err != nil {
		return fail(err)
	}
	w := t.DotWriter()
	msg := "From: " + from + "\r\nTo: " + from + "\r\nSubject: Njiwa-Mail relay test\r\nMessage-ID: <relay-test-" + secure.NewUUID() + "@njiwa-mail>\r\n" +
		"Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nSent by the SMTP relay test. Sandbox mail is captured, not delivered.\r\n"
	if _, err := w.Write([]byte(msg)); err != nil {
		return fail(err)
	}
	if err := w.Close(); err != nil {
		return fail(err)
	}
	_, reply, err := t.ReadResponse(250)
	if err != nil {
		return fail(err)
	}
	if i := strings.Index(reply, "Queued as "); i >= 0 {
		res.MessageID = strings.TrimSpace(strings.SplitN(reply[i+len("Queued as "):], ",", 2)[0])
	}
	var queued bool
	if isUUID(res.MessageID) {
		_ = s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE id=$1::uuid AND environment_id=$2)`, res.MessageID, sc.EnvID).Scan(&queued)
	}
	if !queued {
		return fail(errors.New("the server accepted the message, but it is not in this project's queue: is this address another server?"))
	}
	pass("queued as " + res.MessageID)
	res.OK = true
	_ = cl.Quit()
	return res, nil
}

package mailer

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Step is one check of a connection test. Detail is safe to show an operator:
// it never contains the password.
type Step struct {
	Key     string `json:"key"`
	Title   string `json:"title"`
	OK      bool   `json:"ok"`
	Skipped bool   `json:"skipped,omitempty"`
	Detail  string `json:"detail"`
}

// Diagnose runs the connection checks one by one and stops at the first
// failure: DNS, TCP, TLS (verified certificate), login, and, when sender is
// set, whether the server lets this login send as sender. No mail is sent:
// the sender check issues MAIL FROM and RCPT TO, then RSET.
func Diagnose(ctx context.Context, cfg SMTPConfig, sender string) []Step {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}
	if cfg.HeloName == "" {
		cfg.HeloName = "localhost"
	}
	steps := []Step{
		{Key: "dns", Title: "DNS resolves"},
		{Key: "tcp", Title: "TCP connects"},
		{Key: "tls", Title: "TLS certificate valid"},
		{Key: "login", Title: "Login accepted"},
		{Key: "sender", Title: "Sender allowed"},
	}
	stop := func(i int, detail string) []Step {
		steps[i].Detail = detail
		for j := i + 1; j < len(steps); j++ {
			steps[j].Skipped, steps[j].Detail = true, "not run: an earlier check failed"
		}
		return steps
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	addrs, err := net.DefaultResolver.LookupHost(ctx, cfg.Host)
	if err != nil || len(addrs) == 0 {
		return stop(0, "the host name "+cfg.Host+" does not resolve")
	}
	steps[0].OK, steps[0].Detail = true, strings.Join(addrs, ", ")

	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	if err != nil {
		return stop(1, "cannot connect to port "+strconv.Itoa(cfg.Port)+": check the port, the firewall, and that the server is running")
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}
	steps[1].OK, steps[1].Detail = true, "connected to "+conn.RemoteAddr().String()

	s := &SMTP{cfg: cfg}
	var c net.Conn = conn
	if cfg.Implicit {
		tc := tls.Client(conn, s.tlsConfig())
		if err := tc.HandshakeContext(ctx); err != nil {
			return stop(2, tlsDetail(err))
		}
		c = tc
	}
	cl, err := smtp.NewClient(c, cfg.Host)
	if err != nil {
		if cfg.Implicit {
			return stop(2, "no SMTP greeting after TLS")
		}
		return stop(2, "no SMTP greeting: is this an implicit-TLS port (465)? Then choose implicit TLS")
	}
	defer cl.Close()
	if err := cl.Hello(cfg.HeloName); err != nil {
		return stop(2, "EHLO refused")
	}
	if !cfg.Implicit {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return stop(2, "the server does not offer STARTTLS on this port")
		}
		if err := cl.StartTLS(s.tlsConfig()); err != nil {
			return stop(2, tlsDetail(err))
		}
	}
	state, _ := cl.TLSConnectionState()
	steps[2].OK, steps[2].Detail = true, tls.VersionName(state.Version)
	if len(state.PeerCertificates) > 0 {
		cert := state.PeerCertificates[0]
		steps[2].Detail += ", certificate for " + strings.Join(cert.DNSNames, ", ") + ", valid until " + cert.NotAfter.UTC().Format("2006-01-02")
	}

	if cfg.Username == "" {
		return stop(3, "no username is set")
	}
	if err := cl.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
		return stop(3, "the server refused the username or password ("+replyCode(err)+")")
	}
	steps[3].OK, steps[3].Detail = true, "logged in as "+cfg.Username

	if sender == "" {
		steps[4].Skipped, steps[4].Detail = true, "no test sender address is set"
		return steps
	}
	if err := cl.Mail(sender); err != nil {
		return stop(4, "MAIL FROM "+sender+" refused ("+replyCode(err)+"): allow this login to send as "+sender+" in Mailcow")
	}
	domain := sender[strings.LastIndexByte(sender, '@')+1:]
	if err := cl.Rcpt("postmaster@" + domain); err != nil {
		return stop(4, "RCPT refused ("+replyCode(err)+"): Mailcow rejects "+sender+" for this login; add it under the mailbox's sender ACL")
	}
	_ = cl.Reset()
	_ = cl.Quit()
	steps[4].OK, steps[4].Detail = true, sender+" accepted (no message was sent)"
	return steps
}

func tlsDetail(err error) string {
	r := classifyPre(err, "tls")
	if r.Reason == "smtp_tls_verification_failed" {
		return "the certificate is not valid for this host name or is not trusted: " + err.Error()
	}
	return "TLS handshake failed"
}

func replyCode(err error) string {
	var te interface{ Error() string }
	if errors.As(err, &te) {
		msg := te.Error()
		if len(msg) > 120 {
			msg = msg[:120]
		}
		return msg
	}
	return "error"
}

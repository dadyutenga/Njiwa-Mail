package mailer

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"syscall"
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
	// Code is a stable, provider-neutral problem code for a failed step.
	Code string `json:"code,omitempty"`
}

// Stable problem codes reported by Diagnose.
const (
	CodeDNSNotFound      = "SMTP_DNS_NOT_FOUND"
	CodeHostForbidden    = "SMTP_HOST_FORBIDDEN"
	CodeTCPRefused       = "SMTP_TCP_REFUSED"
	CodeTCPTimeout       = "SMTP_TCP_TIMEOUT"
	CodeTLSModeMismatch  = "SMTP_TLS_MODE_MISMATCH"
	CodeSTARTTLSMissing  = "SMTP_STARTTLS_UNSUPPORTED"
	CodeTLSCertInvalid   = "SMTP_TLS_CERT_INVALID"
	CodeTLSFailed        = "SMTP_TLS_FAILED"
	CodeAuthMissing      = "SMTP_AUTH_MISSING"
	CodeAuthRejected     = "SMTP_AUTH_REJECTED"
	CodeSenderRejected   = "SMTP_SENDER_REJECTED"
	CodeGreetingMissing  = "SMTP_NO_GREETING"
	CodeConnectionFailed = "SMTP_CONNECTION_FAILED"
	// CodeServerTempRefused: the server answered a 4xx (for example 454 to
	// STARTTLS). A managed Postfix does this when its DKIM signer is down: it
	// refuses mail rather than send it unsigned.
	CodeServerTempRefused = "SMTP_SERVER_TEMP_REFUSED"
)

// forbiddenTarget reports addresses a connection test must never reach:
// link-local (including the 169.254.169.254 cloud metadata service),
// multicast and unspecified. Loopback and private ranges stay allowed, since a
// mail server on the same host or private network is a normal deployment.
func forbiddenTarget(ip net.IP) bool {
	return ip == nil || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast()
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
	stop := func(i int, code, detail string) []Step {
		steps[i].Detail, steps[i].Code = detail, code
		for j := i + 1; j < len(steps); j++ {
			steps[j].Skipped, steps[j].Detail = true, "not run: an earlier check failed"
		}
		return steps
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	addrs, err := net.DefaultResolver.LookupHost(ctx, cfg.Host)
	if err != nil || len(addrs) == 0 {
		return stop(0, CodeDNSNotFound, "the host name "+cfg.Host+" does not resolve")
	}
	for _, a := range addrs {
		if forbiddenTarget(net.ParseIP(a)) {
			return stop(0, CodeHostForbidden, cfg.Host+" resolves to "+a+", a link-local or multicast address (such as a cloud metadata service). Use the mail server's real host name.")
		}
	}
	steps[0].OK, steps[0].Detail = true, strings.Join(addrs, ", ")

	// Dial the address just vetted, not the name again, so a DNS answer that
	// changes between the check and the connection cannot redirect the test.
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(addrs[0], strconv.Itoa(cfg.Port)))
	if err != nil {
		port := strconv.Itoa(cfg.Port)
		if errors.Is(err, syscall.ECONNREFUSED) {
			return stop(1, CodeTCPRefused, "connection to port "+port+" was refused: nothing is listening there. Check the port and that the server is running")
		}
		if isTimeout(err) {
			return stop(1, CodeTCPTimeout, "no answer on port "+port+": a firewall or the hosting provider may block it")
		}
		return stop(1, CodeConnectionFailed, "cannot connect to port "+port+": check the port, the firewall, and that the server is running")
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
			if strings.Contains(err.Error(), "first record does not look like a TLS handshake") {
				return stop(2, CodeTLSModeMismatch, "the server answered without TLS: this port expects STARTTLS. Choose STARTTLS (usually port 587)")
			}
			code, detail := tlsDetail(err)
			return stop(2, code, detail)
		}
		c = tc
	}
	cl, err := smtp.NewClient(c, cfg.Host)
	if err != nil {
		if cfg.Implicit {
			return stop(2, CodeGreetingMissing, "no SMTP greeting after TLS")
		}
		return stop(2, CodeTLSModeMismatch, "no SMTP greeting: this looks like an implicit-TLS port. Choose TLS (usually port 465)")
	}
	defer cl.Close()
	if err := cl.Hello(cfg.HeloName); err != nil {
		return stop(2, CodeGreetingMissing, "EHLO refused")
	}
	if !cfg.Implicit {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return stop(2, CodeSTARTTLSMissing, "the server does not offer STARTTLS on this port; Njiwa-Mail never sends credentials without TLS")
		}
		if err := cl.StartTLS(s.tlsConfig()); err != nil {
			code, detail := tlsDetail(err)
			return stop(2, code, detail)
		}
	}
	state, _ := cl.TLSConnectionState()
	steps[2].OK, steps[2].Detail = true, tls.VersionName(state.Version)
	if len(state.PeerCertificates) > 0 {
		cert := state.PeerCertificates[0]
		steps[2].Detail += ", certificate for " + strings.Join(cert.DNSNames, ", ") + ", valid until " + cert.NotAfter.UTC().Format("2006-01-02")
	}

	switch {
	case cfg.Username == "" && cfg.ClientCert != nil:
		// Mutual TLS: the server verified the client certificate during the
		// handshake; whether it grants relaying shows in the sender step.
		steps[3].OK, steps[3].Detail = true, "authenticated with the client certificate"
	case cfg.Username == "":
		return stop(3, CodeAuthMissing, "no username is set")
	default:
		if err := cl.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return stop(3, CodeAuthRejected, "the server refused the username or password ("+replyCode(err)+")")
		}
		steps[3].OK, steps[3].Detail = true, "logged in as "+cfg.Username
	}

	if sender == "" {
		steps[4].Skipped, steps[4].Detail = true, "no test sender address is set"
		return steps
	}
	if err := cl.Mail(sender); err != nil {
		return stop(4, CodeSenderRejected, "MAIL FROM "+sender+" refused ("+replyCode(err)+"): allow this login to send as "+sender+" on your mail server")
	}
	domain := sender[strings.LastIndexByte(sender, '@')+1:]
	if err := cl.Rcpt("postmaster@" + domain); err != nil {
		return stop(4, CodeSenderRejected, "RCPT refused ("+replyCode(err)+"): your mail server rejects "+sender+" for this login; allow the login to send as this address")
	}
	_ = cl.Reset()
	_ = cl.Quit()
	steps[4].OK, steps[4].Detail = true, sender+" accepted (no message was sent)"
	return steps
}

func tlsDetail(err error) (string, string) {
	var te *textproto.Error
	if errors.As(err, &te) && te.Code >= 400 && te.Code < 500 {
		return CodeServerTempRefused, "the server answered " + strconv.Itoa(te.Code) + " " + te.Msg +
			": it is temporarily refusing mail. On a managed host, check that the DKIM signer (opendkim) is running"
	}
	r := classifyPre(err, "tls")
	if r.Reason == "smtp_tls_verification_failed" {
		return CodeTLSCertInvalid, "the certificate is not valid for this host name or is not trusted: " + err.Error()
	}
	return CodeTLSFailed, "TLS handshake failed"
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

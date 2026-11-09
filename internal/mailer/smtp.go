package mailer

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// Outcome is an honest classification of one submission attempt.
type Outcome int

const (
	// Accepted: the server returned a 2xx reply after end-of-data.
	Accepted Outcome = iota
	// TempFailure: proven NOT accepted; a retry cannot duplicate the message.
	TempFailure
	// PermFailure: proven NOT accepted and will not succeed on retry.
	PermFailure
	// Unknown: the message body was sent but no definitive reply was read.
	// The server may have accepted it. Never retry automatically.
	Unknown
)

func (o Outcome) String() string {
	return [...]string{"accepted", "temp_failure", "perm_failure", "unknown"}[o]
}

type Result struct {
	Outcome Outcome
	Reason  string // safe, stable code
	Reply   string // server reply text on acceptance (queue id etc.)
}

type Envelope struct {
	From, To   string
	Recipients []string // To, Cc and Bcc. Empty means To alone.
	Raw        []byte
}

// Transport submits a message. Implementations must classify honestly.
type Transport interface {
	Send(ctx context.Context, e Envelope) Result
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	Implicit bool
	HeloName string
	Timeout  time.Duration
	// RootCAs lets tests trust a private CA. Verification is always on; there
	// is deliberately no option to skip it.
	RootCAs *x509.CertPool
}

type SMTP struct{ cfg SMTPConfig }

func NewSMTP(cfg SMTPConfig) *SMTP {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 40 * time.Second
	}
	if cfg.HeloName == "" {
		cfg.HeloName = "localhost"
	}
	return &SMTP{cfg: cfg}
}

func (s *SMTP) tlsConfig() *tls.Config {
	return &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12, RootCAs: s.cfg.RootCAs}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// classifyPre classifies a failure that happened BEFORE any message body was
// transmitted: the message cannot have been accepted.
func classifyPre(err error, stage string) Result {
	var te *textproto.Error
	if errors.As(err, &te) {
		code := strconv.Itoa(te.Code)
		switch {
		case te.Code >= 400 && te.Code < 500:
			return Result{Outcome: TempFailure, Reason: "smtp_" + code}
		case stage == "auth":
			return Result{Outcome: PermFailure, Reason: "smtp_auth_failed"}
		default:
			return Result{Outcome: PermFailure, Reason: "smtp_" + code}
		}
	}
	var ce *tls.CertificateVerificationError
	var ua x509.UnknownAuthorityError
	var hn x509.HostnameError
	var ci x509.CertificateInvalidError
	if errors.As(err, &ce) || errors.As(err, &ua) || errors.As(err, &hn) || errors.As(err, &ci) {
		return Result{Outcome: PermFailure, Reason: "smtp_tls_verification_failed"}
	}
	if stage == "tls" {
		return Result{Outcome: TempFailure, Reason: "smtp_tls_handshake_failed"}
	}
	if isTimeout(err) {
		return Result{Outcome: TempFailure, Reason: "smtp_timeout"}
	}
	if stage == "connect" {
		return Result{Outcome: TempFailure, Reason: "smtp_connect_failed"}
	}
	return Result{Outcome: TempFailure, Reason: "smtp_io_error"}
}

func (s *SMTP) dial(ctx context.Context) (*smtp.Client, net.Conn, *Result) {
	d := net.Dialer{Timeout: 10 * time.Second}
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		r := classifyPre(err, "connect")
		return nil, nil, &r
	}
	dl := time.Now().Add(s.cfg.Timeout)
	if cd, ok := ctx.Deadline(); ok && cd.Before(dl) {
		dl = cd
	}
	conn.SetDeadline(dl)
	var c net.Conn = conn
	if s.cfg.Implicit {
		tc := tls.Client(conn, s.tlsConfig())
		if err := tc.HandshakeContext(ctx); err != nil {
			conn.Close()
			r := classifyPre(err, "tls")
			return nil, nil, &r
		}
		c = tc
	}
	cl, err := smtp.NewClient(c, s.cfg.Host)
	if err != nil {
		conn.Close()
		r := classifyPre(err, "greeting")
		return nil, nil, &r
	}
	if err := cl.Hello(s.cfg.HeloName); err != nil {
		cl.Close()
		r := classifyPre(err, "ehlo")
		return nil, nil, &r
	}
	if !s.cfg.Implicit {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			cl.Close()
			return nil, nil, &Result{Outcome: PermFailure, Reason: "smtp_starttls_unsupported"}
		}
		if err := cl.StartTLS(s.tlsConfig()); err != nil {
			cl.Close()
			r := classifyPre(err, "tls")
			return nil, nil, &r
		}
	}
	if s.cfg.Username != "" {
		if err := cl.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
			cl.Close()
			r := classifyPre(err, "auth")
			return nil, nil, &r
		}
	}
	return cl, conn, nil
}

// Check connects, negotiates verified TLS and authenticates without sending mail.
func (s *SMTP) Check(ctx context.Context) error {
	cl, _, r := s.dial(ctx)
	if r != nil {
		return fmt.Errorf("smtp check failed: %s", r.Reason)
	}
	defer cl.Close()
	return cl.Quit()
}

// Send submits one message and classifies the outcome conservatively.
func (s *SMTP) Send(ctx context.Context, e Envelope) Result {
	cl, _, r := s.dial(ctx)
	if r != nil {
		return *r
	}
	defer cl.Close()

	if err := cl.Mail(e.From); err != nil {
		return classifyPre(err, "mail")
	}
	rcpts := e.Recipients
	if len(rcpts) == 0 {
		rcpts = []string{e.To}
	}
	for _, rcpt := range rcpts {
		if err := cl.Rcpt(rcpt); err != nil {
			return classifyPre(err, "rcpt")
		}
	}

	// DATA command. A failure here means no body has been sent yet.
	t := cl.Text
	id, err := t.Cmd("DATA")
	if err != nil {
		return classifyPre(err, "data")
	}
	t.StartResponse(id)
	_, _, err = t.ReadResponse(354)
	t.EndResponse(id)
	if err != nil {
		return classifyPre(err, "data")
	}

	// From here the server may accept the message. Anything that is not a
	// definitive SMTP reply is Unknown.
	w := t.DotWriter()
	if _, err := w.Write(e.Raw); err != nil {
		return Result{Outcome: Unknown, Reason: "smtp_write_failed"}
	}
	if err := w.Close(); err != nil {
		return Result{Outcome: Unknown, Reason: "smtp_write_failed"}
	}
	id = t.Next()
	t.StartRequest(id) // the body was written via DotWriter; close out this pipeline slot's request half
	t.EndRequest(id)
	t.StartResponse(id)
	code, msg, err := t.ReadResponse(2)
	t.EndResponse(id)
	if err != nil {
		var te *textproto.Error
		if errors.As(err, &te) {
			switch {
			case te.Code >= 400 && te.Code < 500:
				return Result{Outcome: TempFailure, Reason: "smtp_" + strconv.Itoa(te.Code)}
			case te.Code >= 500:
				return Result{Outcome: PermFailure, Reason: "smtp_" + strconv.Itoa(te.Code)}
			}
		}
		return Result{Outcome: Unknown, Reason: "smtp_ack_lost"}
	}
	reply := strings.TrimSpace(strings.Join(strings.Fields(msg), " "))
	if len(reply) > 200 {
		reply = reply[:200]
	}
	cl.Quit() // best effort; acceptance already acknowledged
	return Result{Outcome: Accepted, Reason: "smtp_" + strconv.Itoa(code), Reply: reply}
}

// Package relay is an SMTP submission server for applications that cannot
// use the HTTP API yet. A client authenticates with AUTH PLAIN over TLS, the
// password being a Njiwa-Mail API key with scope emails:send_raw, and each
// envelope recipient becomes one message through the same accept path as
// POST /v1/emails: the approved-sender, domain, suppression, allowlist,
// rate-limit and idempotency rules all apply. Retrying a message is safe:
// the idempotency key comes from its Message-ID and recipient.
package relay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/jhillyerd/enmime/v2"
	"github.com/njiwa-tz/mail/internal/service"
)

// DefaultMaxBytes is the largest message accepted (the API's own limit).
const DefaultMaxBytes = 6 << 20

// MaxRecipients per message, as for the API (to, cc and bcc together).
const MaxRecipients = 10

// Server is the relay listener.
type Server struct {
	Svc      *service.Service
	Addr     string
	Hostname string
	// TLS is required: AUTH is offered only on a TLS connection. Implicit
	// serves TLS from the first byte (port 465); otherwise STARTTLS (587).
	TLS      *tls.Config
	Implicit bool
	MaxBytes int64
	// Insecure allows AUTH without TLS. Tests only.
	Insecure bool
}

func (s *Server) smtpServer() *smtp.Server {
	if s.MaxBytes <= 0 {
		s.MaxBytes = DefaultMaxBytes
	}
	srv := smtp.NewServer(smtp.BackendFunc(func(c *smtp.Conn) (smtp.Session, error) {
		ip := ""
		if a, ok := c.Conn().RemoteAddr().(*net.TCPAddr); ok {
			ip = a.IP.String()
		}
		return &session{s: s, ip: ip}, nil
	}))
	srv.Domain = s.Hostname
	if srv.Domain == "" {
		srv.Domain = "njiwa-mail-relay"
	}
	srv.TLSConfig = s.TLS
	srv.AllowInsecureAuth = s.Insecure
	srv.MaxMessageBytes = s.MaxBytes
	srv.MaxRecipients = MaxRecipients
	srv.ReadTimeout = 60 * time.Second
	srv.WriteTimeout = 60 * time.Second
	return srv
}

// Serve accepts connections on l until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	if s.TLS == nil && !s.Insecure {
		return errors.New("relay: a TLS certificate is required (RELAY_TLS_CERT, RELAY_TLS_KEY)")
	}
	if s.Implicit {
		l = tls.NewListener(l, s.TLS)
	}
	srv := s.smtpServer()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	slog.Info("smtp relay listening", "addr", l.Addr().String(), "implicit_tls", s.Implicit)
	err := srv.Serve(l)
	if errors.Is(err, smtp.ErrServerClosed) || ctx.Err() != nil {
		return nil
	}
	return err
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	l, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	return s.Serve(ctx, l)
}

func reply(code int, enh smtp.EnhancedCode, msg string) error {
	return &smtp.SMTPError{Code: code, EnhancedCode: enh, Message: msg}
}

var (
	errAuthRequired = reply(530, smtp.EnhancedCode{5, 7, 0}, "Authentication required: AUTH PLAIN with an API key as the password")
	errAuthFailed   = reply(535, smtp.EnhancedCode{5, 7, 8}, "Authentication failed")
)

type session struct {
	s        *Server
	ip       string
	p        *service.Principal
	failures int
	rcpts    []string
}

func (x *session) AuthMechanisms() []string { return []string{sasl.Plain} }

func (x *session) Auth(mech string) (sasl.Server, error) {
	if mech != sasl.Plain {
		return nil, smtp.ErrAuthUnknownMechanism
	}
	return sasl.NewPlainServer(func(_, _, password string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		p, err := x.s.Svc.Authenticate(ctx, "Bearer "+password)
		if err == nil {
			err = p.AllowIP(x.ip)
		}
		if err == nil && !p.HasScope(service.ScopeSendRaw) {
			err = errors.New("missing scope")
		}
		if err != nil {
			x.failures++
			time.Sleep(time.Duration(x.failures) * 500 * time.Millisecond)
			return errAuthFailed
		}
		x.p = p
		return nil
	}), nil
}

func (x *session) Mail(_ string, _ *smtp.MailOptions) error {
	if x.p == nil {
		return errAuthRequired
	}
	return nil
}

func (x *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	if x.p == nil {
		return errAuthRequired
	}
	addr, ok := service.NormalizeEmail(to)
	if !ok {
		return reply(553, smtp.EnhancedCode{5, 1, 3}, "Invalid recipient address")
	}
	x.rcpts = append(x.rcpts, addr)
	return nil
}

func (x *session) Data(r io.Reader) error {
	if x.p == nil {
		return errAuthRequired
	}
	raw, err := io.ReadAll(io.LimitReader(r, x.s.MaxBytes+1))
	if err != nil {
		return reply(451, smtp.EnhancedCode{4, 3, 0}, "Could not read the message")
	}
	if int64(len(raw)) > x.s.MaxBytes {
		return reply(552, smtp.EnhancedCode{5, 3, 4}, "Message too large")
	}
	reqs, err := Requests(raw, x.rcpts)
	if err != nil {
		return reply(554, smtp.EnhancedCode{5, 6, 0}, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var ids []string
	for _, rq := range reqs {
		acc, err := x.s.Svc.Accept(ctx, x.p, rq.Key, rq.Body)
		if err != nil {
			return smtpError(err)
		}
		ids = append(ids, acc.ID)
	}
	slog.Info("relay accepted", "key", x.p.KeyPrefix, "messages", len(ids))
	return &smtp.SMTPError{Code: 250, EnhancedCode: smtp.EnhancedCode{2, 0, 0}, Message: "Queued as " + strings.Join(ids, ",")}
}

func (x *session) Reset()        { x.rcpts = nil }
func (x *session) Logout() error { return nil }

// smtpError maps an accept error to an SMTP reply: a limit or an outage is
// temporary (the client retries; accepted recipients are not sent twice),
// anything else is permanent and names the error code.
func smtpError(err error) error {
	var e *service.Error
	if !errors.As(err, &e) {
		return reply(451, smtp.EnhancedCode{4, 3, 0}, "Temporary failure, try again")
	}
	switch {
	case e.Status == 429:
		return reply(451, smtp.EnhancedCode{4, 7, 0}, "Rate limited ("+e.Code+"), try again later")
	case e.Status >= 500:
		return reply(451, smtp.EnhancedCode{4, 3, 0}, "Temporarily unavailable ("+e.Code+"), try again later")
	case e.Code == "sender_not_approved" || strings.HasPrefix(e.Code, "domain_"):
		return reply(550, smtp.EnhancedCode{5, 7, 1}, "Sender not allowed ("+e.Code+")")
	case strings.HasPrefix(e.Code, "recipient_"):
		return reply(550, smtp.EnhancedCode{5, 1, 1}, "Recipient refused ("+e.Code+")")
	}
	return reply(554, smtp.EnhancedCode{5, 6, 0}, "Message refused ("+e.Code+")")
}

// Request is one accept call: an idempotency key and a POST /v1/emails body.
type Request struct {
	Key  string
	Body []byte
}

var customHeaderRe = regexp.MustCompile(`^X-[A-Za-z0-9-]{1,40}$`)

// Requests turns a relayed message into one send per envelope recipient.
// Cc and Bcc are envelope recipients like any other, so each one gets its
// own copy (and its own status).
func Requests(raw []byte, rcpts []string) ([]Request, error) {
	if len(rcpts) == 0 {
		return nil, errors.New("no recipients")
	}
	env, err := enmime.ReadEnvelope(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("message could not be parsed")
	}
	from, err := mail.ParseAddress(env.GetHeader("From"))
	if err != nil {
		return nil, errors.New("the From header has no valid address")
	}
	base := map[string]any{"from": from.Address, "subject": env.GetHeader("Subject")}
	if env.Text != "" {
		base["text"] = env.Text
	}
	if env.HTML != "" {
		base["html"] = env.HTML
	}
	if rt := env.GetHeader("Reply-To"); rt != "" {
		if a, err := mail.ParseAddress(rt); err == nil {
			base["reply_to"] = a.Address
		}
	}
	hdrs := map[string]string{}
	for _, k := range env.GetHeaderKeys() {
		if customHeaderRe.MatchString(k) && len(hdrs) < 10 {
			hdrs[k] = env.GetHeader(k)
		}
	}
	if len(hdrs) > 0 {
		base["headers"] = hdrs
	}
	var atts []map[string]string
	for _, p := range append(append([]*enmime.Part{}, env.Attachments...), env.Inlines...) {
		ct := strings.ToLower(strings.TrimSpace(strings.SplitN(p.ContentType, ";", 2)[0]))
		name := p.FileName
		if name == "" {
			name = fmt.Sprintf("attachment-%d", len(atts)+1)
		}
		atts = append(atts, map[string]string{"filename": name, "content_type": ct, "content": base64.StdEncoding.EncodeToString(p.Content)})
	}
	if len(atts) > 0 {
		base["attachments"] = atts
	}
	// Same Message-ID and recipient, same key: a client's retry is not a
	// second message. Without a Message-ID the content identifies it.
	id := strings.TrimSpace(env.GetHeader("Message-Id"))
	if id == "" {
		id = string(raw)
	}
	idSum := sha256.Sum256([]byte(id))
	out := make([]Request, 0, len(rcpts))
	for _, to := range rcpts {
		m := map[string]any{"to": to}
		for k, v := range base {
			m[k] = v
		}
		body, _ := json.Marshal(m)
		rs := sha256.Sum256([]byte(to))
		out = append(out, Request{Key: "relay:" + hex.EncodeToString(idSum[:16]) + ":" + hex.EncodeToString(rs[:6]), Body: body})
	}
	return out, nil
}

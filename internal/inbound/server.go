// Package inbound is the private SMTP listener that receives mail relayed by
// Mailcow (replies to in.<domain> and DSNs to the bounce address), and the
// processor that parses what was received.
//
// The listener must only ever be reachable on the private network between
// Mailcow and this service. It has no TLS and no authentication of its own:
// Mailcow keeps port 25, TLS and spam filtering.
package inbound

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ditronics-tz/mail/internal/secure"
	"github.com/ditronics-tz/mail/internal/service"
	"github.com/emersion/go-smtp"
)

const (
	DefaultMaxBytes       = 10 << 20
	DefaultMaxRecipients  = 20
	DefaultMaxAttachments = 10
)

type Server struct {
	Svc  *service.Service
	Addr string
	// AllowedSources are the only peers accepted: the Mailcow host address(es).
	// Empty refuses everyone, so a misconfiguration fails closed.
	AllowedSources []*net.IPNet
	Hostname       string
	MaxBytes       int64
	MaxRecipients  int
	MaxAttachments int
}

// ParseSources parses a comma-separated list of IPs and CIDR ranges.
func ParseSources(list string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, f := range strings.Split(list, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !strings.Contains(f, "/") {
			if ip := net.ParseIP(f); ip == nil {
				return nil, fmt.Errorf("invalid source address %q", f)
			} else if ip.To4() != nil {
				f += "/32"
			} else {
				f += "/128"
			}
		}
		_, n, err := net.ParseCIDR(f)
		if err != nil {
			return nil, fmt.Errorf("invalid source range %q", f)
		}
		out = append(out, n)
	}
	return out, nil
}

func (s *Server) allowed(addr net.Addr) bool {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		return false
	}
	for _, n := range s.AllowedSources {
		if n.Contains(tcp.IP) {
			return true
		}
	}
	return false
}

// filterListener closes connections from any peer that is not an allowed
// source before a single SMTP byte is exchanged.
type filterListener struct {
	net.Listener
	s *Server
}

func (l filterListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if l.s.allowed(c.RemoteAddr()) {
			return c, nil
		}
		slog.Warn("inbound connection refused: source not allowed", "remote", c.RemoteAddr().String())
		c.Close()
	}
}

func (s *Server) smtpServer() *smtp.Server {
	if s.MaxBytes <= 0 {
		s.MaxBytes = DefaultMaxBytes
	}
	if s.MaxRecipients <= 0 {
		s.MaxRecipients = DefaultMaxRecipients
	}
	if s.MaxAttachments <= 0 {
		s.MaxAttachments = DefaultMaxAttachments
	}
	srv := smtp.NewServer(smtp.BackendFunc(func(c *smtp.Conn) (smtp.Session, error) {
		return &session{s: s}, nil
	}))
	srv.Domain = s.Hostname
	if srv.Domain == "" {
		srv.Domain = "ditronics-mail-inbound"
	}
	srv.MaxMessageBytes = s.MaxBytes
	srv.MaxRecipients = s.MaxRecipients
	srv.ReadTimeout = 60 * time.Second
	srv.WriteTimeout = 60 * time.Second
	return srv
}

// Serve accepts connections on l until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	if err := os.MkdirAll(filepath.Dir(s.Svc.SpoolFile("x")), 0o700); err != nil {
		return err
	}
	srv := s.smtpServer()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	slog.Info("inbound smtp listening", "addr", l.Addr().String(), "allowed_sources", len(s.AllowedSources))
	err := srv.Serve(filterListener{Listener: l, s: s})
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

func perm(code int, enh smtp.EnhancedCode, msg string) error {
	return &smtp.SMTPError{Code: code, EnhancedCode: enh, Message: msg}
}

var errTemp = &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "Temporary failure, try again later"}

type session struct {
	s    *Server
	from string
	to   []string
	kind string
}

func (se *session) Reset()        { se.from, se.to, se.kind = "", nil, "" }
func (se *session) Logout() error { return nil }

func (se *session) Mail(from string, _ *smtp.MailOptions) error {
	se.Reset()
	se.from = from // empty for DSNs (null reverse-path)
	return nil
}

// Rcpt rejects unknown recipients with a permanent error so Mailcow bounces
// them and nothing is stored for addresses nobody owns.
func (se *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	kind, err := se.s.Svc.ResolveRecipient(ctx, to)
	if err != nil {
		slog.Error("inbound recipient lookup failed", "err", err)
		return errTemp
	}
	if kind == "" {
		return perm(550, smtp.EnhancedCode{5, 1, 1}, "No such recipient here")
	}
	if se.kind != "" && se.kind != kind {
		// Bounces and ordinary mail are handled differently; make the sender
		// deliver them in separate transactions.
		return &smtp.SMTPError{Code: 452, EnhancedCode: smtp.EnhancedCode{4, 5, 3}, Message: "Deliver this recipient separately"}
	}
	if len(se.to) >= se.s.MaxRecipients {
		return perm(552, smtp.EnhancedCode{5, 5, 3}, "Too many recipients")
	}
	se.kind = kind
	se.to = append(se.to, strings.ToLower(to))
	return nil
}

// Data writes the raw message to disk, syncs it, records it in the spool
// table and only then lets the server answer 250. Nothing is parsed in a way
// that could lose mail: the attachment-count check tolerates a parser fault.
func (se *session) Data(r io.Reader) error {
	if len(se.to) == 0 {
		return perm(554, smtp.EnhancedCode{5, 5, 1}, "No valid recipients")
	}
	raw, err := io.ReadAll(io.LimitReader(r, se.s.MaxBytes+1))
	if err != nil {
		var se2 *smtp.SMTPError
		if errors.As(err, &se2) {
			return err // includes the server's own 552 for oversize data
		}
		if errors.Is(err, smtp.ErrTooLongLine) {
			return perm(554, smtp.EnhancedCode{5, 6, 0}, "Line too long")
		}
		return errTemp
	}
	if int64(len(raw)) > se.s.MaxBytes {
		return perm(552, smtp.EnhancedCode{5, 3, 4}, "Message too large")
	}
	if se.kind == service.KindInbound {
		if n, ok := countSafely(raw); ok && n > se.s.MaxAttachments {
			return perm(552, smtp.EnhancedCode{5, 3, 4}, "Too many attachments")
		}
	}
	id := secure.NewUUID()
	path := se.s.Svc.SpoolFile(id)
	if err := writeSynced(path, raw); err != nil {
		slog.Error("inbound spool write failed", "err", err)
		return errTemp
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := se.s.Svc.Spool(ctx, id, se.kind, se.from, se.to, len(raw)); err != nil {
		os.Remove(path)
		slog.Error("inbound spool record failed", "err", err)
		return errTemp
	}
	slog.Info("inbound accepted", "spool", id, "kind", se.kind, "size", len(raw), "recipients", len(se.to))
	return nil
}

func countSafely(raw []byte) (n int, ok bool) {
	defer func() {
		if recover() != nil {
			n, ok = 0, false
		}
	}()
	n, err := CountAttachments(raw)
	return n, err == nil
}

// writeSynced writes via a temp file, fsyncs it, renames it into place and
// fsyncs the directory, so the message survives a crash once this returns.
func writeSynced(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if d, derr := os.Open(filepath.Dir(path)); derr == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

// ---------------------------------------------------------------- processor

// Processor parses spooled messages: ordinary mail becomes inbound_messages,
// DSNs become bounces.
type Processor struct {
	Svc   *service.Service
	Poll  time.Duration
	Lease time.Duration
	Batch int
}

func (p *Processor) Run(ctx context.Context) {
	if p.Poll <= 0 {
		p.Poll = time.Second
	}
	for ctx.Err() == nil {
		n, err := p.RunOnce(ctx)
		if err != nil {
			slog.Error("inbound processing cycle failed", "err", err)
		}
		if n == 0 || err != nil {
			select {
			case <-ctx.Done():
			case <-time.After(p.Poll):
			}
		}
	}
}

func (p *Processor) RunOnce(ctx context.Context) (int, error) {
	if p.Lease <= 0 {
		p.Lease = 2 * time.Minute
	}
	if p.Batch <= 0 {
		p.Batch = 10
	}
	items, err := p.Svc.ClaimSpool(ctx, p.Batch, p.Lease)
	if err != nil {
		return 0, err
	}
	for _, it := range items {
		if code := p.process(ctx, it); code != "" {
			if err := p.Svc.FailSpool(ctx, it, code); err != nil {
				slog.Error("recording inbound failure failed", "spool", it.ID, "err", err)
			}
		}
	}
	return len(items), nil
}

// process returns "" on success or a safe failure code.
func (p *Processor) process(ctx context.Context, it service.SpoolItem) (code string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("inbound parser panic", "spool", it.ID)
			code = "parse_panic"
		}
	}()
	raw, err := os.ReadFile(p.Svc.SpoolFile(it.ID))
	if err != nil {
		return "raw_missing"
	}
	if it.Kind == service.KindBounce {
		reports := ParseDSN(raw)
		if err := p.Svc.RecordBounce(ctx, it, reports); err != nil {
			slog.Error("recording bounce failed", "spool", it.ID, "err", err)
			return "store_failed"
		}
		os.Remove(p.Svc.SpoolFile(it.ID))
		slog.Info("bounce processed", "spool", it.ID, "reports", len(reports))
		return ""
	}
	parsed, err := ParseMessage(raw)
	if err != nil {
		return "parse_failed"
	}
	n, err := p.Svc.StoreInbound(ctx, it, parsed)
	if err != nil {
		slog.Error("storing inbound message failed", "spool", it.ID, "err", err)
		return "store_failed"
	}
	slog.Info("inbound processed", "spool", it.ID, "stored", n, "attachments", len(parsed.Attachments))
	return ""
}

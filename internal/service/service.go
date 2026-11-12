// Package service implements authentication, durable acceptance, status and
// the dispatch state machine on top of PostgreSQL.
package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/njiwa-tz/mail/internal/db"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/net/idna"

	"github.com/njiwa-tz/mail/internal/provider"
	"github.com/njiwa-tz/mail/internal/secure"
)

type Options struct {
	// NewProvider builds the mail server management client (Mailcow when nil).
	// Tests replace it; see internal/provider.
	NewProvider provider.Factory

	MetadataRetention time.Duration
	CaptureRetention  time.Duration
	UnknownPurge      time.Duration
	MaxAttempts       int

	// Inbound mail. InboundDomain is the subdomain Mailcow relays to the Go
	// listener; BounceAddress is the envelope sender DSNs come back to.
	InboundDomain string
	BounceAddress string
	DataDir       string // spool and attachment storage, outside any web root

	// DNS health checker expectations.
	MailHost     string // Mailcow hostname (MX target, PTR value)
	OutboundIP   string
	DKIMSelector string

	SessionTTL time.Duration

	PublicURL string // origin used in admin password-reset links

	// AllowUnregisteredLiveDomains: see config. Temporary upgrade escape hatch.
	AllowUnregisteredLiveDomains bool

	// Installation-wide abuse limits on accepted Live messages, on top of the
	// per-project quotas (backlog F04). 0 disables a limit.
	LiveGlobalPerMinute, LiveGlobalPerHour int
	LiveDomainPerMinute, LiveDomainPerHour int

	// LiveQueueMax bounds the Live backlog (queued, waiting to retry, sending,
	// scheduled). Beyond it new Live mail is refused with a retryable 503 so
	// the queue cannot grow without limit while the mail server is down.
	// 0 disables the bound.
	LiveQueueMax int
}

type Service struct {
	Pool *db.DB
	Box  *secure.Box
	Opts Options

	queueMu    sync.Mutex
	queueDepth int
	queueAt    time.Time

	bdMu  sync.Mutex // per-domain return-path domains (D04), cached briefly
	bdAt  time.Time
	bdSet map[string]bool
}

func New(pool *db.DB, box *secure.Box, o Options) *Service {
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 4
	}
	if o.MetadataRetention <= 0 {
		o.MetadataRetention = 30 * 24 * time.Hour
	}
	if o.CaptureRetention <= 0 {
		o.CaptureRetention = 24 * time.Hour
	}
	if o.UnknownPurge <= 0 || o.UnknownPurge > 24*time.Hour {
		o.UnknownPurge = 24 * time.Hour
	}
	if o.DKIMSelector == "" {
		o.DKIMSelector = "dkim"
	}
	if o.SessionTTL <= 0 {
		o.SessionTTL = 12 * time.Hour
	}
	o.InboundDomain = strings.ToLower(o.InboundDomain)
	o.BounceAddress = strings.ToLower(o.BounceAddress)
	return &Service{Pool: pool, Box: box, Opts: o}
}

// Error is a safe, client-visible failure. Code never contains user content.
// Message is optional operator-facing detail, used only by the admin API for
// validation feedback on things the operator typed (never secrets or mail).
type Error struct {
	Status     int
	Code       string
	Message    string
	RetryAfter time.Duration
}

func (e *Error) Error() string { return fmt.Sprintf("%d %s", e.Status, e.Code) }

func newErr(status int, code string) *Error { return &Error{Status: status, Code: code} }

// unavailable converts any persistence failure into a 503. Callers must never
// report acceptance when this is returned.
func unavailable(op string, err error) error {
	slog.Error("persistence failure", "op", op, "err", err)
	return &Error{Status: 503, Code: "service_unavailable", RetryAfter: 5 * time.Second}
}

func IsError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

func badRequest(msg string) *Error { return &Error{Status: 400, Code: "invalid_request", Message: msg} }

// notFound maps a lookup error: nil stays nil, no-rows becomes 404, anything
// else is a persistence failure.
func notFound(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, db.ErrNoRows) {
		return newErr(404, "not_found")
	}
	return unavailable("lookup", err)
}

const (
	ScopeSend         = "emails:send"
	ScopeSendRaw      = "emails:send_raw"
	ScopeRead         = "emails:read"
	ScopeInboundRead  = "inbound:read"
	ScopeSuppressions = "suppressions:manage"
)

var allScopes = []string{ScopeSend, ScopeSendRaw, ScopeRead, ScopeInboundRead, ScopeSuppressions}

func validScope(sc string) bool {
	for _, x := range allScopes {
		if x == sc {
			return true
		}
	}
	return false
}

// Page is a server-side pagination request.
type Page struct{ Limit, Offset int }

func (p Page) norm() Page {
	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Limit > 100 {
		p.Limit = 100
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	return p
}

// maps runs a read query and returns rows as JSON-ready maps. Queries must
// cast uuid columns to text and must never select secret or encrypted columns.
func (s *Service) maps(ctx context.Context, sql string, args ...any) ([]map[string]any, error) {
	rows, err := s.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, unavailable("query", err)
	}
	out, err := db.CollectMaps(rows)
	if err != nil {
		return nil, unavailable("query_rows", err)
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}

// page is maps with LIMIT/OFFSET appended; it reports whether more rows exist.
func (s *Service) page(ctx context.Context, pg Page, sql string, args ...any) ([]map[string]any, bool, error) {
	pg = pg.norm()
	n := len(args)
	sql += fmt.Sprintf(" LIMIT $%d OFFSET $%d", n+1, n+2)
	out, err := s.maps(ctx, sql, append(args, pg.Limit+1, pg.Offset)...)
	if err != nil {
		return nil, false, err
	}
	if len(out) > pg.Limit {
		return out[:pg.Limit], true, nil
	}
	return out, false, nil
}

func (s *Service) one(ctx context.Context, sql string, args ...any) (map[string]any, error) {
	out, err := s.maps(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, newErr(404, "not_found")
	}
	return out[0], nil
}

var (
	localRe  = regexp.MustCompile(`^[a-z0-9!#$%&'*+/=?^_{|}~-]+(\.[a-z0-9!#$%&'*+/=?^_{|}~-]+)*$`)
	domainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
)

// NormalizeEmail validates an address and returns it in a stable form.
// The domain is converted to ASCII (IDNA). A non-ASCII local part is kept
// as Unicode when it contains only letters, digits and the usual symbols.
func NormalizeEmail(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 320 || strings.ContainsAny(s, " \t\r\n") {
		return "", false
	}
	at := strings.LastIndex(s, "@")
	if at < 1 || at > 64 || at == len(s)-1 {
		return "", false
	}
	local := strings.ToLower(s[:at])
	asciiDomain, err := idna.Lookup.ToASCII(strings.ToLower(s[at+1:]))
	if err != nil || !localOK(local) || !domainRe.MatchString(strings.ToLower(asciiDomain)) {
		return "", false
	}
	addr := local + "@" + strings.ToLower(asciiDomain)
	if len(addr) > 254 {
		return "", false
	}
	return addr, true
}

func localOK(local string) bool {
	if local == "" || len(local) > 64 || strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") || strings.Contains(local, "..") {
		return false
	}
	if localRe.MatchString(local) {
		return true
	}
	for _, r := range local {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(".!#$%&'*+/=?^_{|}~-", r) {
			continue
		}
		return false
	}
	return true
}

// MaskEmail hides most of an address (g***@example.org) for views that must
// show where mail is stuck without exposing who it is for.
func MaskEmail(e string) string { return maskEmail(e) }

func maskEmail(e string) string {
	at := strings.LastIndexByte(e, '@')
	if at < 1 {
		return "***"
	}
	return e[:1] + "***" + e[at:]
}

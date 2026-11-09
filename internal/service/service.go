// Package service implements authentication, durable acceptance, status and
// the dispatch state machine on top of PostgreSQL.
package service

import (
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/ditronics-tz/mail/internal/secure"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Options struct {
	MetadataRetention time.Duration
	CaptureRetention  time.Duration
	UnknownPurge      time.Duration
	MaxAttempts       int
}

type Service struct {
	Pool *pgxpool.Pool
	Box  *secure.Box
	Opts Options
}

func New(pool *pgxpool.Pool, box *secure.Box, o Options) *Service {
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
	return &Service{Pool: pool, Box: box, Opts: o}
}

// Error is a safe, client-visible failure. Code never contains user content.
type Error struct {
	Status     int
	Code       string
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

const (
	ScopeSend = "emails:send"
	ScopeRead = "emails:read"
)

var emailRe = regexp.MustCompile(`^[a-z0-9!#$%&'*+/=?^_{|}~-]+(\.[a-z0-9!#$%&'*+/=?^_{|}~-]+)*@([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// NormalizeEmail lowercases and strictly validates a bare ASCII address.
func NormalizeEmail(s string) (string, bool) {
	if s != strings.TrimSpace(s) || len(s) > 254 {
		return "", false
	}
	l := strings.ToLower(s)
	at := strings.LastIndexByte(l, '@')
	if at < 1 || at > 64 || !emailRe.MatchString(l) {
		return "", false
	}
	return l, true
}

func maskEmail(e string) string {
	at := strings.LastIndexByte(e, '@')
	if at < 1 {
		return "***"
	}
	return e[:1] + "***" + e[at:]
}

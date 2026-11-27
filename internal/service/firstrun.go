package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/njiwa-tz/mail/internal/db"
)

// First-run setup: while the installation has no user and no organization, the
// dashboard may create both once. After that it is closed for good.

// FirstRunInput is what the setup wizard collects.
type FirstRunInput struct {
	Organization string `json:"organization"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	Password     string `json:"password"`
	// Token must equal SETUP_TOKEN when the server has one.
	Token string `json:"token"`
}

var errSetupClosed = &Error{Status: 409, Code: "setup_closed", Message: "this installation is already set up; sign in instead"}

const needsSetupSQL = `SELECT NOT EXISTS (SELECT 1 FROM users) AND NOT EXISTS (SELECT 1 FROM organization)`

// NeedsSetup reports whether the first-run setup is still open, and whether it
// asks for the setup token.
func (s *Service) NeedsSetup(ctx context.Context) (needs, tokenRequired bool, err error) {
	if err := s.Pool.QueryRow(ctx, needsSetupSQL).Scan(&needs); err != nil {
		return false, false, unavailable("setup_status", err)
	}
	return needs, needs && s.Opts.SetupToken != "", nil
}

func plainName(v, what string, min int) (string, error) {
	v = strings.Join(strings.Fields(v), " ")
	n := utf8.RuneCountInString(v)
	if n < min || n > 80 || strings.IndexFunc(v, unicode.IsControl) >= 0 {
		return "", badRequest(what + " must be between 2 and 80 characters")
	}
	return v, nil
}

var slugSplit = regexp.MustCompile(`[^a-z0-9]+`)

func slugOf(name string) string {
	s := strings.Trim(slugSplit.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	if s == "" {
		s = "organization"
	}
	return s
}

// FirstRun creates the organization and the first user, an installation
// operator, exactly once however many requests race. It returns the user's
// normalised email.
func (s *Service) FirstRun(ctx context.Context, in FirstRunInput, ip string) (string, error) {
	wait, ok, err := s.hitNow(ctx, "setup:ip:"+ip, 15*time.Minute, 10)
	if err != nil {
		return "", unavailable("setup_throttle", err)
	}
	if !ok {
		return "", &Error{Status: 429, Code: "too_many_attempts", RetryAfter: wait}
	}
	needs, _, err := s.NeedsSetup(ctx)
	if err != nil {
		return "", err
	}
	if !needs {
		return "", errSetupClosed
	}
	if want := s.Opts.SetupToken; want != "" {
		a, b := sha256.Sum256([]byte(want)), sha256.Sum256([]byte(strings.TrimSpace(in.Token)))
		if subtle.ConstantTimeCompare(a[:], b[:]) != 1 {
			return "", &Error{Status: 403, Code: "invalid_setup_token", Message: "the setup token is not correct; it is SETUP_TOKEN in the server's .env"}
		}
	}
	org, err := plainName(in.Organization, "the organization name", 2)
	if err != nil {
		return "", err
	}
	name, err := plainName(in.Name, "your name", 2)
	if err != nil {
		return "", err
	}
	email, ok := NormalizeEmail(in.Email)
	if !ok {
		return "", badRequest("invalid email address")
	}
	if err := checkPassword(in.Password); err != nil {
		return "", err
	}
	hash, err := hashPasswordContext(ctx, in.Password)
	if err != nil {
		return "", unavailable("password_hash_busy", err)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", unavailable("setup", err)
	}
	defer tx.Rollback(ctx)
	// The same lock the CLI's user creation takes: one of them wins.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(727002)`); err != nil {
		return "", unavailable("setup_lock", err)
	}
	if err := tx.QueryRow(ctx, needsSetupSQL).Scan(&needs); err != nil {
		return "", unavailable("setup", err)
	}
	if !needs {
		return "", errSetupClosed
	}
	if _, err := tx.Exec(ctx, `INSERT INTO organization(name, slug) VALUES ($1,$2)`, org, slugOf(org)); err != nil {
		return "", unavailable("setup_organization", err)
	}
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO users(email, password_hash, role, installation_operator, display_name)
VALUES ($1,$2,$3,true,$4) RETURNING id::text`, email, hash, RoleAdmin, name).Scan(&id); err != nil {
		return "", unavailable("setup_user", err)
	}
	if err := audit(ctx, tx, Actor{UserID: id, Name: email}, "setup.completed", "user", id, map[string]any{"organization": org, "ip": ip}); err != nil {
		return "", unavailable("audit", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", unavailable("setup", err)
	}
	return email, nil
}

// Organization is the installation's name; empty for one set up from the CLI.
func (s *Service) Organization(ctx context.Context) (map[string]any, error) {
	var name, slug string
	err := s.Pool.QueryRow(ctx, `SELECT name, slug FROM organization WHERE id=1`).Scan(&name, &slug)
	if err != nil && !errors.Is(err, db.ErrNoRows) {
		return nil, unavailable("organization", err)
	}
	return map[string]any{"name": name, "slug": slug}, nil
}

// organizationFooter signs the installation's own system emails.
func (s *Service) organizationFooter(ctx context.Context) string {
	var name string
	if s.Pool.QueryRow(ctx, `SELECT name FROM organization WHERE id=1`).Scan(&name) != nil || name == "" {
		return ""
	}
	return "\n\n" + name
}

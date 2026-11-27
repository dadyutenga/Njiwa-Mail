package service

import (
	"context"
	"crypto/x509"
	"errors"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/mailer"
)

const (
	// recheckEvery keeps a passing connection check well inside its TTL.
	recheckEvery = 6 * time.Hour
	// recheckAfterFailure retries sooner, so a short outage clears by itself.
	recheckAfterFailure = 10 * time.Minute
)

// CheckTransportProfile connects and signs in with a profile's settings and
// records the result against that exact revision. No message is sent. roots
// is the trust store for profiles that bring none; nil uses the system's.
func (s *Service) CheckTransportProfile(ctx context.Context, id string, roots *x509.CertPool) (steps []mailer.Step, ok bool, rev int64, err error) {
	cfg, sender, rev, err := s.ProfileSMTP(ctx, id)
	if err != nil {
		return nil, false, 0, err
	}
	if cfg.RootCAs == nil {
		cfg.RootCAs = roots
	}
	tctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	steps = mailer.Diagnose(tctx, cfg, sender)
	ok = true
	for _, st := range steps {
		ok = ok && (st.OK || (st.Skipped && st.Key == "sender" && sender == ""))
	}
	if err := ctx.Err(); err != nil {
		return nil, false, 0, err // interrupted: not evidence about the server
	}
	if err := s.RecordInstallationCheck(ctx, id, rev, "connection", ok, ProblemCode("connection", steps), nil, false); err != nil {
		return nil, false, 0, &Error{Status: 503, Code: "service_unavailable"}
	}
	return steps, ok, rev, nil
}

// RecheckInstallation runs the active profile's connection check when it is
// due: there is none for the current settings, the last pass is six hours old,
// or the last run failed ten minutes ago. It reports whether it ran. A profile
// that is not configured yet is left alone.
func (s *Service) RecheckInstallation(ctx context.Context, roots *x509.CertPool) (bool, error) {
	var id string
	var result *string
	var checkedAt *time.Time
	err := s.Pool.QueryRow(ctx, `SELECT p.id::text, c.result, c.checked_at FROM transport_profiles p
LEFT JOIN LATERAL (SELECT result, checked_at FROM installation_checks
  WHERE profile_id=p.id AND config_revision=p.config_revision AND check_key='connection' ORDER BY checked_at DESC LIMIT 1) c ON true
WHERE p.is_active`).Scan(&id, &result, &checkedAt)
	if errors.Is(err, db.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if result != nil && checkedAt != nil {
		wait := recheckEvery
		if *result != "pass" {
			wait = recheckAfterFailure
		}
		if time.Since(*checkedAt) < wait {
			return false, nil
		}
	}
	if _, _, _, err := s.CheckTransportProfile(ctx, id, roots); err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == 409 {
			return false, nil // incomplete settings, or the installer has not run
		}
		return false, err
	}
	return true, nil
}

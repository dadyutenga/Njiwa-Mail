package service

import (
	"context"
	"net"

	"github.com/njiwa-tz/mail/internal/verify"
)

// VerifiedAddress is a verification result plus what this environment knows
// about the address (suppressed after a hard bounce or complaint,
// unsubscribed from bulk mail).
type VerifiedAddress struct {
	verify.Check
	Suppressed   bool `json:"suppressed"`
	Unsubscribed bool `json:"unsubscribed"`
}

// MaxVerifyBatch is the most addresses one request may verify.
const MaxVerifyBatch = 100

// VerifyAddresses checks syntax, MX, disposable and role addresses, and the
// environment's suppression and unsubscribe lists. It sends no mail and does
// not connect to the recipient's mail server.
func (s *Service) VerifyAddresses(ctx context.Context, envID string, emails []string) ([]VerifiedAddress, error) {
	if len(emails) == 0 {
		return nil, invalid("emails_required")
	}
	if len(emails) > MaxVerifyBatch {
		return nil, invalid("too_many_addresses")
	}
	s.verifyOnce.Do(func() {
		var r verify.Resolver = net.DefaultResolver
		if s.Opts.VerifyResolver != nil {
			r = s.Opts.VerifyResolver
		}
		s.verifier, s.verifyErr = verify.New(r, NormalizeEmail, s.Opts.DisposableFile)
	})
	if s.verifyErr != nil {
		return nil, unavailable("verify_setup", s.verifyErr)
	}
	out := make([]VerifiedAddress, 0, len(emails))
	for _, e := range emails {
		r := VerifiedAddress{Check: s.verifier.Verify(ctx, e)}
		if r.Syntax {
			h := s.Box.MAC("recipient", []byte(r.Email))
			err := s.Pool.QueryRow(ctx, `SELECT
  EXISTS (SELECT 1 FROM suppressions WHERE environment_id=$1 AND recipient_hash=$2),
  EXISTS (SELECT 1 FROM bulk_unsubscribes WHERE environment_id=$1 AND recipient_hash=$2)`, envID, h).Scan(&r.Suppressed, &r.Unsubscribed)
			if err != nil {
				return nil, unavailable("verify_lookup", err)
			}
			if r.Suppressed {
				r.Reasons = append(r.Reasons, "suppressed")
			}
		}
		out = append(out, r)
	}
	return out, nil
}

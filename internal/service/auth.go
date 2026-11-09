package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Principal struct {
	KeyID, KeyPrefix           string
	ProjectID, EnvironmentID   string
	Mode                       string
	Scopes                     []string
	Origins                    []string
	RestrictRecipients         bool
	Allowlist                  []string
	PerMinute, PerDay          int
	RcptPerMinute, RcptPerHour int
}

func (p *Principal) HasScope(s string) bool {
	for _, x := range p.Scopes {
		if x == s {
			return true
		}
	}
	return false
}

func modeTag(mode string) string {
	if mode == "live" {
		return "liv"
	}
	return "sbx"
}

// newKeyMaterial returns (token, prefix, secretHash). The token is shown once.
func newKeyMaterial(mode string) (string, string, []byte) {
	pb, sb := make([]byte, 6), make([]byte, 32)
	if _, err := rand.Read(pb); err != nil {
		panic(err)
	}
	if _, err := rand.Read(sb); err != nil {
		panic(err)
	}
	prefix := hex.EncodeToString(pb)
	secret := base64.RawURLEncoding.EncodeToString(sb)
	h := sha256.Sum256([]byte(secret))
	return "dmk_" + modeTag(mode) + "_" + prefix + "_" + secret, prefix, h[:]
}

var errInvalidKey = newErr(401, "invalid_api_key")

// Authenticate verifies a Bearer token on every request: prefix lookup,
// constant-time hash compare, revocation, expiry, and project/env state.
func (s *Service) Authenticate(ctx context.Context, header string) (*Principal, error) {
	const bearer = "bearer "
	if len(header) <= len(bearer) || !strings.EqualFold(header[:len(bearer)], bearer) {
		return nil, errInvalidKey
	}
	parts := strings.SplitN(strings.TrimSpace(header[len(bearer):]), "_", 4)
	if len(parts) != 4 || parts[0] != "dmk" || (parts[1] != "sbx" && parts[1] != "liv") || parts[2] == "" || parts[3] == "" {
		return nil, errInvalidKey
	}
	var (
		p                       Principal
		hash                    []byte
		expires, revoked        *time.Time
		projEnabled, envEnabled bool
		mode                    string
	)
	err := s.Pool.QueryRow(ctx, `
SELECT k.id, k.secret_hash, k.scopes, k.expires_at, k.revoked_at,
       p.id, p.enabled, p.send_per_minute, p.send_per_day, p.recipient_per_minute, p.recipient_per_hour,
       e.id, e.mode, e.enabled, e.approved_origins, e.restrict_recipients, e.recipient_allowlist
FROM api_keys k
JOIN environments e ON e.id = k.environment_id AND e.project_id = k.project_id
JOIN projects p ON p.id = k.project_id
WHERE k.prefix = $1`, parts[2]).Scan(
		&p.KeyID, &hash, &p.Scopes, &expires, &revoked,
		&p.ProjectID, &projEnabled, &p.PerMinute, &p.PerDay, &p.RcptPerMinute, &p.RcptPerHour,
		&p.EnvironmentID, &mode, &envEnabled, &p.Origins, &p.RestrictRecipients, &p.Allowlist)
	if errors.Is(err, pgx.ErrNoRows) {
		sum := sha256.Sum256([]byte(parts[3])) // keep timing similar for unknown prefixes
		subtle.ConstantTimeCompare(sum[:], make([]byte, 32))
		return nil, errInvalidKey
	}
	if err != nil {
		return nil, unavailable("authenticate", err)
	}
	sum := sha256.Sum256([]byte(parts[3]))
	if subtle.ConstantTimeCompare(sum[:], hash) != 1 {
		return nil, errInvalidKey
	}
	if revoked != nil || (expires != nil && !expires.After(time.Now())) || modeTag(mode) != parts[1] {
		return nil, errInvalidKey
	}
	if !projEnabled || !envEnabled {
		return nil, newErr(403, "project_disabled")
	}
	p.Mode, p.KeyPrefix = mode, parts[2]
	return &p, nil
}

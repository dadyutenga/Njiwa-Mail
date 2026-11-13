package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
)

// F09: TOTP (RFC 6238, SHA-1, 6 digits, 30 s) for operator sign-in. The secret
// is sealed with ENCRYPTION_KEY. A code is accepted once (the highest used time
// step is stored), repeated failures lock the second factor for a while, and
// recovery codes are single use and stored only as keyed hashes.

const (
	totpPeriod      = 30
	totpDigits      = 6
	totpSkew        = 1 // accept one step either side for clock drift
	mfaMaxFailures  = 5
	mfaLockDuration = 15 * time.Minute
	recoveryCodes   = 10
	mfaIssuer       = "Njiwa-Mail"
)

var (
	errMFARequired = newErr(401, "mfa_required")
	errBadCode     = newErr(401, "invalid_mfa_code")
	b32            = base32.StdEncoding.WithPadding(base32.NoPadding)
)

// totpAt returns the code for a time step.
func totpAt(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[off])&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	mod := uint32(1)
	for i := 0; i < totpDigits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", totpDigits, bin%mod)
}

// totpMatch returns the time step that matches code within the skew window.
func totpMatch(secret []byte, code string, now time.Time) (int64, bool) {
	if len(code) != totpDigits {
		return 0, false
	}
	cur := now.Unix() / totpPeriod
	hit, found := int64(0), false
	for d := int64(-totpSkew); d <= totpSkew; d++ { // no early exit: constant work
		if subtle.ConstantTimeCompare([]byte(totpAt(secret, cur+d)), []byte(code)) == 1 {
			hit, found = cur+d, true
		}
	}
	return hit, found
}

func normalizeCode(code string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(code)))
}

func isTOTPShape(code string) bool {
	if len(code) != totpDigits {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (s *Service) recoveryHash(code string) string {
	return hex.EncodeToString(s.Box.MAC("mfa-recovery", []byte(normalizeCode(code))))
}

func newRecoveryCode() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	t := b32.EncodeToString(b) // 10 characters
	return t[:5] + "-" + t[5:]
}

// checkSecondFactor verifies a TOTP or recovery code for an enabled account
// during sign-in, enforcing lockout and replay protection.
func (s *Service) checkSecondFactor(ctx context.Context, userID, code string) (recoveryUsed bool, err error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	var enc []byte
	var enabled bool
	var locked *time.Time
	if err := tx.QueryRow(ctx, `SELECT totp_secret_enc, totp_enabled, totp_locked_until FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&enc, &enabled, &locked); err != nil {
		return false, unavailable("mfa_lock", err)
	}
	if !enabled || len(enc) == 0 {
		return false, errBadCode
	}
	if locked != nil && locked.After(time.Now()) {
		return false, &Error{Status: 429, Code: "mfa_locked", RetryAfter: time.Until(*locked)}
	}
	secret, err := s.Box.Open(enc, []byte("totp:"+userID))
	if err != nil || len(secret) != 20 {
		return false, errBadCode
	}
	code = normalizeCode(code)
	ok := false
	if isTOTPShape(code) {
		if step, match := totpMatch(secret, code, time.Now()); match {
			// Single use: only the first sign-in with this step wins.
			tag, err := tx.Exec(ctx, `UPDATE users SET totp_last_step=$2 WHERE id=$1 AND totp_last_step < $2`, userID, step)
			if err != nil {
				return false, unavailable("mfa_step", err)
			}
			ok = tag.RowsAffected() == 1
		}
	} else if len(code) >= 8 {
		h := s.recoveryHash(code)
		tag, err := tx.Exec(ctx, `UPDATE users SET recovery_hashes=array_remove(recovery_hashes,$2) WHERE id=$1 AND $2 = ANY(recovery_hashes)`, userID, h)
		if err != nil {
			return false, unavailable("mfa_recovery", err)
		}
		ok, recoveryUsed = tag.RowsAffected() == 1, tag.RowsAffected() == 1
	}
	if !ok {
		if _, err := tx.Exec(ctx, `UPDATE users SET
totp_locked_until=CASE WHEN totp_failed+1 >= $2 THEN now() + ($3 * interval '1 second') ELSE totp_locked_until END,
totp_failed=CASE WHEN totp_failed+1 >= $2 THEN 0 ELSE totp_failed+1 END WHERE id=$1`,
			userID, mfaMaxFailures, int(mfaLockDuration.Seconds())); err != nil {
			return false, unavailable("mfa_fail", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return false, unavailable("commit", err)
		}
		return false, errBadCode
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET totp_failed=0, totp_locked_until=NULL WHERE id=$1`, userID); err != nil {
		return false, unavailable("mfa_reset", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, unavailable("commit", err)
	}
	return recoveryUsed, nil
}

// MFAStart creates a pending secret for the signed-in user. It is not active
// until MFAConfirm proves the authenticator app produces valid codes.
func (s *Service) MFAStart(ctx context.Context, u *AdminUser) (map[string]any, error) {
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return nil, unavailable("mfa_secret", err)
	}
	enc, err := s.Box.Seal(secret, []byte("totp:"+u.ID))
	if err != nil {
		return nil, unavailable("mfa_seal", err)
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE users SET totp_secret_enc=$2, totp_last_step=0, totp_failed=0, totp_locked_until=NULL, recovery_hashes='{}'
WHERE id=$1 AND NOT totp_enabled AND EXISTS (
  SELECT 1 FROM sessions WHERE user_id=$1 AND token_hash=$3 AND auth_version=users.auth_version AND expires_at>now()
)`, u.ID, enc, u.tokenHash)
	if err != nil {
		return nil, unavailable("mfa_start", err)
	}
	if tag.RowsAffected() != 1 {
		var enabled bool
		if err := s.Pool.QueryRow(ctx, `SELECT totp_enabled FROM users WHERE id=$1`, u.ID).Scan(&enabled); err != nil {
			return nil, notFound(err)
		}
		if enabled {
			return nil, newErr(409, "mfa_already_enabled")
		}
		return nil, newErr(401, "unauthenticated")
	}
	b := b32.EncodeToString(secret)
	q := url.Values{"secret": {b}, "issuer": {mfaIssuer}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	uri := "otpauth://totp/" + url.PathEscape(mfaIssuer+":"+u.Email) + "?" + q.Encode()
	return map[string]any{"secret": b, "otpauth_uri": uri}, nil
}

// MFAConfirm activates the pending secret and returns the recovery codes once.
func (s *Service) MFAConfirm(ctx context.Context, u *AdminUser, code string) ([]string, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	var enc []byte
	var enabled bool
	var authVersion int64
	if err := tx.QueryRow(ctx, `SELECT totp_secret_enc, totp_enabled, auth_version FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&enc, &enabled, &authVersion); err != nil {
		return nil, notFound(err)
	}
	if enabled {
		return nil, newErr(409, "mfa_already_enabled")
	}
	var sessionCurrent bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE user_id=$1 AND token_hash=$2 AND auth_version=$3 AND expires_at>now())`, u.ID, u.tokenHash, authVersion).Scan(&sessionCurrent); err != nil {
		return nil, unavailable("session_check", err)
	}
	if !sessionCurrent {
		return nil, newErr(401, "unauthenticated")
	}
	if len(enc) == 0 {
		return nil, newErr(409, "mfa_not_started")
	}
	secret, err := s.Box.Open(enc, []byte("totp:"+u.ID))
	if err != nil || len(secret) != 20 {
		return nil, newErr(409, "mfa_not_started")
	}
	step, ok := totpMatch(secret, normalizeCode(code), time.Now())
	if !ok {
		return nil, errBadCode
	}
	codes := make([]string, recoveryCodes)
	hashes := make([]string, recoveryCodes)
	for i := range codes {
		codes[i] = newRecoveryCode()
		hashes[i] = s.recoveryHash(codes[i])
	}
	tag, err := tx.Exec(ctx, `UPDATE users SET totp_enabled=true, totp_last_step=$2, recovery_hashes=$3, auth_version=auth_version+1 WHERE id=$1 AND NOT totp_enabled AND totp_secret_enc=$4`, u.ID, step, hashes, enc)
	if err != nil {
		return nil, unavailable("mfa_confirm", err)
	}
	if tag.RowsAffected() != 1 {
		return nil, newErr(409, "mfa_already_enabled")
	}
	// Other sessions signed in without the second factor end now.
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1 AND token_hash <> $2`, u.ID, u.tokenHash); err != nil {
		return nil, unavailable("session_revoke", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE sessions SET auth_version=(SELECT auth_version FROM users WHERE id=$1) WHERE user_id=$1 AND token_hash=$2`, u.ID, u.tokenHash); err != nil {
		return nil, unavailable("session_refresh", err)
	}
	if err := audit(ctx, tx, u.Actor(), "mfa.enable", "user", u.ID, nil); err != nil {
		return nil, unavailable("audit", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, unavailable("commit", err)
	}
	return codes, nil
}

// MFADisable turns the second factor off for the signed-in user. It needs the
// password and a valid code, and is refused while the operator requires MFA.
func (s *Service) MFADisable(ctx context.Context, u *AdminUser, password, code string) error {
	if s.settingInt(ctx, "require_mfa") == 1 {
		return newErr(409, "mfa_required_by_policy")
	}
	var hash string
	var enabled bool
	if err := s.Pool.QueryRow(ctx, `SELECT password_hash, totp_enabled FROM users WHERE id=$1`, u.ID).Scan(&hash, &enabled); err != nil {
		return notFound(err)
	}
	if !enabled {
		return newErr(409, "mfa_not_enabled")
	}
	if len(password) > maxPasswordBytes {
		return errBadLogin
	}
	valid, err := withPasswordHashSlot(ctx, func() bool { return verifyPassword(hash, password) })
	if err != nil {
		return unavailable("password_hash_busy", err)
	}
	if !valid {
		return errBadLogin
	}
	if _, err := s.checkSecondFactor(ctx, u.ID, code); err != nil {
		return err
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE users SET totp_enabled=false, totp_secret_enc=NULL, totp_last_step=0, recovery_hashes='{}', auth_version=auth_version+1 WHERE id=$1 AND password_hash=$2 AND totp_enabled`, u.ID, hash)
	if err != nil {
		return unavailable("mfa_disable", err)
	}
	if tag.RowsAffected() != 1 {
		return errBadLogin
	}
	return s.Audit(ctx, u.Actor(), "mfa.disable", "user", u.ID, nil)
}

// ResetMFA clears another user's second factor (lost device and recovery
// codes) and ends their sessions. It is an installation-operator action.
func (s *Service) ResetMFA(ctx context.Context, actor Actor, userID string) error {
	if !uuidRe.MatchString(userID) {
		return newErr(404, "not_found")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE users SET totp_enabled=false, totp_secret_enc=NULL, totp_last_step=0, totp_failed=0, totp_locked_until=NULL, recovery_hashes='{}', auth_version=auth_version+1 WHERE id=$1`, userID)
	if err != nil {
		return unavailable("mfa_reset", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, userID); err != nil {
		return unavailable("session_revoke", err)
	}
	if err := audit(ctx, tx, actor, "mfa.reset", "user", userID, nil); err != nil {
		return unavailable("audit", err)
	}
	return tx.Commit(ctx)
}

// RecoveryCodesRemaining tells the user how many unused codes are left.
func (s *Service) recoveryRemaining(ctx context.Context, id string) int {
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT coalesce(cardinality(recovery_hashes),0) FROM users WHERE id=$1`, id).Scan(&n); err != nil && !errors.Is(err, db.ErrNoRows) {
		return 0
	}
	return n
}

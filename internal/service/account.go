package service

import (
	"context"
	"errors"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
)

// Account lifecycle for the signed-in user: change the password, see and end sessions, ask for
// the account to be deleted (with a grace period) and read the user's own audit trail.

// AccountDeletionGrace is how long a deletion request can be cancelled.
const AccountDeletionGrace = 14 * 24 * time.Hour

// verifyOwnPassword checks the signed-in user's current password with a small failure budget, so
// a stolen session cannot be used to guess it.
func (s *Service) verifyOwnPassword(ctx context.Context, userID, password string) (hash string, err error) {
	wait, ok, herr := s.hitNow(ctx, "acct:"+userID, 15*time.Minute, 10)
	if herr != nil {
		return "", unavailable("rate", herr)
	}
	if !ok {
		return "", &Error{Status: 429, Code: "rate_limited", RetryAfter: wait}
	}
	if err := s.Pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, userID).Scan(&hash); err != nil {
		return "", notFound(err)
	}
	if len(password) > maxPasswordBytes {
		return "", errBadLogin
	}
	valid, err := withPasswordHashSlot(ctx, func() bool { return verifyPassword(hash, password) })
	if err != nil {
		return "", unavailable("password_hash_busy", err)
	}
	if !valid {
		return "", errBadLogin
	}
	return hash, nil
}

// ChangePassword sets a new password after checking the current one. Every other session of the
// user ends; the one making the request stays.
func (s *Service) ChangePassword(ctx context.Context, u *AdminUser, current, next string) error {
	hash, err := s.verifyOwnPassword(ctx, u.ID, current)
	if err != nil {
		return err
	}
	if err := checkPassword(next); err != nil {
		return err
	}
	if next == current {
		return badRequest("choose a password different from the current one")
	}
	nh, err := hashPasswordContext(ctx, next)
	if err != nil {
		return unavailable("password_hash_busy", err)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	var version int64
	err = tx.QueryRow(ctx, `UPDATE users SET password_hash=$2, auth_version=auth_version+1 WHERE id=$1 AND password_hash=$3 RETURNING auth_version`, u.ID, nh, hash).Scan(&version)
	if errors.Is(err, db.ErrNoRows) {
		return errBadLogin // the password changed while this request was running
	}
	if err != nil {
		return unavailable("password_change", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1 AND token_hash<>$2`, u.ID, u.tokenHash); err != nil {
		return unavailable("session_revoke", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE sessions SET auth_version=$3 WHERE user_id=$1 AND token_hash=$2`, u.ID, u.tokenHash, version); err != nil {
		return unavailable("session_keep", err)
	}
	if err := audit(ctx, tx, u.Actor(), "auth.password_change", "user", u.ID, nil); err != nil {
		return unavailable("audit", err)
	}
	return tx.Commit(ctx)
}

// RevokeOtherSessions ends every session of the user except the current one.
func (s *Service) RevokeOtherSessions(ctx context.Context, u *AdminUser) (int64, error) {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1 AND token_hash<>$2`, u.ID, u.tokenHash)
	if err != nil {
		return 0, unavailable("session_revoke", err)
	}
	return tag.RowsAffected(), nil
}

// RevokeUserSessions ends every session of another user (operator). Their password and second
// factor are untouched; they sign in again.
func (s *Service) RevokeUserSessions(ctx context.Context, actor Actor, userID string) (int64, error) {
	if !uuidRe.MatchString(userID) {
		return 0, newErr(404, "not_found")
	}
	var ok bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id=$1)`, userID).Scan(&ok); err != nil {
		return 0, unavailable("user", err)
	}
	if !ok {
		return 0, newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, userID)
	if err != nil {
		return 0, unavailable("session_revoke", err)
	}
	if err := s.Audit(ctx, actor, "user.sessions_revoked", "user", userID, map[string]any{"sessions": tag.RowsAffected()}); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// UserSessions lists another user's live sessions (operator).
func (s *Service) UserSessions(ctx context.Context, userID string) ([]map[string]any, error) {
	if !uuidRe.MatchString(userID) {
		return nil, newErr(404, "not_found")
	}
	return s.ListSessions(ctx, userID)
}

// UserAudit is what a user did, newest first (their sign-ins and every change they made).
func (s *Service) UserAudit(ctx context.Context, userID, action string, pg Page) ([]map[string]any, bool, error) {
	if !uuidRe.MatchString(userID) {
		return nil, false, newErr(404, "not_found")
	}
	return s.page(ctx, pg, `SELECT id, at, actor, action, target_type, target_id, details FROM audit_log
WHERE user_id=$1 AND ($2='' OR action LIKE replace(replace(replace($2, '\', '\\'), '%', '\%'), '_', '\_') || '%' ESCAPE '\') ORDER BY id DESC`, userID, action)
}

// ---------------------------------------------------------------- deletion with a grace period

// canLeave reports why a user cannot leave: they are the last enabled user or the last enabled
// installation operator. It runs inside the transaction that holds the user lock.
func canLeave(ctx context.Context, tx *db.Tx, userID string) error {
	var others, operators int
	var operator bool
	if err := tx.QueryRow(ctx, `SELECT installation_operator FROM users WHERE id=$1`, userID).Scan(&operator); err != nil {
		return notFound(err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE installation_operator) FROM users WHERE id<>$1 AND NOT disabled`, userID).Scan(&others, &operators); err != nil {
		return unavailable("user_count", err)
	}
	if others == 0 {
		return newErr(409, "last_user")
	}
	if operator && operators == 0 {
		return newErr(409, "last_operator")
	}
	return nil
}

// RequestAccountDeletion schedules the user's account for removal after the grace period. It
// needs the password, and the authenticator code when a second factor is set. Signing in during
// the grace period keeps working; cancelling clears the request.
func (s *Service) RequestAccountDeletion(ctx context.Context, u *AdminUser, password, code string) (time.Time, error) {
	if _, err := s.verifyOwnPassword(ctx, u.ID, password); err != nil {
		return time.Time{}, err
	}
	var mfa bool
	if err := s.Pool.QueryRow(ctx, `SELECT totp_enabled FROM users WHERE id=$1`, u.ID).Scan(&mfa); err != nil {
		return time.Time{}, notFound(err)
	}
	if mfa {
		if _, err := s.checkSecondFactor(ctx, u.ID, code); err != nil {
			return time.Time{}, err
		}
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return time.Time{}, unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(727003)`); err != nil {
		return time.Time{}, unavailable("lock", err)
	}
	if err := canLeave(ctx, tx, u.ID); err != nil {
		return time.Time{}, err
	}
	var due time.Time
	if err := tx.QueryRow(ctx, `UPDATE users SET deletion_requested_at=COALESCE(deletion_requested_at, now()), deletion_due_at=COALESCE(deletion_due_at, now() + $2::interval)
WHERE id=$1 RETURNING deletion_due_at`, u.ID, AccountDeletionGrace.String()).Scan(&due); err != nil {
		return time.Time{}, unavailable("deletion_request", err)
	}
	if err := audit(ctx, tx, u.Actor(), "account.deletion_requested", "user", u.ID, map[string]any{"due": due}); err != nil {
		return time.Time{}, unavailable("audit", err)
	}
	return due, tx.Commit(ctx)
}

func (s *Service) CancelAccountDeletion(ctx context.Context, u *AdminUser) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE users SET deletion_requested_at=NULL, deletion_due_at=NULL WHERE id=$1 AND deletion_due_at IS NOT NULL`, u.ID)
	if err != nil {
		return unavailable("deletion_cancel", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(409, "no_deletion_pending")
	}
	return s.Audit(ctx, u.Actor(), "account.deletion_cancelled", "user", u.ID, nil)
}

// PurgeDueAccounts removes accounts whose grace period has ended. An account that has become the
// last user or the last operator in the meantime is kept and its request is cleared. It returns
// how many accounts were removed.
func (s *Service) PurgeDueAccounts(ctx context.Context) (int, error) {
	ids, err := s.maps(ctx, `SELECT id::text AS id FROM users WHERE deletion_due_at IS NOT NULL AND deletion_due_at <= now() ORDER BY deletion_due_at LIMIT 50`)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, row := range ids {
		id := row["id"].(string)
		tx, err := s.Pool.Begin(ctx)
		if err != nil {
			return removed, unavailable("begin", err)
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(727003)`); err != nil {
			tx.Rollback(ctx)
			return removed, unavailable("lock", err)
		}
		var still bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id=$1 AND deletion_due_at <= now() FOR UPDATE)`, id).Scan(&still); err != nil || !still {
			tx.Rollback(ctx)
			continue
		}
		if err := canLeave(ctx, tx, id); err != nil {
			if _, e := tx.Exec(ctx, `UPDATE users SET deletion_requested_at=NULL, deletion_due_at=NULL WHERE id=$1`, id); e == nil {
				_ = audit(ctx, tx, Actor{Name: "system"}, "account.deletion_blocked", "user", id, map[string]any{"reason": err.Error()})
				_ = tx.Commit(ctx)
			} else {
				tx.Rollback(ctx)
			}
			continue
		}
		if err := audit(ctx, tx, Actor{Name: "system"}, "account.deleted", "user", id, nil); err != nil {
			tx.Rollback(ctx)
			return removed, unavailable("audit", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id=$1`, id); err != nil {
			tx.Rollback(ctx)
			return removed, unavailable("account_delete", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return removed, unavailable("commit", err)
		}
		removed++
	}
	return removed, nil
}

package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/argon2"
)

// Admin users sign in with email and password (argon2id) and get a
// server-side session. There is no signup: the first owner is created by CLI.

const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleViewer = "viewer"

	argonTime    = 2
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 2
	argonKeyLen  = 32

	MinPasswordLen = 12
)

// RoleLevel orders roles: viewer < admin < owner. Unknown roles get 0.
func RoleLevel(role string) int {
	switch role {
	case RoleViewer:
		return 1
	case RoleAdmin:
		return 2
	case RoleOwner:
		return 3
	}
	return 0
}

func hashPassword(pw string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic(err)
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

func verifyPassword(encoded, pw string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var ver int
	var mem, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &ver); err != nil || ver != argon2.Version {
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &p); err != nil || mem > 1<<20 || t > 10 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, mem, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash keeps login timing similar when the account does not exist.
var dummyHash = hashPassword("ditronics-mail-timing-equaliser")

func checkPassword(pw string) error {
	if len(pw) < MinPasswordLen || len(pw) > 256 {
		return badRequest(fmt.Sprintf("password must be between %d and 256 characters", MinPasswordLen))
	}
	return nil
}

func validRole(r string) error {
	if RoleLevel(r) == 0 {
		return badRequest("role must be owner, admin or viewer")
	}
	return nil
}

// CreateUser adds an admin user. Used by the CLI for the first owner and by
// owners through the admin API.
func (s *Service) CreateUser(ctx context.Context, email, password, role string) (string, error) {
	e, ok := NormalizeEmail(email)
	if !ok {
		return "", badRequest("invalid email address")
	}
	if err := validRole(role); err != nil {
		return "", err
	}
	if err := checkPassword(password); err != nil {
		return "", err
	}
	var id string
	err := s.Pool.QueryRow(ctx, `INSERT INTO users(email, password_hash, role) VALUES ($1,$2,$3) ON CONFLICT (email) DO NOTHING RETURNING id::text`,
		e, hashPassword(password), role).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", newErr(409, "user_exists")
	}
	if err != nil {
		return "", unavailable("user_create", err)
	}
	return id, nil
}

// UpdateUser changes role, disabled state or password. The last enabled owner
// can be neither demoted nor disabled, so the installation cannot lock itself
// out. Disabling a user or changing a password ends that user's sessions.
func (s *Service) UpdateUser(ctx context.Context, id string, role *string, disabled *bool, password *string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	if role != nil {
		if err := validRole(*role); err != nil {
			return err
		}
	}
	var hash *string
	if password != nil {
		if err := checkPassword(*password); err != nil {
			return err
		}
		h := hashPassword(*password)
		hash = &h
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return unavailable("lock", err)
	}
	tag, err := tx.Exec(ctx, `UPDATE users SET role=COALESCE($2,role), disabled=COALESCE($3,disabled), password_hash=COALESCE($4,password_hash) WHERE id=$1`,
		id, role, disabled, hash)
	if err != nil {
		return unavailable("user_update", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	var owners int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='owner' AND NOT disabled`).Scan(&owners); err != nil {
		return unavailable("owner_count", err)
	}
	if owners == 0 {
		return newErr(409, "last_owner")
	}
	if (disabled != nil && *disabled) || hash != nil || role != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
			return unavailable("session_revoke", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable("commit", err)
	}
	return nil
}

func (s *Service) ListUsers(ctx context.Context, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT id::text AS id, email, role, disabled, last_login_at, created_at FROM users ORDER BY email`)
}

// AdminUser is the authenticated operator behind an admin request.
type AdminUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
	// CSRF must be echoed in X-CSRF-Token on every state-changing request.
	CSRF        string    `json:"csrf_token"`
	ExpiresAt   time.Time `json:"expires_at"`
	TOTPEnabled bool      `json:"totp_enabled"`
}

func (u *AdminUser) Can(minRole string) bool { return RoleLevel(u.Role) >= RoleLevel(minRole) }

func (s *Service) csrfFor(tokenHash []byte) string {
	return hex.EncodeToString(s.Box.MAC("admin-csrf", tokenHash))
}

var errBadLogin = newErr(401, "invalid_credentials")

// Login verifies credentials and opens a session. Attempts are throttled per
// account and per IP before the password is checked; the error is the same
// for an unknown account, a wrong password and a disabled account.
func (s *Service) Login(ctx context.Context, email, password, ip, userAgent, totpCode string) (token string, u *AdminUser, err error) {
	e, _ := NormalizeEmail(strings.TrimSpace(email))
	acct := hex.EncodeToString(s.Box.MAC("login-account", []byte(strings.ToLower(strings.TrimSpace(email)))))
	for _, b := range []struct {
		key   string
		limit int
	}{{"login:acct:" + acct, s.settingInt(ctx, "login_attempts_per_account")}, {"login:ip:" + ip, s.settingInt(ctx, "login_attempts_per_ip")}} {
		wait, ok, err := s.hitNow(ctx, b.key, 15*time.Minute, b.limit)
		if err != nil {
			return "", nil, unavailable("login_throttle", err)
		}
		if !ok {
			return "", nil, &Error{Status: 429, Code: "too_many_attempts", RetryAfter: wait}
		}
	}
	var id, hash, role string
	var disabled, totpEnabled bool
	err = s.Pool.QueryRow(ctx, `SELECT id::text, password_hash, role, disabled, totp_enabled FROM users WHERE email=$1`, e).Scan(&id, &hash, &role, &disabled, &totpEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		verifyPassword(dummyHash, password)
		return "", nil, errBadLogin
	}
	if err != nil {
		return "", nil, unavailable("login", err)
	}
	if !verifyPassword(hash, password) || disabled {
		return "", nil, errBadLogin
	}
	if totpEnabled {
		ok, err := s.userTOTP(ctx, id, totpCode)
		if err != nil || !ok {
			return "", nil, errBadLogin
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, unavailable("session_token", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	th := sha256.Sum256([]byte(token))
	exp := time.Now().Add(s.Opts.SessionTTL)
	if len(userAgent) > 200 {
		userAgent = userAgent[:200]
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", nil, unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO sessions(token_hash, user_id, expires_at, ip, user_agent) VALUES ($1,$2,$3,$4,$5)`, th[:], id, exp, ip, userAgent); err != nil {
		return "", nil, unavailable("session_create", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET last_login_at=now() WHERE id=$1`, id); err != nil {
		return "", nil, unavailable("last_login", err)
	}
	u = &AdminUser{ID: id, Email: e, Role: role, CSRF: s.csrfFor(th[:]), ExpiresAt: exp, TOTPEnabled: totpEnabled}
	if err = audit(ctx, tx, Actor{UserID: id, Name: e}, "auth.login", "user", id, map[string]any{"ip": ip}); err != nil {
		return "", nil, unavailable("audit", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return "", nil, unavailable("commit", err)
	}
	return token, u, nil
}

// Session resolves a session cookie value to its user, or 401.
func (s *Service) Session(ctx context.Context, token string) (*AdminUser, error) {
	if token == "" || len(token) > 128 {
		return nil, newErr(401, "unauthenticated")
	}
	th := sha256.Sum256([]byte(token))
	var u AdminUser
	err := s.Pool.QueryRow(ctx, `SELECT u.id::text, u.email, u.role, u.totp_enabled, s.expires_at FROM sessions s JOIN users u ON u.id=s.user_id
WHERE s.token_hash=$1 AND s.expires_at > now() AND NOT u.disabled`, th[:]).Scan(&u.ID, &u.Email, &u.Role, &u.TOTPEnabled, &u.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, newErr(401, "unauthenticated")
	}
	if err != nil {
		return nil, unavailable("session", err)
	}
	u.CSRF = s.csrfFor(th[:])
	return &u, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	th := sha256.Sum256([]byte(token))
	_, err := s.Pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, th[:])
	return err
}

// hitNow charges a rate-limit bucket in its own transaction.
func (s *Service) hitNow(ctx context.Context, key string, window time.Duration, limit int) (time.Duration, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx)
	wait, ok, err := hit(ctx, tx, key, window, limit)
	if err != nil {
		return 0, false, err
	}
	return wait, ok, tx.Commit(ctx)
}

// ---------------------------------------------------------------- audit

// Actor identifies who performed a write: an admin user or the operator CLI.
type Actor struct {
	UserID string
	KeyID  string // set when an API key performed the write
	Name   string
}

var CLIActor = Actor{Name: "cli"}

func (u *AdminUser) Actor() Actor { return Actor{UserID: u.ID, Name: u.Email} }

func audit(ctx context.Context, tx pgx.Tx, a Actor, action, targetType, targetID string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	dj, _ := json.Marshal(details)
	_, err := tx.Exec(ctx, `INSERT INTO audit_log(user_id, api_key_id, actor, action, target_type, target_id, details) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		nullable(a.UserID), nullable(a.KeyID), a.Name, action, targetType, targetID, dj)
	return err
}

// Audit records a completed write. details must hold safe values only: never
// secrets, message content, variables or full recipient addresses.
func (s *Service) Audit(ctx context.Context, a Actor, action, targetType, targetID string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	dj, _ := json.Marshal(details)
	_, err := s.Pool.Exec(ctx, `INSERT INTO audit_log(user_id, api_key_id, actor, action, target_type, target_id, details) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		nullable(a.UserID), nullable(a.KeyID), a.Name, action, targetType, targetID, dj)
	if err != nil {
		return unavailable("audit", err)
	}
	return nil
}

func (s *Service) ListAudit(ctx context.Context, actor, action string, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT id, at, user_id::text AS user_id, api_key_id::text AS api_key_id, actor, action, target_type, target_id, details FROM audit_log
WHERE ($1='' OR actor=$1) AND ($2='' OR action LIKE $2 || '%') ORDER BY id DESC`, actor, action)
}

// UserIDByEmail finds a user for the operator CLI.
func (s *Service) UserIDByEmail(ctx context.Context, email string) (string, error) {
	var id string
	err := s.Pool.QueryRow(ctx, `SELECT id::text FROM users WHERE email=$1`, strings.ToLower(strings.TrimSpace(email))).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", &Error{Status: 404, Code: "not_found", Message: "user not found"}
	}
	return id, err
}

package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/oidc"
	"github.com/njiwa-tz/mail/internal/secure"
)

// SSOProviderInput is an OpenID Connect provider an operator configures.
// ClientSecret may be left empty on update to keep the stored one.
type SSOProviderInput struct {
	Name           string   `json:"name"`
	Issuer         string   `json:"issuer"`
	ClientID       string   `json:"client_id"`
	ClientSecret   string   `json:"client_secret"`
	AllowedDomains []string `json:"allowed_domains"`
	AutoCreate     bool     `json:"auto_create"`
	Enabled        *bool    `json:"enabled"`
}

var errSSOFailed = newErr(401, "sso_failed")

func normDomains(in []string) ([]string, error) {
	out := []string{}
	for _, d := range in {
		d = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(d), "@")))
		if d == "" {
			continue
		}
		if !domainRe.MatchString(d) {
			return nil, badRequest("allowed domains are like example.com")
		}
		out = append(out, d)
	}
	return out, nil
}

func (in *SSOProviderInput) check(create bool) error {
	in.Name = clean(strings.TrimSpace(in.Name), 60)
	in.Issuer = strings.TrimRight(strings.TrimSpace(in.Issuer), "/")
	in.ClientID = strings.TrimSpace(in.ClientID)
	if in.Name == "" || in.ClientID == "" || len(in.ClientID) > 300 {
		return badRequest("give the provider a name and a client id")
	}
	if !oidc.SecureURL(in.Issuer) {
		return badRequest("the issuer must be an https:// address")
	}
	if create && in.ClientSecret == "" {
		return badRequest("enter the client secret")
	}
	if len(in.ClientSecret) > 2000 {
		return badRequest("the client secret is too long")
	}
	var err error
	if in.AllowedDomains, err = normDomains(in.AllowedDomains); err != nil {
		return err
	}
	if in.AutoCreate && len(in.AllowedDomains) == 0 {
		return badRequest("creating users on first sign-in needs at least one allowed email domain")
	}
	return nil
}

func (s *Service) redirectURI() (string, error) {
	pu := strings.TrimRight(strings.TrimSpace(s.Opts.PublicURL), "/")
	if pu == "" {
		return "", &Error{Status: 409, Code: "public_url_missing", Message: "set PUBLIC_URL on the server: the provider needs the address to return to"}
	}
	return pu + "/admin/v1/auth/sso/callback", nil
}

// CreateSSOProvider adds a provider after checking that its discovery
// document is reachable and names the same issuer.
func (s *Service) CreateSSOProvider(ctx context.Context, in SSOProviderInput) (string, error) {
	if err := in.check(true); err != nil {
		return "", err
	}
	if _, err := s.oidc.Discover(ctx, in.Issuer); err != nil {
		return "", &Error{Status: 422, Code: "sso_issuer_unreachable", Message: "the provider's discovery document could not be read: " + clean(err.Error(), 200)}
	}
	id := secure.NewUUID()
	sealed, err := s.Box.Seal([]byte(in.ClientSecret), []byte("sso:"+id))
	if err != nil {
		return "", unavailable("seal", err)
	}
	enabled := in.Enabled == nil || *in.Enabled
	tag, err := s.Pool.Exec(ctx, `INSERT INTO sso_providers(id, name, issuer, client_id, client_secret_enc, allowed_domains, auto_create, enabled)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (name) DO NOTHING`, id, in.Name, in.Issuer, in.ClientID, sealed, in.AllowedDomains, in.AutoCreate, enabled)
	if err != nil {
		return "", unavailable("sso_provider", err)
	}
	if tag.RowsAffected() == 0 {
		return "", &Error{Status: 409, Code: "sso_provider_exists", Message: "a provider with this name exists"}
	}
	return id, nil
}

func (s *Service) UpdateSSOProvider(ctx context.Context, id string, in SSOProviderInput) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	if err := in.check(false); err != nil {
		return err
	}
	if _, err := s.oidc.Discover(ctx, in.Issuer); err != nil {
		return &Error{Status: 422, Code: "sso_issuer_unreachable", Message: "the provider's discovery document could not be read: " + clean(err.Error(), 200)}
	}
	enabled := in.Enabled == nil || *in.Enabled
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE sso_providers SET name=$2, issuer=$3, client_id=$4, allowed_domains=$5, auto_create=$6, enabled=$7 WHERE id=$1`,
		id, in.Name, in.Issuer, in.ClientID, in.AllowedDomains, in.AutoCreate, enabled)
	if err != nil {
		if strings.Contains(err.Error(), "sso_providers_name_key") {
			return &Error{Status: 409, Code: "sso_provider_exists", Message: "a provider with this name exists"}
		}
		return unavailable("sso_provider", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	if in.ClientSecret != "" {
		sealed, err := s.Box.Seal([]byte(in.ClientSecret), []byte("sso:"+id))
		if err != nil {
			return unavailable("seal", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE sso_providers SET client_secret_enc=$2 WHERE id=$1`, id, sealed); err != nil {
			return unavailable("sso_provider", err)
		}
	}
	return tx.Commit(ctx)
}

func (s *Service) DeleteSSOProvider(ctx context.Context, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM sso_providers WHERE id=$1`, id)
	if err != nil {
		return unavailable("sso_provider", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

// ListSSOProviders is the operator's view; the secret is never returned.
func (s *Service) ListSSOProviders(ctx context.Context) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT id::text AS id, name, issuer, client_id, allowed_domains, auto_create, enabled, created_at,
  (SELECT count(*) FROM sso_identities i WHERE i.provider_id=sso_providers.id) AS linked_users
FROM sso_providers ORDER BY name`)
}

// PublicSSOProviders is what the sign-in page shows: names only.
func (s *Service) PublicSSOProviders(ctx context.Context) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT id::text AS id, name FROM sso_providers WHERE enabled ORDER BY name`)
}

func (s *Service) ssoThrottle(ctx context.Context, ip string) error {
	wait, ok, err := s.hitNow(ctx, "sso:ip:"+ip, 15*time.Minute, s.settingInt(ctx, "login_attempts_per_ip"))
	if err != nil {
		return unavailable("sso_throttle", err)
	}
	if !ok {
		return &Error{Status: 429, Code: "too_many_attempts", RetryAfter: wait}
	}
	return nil
}

// StartSSO returns the provider's authorization URL for a new sign-in.
func (s *Service) StartSSO(ctx context.Context, providerID, ip string) (string, error) {
	if !uuidRe.MatchString(providerID) {
		return "", newErr(404, "not_found")
	}
	redirect, err := s.redirectURI()
	if err != nil {
		return "", err
	}
	if err := s.ssoThrottle(ctx, ip); err != nil {
		return "", err
	}
	var issuer, clientID string
	err = s.Pool.QueryRow(ctx, `SELECT issuer, client_id FROM sso_providers WHERE id=$1 AND enabled`, providerID).Scan(&issuer, &clientID)
	if errors.Is(err, db.ErrNoRows) {
		return "", newErr(404, "not_found")
	}
	if err != nil {
		return "", unavailable("sso_provider", err)
	}
	p, err := s.oidc.Discover(ctx, issuer)
	if err != nil {
		slog.Warn("sso discovery failed", "err", err)
		return "", &Error{Status: 502, Code: "sso_unavailable"}
	}
	state, err1 := randToken(32)
	nonce, err2 := randToken(24)
	verifier, err3 := randToken(48)
	if err1 != nil || err2 != nil || err3 != nil {
		return "", unavailable("sso_random", errors.New("random source failed"))
	}
	sh := sha256.Sum256([]byte(state))
	sealed, err := s.Box.Seal([]byte(verifier), []byte("sso-state:"+hex.EncodeToString(sh[:])))
	if err != nil {
		return "", unavailable("seal", err)
	}
	if _, err := s.Pool.Exec(ctx, `DELETE FROM sso_states WHERE expires_at < now()`); err != nil {
		return "", unavailable("sso_state", err)
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO sso_states(state_hash, provider_id, nonce, verifier_enc, expires_at) VALUES ($1,$2,$3,$4, now() + interval '10 minutes')`,
		sh[:], providerID, nonce, sealed); err != nil {
		return "", unavailable("sso_state", err)
	}
	return oidc.AuthCodeURL(p, clientID, redirect, state, nonce, verifier), nil
}

// SSOResult is the end of a provider sign-in: a session, or a pending
// sign-in that still needs the account's authenticator code.
type SSOResult struct {
	Token   string
	User    *AdminUser
	Pending string
}

// FinishSSO handles the provider's redirect back. Every failure is the same
// 401 sso_failed; the reason goes to the log only.
func (s *Service) FinishSSO(ctx context.Context, state, code, ip, userAgent string) (*SSOResult, error) {
	fail := func(why string, err error) (*SSOResult, error) {
		slog.Warn("sso sign-in refused", "why", why, "err", err)
		return nil, errSSOFailed
	}
	if state == "" || code == "" || len(state) > 200 || len(code) > 4000 {
		return fail("missing state or code", nil)
	}
	if err := s.ssoThrottle(ctx, ip); err != nil {
		return nil, err
	}
	sh := sha256.Sum256([]byte(state))
	var providerID, nonce string
	var sealed []byte
	// The state is used once, whatever happens next.
	err := s.Pool.QueryRow(ctx, `DELETE FROM sso_states WHERE state_hash=$1 AND expires_at > now() RETURNING provider_id::text, nonce, verifier_enc`, sh[:]).Scan(&providerID, &nonce, &sealed)
	if errors.Is(err, db.ErrNoRows) {
		return fail("unknown or expired state", nil)
	}
	if err != nil {
		return nil, unavailable("sso_state", err)
	}
	verifier, err := s.Box.Open(sealed, []byte("sso-state:"+hex.EncodeToString(sh[:])))
	if err != nil {
		return fail("verifier unreadable", err)
	}
	var issuer, clientID string
	var secretEnc []byte
	var domains []string
	var autoCreate bool
	err = s.Pool.QueryRow(ctx, `SELECT issuer, client_id, client_secret_enc, allowed_domains, auto_create FROM sso_providers WHERE id=$1 AND enabled`, providerID).
		Scan(&issuer, &clientID, &secretEnc, &domains, &autoCreate)
	if err != nil {
		return fail("provider gone or disabled", err)
	}
	secret, err := s.Box.Open(secretEnc, []byte("sso:"+providerID))
	if err != nil {
		return fail("client secret unreadable", err)
	}
	redirect, err := s.redirectURI()
	if err != nil {
		return fail("no public url", err)
	}
	p, err := s.oidc.Discover(ctx, issuer)
	if err != nil {
		return fail("discovery", err)
	}
	raw, err := s.oidc.Exchange(ctx, p, clientID, string(secret), redirect, code, string(verifier))
	if err != nil {
		return fail("code exchange", err)
	}
	claims, err := s.oidc.Verify(ctx, p, raw, clientID, nonce)
	if err != nil {
		return fail("id token", err)
	}
	at := strings.LastIndexByte(claims.Email, '@')
	if at < 1 {
		return fail("email shape", nil)
	}
	domain := claims.Email[at+1:]
	domainOK := len(domains) == 0
	for _, d := range domains {
		domainOK = domainOK || d == domain
	}
	if !domainOK {
		return fail("email domain not allowed", nil)
	}
	userID, err := s.ssoUser(ctx, providerID, claims, autoCreate && len(domains) > 0)
	if err != nil {
		return fail("user", err)
	}
	var mfa bool
	if err := s.Pool.QueryRow(ctx, `SELECT totp_enabled FROM users WHERE id=$1`, userID).Scan(&mfa); err != nil {
		return nil, unavailable("sso_user", err)
	}
	if mfa {
		pend, err := randToken(32)
		if err != nil {
			return nil, unavailable("sso_random", err)
		}
		ph := sha256.Sum256([]byte(pend))
		if _, err := s.Pool.Exec(ctx, `DELETE FROM sso_pending WHERE expires_at < now()`); err != nil {
			return nil, unavailable("sso_pending", err)
		}
		if _, err := s.Pool.Exec(ctx, `INSERT INTO sso_pending(token_hash, user_id, expires_at) VALUES ($1,$2, now() + interval '5 minutes')`, ph[:], userID); err != nil {
			return nil, unavailable("sso_pending", err)
		}
		return &SSOResult{Pending: pend}, nil
	}
	tok, u, err := s.openSession(ctx, userID, ip, userAgent, "sso", false, false)
	if err != nil {
		return nil, err
	}
	return &SSOResult{Token: tok, User: u}, nil
}

// ssoUser finds the account of a verified identity: by the provider's
// subject first, then by email (linking the subject), else creates one when
// the provider allows it. Disabled accounts never sign in.
func (s *Service) ssoUser(ctx context.Context, providerID string, cl *oidc.Claims, create bool) (string, error) {
	var id string
	var disabled bool
	var role string
	err := s.Pool.QueryRow(ctx, `SELECT u.id::text, u.disabled, u.role FROM sso_identities i JOIN users u ON u.id=i.user_id WHERE i.provider_id=$1 AND i.subject=$2`,
		providerID, cl.Subject).Scan(&id, &disabled, &role)
	if err == nil {
		if disabled || role != RoleAdmin {
			return "", errors.New("account disabled")
		}
		return id, nil
	}
	if !errors.Is(err, db.ErrNoRows) {
		return "", err
	}
	err = s.Pool.QueryRow(ctx, `SELECT id::text, disabled, role FROM users WHERE email=$1`, cl.Email).Scan(&id, &disabled, &role)
	switch {
	case err == nil:
		if disabled || role != RoleAdmin {
			return "", errors.New("account disabled")
		}
	case errors.Is(err, db.ErrNoRows):
		if !create {
			return "", errors.New("no account for this email and creation is off")
		}
		pw, rerr := randToken(36) // nobody knows it; the account signs in through the provider
		if rerr != nil {
			return "", rerr
		}
		if id, err = s.CreateUserWith(ctx, cl.Email, pw, RoleAdmin, false); err != nil {
			return "", err
		}
		if err := s.Audit(ctx, Actor{Name: "sso"}, "user.create_sso", "user", id, map[string]any{"provider_id": providerID}); err != nil {
			slog.Warn("sso audit failed", "err", err)
		}
	default:
		return "", err
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO sso_identities(provider_id, subject, user_id) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, providerID, cl.Subject, id); err != nil {
		return "", err
	}
	return id, nil
}

// CompleteSSO takes the authenticator (or recovery) code of an account whose
// provider sign-in is pending. Five wrong codes end the pending sign-in.
func (s *Service) CompleteSSO(ctx context.Context, pending, code, ip, userAgent string) (string, *AdminUser, error) {
	if pending == "" || len(pending) > 200 {
		return "", nil, errSSOFailed
	}
	if err := s.ssoThrottle(ctx, ip); err != nil {
		return "", nil, err
	}
	ph := sha256.Sum256([]byte(pending))
	var userID string
	var attempts int
	err := s.Pool.QueryRow(ctx, `UPDATE sso_pending SET attempts=attempts+1 WHERE token_hash=$1 AND expires_at > now() RETURNING user_id::text, attempts`, ph[:]).Scan(&userID, &attempts)
	if errors.Is(err, db.ErrNoRows) {
		return "", nil, errSSOFailed
	}
	if err != nil {
		return "", nil, unavailable("sso_pending", err)
	}
	if attempts > 5 {
		_, _ = s.Pool.Exec(ctx, `DELETE FROM sso_pending WHERE token_hash=$1`, ph[:])
		return "", nil, errSSOFailed
	}
	if strings.TrimSpace(code) == "" {
		return "", nil, errMFARequired
	}
	recovery, err := s.checkSecondFactor(ctx, userID, code)
	if err != nil {
		return "", nil, err
	}
	if _, err := s.Pool.Exec(ctx, `DELETE FROM sso_pending WHERE token_hash=$1`, ph[:]); err != nil {
		return "", nil, unavailable("sso_pending", err)
	}
	return s.openSession(ctx, userID, ip, userAgent, "sso", true, recovery)
}

// openSession starts a session for a user who has been authenticated by
// other means (a provider). recovery ends the account's other sessions.
func (s *Service) openSession(ctx context.Context, userID, ip, userAgent, method string, mfa, recovery bool) (string, *AdminUser, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, unavailable("session_token", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	th := sha256.Sum256([]byte(token))
	exp := time.Now().Add(s.Opts.SessionTTL)
	if len(userAgent) > 200 {
		userAgent = userAgent[:200]
		for !utf8.ValidString(userAgent) {
			userAgent = userAgent[:len(userAgent)-1]
		}
	}
	requireMFA := s.settingInt(ctx, "require_mfa") == 1
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", nil, unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	var email, role string
	var version int64
	var disabled, operator, totp bool
	if err := tx.QueryRow(ctx, `SELECT email, role, auth_version, disabled, installation_operator, totp_enabled FROM users WHERE id=$1 FOR UPDATE`, userID).
		Scan(&email, &role, &version, &disabled, &operator, &totp); err != nil {
		return "", nil, unavailable("login_state", err)
	}
	if disabled || role != RoleAdmin {
		return "", nil, errSSOFailed
	}
	if recovery {
		if err := tx.QueryRow(ctx, `UPDATE users SET auth_version=auth_version+1 WHERE id=$1 RETURNING auth_version`, userID).Scan(&version); err != nil {
			return "", nil, unavailable("session_revoke", err)
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO sessions(token_hash, user_id, auth_version, expires_at, ip, user_agent) VALUES ($1,$2,$3,$4,$5,$6)`, th[:], userID, version, exp, ip, userAgent); err != nil {
		return "", nil, unavailable("session_create", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET last_login_at=now() WHERE id=$1`, userID); err != nil {
		return "", nil, unavailable("last_login", err)
	}
	u := &AdminUser{ID: userID, Email: email, Role: role, Operator: operator, MFAEnabled: totp, CSRF: s.csrfFor(th[:]), ExpiresAt: exp, tokenHash: th[:]}
	u.MFAEnrollmentRequired = !totp && requireMFA
	if err := audit(ctx, tx, Actor{UserID: userID, Name: email}, "auth.login", "user", userID, map[string]any{"ip": ip, "method": method, "mfa": mfa, "recovery_code": recovery}); err != nil {
		return "", nil, unavailable("audit", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", nil, unavailable("commit", err)
	}
	return token, u, nil
}

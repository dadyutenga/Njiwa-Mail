package test

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/service"
)

// totpCode computes the authenticator code for a base32 secret at now+offset steps.
func totpCode(t *testing.T, b32secret string, offsetSteps int64) string {
	t.Helper()
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(b32secret)
	if err != nil {
		t.Fatal(err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(time.Now().Unix()/30+offsetSteps))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[off])&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	return fmt.Sprintf("%06d", bin%1000000)
}

func (e *env) loginWith(email, code string) resp {
	body := map[string]any{"email": email, "password": adminPass}
	if code != "" {
		body["code"] = code
	}
	return e.raw("POST", "/admin/v1/auth/login", nil, body)
}

// enrol turns MFA on for a signed-in user and returns the secret and recovery codes.
func (e *env) enrol(s session) (secret string, recovery []string) {
	e.t.Helper()
	secret = str(e.admOK(s, "POST", "/admin/v1/auth/mfa/enroll", map[string]any{}).JSON, "secret")
	r := e.admOK(s, "POST", "/admin/v1/auth/mfa/confirm", map[string]any{"code": totpCode(e.t, secret, 0)})
	for _, c := range r.JSON["recovery_codes"].([]any) {
		recovery = append(recovery, c.(string))
	}
	return
}

// F09: only installation operators reach installation-level routes; other
// admins keep their project work; the last operator cannot be removed.
func TestInstallationOperatorPermission(t *testing.T) {
	e := setup(t)
	op := e.user("operator@njiwa.co.tz", "admin")
	e.provision("alpha")
	r := e.admOK(op, "POST", "/admin/v1/users", map[string]any{"email": "limited@njiwa.co.tz", "password": adminPass, "installation_operator": false})
	limitedID := str(r.JSON, "id")
	limited := e.login("limited@njiwa.co.tz")

	if me := e.admOK(limited, "GET", "/admin/v1/auth/me", nil); me.JSON["installation_operator"] != false {
		t.Fatalf("me: %s", me.Raw)
	}
	for _, rt := range [][2]string{{"GET", "/admin/v1/installation"}, {"GET", "/admin/v1/installation/transport-profiles"},
		{"PUT", "/admin/v1/installation/wizard"}, {"POST", "/admin/v1/installation/transport-profiles"},
		{"GET", "/admin/v1/domain-claims"}, {"POST", "/admin/v1/domain-claims/transfer"},
		{"GET", "/admin/v1/mail-server"}, {"PUT", "/admin/v1/mail-server"}, {"POST", "/admin/v1/mail-server/test"},
		{"GET", "/admin/v1/users"}, {"POST", "/admin/v1/users"}, {"PUT", "/admin/v1/settings"}} {
		res := e.adm(limited, rt[0], rt[1], map[string]any{})
		if res.Code != 403 || errCode(res) != "operator_required" {
			t.Errorf("%s %s as restricted admin: %d %s", rt[0], rt[1], res.Code, res.Raw)
		}
		if res := e.adm(op, rt[0], rt[1], map[string]any{}); res.Code == 403 && errCode(res) == "operator_required" {
			t.Errorf("%s %s refused an operator", rt[0], rt[1])
		}
	}
	// Project-level work is untouched.
	for _, p := range []string{"/admin/v1/overview", "/admin/v1/projects", "/admin/v1/keys", "/admin/v1/audit", "/admin/v1/settings"} {
		e.admOK(limited, "GET", p, nil)
	}

	// The last operator cannot be demoted or disabled.
	opID := ""
	for _, u := range items(e.admOK(op, "GET", "/admin/v1/users", nil)) {
		if str(u, "email") == "operator@njiwa.co.tz" {
			opID = str(u, "id")
		}
	}
	if res := e.adm(op, "PATCH", "/admin/v1/users/"+opID, map[string]any{"installation_operator": false}); res.Code != 409 || errCode(res) != "last_operator" {
		t.Fatalf("demote last operator: %d %s", res.Code, res.Raw)
	}
	if res := e.adm(op, "PATCH", "/admin/v1/users/"+opID, map[string]any{"disabled": true}); res.Code != 409 {
		t.Fatalf("disable last user: %d %s", res.Code, res.Raw)
	}
	// Promote, then the former operator may be demoted; the demoted session ends.
	e.admOK(op, "PATCH", "/admin/v1/users/"+limitedID, map[string]any{"installation_operator": true})
	if res := e.adm(limited, "GET", "/admin/v1/users", nil); res.Code != 401 {
		t.Fatalf("session should end on a permission change: %d", res.Code)
	}
	limited = e.login("limited@njiwa.co.tz")
	e.admOK(limited, "GET", "/admin/v1/users", nil)
	e.admOK(limited, "PATCH", "/admin/v1/users/"+opID, map[string]any{"installation_operator": false})
	if n := e.count(`SELECT count(*) FROM audit_log WHERE action='user.operator'`); n != 2 {
		t.Fatalf("audit rows for operator changes: %d", n)
	}
}

// The first user is always an operator, even when asked otherwise.
func TestFirstUserIsAlwaysOperator(t *testing.T) {
	e := setup(t)
	if _, err := e.svc.CreateUserWith(t.Context(), "first@njiwa.co.tz", adminPass, "admin", false); err != nil {
		t.Fatal(err)
	}
	s := e.login("first@njiwa.co.tz")
	e.admOK(s, "GET", "/admin/v1/installation", nil)
}

func TestMFAEnrolLoginReplayAndRecovery(t *testing.T) {
	e := setup(t)
	s := e.user("mfa@njiwa.co.tz", "admin")
	e.must(e.svc.UpdateSettings(t.Context(), map[string]int{"login_attempts_per_account": 200, "login_attempts_per_ip": 500}))

	// A wrong code during confirmation does not enable anything.
	secret := str(e.admOK(s, "POST", "/admin/v1/auth/mfa/enroll", map[string]any{}).JSON, "secret")
	if !strings.HasPrefix(str(e.admOK(s, "POST", "/admin/v1/auth/mfa/enroll", map[string]any{}).JSON, "otpauth_uri"), "otpauth://totp/") {
		t.Fatal("no otpauth uri")
	}
	secret = str(e.admOK(s, "POST", "/admin/v1/auth/mfa/enroll", map[string]any{}).JSON, "secret") // re-start replaces the pending secret
	if res := e.adm(s, "POST", "/admin/v1/auth/mfa/confirm", map[string]any{"code": "000000"}); res.Code != 401 || errCode(res) != "invalid_mfa_code" {
		t.Fatalf("bad confirm: %d %s", res.Code, res.Raw)
	}
	if r := e.loginWith("mfa@njiwa.co.tz", ""); r.Code != 200 {
		t.Fatalf("not enabled yet, password-only login must work: %d", r.Code)
	}
	spent := totpCode(t, secret, 0) // the exact code used to confirm (a later "current" code may belong to the next 30 s step)
	r := e.admOK(s, "POST", "/admin/v1/auth/mfa/confirm", map[string]any{"code": spent})
	recovery := r.JSON["recovery_codes"].([]any)
	if len(recovery) != 10 {
		t.Fatalf("recovery codes: %v", recovery)
	}
	if res := e.adm(s, "POST", "/admin/v1/auth/mfa/enroll", map[string]any{}); res.Code != 409 {
		t.Fatalf("enrol again: %d", res.Code)
	}
	if n := e.count(`SELECT count(*) FROM audit_log WHERE action='mfa.enable'`); n != 1 {
		t.Fatalf("audit: %d", n)
	}

	// Password alone is no longer enough; a wrong code is refused.
	if res := e.loginWith("mfa@njiwa.co.tz", ""); res.Code != 401 || errCode(res) != "mfa_required" {
		t.Fatalf("no code: %d %s", res.Code, res.Raw)
	}
	if res := e.loginWith("mfa@njiwa.co.tz", "123456"); res.Code != 401 || errCode(res) != "invalid_mfa_code" {
		t.Fatalf("wrong code: %d %s", res.Code, res.Raw)
	}
	// Confirmation used the current step, so that code is spent; the next step works once.
	if res := e.loginWith("mfa@njiwa.co.tz", spent); res.Code != 401 {
		t.Fatalf("code used at confirmation must not be accepted again: %d %s", res.Code, res.Raw)
	}
	next := totpCode(t, secret, 1)
	if res := e.loginWith("mfa@njiwa.co.tz", next); res.Code != 200 || res.JSON["mfa_enabled"] != true {
		t.Fatalf("valid code: %d %s", res.Code, res.Raw)
	}
	if res := e.loginWith("mfa@njiwa.co.tz", next); res.Code != 401 {
		t.Fatalf("replayed code accepted: %d %s", res.Code, res.Raw)
	}

	// A recovery code works once.
	rc := recovery[0].(string)
	if res := e.loginWith("mfa@njiwa.co.tz", strings.ToLower(rc)); res.Code != 200 {
		t.Fatalf("recovery code: %d %s", res.Code, res.Raw)
	}
	if res := e.loginWith("mfa@njiwa.co.tz", rc); res.Code != 401 {
		t.Fatalf("recovery code reused: %d", res.Code)
	}
	var stored string
	e.must(e.pool.QueryRow(t.Context(), `SELECT array_to_string(recovery_hashes, ',') FROM users WHERE email='mfa@njiwa.co.tz'`).Scan(&stored))
	if strings.Contains(stored, rc) || strings.Contains(stored, strings.ReplaceAll(rc, "-", "")) {
		t.Fatal("recovery codes are stored in the clear")
	}
	var enc []byte
	e.must(e.pool.QueryRow(t.Context(), `SELECT totp_secret_enc FROM users WHERE email='mfa@njiwa.co.tz'`).Scan(&enc))
	if len(enc) == 0 || strings.Contains(string(enc), secret) {
		t.Fatal("secret not sealed")
	}
	if n := e.count(`SELECT count(*) FROM audit_log WHERE action='auth.login' AND details->>'recovery_code'='true'`); n != 1 {
		t.Fatalf("recovery login audit: %d", n)
	}
}

func TestMFALockoutAndDisable(t *testing.T) {
	e := setup(t)
	s := e.user("lock@njiwa.co.tz", "admin")
	e.must(e.svc.UpdateSettings(t.Context(), map[string]int{"login_attempts_per_account": 200, "login_attempts_per_ip": 500}))
	secret, recovery := e.enrol(s)
	for i := 0; i < 5; i++ {
		if res := e.loginWith("lock@njiwa.co.tz", "111111"); res.Code != 401 {
			t.Fatalf("attempt %d: %d", i, res.Code)
		}
	}
	// Locked: even a correct code is refused for a while.
	res := e.loginWith("lock@njiwa.co.tz", totpCode(t, secret, 1))
	if res.Code != 429 || errCode(res) != "mfa_locked" {
		t.Fatalf("locked: %d %s", res.Code, res.Raw)
	}
	_, err := e.pool.Exec(t.Context(), `UPDATE users SET totp_locked_until=now() - interval '1 second' WHERE email='lock@njiwa.co.tz'`)
	e.must(err)
	if res := e.loginWith("lock@njiwa.co.tz", totpCode(t, secret, 1)); res.Code != 200 {
		t.Fatalf("after lockout: %d %s", res.Code, res.Raw)
	}

	// Disabling needs password and a valid code.
	// (A TOTP step is single use, so recovery codes serve for the next sign-in and the disable.)
	r := e.loginWith("lock@njiwa.co.tz", recovery[0])
	if r.Code != 200 {
		t.Fatalf("recovery sign-in: %d %s", r.Code, r.Raw)
	}
	s = session{cookie: strings.SplitN(r.Hdr.Get("Set-Cookie"), ";", 2)[0], csrf: str(r.JSON, "csrf_token")}
	if res := e.adm(s, "POST", "/admin/v1/auth/mfa/disable", map[string]any{"password": "wrong-password-here", "code": recovery[1]}); res.Code != 401 {
		t.Fatalf("disable wrong password: %d %s", res.Code, res.Raw)
	}
	e.admOK(s, "POST", "/admin/v1/auth/mfa/disable", map[string]any{"password": adminPass, "code": recovery[1]})
	if res := e.loginWith("lock@njiwa.co.tz", ""); res.Code != 200 || res.JSON["mfa_enabled"] != false {
		t.Fatalf("after disable: %d %s", res.Code, res.Raw)
	}
}

func TestMFAMissingSecretRejectsEmptyKeyCode(t *testing.T) {
	e := setup(t)
	s := e.user("missing-secret@njiwa.co.tz", "admin")
	e.enrol(s)
	_, err := e.pool.Exec(t.Context(), `UPDATE users SET totp_secret_enc=NULL WHERE email='missing-secret@njiwa.co.tz'`)
	e.must(err)
	// An empty HMAC key is public knowledge, even though its TOTP looks valid.
	res := e.loginWith("missing-secret@njiwa.co.tz", totpCode(t, "", 1))
	if res.Code != 401 || errCode(res) != "invalid_mfa_code" {
		t.Fatalf("empty-key TOTP accepted: %d %s", res.Code, res.Raw)
	}
}

func TestMFAConcurrentFailuresLockAccount(t *testing.T) {
	e := setup(t)
	s := e.user("concurrent-mfa@njiwa.co.tz", "admin")
	secret, _ := e.enrol(s)
	e.must(e.svc.UpdateSettings(t.Context(), map[string]int{"login_attempts_per_account": 200, "login_attempts_per_ip": 500}))
	const attempts = 5
	var wg sync.WaitGroup
	errs := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := e.svc.LoginMFA(context.Background(), "concurrent-mfa@njiwa.co.tz", adminPass, "invalid", "127.0.0.1", "test")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if e, ok := service.IsError(err); !ok || e.Code != "invalid_mfa_code" {
			t.Fatalf("concurrent failure: %v", err)
		}
	}
	_, _, err := e.svc.LoginMFA(t.Context(), "concurrent-mfa@njiwa.co.tz", adminPass, totpCode(t, secret, 1), "127.0.0.1", "test")
	if e, ok := service.IsError(err); !ok || e.Code != "mfa_locked" {
		t.Fatalf("account not locked after five failures: %v", err)
	}
}

func TestRequireMFAPolicyAndOperatorReset(t *testing.T) {
	e := setup(t)
	op := e.user("op@njiwa.co.tz", "admin")
	other := e.user("other@njiwa.co.tz", "admin")
	e.must(e.svc.UpdateSettings(t.Context(), map[string]int{"login_attempts_per_account": 200, "login_attempts_per_ip": 500}))
	e.admOK(op, "PUT", "/admin/v1/settings", map[string]int{"require_mfa": 1})

	if res := e.adm(op, "GET", "/admin/v1/users", nil); res.Code != 403 {
		t.Fatalf("the operator is bound by the policy too: %d", res.Code)
	}
	e.enrol(op)
	// Existing sessions are caught immediately: everything but enrolment is refused.
	if res := e.adm(other, "GET", "/admin/v1/overview", nil); res.Code != 403 || errCode(res) != "mfa_enrollment_required" {
		t.Fatalf("policy: %d %s", res.Code, res.Raw)
	}
	if me := e.admOK(other, "GET", "/admin/v1/auth/me", nil); me.JSON["mfa_enrollment_required"] != true {
		t.Fatalf("me: %s", me.Raw)
	}
	secret, _ := e.enrol(other)
	e.admOK(other, "GET", "/admin/v1/overview", nil)

	// MFA cannot be switched off while the policy requires it.
	if res := e.adm(other, "POST", "/admin/v1/auth/mfa/disable", map[string]any{"password": adminPass, "code": totpCode(t, secret, 1)}); res.Code != 409 || errCode(res) != "mfa_required_by_policy" {
		t.Fatalf("disable under policy: %d %s", res.Code, res.Raw)
	}

	// Lost device: an operator resets it; the user's sessions end and they enrol again.
	otherID := ""
	for _, u := range items(e.admOK(op, "GET", "/admin/v1/users", nil)) {
		if str(u, "email") == "other@njiwa.co.tz" {
			otherID = str(u, "id")
			if u["mfa_enabled"] != true {
				t.Fatal("users list should show mfa_enabled")
			}
		}
	}
	e.admOK(op, "PATCH", "/admin/v1/users/"+otherID, map[string]any{"reset_mfa": true})
	if res := e.adm(other, "GET", "/admin/v1/auth/me", nil); res.Code != 401 {
		t.Fatalf("session after reset: %d", res.Code)
	}
	again := e.login("other@njiwa.co.tz")
	if res := e.adm(again, "GET", "/admin/v1/overview", nil); res.Code != 403 {
		t.Fatalf("must enrol again: %d", res.Code)
	}
	if n := e.count(`SELECT count(*) FROM audit_log WHERE action='mfa.reset'`); n != 1 {
		t.Fatalf("audit: %d", n)
	}
	// The operator can lift the policy again.
	e.admOK(op, "PUT", "/admin/v1/settings", map[string]int{"require_mfa": 0})
	e.admOK(again, "GET", "/admin/v1/overview", nil)
}

// T1.2: signing in with a recovery code (lost authenticator) ends every other
// session of the account; a TOTP sign-in does not.
func TestRecoveryCodeSignInEndsOtherSessions(t *testing.T) {
	e := setup(t)
	s := e.user("lost@njiwa.co.tz", "admin")
	secret, recovery := e.enrol(s)
	other := e.loginWith("lost@njiwa.co.tz", totpCode(t, secret, 1))
	if other.Code != 200 {
		t.Fatalf("%d %s", other.Code, other.Raw)
	}
	o := session{cookie: strings.SplitN(other.Hdr.Get("Set-Cookie"), ";", 2)[0], csrf: str(other.JSON, "csrf_token")}
	if r := e.adm(s, "GET", "/admin/v1/auth/me", nil); r.Code != 200 {
		t.Fatalf("a TOTP sign-in must not end the first session: %d", r.Code)
	}
	r := e.loginWith("lost@njiwa.co.tz", recovery[0])
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	n := session{cookie: strings.SplitN(r.Hdr.Get("Set-Cookie"), ";", 2)[0], csrf: str(r.JSON, "csrf_token")}
	for _, old := range []session{s, o} {
		if res := e.adm(old, "GET", "/admin/v1/auth/me", nil); res.Code != 401 {
			t.Fatalf("session from before the recovery sign-in still works: %d", res.Code)
		}
	}
	if res := e.adm(n, "GET", "/admin/v1/auth/me", nil); res.Code != 200 {
		t.Fatalf("new session: %d %s", res.Code, res.Raw)
	}
}

// T1.2: changing a user's operator access, password or enabled state ends that
// user's existing sessions immediately; an unrelated change does not.
func TestAccountChangesEndSessions(t *testing.T) {
	e := setup(t)
	op := e.user("op@njiwa.co.tz", "admin")
	idOf := func(email string) string {
		var id string
		e.must(e.pool.QueryRow(t.Context(), `SELECT id::text FROM users WHERE email=$1`, email).Scan(&id))
		return id
	}
	for _, c := range []struct {
		name string
		body map[string]any
	}{
		{"operator access removed", map[string]any{"installation_operator": false}},
		{"password changed", map[string]any{"password": "another-long-password-1"}},
		{"account disabled", map[string]any{"disabled": true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			email := strings.ReplaceAll(c.name, " ", "-") + "@njiwa.co.tz"
			u := e.user(email, "admin")
			if r := e.adm(u, "GET", "/admin/v1/auth/me", nil); r.Code != 200 {
				t.Fatalf("before: %d", r.Code)
			}
			e.admOK(op, "PATCH", "/admin/v1/users/"+idOf(email), c.body)
			if r := e.adm(u, "GET", "/admin/v1/auth/me", nil); r.Code != 401 {
				t.Fatalf("session survived %s: %d", c.name, r.Code)
			}
			if r := e.adm(op, "GET", "/admin/v1/auth/me", nil); r.Code != 200 {
				t.Fatalf("the operator's own session ended: %d", r.Code)
			}
		})
	}
	// Setting the same operator value again is not a change.
	u := e.user("same@njiwa.co.tz", "admin")
	e.admOK(op, "PATCH", "/admin/v1/users/"+idOf("same@njiwa.co.tz"), map[string]any{"installation_operator": true})
	if r := e.adm(u, "GET", "/admin/v1/auth/me", nil); r.Code != 200 {
		t.Fatalf("an unchanged permission ended the session: %d", r.Code)
	}
}

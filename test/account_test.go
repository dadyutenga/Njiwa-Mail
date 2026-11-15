package test

import (
	"context"
	"strings"
	"testing"
)

func TestAccountPasswordSessionsAndAudit(t *testing.T) {
	e := setup(t)
	op := e.user("operator@njiwa.co.tz", "admin")
	e.must(func() error {
		_, err := e.svc.CreateUserWith(context.Background(), "ann@njiwa.co.tz", adminPass, "admin", false)
		return err
	}())
	ann := e.login("ann@njiwa.co.tz")
	other := e.login("ann@njiwa.co.tz") // a second device

	// Wrong current password, weak and unchanged new passwords are refused.
	if r := e.adm(ann, "POST", "/admin/v1/auth/password", map[string]any{"current_password": "nope-nope-nope-nope", "new_password": "a-new-long-passphrase-1"}); r.Code != 401 {
		t.Fatalf("wrong current: %d %s", r.Code, r.Raw)
	}
	if r := e.adm(ann, "POST", "/admin/v1/auth/password", map[string]any{"current_password": adminPass, "new_password": "short"}); r.Code != 400 && r.Code != 422 {
		t.Fatalf("weak: %d %s", r.Code, r.Raw)
	}
	if r := e.adm(ann, "POST", "/admin/v1/auth/password", map[string]any{"current_password": adminPass, "new_password": adminPass}); r.Code != 400 {
		t.Fatalf("same: %d %s", r.Code, r.Raw)
	}
	const newPass = "a-new-long-passphrase-1"
	e.admOK(ann, "POST", "/admin/v1/auth/password", map[string]any{"current_password": adminPass, "new_password": newPass})
	// This session stays; the other device is signed out; the old password no longer works.
	e.admOK(ann, "GET", "/admin/v1/auth/me", nil)
	if r := e.adm(other, "GET", "/admin/v1/auth/me", nil); r.Code != 401 {
		t.Fatalf("other device after password change: %d", r.Code)
	}
	if r := e.raw("POST", "/admin/v1/auth/login", nil, map[string]any{"email": "ann@njiwa.co.tz", "password": adminPass}); r.Code != 401 {
		t.Fatalf("old password: %d", r.Code)
	}
	if r := e.raw("POST", "/admin/v1/auth/login", nil, map[string]any{"email": "ann@njiwa.co.tz", "password": newPass}); r.Code != 200 {
		t.Fatalf("new password: %d %s", r.Code, r.Raw)
	}

	// Revoke the other sessions.
	d1 := e.raw("POST", "/admin/v1/auth/login", nil, map[string]any{"email": "ann@njiwa.co.tz", "password": newPass})
	d2 := e.raw("POST", "/admin/v1/auth/login", nil, map[string]any{"email": "ann@njiwa.co.tz", "password": newPass})
	dev2 := session{cookie: strings.SplitN(d2.Hdr.Get("Set-Cookie"), ";", 2)[0], csrf: str(d2.JSON, "csrf_token")}
	_ = d1
	if n := len(e.admOK(ann, "GET", "/admin/v1/auth/sessions", nil).JSON["items"].([]any)); n < 3 {
		t.Fatalf("sessions: %d", n)
	}
	rv := e.admOK(ann, "POST", "/admin/v1/auth/sessions/revoke-others", map[string]any{})
	if rv.JSON["revoked"].(float64) < 2 {
		t.Fatalf("revoke others: %s", rv.Raw)
	}
	if r := e.adm(dev2, "GET", "/admin/v1/auth/me", nil); r.Code != 401 {
		t.Fatalf("revoked session still works: %d", r.Code)
	}
	e.admOK(ann, "GET", "/admin/v1/auth/me", nil)
	if n := len(e.admOK(ann, "GET", "/admin/v1/auth/sessions", nil).JSON["items"].([]any)); n != 1 {
		t.Fatalf("sessions left: %d", n)
	}

	// Own audit trail: only this user's actions, filterable.
	e.admOK(op, "GET", "/admin/v1/overview", nil)
	au := e.admOK(ann, "GET", "/admin/v1/auth/audit", nil)
	if !strings.Contains(au.Raw, "auth.password_change") || strings.Contains(au.Raw, "operator@njiwa.co.tz") {
		t.Fatalf("own audit: %s", au.Raw)
	}
	if f := e.admOK(ann, "GET", "/admin/v1/auth/audit?action=auth.password", nil); len(f.JSON["items"].([]any)) != 1 {
		t.Fatalf("audit filter: %s", f.Raw)
	}

	// Operators see and end another user's sessions and read their audit; others cannot.
	annID := str(e.admOK(ann, "GET", "/admin/v1/auth/me", nil).JSON, "id")
	if r := e.adm(ann, "GET", "/admin/v1/users/"+annID+"/sessions", nil); r.Code != 403 {
		t.Fatalf("non-operator sessions view: %d", r.Code)
	}
	if r := e.adm(ann, "GET", "/admin/v1/users/"+annID+"/audit", nil); r.Code != 403 {
		t.Fatalf("non-operator audit view: %d", r.Code)
	}
	if n := len(e.admOK(op, "GET", "/admin/v1/users/"+annID+"/sessions", nil).JSON["items"].([]any)); n != 1 {
		t.Fatalf("operator sessions view: %d", n)
	}
	if !strings.Contains(e.admOK(op, "GET", "/admin/v1/users/"+annID+"/audit", nil).Raw, "auth.password_change") {
		t.Fatal("operator audit view")
	}
	e.admOK(op, "POST", "/admin/v1/users/"+annID+"/sessions/revoke", map[string]any{})
	if r := e.adm(ann, "GET", "/admin/v1/auth/me", nil); r.Code != 401 {
		t.Fatalf("operator revoke: %d", r.Code)
	}
	if n := e.count(`SELECT count(*) FROM audit_log WHERE action='user.sessions_revoked'`); n != 1 {
		t.Fatalf("operator revoke audited %d times", n)
	}
	if r := e.adm(op, "POST", "/admin/v1/users/00000000-0000-4000-8000-000000000000/sessions/revoke", map[string]any{}); r.Code != 404 {
		t.Fatalf("unknown user: %d", r.Code)
	}
}

func TestAccountDeletionGracePeriod(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	op := e.user("operator@njiwa.co.tz", "admin")
	// The only user cannot ask to leave.
	if r := e.adm(op, "POST", "/admin/v1/auth/delete-account", map[string]any{"password": adminPass}); r.Code != 409 || errCode(r) != "last_user" {
		t.Fatalf("last user: %d %s", r.Code, r.Raw)
	}
	_, err := e.svc.CreateUserWith(ctx, "ben@njiwa.co.tz", adminPass, "admin", false)
	e.must(err)
	ben := e.login("ben@njiwa.co.tz")
	benID := str(e.admOK(ben, "GET", "/admin/v1/auth/me", nil).JSON, "id")

	if r := e.adm(ben, "POST", "/admin/v1/auth/delete-account", map[string]any{"password": "wrong-wrong-wrong-1"}); r.Code != 401 {
		t.Fatalf("wrong password: %d", r.Code)
	}
	if r := e.adm(ben, "POST", "/admin/v1/auth/cancel-deletion", map[string]any{}); r.Code != 409 {
		t.Fatalf("nothing to cancel: %d", r.Code)
	}
	r := e.admOK(ben, "POST", "/admin/v1/auth/delete-account", map[string]any{"password": adminPass})
	if r.JSON["deletion_due_at"] == nil {
		t.Fatalf("no due date: %s", r.Raw)
	}
	// Asking twice keeps the first date; the account works and says it is pending.
	again := e.admOK(ben, "POST", "/admin/v1/auth/delete-account", map[string]any{"password": adminPass})
	if again.JSON["deletion_due_at"] != r.JSON["deletion_due_at"] {
		t.Fatalf("due date moved: %v vs %v", again.JSON, r.JSON)
	}
	if me := e.admOK(ben, "GET", "/admin/v1/auth/me", nil); me.JSON["deletion_due_at"] == nil {
		t.Fatalf("me hides the pending deletion: %s", me.Raw)
	}
	// Before the date nothing is removed; cancelling clears it.
	if n, err := e.svc.PurgeDueAccounts(ctx); err != nil || n != 0 {
		t.Fatalf("early purge: %d %v", n, err)
	}
	e.admOK(ben, "POST", "/admin/v1/auth/cancel-deletion", map[string]any{})
	if me := e.admOK(ben, "GET", "/admin/v1/auth/me", nil); me.JSON["deletion_due_at"] != nil {
		t.Fatalf("still pending: %s", me.Raw)
	}

	// MFA users must give a code.
	secret, _ := e.enrol(ben)
	if r := e.adm(ben, "POST", "/admin/v1/auth/delete-account", map[string]any{"password": adminPass}); r.Code != 401 {
		t.Fatalf("mfa user without a code: %d %s", r.Code, r.Raw)
	}
	e.admOK(ben, "POST", "/admin/v1/auth/delete-account", map[string]any{"password": adminPass, "code": totpCode(t, secret, 1)})

	// After the grace period the account and its sessions go; the audit trail says so.
	_, err = e.pool.Exec(ctx, `UPDATE users SET deletion_due_at = now() - interval '1 minute' WHERE id=$1`, benID)
	e.must(err)
	if n, err := e.svc.PurgeDueAccounts(ctx); err != nil || n != 1 {
		t.Fatalf("purge: %d %v", n, err)
	}
	if n := e.count(`SELECT count(*) FROM users WHERE id=$1`, benID); n != 0 {
		t.Fatal("account not removed")
	}
	if n := e.count(`SELECT count(*) FROM sessions WHERE user_id=$1`, benID); n != 0 {
		t.Fatal("sessions left")
	}
	if r := e.adm(ben, "GET", "/admin/v1/auth/me", nil); r.Code != 401 {
		t.Fatalf("deleted user: %d", r.Code)
	}
	if n := e.count(`SELECT count(*) FROM audit_log WHERE action='account.deleted' AND target_id=$1`, benID); n != 1 {
		t.Fatalf("deletion audit: %d", n)
	}

	// An account that became the last one in the meantime is kept and the request cleared.
	cy, err := e.svc.CreateUserWith(ctx, "cy@njiwa.co.tz", adminPass, "admin", false)
	e.must(err)
	_, err = e.pool.Exec(ctx, `UPDATE users SET deletion_due_at = now() - interval '1 minute', deletion_requested_at = now() WHERE id=$1`, cy)
	e.must(err)
	_, err = e.pool.Exec(ctx, `UPDATE users SET disabled=true WHERE id<>$1`, cy)
	e.must(err)
	if n, err := e.svc.PurgeDueAccounts(ctx); err != nil || n != 0 {
		t.Fatalf("last user purge: %d %v", n, err)
	}
	if n := e.count(`SELECT count(*) FROM users WHERE id=$1 AND deletion_due_at IS NULL`, cy); n != 1 {
		t.Fatal("blocked request not cleared")
	}
	if n := e.count(`SELECT count(*) FROM audit_log WHERE action='account.deletion_blocked'`); n != 1 {
		t.Fatalf("blocked audit: %d", n)
	}
}

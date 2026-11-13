package test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/service"
)

func TestOneRoleMigrationPreservesAccountsAndSessions(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	legacy := e.user("legacy@example.com", "owner")
	admin := e.user("admin@example.com", "admin")
	_, err := e.svc.CreateUser(ctx, "disabled@example.com", adminPass, "")
	e.must(err)
	exec := func(query string) {
		_, err := e.pool.Exec(ctx, query)
		e.must(err)
	}
	// Restore the previous schema and data to exercise an upgrade, rather than
	// only the fresh-install path used by setup.
	exec(`ALTER TABLE users DROP CONSTRAINT users_role_check`)
	exec(`ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('owner','admin'))`)
	exec(`UPDATE users SET role='owner' WHERE email IN ('legacy@example.com','disabled@example.com')`)
	exec(`UPDATE users SET disabled=true WHERE email='disabled@example.com'`)
	exec(`DELETE FROM schema_migrations WHERE version='0008_one_role.sql'`)
	e.must(db.Migrate(ctx, e.pool))
	e.must(db.Migrate(ctx, e.pool)) // Re-running migrations is safe.
	if e.count(`SELECT count(*) FROM users WHERE role='admin'`) != 3 || e.count(`SELECT count(*) FROM users WHERE disabled`) != 1 {
		t.Fatal("migration did not preserve and normalize accounts")
	}
	for _, sess := range []session{legacy, admin} {
		if r := e.admOK(sess, "GET", "/admin/v1/auth/me", nil); str(r.JSON, "role") != "admin" {
			t.Fatalf("existing session did not become admin: %s", r.Raw)
		}
		e.admOK(sess, "GET", "/admin/v1/users", nil)
	}
	e.login("legacy@example.com") // The existing password still works.
	if _, err := e.pool.Exec(ctx, `UPDATE users SET role='owner' WHERE email='legacy@example.com'`); err == nil {
		t.Fatal("database accepted a second role after migration")
	}
	_, recipients, err := e.svc.DailySummary(ctx)
	e.must(err)
	if len(recipients) != 2 || recipients[0] != "admin@example.com" || recipients[1] != "legacy@example.com" {
		t.Fatalf("summary recipients: %v", recipients)
	}
}

func TestLastEnabledAdminCannotBeDisabledConcurrently(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	var ids []string
	// Both are operators: the race is about the last enabled user. (Since
	// f08eff8 CreateUser makes non-operators except the first user, so the
	// first one's disable could be refused as last_operator instead, and the
	// result depended on goroutine order.)
	for _, email := range []string{"first@example.com", "second@example.com"} {
		id, err := e.svc.CreateUserWith(ctx, email, adminPass, "", true)
		e.must(err)
		ids = append(ids, id)
	}
	start := make(chan struct{})
	results := make(chan error, len(ids))
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Go(func() {
			<-start
			disabled := true
			results <- e.svc.UpdateUser(ctx, id, nil, &disabled, nil)
		})
	}
	close(start)
	wg.Wait()
	close(results)
	var succeeded, rejected int
	for err := range results {
		var apiErr *service.Error
		switch {
		case err == nil:
			succeeded++
		case errors.As(err, &apiErr) && apiErr.Status == 409 && apiErr.Code == "last_user":
			rejected++
		default:
			t.Fatalf("disable result: %v", err)
		}
	}
	if succeeded != 1 || rejected != 1 || e.count(`SELECT count(*) FROM users WHERE NOT disabled`) != 1 {
		t.Fatalf("last-user protection: succeeded=%d rejected=%d", succeeded, rejected)
	}
}

func TestSharedWorkspaceAccess(t *testing.T) {
	e := setup(t)
	e.provision("sacas")
	admin := e.user("test@example.com", "")
	for _, path := range []string{
		"/admin/v1/users", "/admin/v1/settings", "/admin/v1/mail-server",
		"/admin/v1/projects", "/admin/v1/audit", "/admin/v1/analytics",
		wsBase + "/context", wsBase + "/overview", wsBase + "/messages",
		wsBase + "/inbox", wsBase + "/domains", wsBase + "/mailboxes",
		wsBase + "/senders", wsBase + "/templates", wsBase + "/automations",
		wsBase + "/suppressions", wsBase + "/keys", wsBase + "/webhooks",
	} {
		if r := e.raw("GET", path, nil, nil); r.Code != 401 {
			t.Fatalf("anonymous GET %s: %d", path, r.Code)
		}
		e.admOK(admin, "GET", path, nil)
	}
	e.admOK(admin, "PUT", "/admin/v1/settings", map[string]int{"login_attempts_per_account": 20})
	e.admOK(admin, "POST", wsBase+"/privacy/delete", map[string]string{"email": "absent@example.com"})
	if r := e.raw("PUT", "/admin/v1/settings", map[string]string{"Cookie": admin.cookie}, map[string]int{"login_attempts_per_account": 30}); r.Code != 403 || errCode(r) != "csrf_token_invalid" {
		t.Fatalf("settings write without CSRF: %d %s", r.Code, r.Raw)
	}
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO users (email,password_hash) VALUES ('default@example.com','unused')`); err != nil {
		t.Fatalf("default role insert: %v", err)
	}
	if e.count(`SELECT count(*) FROM users WHERE email='default@example.com' AND role='admin'`) != 1 {
		t.Fatal("database did not default to the shared admin role")
	}
}

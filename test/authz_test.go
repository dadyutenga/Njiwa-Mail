package test

import (
	"context"
	"strings"
	"testing"

	"github.com/ditronics-tz/mail/internal/secure"
	"github.com/ditronics-tz/mail/internal/service"
)

// TestAuthorizationBoundaries checks the cases the UI cannot enforce:
// no session, another project's API key, and another operator's session or
// password-reset token.
func TestAuthorizationBoundaries(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	alpha := e.provision("alpha")
	beta := e.provision("beta")
	owner := e.user("owner@ditronics.co.tz", "owner")
	admin := e.user("admin@ditronics.co.tz", "admin")

	t.Run("unauthenticated", func(t *testing.T) {
		for _, p := range []string{"/admin/v1/overview", "/admin/v1/users", "/admin/v1/messages", "/admin/v1/sandbox", "/admin/v1/auth/sessions"} {
			if r := e.raw("GET", p, nil, nil); r.Code != 401 {
				t.Errorf("GET %s without a session: %d", p, r.Code)
			}
		}
		if r := e.raw("POST", "/admin/v1/users", nil, map[string]any{"email": "x@ditronics.co.tz", "password": adminPass, "role": "owner"}); r.Code != 401 {
			t.Errorf("anonymous user create: %d", r.Code)
		}
		if r := e.get("", "00000000-0000-4000-8000-000000000001"); r.Code != 401 {
			t.Errorf("anonymous email status: %d", r.Code)
		}
	})

	t.Run("api key cannot cross projects", func(t *testing.T) {
		sent := e.post(alpha.live, idem("alpha-own"), otpBody(testRcpt))
		if sent.Code != 202 {
			t.Fatalf("send: %d %s", sent.Code, sent.Raw)
		}
		id := str(sent.JSON, "id")
		if r := e.get(beta.live, id); r.Code != 404 {
			t.Fatalf("other project read status: %d %s", r.Code, r.Raw)
		}
		if r := e.do("POST", "/v1/emails/"+id+"/cancel", beta.live, "", nil); r.Code == 200 || e.state(id) == "cancelled" {
			t.Fatalf("other project cancelled the message: %d state=%s", r.Code, e.state(id))
		}
		if r := e.get(alpha.liveRead, id); r.Code != 200 || str(r.JSON, "id") != id {
			t.Fatalf("owning key should read status: %d %s", r.Code, r.Raw)
		}
		if strings.Contains(e.get(alpha.live, id).Raw, testRcpt) {
			t.Fatal("status response includes the recipient")
		}
	})

	t.Run("client cannot choose a role", func(t *testing.T) {
		r := e.raw("POST", "/admin/v1/auth/login", nil, map[string]any{"email": "admin@ditronics.co.tz", "password": adminPass, "role": "owner"})
		if r.Code != 200 || str(r.JSON, "role") != "admin" {
			t.Fatalf("login role: %d %s", r.Code, r.Raw)
		}
		if r := e.adm(admin, "GET", "/admin/v1/users", nil); r.Code != 403 {
			t.Fatalf("admin listed users: %d", r.Code)
		}
		var ownerID string
		e.must(e.pool.QueryRow(ctx, `SELECT id::text FROM users WHERE email='owner@ditronics.co.tz'`).Scan(&ownerID))
		if r := e.adm(admin, "PATCH", "/admin/v1/users/"+ownerID, map[string]any{"role": "admin", "password": "not-the-owner-password"}); r.Code != 403 {
			t.Fatalf("admin updated the owner: %d %s", r.Code, r.Raw)
		}
		if r := e.adm(owner, "GET", "/admin/v1/auth/me", nil); str(r.JSON, "role") != "owner" {
			t.Fatalf("owner role changed: %s", r.Raw)
		}
	})

	t.Run("session ids do not cross users", func(t *testing.T) {
		var ownerSession string
		e.must(e.pool.QueryRow(ctx, `SELECT encode(s.token_hash,'hex') FROM sessions s JOIN users u ON u.id=s.user_id WHERE u.email='owner@ditronics.co.tz' ORDER BY s.created_at DESC LIMIT 1`).Scan(&ownerSession))
		listed := e.admOK(admin, "GET", "/admin/v1/auth/sessions", nil)
		if strings.Contains(listed.Raw, ownerSession) {
			t.Fatal("admin session list included the owner's session")
		}
		if r := e.adm(admin, "DELETE", "/admin/v1/auth/sessions/"+ownerSession, nil); r.Code != 404 {
			t.Fatalf("admin revoked the owner's session: %d %s", r.Code, r.Raw)
		}
		if r := e.adm(owner, "GET", "/admin/v1/overview", nil); r.Code != 200 {
			t.Fatalf("owner session was revoked: %d", r.Code)
		}
	})

	t.Run("reset token is bound to one user and not readable", func(t *testing.T) {
		e.svc.Opts.PublicURL = "https://mail.example.com"
		before := e.count(`SELECT count(*) FROM messages WHERE idempotency_key LIKE 'reset-%'`)
		if _, err := e.pool.Exec(ctx, `DELETE FROM sender_identities si USING environments env WHERE si.environment_id=env.id AND env.mode='live' AND env.project_id=(SELECT id FROM projects WHERE name='beta')`); err != nil {
			t.Fatal(err)
		}
		e.must(e.svc.RequestPasswordReset(ctx, "owner@ditronics.co.tz"))
		var id string
		var enc []byte
		e.must(e.pool.QueryRow(ctx, `SELECT id::text, payload_enc FROM messages WHERE idempotency_key LIKE 'reset-%' ORDER BY created_at DESC LIMIT 1`).Scan(&id, &enc))
		if e.count(`SELECT count(*) FROM messages WHERE idempotency_key LIKE 'reset-%'`) != before+1 {
			t.Fatal("reset was not queued exactly once")
		}
		pt, err := e.box.Open(enc, []byte(id))
		if err != nil {
			t.Fatal(err)
		}
		token := resetToken(string(pt))
		if token == "" {
			t.Fatal("reset payload has no token")
		}
		var mode string
		e.must(e.pool.QueryRow(ctx, `SELECT e.mode FROM messages m JOIN environments e ON e.id=m.environment_id WHERE m.id=$1`, id).Scan(&mode))
		if mode != "live" {
			t.Fatalf("reset queued into %s, want live", mode)
		}
		for _, p := range []string{"/admin/v1/messages/" + id, "/admin/v1/sandbox/" + id, "/admin/v1/sandbox", "/admin/v1/messages"} {
			if r := e.adm(admin, "GET", p, nil); strings.Contains(r.Raw, token) {
				t.Fatalf("%s exposed the reset token", p)
			}
		}
		if e.runWorker() < 1 {
			t.Fatal("worker did not take the reset message")
		}
		if e.state(id) != "submitted" {
			t.Fatalf("reset state: %s", e.state(id))
		}
		if e.count(`SELECT count(*) FROM sandbox_captures WHERE message_id=$1`, id) != 0 {
			t.Fatal("reset was stored as a sandbox capture")
		}
		if e.count(`SELECT count(*) FROM messages WHERE id=$1 AND payload_enc IS NOT NULL`, id) != 0 {
			t.Fatal("reset payload kept after delivery")
		}
		// No live sender: do not fall back to a capturable sandbox message.
		if _, err := e.pool.Exec(ctx, `DELETE FROM sender_identities si USING environments env WHERE si.environment_id=env.id AND env.mode='live'`); err != nil {
			t.Fatal(err)
		}
		e.must(e.svc.RequestPasswordReset(ctx, "admin@ditronics.co.tz"))
		if e.count(`SELECT count(*) FROM messages WHERE idempotency_key LIKE 'reset-%'`) != before+1 {
			t.Fatal("reset without a live sender was still queued")
		}

		newPass := "replacement-passphrase"
		if r := e.raw("POST", "/admin/v1/auth/reset", nil, map[string]any{"token": token, "password": newPass}); r.Code != 200 {
			t.Fatalf("reset: %d %s", r.Code, r.Raw)
		}
		if r := e.raw("POST", "/admin/v1/auth/reset", nil, map[string]any{"token": token, "password": newPass}); r.Code == 200 {
			t.Fatal("reset token reused")
		}
		if r := e.raw("POST", "/admin/v1/auth/login", nil, map[string]any{"email": "owner@ditronics.co.tz", "password": newPass}); r.Code != 200 || str(r.JSON, "role") != "owner" {
			t.Fatalf("owner login after reset: %d %s", r.Code, r.Raw)
		}
		if r := e.raw("POST", "/admin/v1/auth/login", nil, map[string]any{"email": "admin@ditronics.co.tz", "password": newPass}); r.Code == 200 {
			t.Fatal("owner reset token changed the admin password")
		}
		if r := e.adm(owner, "GET", "/admin/v1/overview", nil); r.Code != 401 {
			t.Fatalf("reset did not end the owner's old session: %d", r.Code)
		}
		if r := e.adm(admin, "GET", "/admin/v1/overview", nil); r.Code != 200 {
			t.Fatalf("admin session ended by another user's reset: %d", r.Code)
		}
	})

	t.Run("confidential sandbox mail is not captured", func(t *testing.T) {
		var pid, eid string
		e.must(e.pool.QueryRow(ctx, `SELECT p.id::text, e.id::text FROM projects p JOIN environments e ON e.project_id=p.id WHERE p.name='alpha' AND e.mode='sandbox'`).Scan(&pid, &eid))
		secret := "sandbox-reset-secret-token-0123456789"
		mid := secure.NewUUID()
		body := []byte(`{"from":"security@notify.example.com","to":"owner@ditronics.co.tz","template":"raw","raw":true,"confidential":true,"subject":"Password reset","text":"token=` + secret + `"}`)
		sealed, err := e.box.Seal(body, []byte(mid))
		e.must(err)
		_, err = e.pool.Exec(ctx, `INSERT INTO messages (id, project_id, environment_id, template, template_version, state, payload_enc,
recipient_hash, request_hmac, idempotency_key, expires_at)
VALUES ($1,$2,$3,'raw',0,'queued',$4,$5,$6,$7, now() + interval '1 hour')`,
			mid, pid, eid, sealed, e.box.MAC("recipient", []byte("owner@ditronics.co.tz")), e.box.MAC("request", body), "reset-forced-"+mid)
		e.must(err)
		e.runWorker()
		if e.state(mid) == "captured" || e.count(`SELECT count(*) FROM sandbox_captures WHERE message_id=$1`, mid) != 0 {
			t.Fatal("confidential message was captured")
		}
		if r := e.adm(admin, "GET", "/admin/v1/sandbox/"+mid, nil); r.Code == 200 || strings.Contains(r.Raw, secret) {
			t.Fatalf("sandbox view of confidential mail: %d %s", r.Code, r.Raw)
		}
	})
}

func resetToken(body string) string {
	const mark = "token="
	i := strings.Index(body, mark)
	if i < 0 {
		return ""
	}
	rest := body[i+len(mark):]
	end := 0
	for end < len(rest) {
		c := rest[end]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			break
		}
		end++
	}
	return rest[:end]
}

// Keep the service import used if a future assertion needs a typed error.
var _ = service.RoleOwner

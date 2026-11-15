package test

import (
	"context"
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/api"
)

// A project is opened only by its members and by installation operators. The role decides
// what a member may do, and the views that look across every project are for operators.
func TestProjectMembersAndRoles(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	op := e.user("op@njiwa.co.tz", "admin")
	for _, a := range []string{"alice", "bob", "carol", "dave"} {
		_, err := e.svc.CreateUserWith(ctx, a+"@njiwa.co.tz", adminPass, "admin", false)
		e.must(err)
	}
	alice, bob, carol, dave := e.login("alice@njiwa.co.tz"), e.login("bob@njiwa.co.tz"), e.login("carol@njiwa.co.tz"), e.login("dave@njiwa.co.tz")
	e.provision("sacas")
	e.provision("other")
	ctxPath := wsBase + "/context"

	// Strangers: the project does not exist for them, and their project list is empty.
	if r := e.adm(alice, "GET", ctxPath, nil); r.Code != 404 || errCode(r) != "project_not_found" {
		t.Fatalf("stranger opened a project: %d %s", r.Code, r.Raw)
	}
	if got := items(e.admOK(alice, "GET", "/admin/v1/projects", nil)); len(got) != 0 {
		t.Fatalf("a stranger sees projects: %v", got)
	}
	if got := items(e.admOK(op, "GET", "/admin/v1/projects", nil)); len(got) != 2 {
		t.Fatalf("an operator should see both projects: %d", len(got))
	}

	// An operator makes alice the owner; owners add the others.
	add := func(who session, email, role string) resp {
		return e.adm(who, "POST", wsBase+"/members", map[string]any{"email": email, "role": role})
	}
	if r := add(op, "alice@njiwa.co.tz", "owner"); r.Code != 200 {
		t.Fatalf("operator adds an owner: %d %s", r.Code, r.Raw)
	}
	for who, role := range map[string]string{"bob": "viewer", "carol": "developer", "dave": "admin"} {
		if r := add(alice, who+"@njiwa.co.tz", role); r.Code != 200 {
			t.Fatalf("owner adds %s: %d %s", who, r.Code, r.Raw)
		}
	}
	if r := add(alice, "nobody@njiwa.co.tz", "viewer"); r.Code != 404 || errCode(r) != "user_not_found" {
		t.Fatalf("unknown user: %d %s", r.Code, r.Raw)
	}
	if r := add(alice, "bob@njiwa.co.tz", "king"); r.Code != 400 {
		t.Fatalf("bad role: %d", r.Code)
	}
	if got := e.admOK(bob, "GET", ctxPath, nil); got.JSON["role"] != "viewer" {
		t.Fatalf("the context should tell the role: %s", got.Raw)
	}

	// Viewer: reads, and the read-only checks; no changes.
	e.admOK(bob, "GET", wsBase+"/templates", nil)
	e.admOK(bob, "GET", wsBase+"/members", nil)
	if r := e.adm(bob, "POST", wsBase+"/recipients/check", map[string]any{"emails": []string{"a@example.org"}}); r.Code == 403 {
		t.Fatalf("a viewer may run the read-only checks: %d %s", r.Code, r.Raw)
	}
	for _, c := range [][2]string{{"POST", "/lists"}, {"POST", "/keys"}, {"POST", "/members"}, {"POST", "/privacy/export"}} {
		if r := e.adm(bob, c[0], wsBase+c[1], map[string]any{}); r.Code != 403 || errCode(r) != "insufficient_role" {
			t.Fatalf("viewer %s %s: %d %s", c[0], c[1], r.Code, r.Raw)
		}
	}

	// Developer: edits content, but not keys, personal-data tools or members.
	e.admOK(carol, "POST", wsBase+"/lists", map[string]any{"name": "News"})
	for _, c := range [][2]string{{"POST", "/keys"}, {"POST", "/privacy/export"}, {"POST", "/members"}, {"POST", "/senders"}} {
		if r := e.adm(carol, c[0], wsBase+c[1], map[string]any{}); r.Code != 403 || errCode(r) != "insufficient_role" {
			t.Fatalf("developer %s %s: %d %s", c[0], c[1], r.Code, r.Raw)
		}
	}

	// Admin: personal-data tools and keys, but not members; and only an owner makes an owner.
	e.admOK(dave, "POST", wsBase+"/privacy/export", map[string]any{"email": "someone@example.org"})
	if r := e.adm(dave, "POST", wsBase+"/keys", map[string]any{"scopes": []string{"emails:send"}, "label": "k"}); r.Code == 403 {
		t.Fatalf("an admin may create keys: %d %s", r.Code, r.Raw)
	}
	if r := add(dave, "bob@njiwa.co.tz", "developer"); r.Code != 403 || errCode(r) != "insufficient_role" {
		t.Fatalf("an admin changed members: %d %s", r.Code, r.Raw)
	}
	// Environment settings (origins, recipient restriction) need the admin role in the project.
	if r := e.adm(carol, "PATCH", "/admin/v1/projects/sacas/environments/live", map[string]any{"restrict_recipients": true}); r.Code != 404 {
		t.Fatalf("a developer changed environment settings: %d %s", r.Code, r.Raw)
	}
	e.admOK(dave, "PATCH", "/admin/v1/projects/sacas/environments/live", map[string]any{"restrict_recipients": true})

	// Membership is per project: bob is nobody in the other one, and his list shows one project.
	if r := e.adm(bob, "GET", "/admin/v1/w/other/sandbox/context", nil); r.Code != 404 {
		t.Fatalf("a member of one project opened another: %d", r.Code)
	}
	mine := items(e.admOK(bob, "GET", "/admin/v1/projects", nil))
	if len(mine) != 1 || str(mine[0], "name") != "sacas" || str(mine[0], "role") != "viewer" {
		t.Fatalf("bob's projects: %v", mine)
	}

	// The cross-project views are for operators.
	for _, p := range []string{"/admin/v1/keys", "/admin/v1/messages", "/admin/v1/domains", "/admin/v1/webhooks", "/admin/v1/audit", "/admin/v1/overview"} {
		if r := e.adm(alice, "GET", p, nil); r.Code != 403 || errCode(r) != "operator_required" {
			t.Fatalf("GET %s as an owner without the operator permission: %d %s", p, r.Code, r.Raw)
		}
		e.admOK(op, "GET", p, nil)
	}

	// The last owner stays: demoting or removing alice is refused until another owner exists.
	var aliceID, daveID string
	for _, m := range items(e.admOK(alice, "GET", wsBase+"/members", nil)) {
		switch str(m, "email") {
		case "alice@njiwa.co.tz":
			aliceID = str(m, "user_id")
		case "dave@njiwa.co.tz":
			daveID = str(m, "user_id")
		}
	}
	if r := e.adm(alice, "PATCH", wsBase+"/members/"+aliceID, map[string]any{"role": "viewer"}); r.Code != 409 || errCode(r) != "last_owner" {
		t.Fatalf("demoted the last owner: %d %s", r.Code, r.Raw)
	}
	if r := e.adm(alice, "DELETE", wsBase+"/members/"+aliceID, nil); r.Code != 409 || errCode(r) != "last_owner" {
		t.Fatalf("removed the last owner: %d %s", r.Code, r.Raw)
	}
	e.admOK(alice, "PATCH", wsBase+"/members/"+daveID, map[string]any{"role": "owner"})
	e.admOK(dave, "PATCH", wsBase+"/members/"+aliceID, map[string]any{"role": "viewer"}) // now fine

	// Removing a member closes the project to them at once.
	var bobID string
	for _, m := range items(e.admOK(dave, "GET", wsBase+"/members", nil)) {
		if str(m, "email") == "bob@njiwa.co.tz" {
			bobID = str(m, "user_id")
		}
	}
	e.admOK(dave, "DELETE", wsBase+"/members/"+bobID, nil)
	if r := e.adm(bob, "GET", ctxPath, nil); r.Code != 404 {
		t.Fatalf("a removed member still opens the project: %d", r.Code)
	}

	// A person who creates a project owns it; others cannot open it.
	e.admOK(carol, "POST", "/admin/v1/projects", map[string]any{"name": "carols"})
	if got := e.admOK(carol, "GET", "/admin/v1/w/carols/sandbox/context", nil); got.JSON["role"] != "owner" {
		t.Fatalf("the creator should own the project: %s", got.Raw)
	}
	if r := e.adm(dave, "GET", "/admin/v1/w/carols/sandbox/context", nil); r.Code != 404 {
		t.Fatalf("another member opened a project they are not in: %d", r.Code)
	}
	// Disabled users cannot be added.
	_, err := e.svc.CreateUserWith(ctx, "erin@njiwa.co.tz", adminPass, "admin", false)
	e.must(err)
	_, err = e.pool.Exec(ctx, `UPDATE users SET disabled=true WHERE email='erin@njiwa.co.tz'`)
	e.must(err)
	if r := add(dave, "erin@njiwa.co.tz", "viewer"); r.Code != 400 || !strings.Contains(r.Raw, "disabled") {
		t.Fatalf("added a disabled user: %d %s", r.Code, r.Raw)
	}
}

// The role needed by each route, at the edges: a mistake here is a hole or a lock-out.
func TestWorkspaceRouteRoles(t *testing.T) {
	cases := []struct{ method, rel, want string }{
		{"GET", "/messages", "viewer"},
		{"GET", "/keys", "viewer"},
		{"POST", "/recipients/verify", "viewer"},
		{"POST", "/templates/:id/versions/:version/preview", "viewer"},
		{"POST", "/compose", "developer"},
		{"POST", "/lists", "developer"},
		{"PATCH", "/forms/submissions/:id", "developer"},
		{"POST", "/forms/submissions/:id/reply", "developer"},
		{"POST", "/chat/conversations/:id/messages", "developer"},
		{"POST", "/forms", "admin"},
		{"PUT", "/forms/:id", "admin"},
		{"POST", "/chat/widgets", "admin"},
		{"POST", "/keys", "admin"},
		{"POST", "/webhooks", "admin"},
		{"DELETE", "/senders/:id", "admin"},
		{"POST", "/domains/:id/verify-message", "admin"},
		{"POST", "/privacy/delete", "admin"},
		{"GET", "/messages/:id/export", "admin"},
		{"GET", "/inbound-ingest", "admin"},
		{"POST", "/transport-test", "admin"},
		{"GET", "/members", "viewer"},
		{"POST", "/members", "owner"},
		{"DELETE", "/members/:uid", "owner"},
	}
	for _, c := range cases {
		if got := api.NeededRole(c.method, c.rel); got != c.want {
			t.Errorf("%s %s needs %s, want %s", c.method, c.rel, got, c.want)
		}
	}
}

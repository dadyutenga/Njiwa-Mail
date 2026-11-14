package test

import (
	"context"
	"strings"
	"testing"
)

// Contacts and lists: import with mapping (dry run first), status from the
// sending tables, list-scoped membership, resubscribe never lifts a
// suppression, and one environment never sees another's contacts.
func TestContactsListsAndImport(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")
	e.provision("other")
	l := e.admOK(admin, "POST", wsBase+"/lists", map[string]any{"name": "Newsletter", "description": "Monthly"})
	listID := str(l.JSON, "id")
	if r := e.adm(admin, "POST", wsBase+"/lists", map[string]any{"name": "Newsletter"}); r.Code != 409 {
		t.Fatalf("duplicate list: %d", r.Code)
	}
	csv := "E-mail,Full name,Plan,City\nada@example.org,Ada,pro,Dar\nbad-address,X,free,Arusha\nbob@example.org,Bob,free,Mwanza\nada@example.org,Ada again,pro,Dar\n"
	in := map[string]any{"format": "csv", "data": csv, "list_id": listID,
		"mapping": map[string]any{"email": "E-mail", "name": "Full name", "attributes": map[string]string{"plan": "Plan", "city": "City"}}}
	dry := e.admOK(admin, "POST", wsBase+"/contacts/import?dry_run=1", in)
	if dry.JSON["rows"] != float64(4) || dry.JSON["invalid"] != float64(2) || dry.JSON["created"] != float64(0) ||
		!strings.Contains(dry.Raw, `"plan":"pro"`) {
		t.Fatalf("dry run: %s", dry.Raw)
	}
	if n := e.count(`SELECT count(*) FROM contacts`); n != 0 {
		t.Fatalf("dry run wrote %d contacts", n)
	}
	rep := e.admOK(admin, "POST", wsBase+"/contacts/import", in)
	if rep.JSON["created"] != float64(2) || rep.JSON["invalid"] != float64(2) || !strings.Contains(rep.Raw, "duplicate_in_file") {
		t.Fatalf("import: %s", rep.Raw)
	}
	// The address and name are encrypted at rest; the masked form is listed.
	if n := e.count(`SELECT count(*) FROM contacts WHERE email_masked='a***@example.org' AND position('ada@example.org'::bytea in data_enc)=0`); n != 1 {
		t.Fatalf("stored contact: %d", n)
	}
	page := e.admOK(admin, "GET", wsBase+"/contacts?list="+listID, nil)
	items := page.JSON["items"].([]any)
	if len(items) != 2 || strings.Contains(page.Raw, "ada@example.org") {
		t.Fatalf("list: %s", page.Raw)
	}
	// Find by exact address without plain-text storage.
	found := e.admOK(admin, "GET", wsBase+"/contacts?q=Ada@Example.org", nil)
	adaID := str(found.JSON["items"].([]any)[0].(map[string]any), "id")
	ada := e.admOK(admin, "GET", wsBase+"/contacts/"+adaID, nil)
	if str(ada.JSON, "email") != "ada@example.org" || str(ada.JSON, "name") != "Ada" || str(ada.JSON, "status") != "active" {
		t.Fatalf("contact: %s", ada.Raw)
	}
	// Sending history comes from messages to the address: one submitted, not bounced.
	var pid, eid string
	e.must(e.pool.QueryRow(ctx, `SELECT e.project_id::text, e.id::text FROM environments e JOIN projects p ON p.id=e.project_id WHERE p.name='sacas' AND e.mode='sandbox'`).Scan(&pid, &eid))
	_, err := e.pool.Exec(ctx, `INSERT INTO messages (id, project_id, environment_id, template, template_version, state, recipient_hash, request_hmac, idempotency_key, expires_at)
VALUES (gen_random_uuid(),$1,$2,'otp',1,'submitted',$3,'x','contact-history-1', now()+interval '1 day')`, pid, eid, e.box.MAC("recipient", []byte("ada@example.org")))
	e.must(err)
	ada = e.admOK(admin, "GET", wsBase+"/contacts/"+adaID, nil)
	if ada.JSON["messages"] != float64(1) || ada.JSON["submitted"] != float64(1) || ada.JSON["bounced"] != float64(0) {
		t.Fatalf("history: %s", ada.Raw)
	}
	// Re-import with update: the name changes, attributes merge, no duplicate.
	in["data"] = "E-mail,Full name,Plan,City\nada@example.org,Ada Lovelace,team,\n"
	in["update_existing"] = true
	e.admOK(admin, "POST", wsBase+"/contacts/import", in)
	ada = e.admOK(admin, "GET", wsBase+"/contacts/"+adaID, nil)
	if str(ada.JSON, "name") != "Ada Lovelace" || !strings.Contains(ada.Raw, `"plan":"team"`) || !strings.Contains(ada.Raw, `"city":"Dar"`) {
		t.Fatalf("updated: %s", ada.Raw)
	}
	// Status follows the sending tables: a hard bounce makes it "bounced", and
	// resubscribe does not lift that.
	e.must(e.svc.Suppress(ctx, "sacas", "sandbox", "ada@example.org", "hard_bounce"))
	if s := str(e.admOK(admin, "GET", wsBase+"/contacts/"+adaID, nil).JSON, "status"); s != "bounced" {
		t.Fatalf("after suppression: %s", s)
	}
	e.admOK(admin, "POST", wsBase+"/contacts/"+adaID+"/resubscribe", map[string]any{})
	if s := str(e.admOK(admin, "GET", wsBase+"/contacts/"+adaID, nil).JSON, "status"); s != "bounced" {
		t.Fatalf("resubscribe lifted a suppression: %s", s)
	}
	if r := e.admOK(admin, "GET", wsBase+"/contacts?status=bounced", nil); len(r.JSON["items"].([]any)) != 1 {
		t.Fatalf("status filter: %s", r.Raw)
	}
	// Membership: remove Bob from the list.
	bob := e.admOK(admin, "GET", wsBase+"/contacts?q=bob@example.org", nil)
	bobID := str(bob.JSON["items"].([]any)[0].(map[string]any), "id")
	e.admOK(admin, "POST", wsBase+"/lists/"+listID+"/members", map[string]any{"contact_ids": []string{bobID}, "remove": true})
	lists := e.admOK(admin, "GET", wsBase+"/lists", nil)
	if !strings.Contains(lists.Raw, `"members":1`) {
		t.Fatalf("lists: %s", lists.Raw)
	}
	// Another project's workspace sees none of it.
	if r := e.adm(admin, "GET", "/admin/v1/w/other/sandbox/contacts/"+adaID, nil); r.Code != 404 {
		t.Fatalf("other project: %d", r.Code)
	}
	if r := e.admOK(admin, "GET", "/admin/v1/w/other/sandbox/contacts", nil); len(r.JSON["items"].([]any)) != 0 {
		t.Fatalf("other project lists: %s", r.Raw)
	}
	if r := e.adm(admin, "POST", "/admin/v1/w/other/sandbox/lists/"+listID+"/members", map[string]any{"contact_ids": []string{adaID}}); r.Code == 200 && !strings.Contains(r.Raw, `"changed":0`) {
		t.Fatalf("cross-project membership: %d %s", r.Code, r.Raw)
	}
	e.admOK(admin, "DELETE", wsBase+"/contacts/"+bobID, nil)
	if n := e.count(`SELECT count(*) FROM contacts`); n != 1 {
		t.Fatalf("after delete: %d", n)
	}
}

// The public contacts API: an application adds a sign-up to a list.
func TestContactsAPI(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	k := e.provision("sacas")
	e.provision("other")
	sc, err := e.svc.ResolveScope(ctx, "sacas", "live")
	e.must(err)
	listID, err := e.svc.CreateList(ctx, sc, "Newsletter", "")
	e.must(err)
	osc, err := e.svc.ResolveScope(ctx, "other", "live")
	e.must(err)
	otherList, err := e.svc.CreateList(ctx, osc, "Theirs", "")
	e.must(err)
	key, _, err := e.svc.CreateKey(ctx, "sacas", "live", []string{"contacts:manage"}, "crm", 0)
	e.must(err)
	if r := e.do("POST", "/v1/contacts", k.live, "", map[string]any{"email": "a@example.org"}); r.Code != 403 {
		t.Fatalf("sending key: %d", r.Code)
	}
	r := e.do("POST", "/v1/contacts", key, "", map[string]any{"email": "Signup@Example.org", "name": "Neema",
		"attributes": map[string]any{"source": "website"}, "lists": []string{listID}})
	if r.Code != 200 || r.JSON["created"] != true {
		t.Fatalf("create: %d %s", r.Code, r.Raw)
	}
	if r := e.do("POST", "/v1/contacts", key, "", map[string]any{"email": "signup@example.org", "lists": []string{listID}}); r.JSON["created"] != false {
		t.Fatalf("repeat: %s", r.Raw)
	}
	// Another project's list cannot be used; a bad address is refused.
	if r := e.do("POST", "/v1/contacts", key, "", map[string]any{"email": "x@example.org", "lists": []string{otherList}}); r.Code != 404 {
		t.Fatalf("other project's list: %d %s", r.Code, r.Raw)
	}
	if r := e.do("POST", "/v1/contacts", key, "", map[string]any{"email": "nope"}); r.Code != 422 || errCode(r) != "invalid_email" {
		t.Fatalf("bad address: %d %s", r.Code, r.Raw)
	}
	lists := e.do("GET", "/v1/lists", key, "", nil)
	if !strings.Contains(lists.Raw, `"members":1`) || strings.Contains(lists.Raw, "Theirs") {
		t.Fatalf("lists: %s", lists.Raw)
	}
	if r := e.do("DELETE", "/v1/contacts/signup@example.org", key, "", nil); r.Code != 200 {
		t.Fatalf("delete: %d %s", r.Code, r.Raw)
	}
	if r := e.do("DELETE", "/v1/contacts/signup@example.org", key, "", nil); r.Code != 404 {
		t.Fatalf("delete again: %d", r.Code)
	}
}

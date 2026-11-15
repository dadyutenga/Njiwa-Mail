package test

import (
	"strings"
	"testing"
)

func TestBulkPrivacyDelete(t *testing.T) {
	e := setup(t)
	e.svc.Opts.FormsEnabled, e.svc.Opts.ChatEnabled = true, true
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")
	gone, also, stays := "neema@example.org", "juma@example.org", "asha@example.org"

	for _, a := range []string{gone, also, stays} {
		e.admOK(admin, "POST", wsBase+"/contacts", map[string]any{"email": a, "name": "Name of " + a})
	}
	f := e.admOK(admin, "POST", wsBase+"/forms", map[string]any{"name": "Contact", "allowed_origins": []string{siteOrigin}})
	fp := "/v1/forms/" + str(f.JSON, "id")
	for _, a := range []string{gone, stays} {
		if r := e.formVals(fp, map[string]string{"name": "N", "email": a, "message": "About " + a}, map[string]string{"Origin": siteOrigin}); r.Code != 200 {
			t.Fatalf("form: %d %s", r.Code, r.Raw)
		}
		e.resetFormLimits()
	}
	w := e.admOK(admin, "POST", wsBase+"/chat/widgets", map[string]any{"name": "Site", "allowed_origins": []string{siteOrigin}})
	cp := "/v1/chat/" + str(w.JSON, "id")
	for _, a := range []string{gone, stays} {
		if r := e.chatCall("POST", cp+"/conversations", "", map[string]any{"name": "N", "email": a, "message": "Chat " + a}, siteOrigin); r.Code != 201 {
			t.Fatalf("chat: %d %s", r.Code, r.Raw)
		}
		e.resetChatLimits()
	}

	// Validation.
	if r := e.adm(admin, "POST", wsBase+"/privacy/delete-bulk", map[string]any{"emails": []string{}}); r.Code != 400 {
		t.Fatalf("empty: %d", r.Code)
	}
	many := make([]string, 501)
	for i := range many {
		many[i] = "x@example.org"
	}
	if r := e.adm(admin, "POST", wsBase+"/privacy/delete-bulk", map[string]any{"emails": many}); r.Code != 400 {
		t.Fatalf("too many: %d", r.Code)
	}

	// A dry run reports and deletes nothing; addresses never come back in clear.
	list := []string{gone, strings.ToUpper(gone), also, "not-an-address", "nobody@example.org"}
	dry := e.admOK(admin, "POST", wsBase+"/privacy/delete-bulk", map[string]any{"emails": list, "dry_run": true})
	if strings.Contains(dry.Raw, "neema@") || strings.Contains(dry.Raw, "juma@") {
		t.Fatalf("address in clear: %s", dry.Raw)
	}
	sum := dry.JSON["summary"].(map[string]any)
	if sum["would_delete"] != float64(2) || sum["invalid"] != float64(1) || sum["nothing_found"] != float64(1) || len(dry.JSON["results"].([]any)) != 4 {
		t.Fatalf("dry-run summary (duplicates once): %s", dry.Raw)
	}
	if n := e.count(`SELECT count(*) FROM contacts`); n != 3 {
		t.Fatalf("a dry run deleted contacts: %d", n)
	}
	first := dry.JSON["results"].([]any)[0].(map[string]any)["found"].(map[string]any)
	if first["contacts"] != float64(1) || first["form_submissions"] != float64(1) || first["chats"] != float64(1) {
		t.Fatalf("footprint: %v", first)
	}

	// The real thing: both go, everything about them; the third person is untouched.
	res := e.admOK(admin, "POST", wsBase+"/privacy/delete-bulk", map[string]any{"emails": list})
	if res.JSON["summary"].(map[string]any)["deleted"] != float64(2) {
		t.Fatalf("delete: %s", res.Raw)
	}
	if n := e.count(`SELECT count(*) FROM contacts`); n != 1 {
		t.Fatalf("contacts left: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM form_submissions`); n != 1 {
		t.Fatalf("form submissions left: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM chat_conversations`); n != 1 {
		t.Fatalf("chats left: %d", n)
	}
	left := e.admOK(admin, "POST", wsBase+"/privacy/export", map[string]any{"email": stays})
	if !strings.Contains(left.Raw, "About "+stays) || !strings.Contains(left.Raw, "Chat "+stays) {
		t.Fatalf("the other person lost data: %s", left.Raw)
	}
	// Running it again finds nothing; it is audited as a count, never with addresses.
	again := e.admOK(admin, "POST", wsBase+"/privacy/delete-bulk", map[string]any{"emails": list})
	if again.JSON["summary"].(map[string]any)["deleted"] != nil {
		t.Fatalf("second run: %s", again.Raw)
	}
	if n := e.count(`SELECT count(*) FROM audit_log WHERE action='privacy.delete_bulk' AND details::text NOT LIKE '%@%'`); n != 3 {
		t.Fatalf("audit entries without addresses: %d", n)
	}
	// Another project cannot be reached by naming it.
	if r := e.adm(admin, "POST", "/admin/v1/w/missing/sandbox/privacy/delete-bulk", map[string]any{"emails": []string{gone}}); r.Code != 404 {
		t.Fatalf("missing project: %d", r.Code)
	}
}

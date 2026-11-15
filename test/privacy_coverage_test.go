package test

import (
	"strings"
	"testing"
)

// A person's data lives in more places than messages: their contact record, what they wrote
// in a form and their chats. Export must show all of it and delete must remove all of it,
// without touching anyone else, whether or not forms and chat are switched on at the moment.
func TestPrivacyCoversContactsFormsAndChats(t *testing.T) {
	e := setup(t)
	e.svc.Opts.FormsEnabled, e.svc.Opts.ChatEnabled = true, true
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")
	person, other := "neema@example.org", "asha@example.org"

	// Contacts: the person and someone else, both on a list.
	e.admOK(admin, "POST", wsBase+"/lists", map[string]any{"name": "News"})
	lists := items(e.admOK(admin, "GET", wsBase+"/lists", nil))
	lid := str(lists[0], "id")
	for _, a := range []string{person, other} {
		e.admOK(admin, "POST", wsBase+"/contacts", map[string]any{"email": a, "name": "Name of " + a, "lists": []string{lid}, "attributes": map[string]any{"city": "Arusha"}})
	}
	// A form submission from each.
	f := e.admOK(admin, "POST", wsBase+"/forms", map[string]any{"name": "Contact", "allowed_origins": []string{siteOrigin}})
	fp := "/v1/forms/" + str(f.JSON, "id")
	for _, a := range []string{person, other} {
		if r := e.formVals(fp, map[string]string{"name": "N", "email": a, "message": "Please call me about " + a}, map[string]string{"Origin": siteOrigin}); r.Code != 200 {
			t.Fatalf("form: %d %s", r.Code, r.Raw)
		}
		e.resetFormLimits()
	}
	// A chat from each, with an address.
	w := e.admOK(admin, "POST", wsBase+"/chat/widgets", map[string]any{"name": "Site", "allowed_origins": []string{siteOrigin}})
	cp := "/v1/chat/" + str(w.JSON, "id")
	for _, a := range []string{person, other} {
		if r := e.chatCall("POST", cp+"/conversations", "", map[string]any{"name": "N", "email": a, "message": "Chat from " + a}, siteOrigin); r.Code != 201 {
			t.Fatalf("chat: %d %s", r.Code, r.Raw)
		}
		e.resetChatLimits()
	}

	// Export: everything the person gave us, and nothing of the other person.
	x := e.admOK(admin, "POST", wsBase+"/privacy/export", map[string]any{"email": person})
	for _, want := range []string{"Name of " + person, "Arusha", "News", "Please call me about " + person, "Chat from " + person} {
		if !strings.Contains(x.Raw, want) {
			t.Fatalf("export lacks %q: %s", want, x.Raw)
		}
	}
	if strings.Contains(x.Raw, other) {
		t.Fatalf("export shows someone else: %s", x.Raw)
	}
	csv := e.adm(admin, "POST", wsBase+"/privacy/export?format=csv", map[string]any{"email": person})
	if csv.Code != 200 || !strings.Contains(csv.Raw, "contact,") || !strings.Contains(csv.Raw, "form_submission,") || !strings.Contains(csv.Raw, "chat_message,") ||
		!strings.Contains(csv.Raw, "Chat from "+person) || strings.Contains(csv.Raw, other) {
		t.Fatalf("csv: %d %s", csv.Code, csv.Raw)
	}

	// The rows outlive the switches, so the privacy tools must keep working with them off.
	e.svc.Opts.FormsEnabled, e.svc.Opts.ChatEnabled = false, false
	e.admOK(admin, "POST", wsBase+"/privacy/delete", map[string]any{"email": person})
	hash := e.box.MAC("recipient", []byte(person))
	for table, col := range map[string]string{"contacts": "email_hash", "form_submissions": "sender_hash", "chat_conversations": "email_hash"} {
		if n := e.count(`SELECT count(*) FROM `+table+` WHERE `+col+`=$1`, hash); n != 0 {
			t.Fatalf("%s still holds %d rows about the person", table, n)
		}
	}
	for table, n := range map[string]int{"contacts": 1, "form_submissions": 1, "chat_conversations": 1} {
		if got := e.count(`SELECT count(*) FROM ` + table); got != n {
			t.Fatalf("%s: %d rows, the other person's row should remain (%d)", table, got, n)
		}
	}
	if n := e.count(`SELECT count(*) FROM contact_list_members`); n != 1 {
		t.Fatalf("list memberships: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM chat_messages`); n != 1 {
		t.Fatalf("chat messages: %d", n)
	}
	x = e.admOK(admin, "POST", wsBase+"/privacy/export", map[string]any{"email": person})
	if strings.Contains(x.Raw, "Please call me") || strings.Contains(x.Raw, "Chat from") {
		t.Fatalf("export after delete: %s", x.Raw)
	}
}

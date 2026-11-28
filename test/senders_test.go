package test

import (
	"context"
	"strings"
	"testing"
)

func (e *env) senderID(env, email string) string {
	e.t.Helper()
	var id string
	e.must(e.pool.QueryRow(context.Background(), `SELECT si.id::text FROM sender_identities si JOIN environments v ON v.id=si.environment_id
JOIN projects p ON p.id=si.project_id WHERE p.name='sacas' AND v.mode=$1 AND si.email=$2`, env, email).Scan(&id))
	return id
}

const secondSender = "hello@notify.example.com"

// A key limited to senders cannot use another; disabling stops a sender and
// keeps it; the sender's reply-to applies; a template can pin its sender.
func TestSenderLimitsReplyToDisableAndTemplatePin(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.admOK(admin, "POST", wsBase+"/senders", map[string]any{"email": secondSender, "display_name": "Hello", "purpose": "support", "reply_to": "Replies@example.org"})

	prefix := strings.Split(k.sbx, "_")[2]
	e.admOK(admin, "PUT", wsBase+"/keys/"+prefix+"/senders", map[string]any{"senders": []string{liveFrom}})
	body := otpBody(testRcpt)
	body["from"] = secondSender
	if r := e.post(k.sbx, idem("limited"), body); r.Code != 403 || errCode(r) != "sender_not_allowed" {
		t.Fatalf("limited key: %d %s", r.Code, r.Raw)
	}
	body["from"] = liveFrom
	if r := e.post(k.sbx, idem("allowed"), body); r.Code != 202 {
		t.Fatalf("allowed sender: %d %s", r.Code, r.Raw)
	}
	e.admOK(admin, "PUT", wsBase+"/keys/"+prefix+"/senders", map[string]any{"senders": []string{}})

	body["from"] = secondSender
	r := e.post(k.sbx, idem("replyto"), body)
	if r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	if n := e.count(`SELECT count(*) FROM messages WHERE id=$1 AND reply_to='replies@example.org'`, str(r.JSON, "id")); n != 1 {
		t.Fatal("the sender's reply-to was not applied")
	}

	// A template pins its sender: the request may leave "from" out.
	tpl := e.admOK(admin, "POST", wsBase+"/templates", map[string]any{"slug": "welcome", "description": "Welcome"})
	tid := str(tpl.JSON, "id")
	e.admOK(admin, "PUT", wsBase+"/templates/"+tid+"/draft", map[string]any{"variable_schema": []any{}, "content": map[string]any{"en": map[string]any{"subject": "Hi", "html": "<p>Hi</p>", "text": "Hi"}}})
	e.admOK(admin, "POST", wsBase+"/templates/"+tid+"/versions/1/publish", nil)
	e.admOK(admin, "PUT", wsBase+"/templates/"+tid+"/default-sender", map[string]any{"email": secondSender})
	pinned := map[string]any{"to": testRcpt, "template": "welcome"}
	r = e.post(k.sbx, idem("pinned"), pinned)
	if r.Code != 202 {
		t.Fatalf("pinned sender: %d %s", r.Code, r.Raw)
	}
	if n := e.count(`SELECT count(*) FROM messages WHERE id=$1 AND sender=$2`, str(r.JSON, "id"), secondSender); n != 1 {
		t.Fatal("not sent from the pinned sender")
	}
	if again := e.post(k.sbx, idem("pinned"), pinned); str(again.JSON, "id") != str(r.JSON, "id") {
		t.Fatalf("a retry without from: %d %s", again.Code, again.Raw)
	}

	e.admOK(admin, "PATCH", wsBase+"/senders/"+e.senderID("sandbox", secondSender), map[string]any{"disabled": true})
	if r := e.post(k.sbx, idem("disabled"), body); r.Code != 403 || errCode(r) != "sender_not_approved" {
		t.Fatalf("disabled sender: %d %s", r.Code, r.Raw)
	}
	l := e.admOK(admin, "GET", wsBase+"/senders", nil)
	if !strings.Contains(l.Raw, secondSender) || !strings.Contains(l.Raw, `"messages":`) {
		t.Fatalf("a disabled sender keeps its history: %s", l.Raw)
	}
}

// One default per purpose, and marketing needs a bulk domain.
func TestSenderPurposeDefaults(t *testing.T) {
	e := setup(t)
	e.provision("sacas")
	admin := e.user("admin@njiwa.co.tz", "admin")
	if r := e.adm(admin, "POST", wsBase+"/senders", map[string]any{"email": "news@notify.example.com", "purpose": "marketing"}); r.Code != 409 || errCode(r) != "marketing_needs_bulk_domain" {
		t.Fatalf("marketing without a bulk domain: %d %s", r.Code, r.Raw)
	}
	_, err := e.pool.Exec(context.Background(), `UPDATE domains SET bulk=true WHERE name='notify.example.com'`)
	e.must(err)
	e.admOK(admin, "POST", wsBase+"/senders", map[string]any{"email": "news@notify.example.com", "purpose": "marketing"})
	e.admOK(admin, "POST", wsBase+"/senders", map[string]any{"email": secondSender})
	defaults := func() string {
		var out string
		e.must(e.pool.QueryRow(context.Background(), `SELECT string_agg(si.purpose||'='||si.email, ',' ORDER BY si.purpose) FROM sender_identities si
JOIN environments v ON v.id=si.environment_id JOIN projects p ON p.id=si.project_id WHERE p.name='sacas' AND v.mode='sandbox' AND si.is_default`).Scan(&out))
		return out
	}
	first := defaults()
	if !strings.Contains(first, "marketing=news@notify.example.com") || strings.Count(first, "transactional=") != 1 {
		t.Fatalf("defaults: %s", first)
	}
	e.admOK(admin, "POST", wsBase+"/senders/"+e.senderID("sandbox", secondSender)+"/default", nil)
	if got := defaults(); got != "marketing=news@notify.example.com,transactional="+secondSender {
		t.Fatalf("defaults: %s", got)
	}
}

// Deleting a sender in use needs a replacement, which takes its place
// everywhere. Projects cannot reach each other's senders.
func TestDeleteSenderInUseAndIsolation(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	k := e.provision("sacas")
	e.provisionFrom("other", "x@other.example.org")
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.admOK(admin, "POST", wsBase+"/senders", map[string]any{"email": secondSender})
	old, repl := e.senderID("sandbox", liveFrom), e.senderID("sandbox", secondSender)
	prefix := strings.Split(k.sbx, "_")[2]
	e.admOK(admin, "PUT", wsBase+"/keys/"+prefix+"/senders", map[string]any{"senders": []string{liveFrom}})

	var foreign string
	e.must(e.pool.QueryRow(ctx, `SELECT si.id::text FROM sender_identities si JOIN projects p ON p.id=si.project_id WHERE p.name='other' LIMIT 1`).Scan(&foreign))
	if r := e.adm(admin, "PUT", wsBase+"/keys/"+prefix+"/senders", map[string]any{"senders": []string{"x@other.example.org"}}); r.Code != 400 {
		t.Fatalf("another project's sender on a key: %d %s", r.Code, r.Raw)
	}
	if r := e.adm(admin, "PATCH", wsBase+"/senders/"+foreign, map[string]any{"disabled": true}); r.Code != 404 {
		t.Fatalf("edit across projects: %d", r.Code)
	}
	if r := e.adm(admin, "DELETE", wsBase+"/senders/"+old, nil); r.Code != 409 || errCode(r) != "sender_in_use" {
		t.Fatalf("in use: %d %s", r.Code, r.Raw)
	}
	u := e.admOK(admin, "GET", wsBase+"/senders/"+old+"/usage", nil)
	if !strings.Contains(u.Raw, prefix) {
		t.Fatalf("usage: %s", u.Raw)
	}
	if r := e.adm(admin, "DELETE", wsBase+"/senders/"+old, map[string]any{"replace_with": foreign}); r.Code != 400 {
		t.Fatalf("replace with another project's sender: %d %s", r.Code, r.Raw)
	}
	e.admOK(admin, "DELETE", wsBase+"/senders/"+old, map[string]any{"replace_with": repl})
	if n := e.count(`SELECT count(*) FROM api_keys WHERE prefix=$1 AND allowed_senders = ARRAY[$2]`, prefix, secondSender); n != 1 {
		t.Fatal("the key was not moved to the replacement")
	}
	body := otpBody(testRcpt)
	body["from"] = secondSender
	if r := e.post(k.sbx, idem("after"), body); r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
}

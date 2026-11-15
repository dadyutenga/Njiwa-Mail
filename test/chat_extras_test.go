package test

import (
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/service"
)

// The site's own server signs who a visitor is, so a chat can trust the email: a valid signature
// marks the conversation identified and locks the address, a wrong one is refused, and a chat can
// refuse visitors who are not identified.
func TestChatSignedIdentity(t *testing.T) {
	e := setup(t)
	e.svc.Opts.ChatEnabled = true
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")
	w := e.admOK(admin, "POST", wsBase+"/chat/widgets", map[string]any{"name": "Site", "allowed_origins": []string{siteOrigin}})
	wid, secret := str(w.JSON, "id"), str(w.JSON, "identity_secret")
	if len(secret) < 32 {
		t.Fatalf("the secret is shown once at creation: %s", w.Raw)
	}
	if got := e.admOK(admin, "GET", wsBase+"/chat/widgets", nil); strings.Contains(got.Raw, secret) || !strings.Contains(got.Raw, `"has_identity_secret":true`) {
		t.Fatalf("the list must not show the secret: %s", got.Raw)
	}
	base := "/v1/chat/" + wid
	start := func(body map[string]any) resp {
		e.resetChatLimits() // each attempt counts against the per-address limit; these tests are not about that
		return e.chatCall("POST", base+"/conversations", "", body, siteOrigin)
	}

	// A valid signature (any letter case of the email) identifies the visitor.
	sig := service.ChatIdentityHash(secret, "Neema@Example.org")
	r := start(map[string]any{"name": "Neema", "email": "neema@example.org", "identity": sig, "message": "Hello"})
	if r.Code != 201 {
		t.Fatalf("signed start: %d %s", r.Code, r.Raw)
	}
	cid, tok := str(r.JSON, "conversation_id"), str(r.JSON, "token")
	if c := items(e.admOK(admin, "GET", wsBase+"/chat/conversations", nil)); len(c) != 1 || c[0]["identity_verified"] != true {
		t.Fatalf("the conversation should be identified: %v", c)
	}
	// An identified visitor cannot swap the address for another.
	if r := e.chatCall("PUT", base+"/conversations/"+cid+"/contact", tok, map[string]any{"email": "other@example.org"}, siteOrigin); r.Code != 422 || errCode(r) != "identity_locked" {
		t.Fatalf("contact change: %d %s", r.Code, r.Raw)
	}
	// Wrong, mismatched or address-less signatures are refused, not silently ignored.
	e.resetChatLimits()
	for name, body := range map[string]map[string]any{
		"wrong":      {"email": "neema@example.org", "identity": strings.Repeat("0", 64), "message": "Hi"},
		"other mail": {"email": "asha@example.org", "identity": sig, "message": "Hi"},
		"no email":   {"identity": sig, "message": "Hi"},
	} {
		if r := start(body); r.Code != 422 || errCode(r) != "identity_invalid" {
			t.Fatalf("%s: %d %s", name, r.Code, r.Raw)
		}
	}
	// A visitor without a signature is simply not identified.
	if r := start(map[string]any{"email": "walkin@example.org", "message": "Hi"}); r.Code != 201 {
		t.Fatalf("anonymous: %d %s", r.Code, r.Raw)
	}
	if c := items(e.admOK(admin, "GET", wsBase+"/chat/conversations", nil)); len(c) != 2 {
		t.Fatalf("conversations: %d", len(c))
	} else {
		verified := 0
		for _, x := range c {
			if x["identity_verified"] == true {
				verified++
			}
		}
		if verified != 1 {
			t.Fatalf("only one visitor was identified: %v", c)
		}
	}

	// A chat can require identification.
	e.admOK(admin, "PUT", wsBase+"/chat/widgets/"+wid, map[string]any{"name": "Site", "allowed_origins": []string{siteOrigin}, "require_identity": true})
	if cfg := e.chatCall("GET", base+"/config", "", nil, siteOrigin); cfg.JSON["identity_required"] != true {
		t.Fatalf("config: %s", cfg.Raw)
	}
	e.resetChatLimits()
	if r := start(map[string]any{"email": "walkin@example.org", "message": "Hi"}); r.Code != 422 || errCode(r) != "identity_required" {
		t.Fatalf("anonymous on a chat that needs identity: %d %s", r.Code, r.Raw)
	}
	if r := start(map[string]any{"email": "neema@example.org", "identity": sig, "message": "Hi again"}); r.Code != 201 {
		t.Fatalf("signed on a chat that needs identity: %d %s", r.Code, r.Raw)
	}

	// Rotating the secret returns a new one once and ends the old one.
	rot := e.admOK(admin, "POST", wsBase+"/chat/widgets/"+wid+"/identity-secret", nil)
	fresh := str(rot.JSON, "identity_secret")
	if fresh == "" || fresh == secret {
		t.Fatalf("rotate: %s", rot.Raw)
	}
	e.resetChatLimits()
	if r := start(map[string]any{"email": "neema@example.org", "identity": sig, "message": "Old signature"}); r.Code != 422 || errCode(r) != "identity_invalid" {
		t.Fatalf("the old secret still works: %d %s", r.Code, r.Raw)
	}
	if r := start(map[string]any{"email": "neema@example.org", "identity": service.ChatIdentityHash(fresh, "neema@example.org"), "message": "New"}); r.Code != 201 {
		t.Fatalf("the new secret: %d %s", r.Code, r.Raw)
	}
}

// Banning a visitor closes their conversation and stops them writing in it or starting another;
// lifting the ban undoes it.
func TestChatBanAndUnban(t *testing.T) {
	e := setup(t)
	e.svc.Opts.ChatEnabled = true
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")
	w := e.admOK(admin, "POST", wsBase+"/chat/widgets", map[string]any{"name": "Site", "allowed_origins": []string{siteOrigin}})
	base := "/v1/chat/" + str(w.JSON, "id")
	r := e.chatCall("POST", base+"/conversations", "", map[string]any{"email": "rude@example.org", "message": "abuse"}, siteOrigin)
	cid, tok := str(r.JSON, "conversation_id"), str(r.JSON, "token")

	e.admOK(admin, "POST", wsBase+"/chat/conversations/"+cid+"/ban", map[string]any{"reason": "abuse"})
	d := e.admOK(admin, "GET", wsBase+"/chat/conversations/"+cid, nil)
	if d.JSON["banned"] != true || d.JSON["state"] != "closed" {
		t.Fatalf("banned conversation: %s", d.Raw)
	}
	if r := e.chatCall("POST", base+"/conversations/"+cid+"/messages", tok, map[string]any{"body": "again"}, siteOrigin); r.Code != 403 || errCode(r) != "visitor_banned" {
		t.Fatalf("a banned visitor wrote: %d %s", r.Code, r.Raw)
	}
	e.resetChatLimits()
	if r := e.chatCall("POST", base+"/conversations", "", map[string]any{"email": "rude@example.org", "message": "new try"}, siteOrigin); r.Code != 403 || errCode(r) != "visitor_banned" {
		t.Fatalf("a banned visitor started a chat: %d %s", r.Code, r.Raw)
	}
	// Nothing about the ban is stored in the clear.
	if n := e.count(`SELECT count(*) FROM chat_bans WHERE environment_id=(SELECT environment_id FROM chat_conversations WHERE id=$1)`, cid); n != 1 {
		t.Fatalf("ban rows: %d", n)
	}

	e.admOK(admin, "DELETE", wsBase+"/chat/conversations/"+cid+"/ban", nil)
	e.resetChatLimits()
	if r := e.chatCall("POST", base+"/conversations", "", map[string]any{"email": "rude@example.org", "message": "sorry"}, siteOrigin); r.Code != 201 {
		t.Fatalf("after the ban was lifted: %d %s", r.Code, r.Raw)
	}
	if n := e.count(`SELECT count(*) FROM chat_conversations WHERE banned_at IS NOT NULL`); n != 0 {
		t.Fatalf("conversations still marked banned: %d", n)
	}
}

func TestChatSnippetsAndAnalytics(t *testing.T) {
	e := setup(t)
	e.svc.Opts.ChatEnabled = true
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")

	// Canned replies.
	for name, body := range map[string]map[string]any{"no title": {"title": "", "body": "x"}, "no text": {"title": "t", "body": " "}} {
		if r := e.adm(admin, "POST", wsBase+"/chat/snippets", body); r.Code != 400 {
			t.Fatalf("%s: %d", name, r.Code)
		}
	}
	s1 := e.admOK(admin, "POST", wsBase+"/chat/snippets", map[string]any{"title": "Opening hours", "body": "We are open 8 to 5."})
	if r := e.adm(admin, "POST", wsBase+"/chat/snippets", map[string]any{"title": "Opening hours", "body": "again"}); r.Code != 409 || errCode(r) != "snippet_exists" {
		t.Fatalf("duplicate title: %d %s", r.Code, r.Raw)
	}
	e.admOK(admin, "PUT", wsBase+"/chat/snippets/"+str(s1.JSON, "id"), map[string]any{"title": "Hours", "body": "Open 8 to 5, Monday to Friday."})
	got := items(e.admOK(admin, "GET", wsBase+"/chat/snippets", nil))
	if len(got) != 1 || str(got[0], "title") != "Hours" {
		t.Fatalf("snippets: %v", got)
	}
	e.admOK(admin, "DELETE", wsBase+"/chat/snippets/"+str(s1.JSON, "id"), nil)
	if got := items(e.admOK(admin, "GET", wsBase+"/chat/snippets", nil)); len(got) != 0 {
		t.Fatalf("after delete: %v", got)
	}

	// Analytics: three conversations, two answered.
	w := e.admOK(admin, "POST", wsBase+"/chat/widgets", map[string]any{"name": "Site", "allowed_origins": []string{siteOrigin}})
	base := "/v1/chat/" + str(w.JSON, "id")
	var ids []string
	for i, msg := range []string{"one", "two", "three"} {
		e.resetChatLimits()
		r := e.chatCall("POST", base+"/conversations", "", map[string]any{"message": "Question " + msg}, siteOrigin)
		if r.Code != 201 {
			t.Fatalf("start %d: %d %s", i, r.Code, r.Raw)
		}
		ids = append(ids, str(r.JSON, "conversation_id"))
	}
	for _, id := range ids[:2] {
		e.admOK(admin, "POST", wsBase+"/chat/conversations/"+id+"/messages", map[string]any{"body": "Answer"})
	}
	a := e.admOK(admin, "GET", wsBase+"/chat/analytics?days=7", nil)
	tot := a.JSON["totals"].(map[string]any)
	if tot["conversations"].(float64) != 3 || tot["answered"].(float64) != 2 || tot["unanswered"].(float64) != 1 || tot["first_answer_p50_seconds"] == nil {
		t.Fatalf("totals: %s", a.Raw)
	}
	msgs := a.JSON["messages"].(map[string]any)
	if msgs["from_visitors"].(float64) != 3 || msgs["from_agents"].(float64) != 2 {
		t.Fatalf("messages: %s", a.Raw)
	}
	if len(a.JSON["daily"].([]any)) != 1 {
		t.Fatalf("daily: %s", a.Raw)
	}
	if bad := e.admOK(admin, "GET", wsBase+"/chat/analytics?days=9999", nil); bad.JSON["days"].(float64) != 30 {
		t.Fatalf("an out-of-range period falls back to 30 days: %s", bad.Raw)
	}
}

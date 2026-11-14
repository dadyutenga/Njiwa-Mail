package test

import (
	"context"
	"strings"
	"testing"
	"time"
)

func (e *env) chatCall(method, path, token string, body any, origin string) resp {
	h := map[string]string{}
	if token != "" {
		h["Authorization"] = "Bearer " + token
	}
	if origin != "" {
		h["Origin"] = origin
	}
	return e.raw(method, path, h, body)
}

func (e *env) resetChatLimits() {
	_, err := e.pool.Exec(context.Background(), `DELETE FROM rate_limit_buckets WHERE scope_key LIKE 'ch:%' OR scope_key LIKE 'fm:%'`)
	e.must(err)
}

func TestChatIsOffByDefault(t *testing.T) {
	e := setup(t)
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")
	if r := e.adm(admin, "GET", wsBase+"/chat/widgets", nil); r.Code != 404 || errCode(r) != "chat_disabled" {
		t.Fatalf("admin: %d %s", r.Code, r.Raw)
	}
	id := "00000000-0000-4000-8000-000000000000"
	for _, p := range []string{"/config", "/widget.js"} {
		if r := e.raw("GET", "/v1/chat/"+id+p, nil, nil); r.Code != 404 {
			t.Fatalf("public %s: %d", p, r.Code)
		}
	}
	if r := e.raw("POST", "/v1/chat/"+id+"/conversations", nil, map[string]string{"message": "hi"}); r.Code != 404 {
		t.Fatalf("start: %d", r.Code)
	}
}

func TestChatConversationLifecycle(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.svc.Opts.ChatEnabled = true
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")
	e.provision("other")

	w := e.admOK(admin, "POST", wsBase+"/chat/widgets", map[string]any{"name": "Site", "allowed_origins": []string{siteOrigin + "/"},
		"title": "Talk to us", "welcome": "Habari!", "announcement": "Closed on Sunday", "reply_from": liveFrom,
		"links": []map[string]string{{"label": "Docs", "url": "https://docs.example.com"}}})
	wid := str(w.JSON, "id")
	base := "/v1/chat/" + wid

	// The widget script is one static file; the config is for allowed pages only.
	js := e.raw("GET", base+"/widget.js", nil, nil)
	if js.Code != 200 || !strings.HasPrefix(js.Hdr.Get("Content-Type"), "application/javascript") || !strings.Contains(js.Raw, "attachShadow") {
		t.Fatalf("widget.js: %d %v", js.Code, js.Hdr)
	}
	cfg := e.chatCall("GET", base+"/config", "", nil, siteOrigin)
	if cfg.Code != 200 || cfg.JSON["announcement"] != "Closed on Sunday" || cfg.Hdr.Get("Access-Control-Allow-Origin") != siteOrigin || len(cfg.JSON["links"].([]any)) != 1 {
		t.Fatalf("config: %d %v %s", cfg.Code, cfg.Hdr, cfg.Raw)
	}
	if o := e.chatCall("GET", base+"/config", "", nil, "https://evil.example"); o.Hdr.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("config for another origin: %v", o.Hdr)
	}
	pre := e.raw("OPTIONS", base+"/conversations/x/messages", map[string]string{"Origin": siteOrigin}, nil)
	if pre.Code != 204 || !strings.Contains(pre.Hdr.Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatalf("preflight: %d %v", pre.Code, pre.Hdr)
	}

	// Start: origin rules and validation first.
	start := map[string]any{"name": "Neema", "email": "Visitor@Example.org", "message": "Do you deliver to Arusha?"}
	if r := e.chatCall("POST", base+"/conversations", "", start, "https://evil.example"); r.Code != 403 || errCode(r) != "origin_not_allowed" {
		t.Fatalf("other origin: %d %s", r.Code, r.Raw)
	}
	if r := e.chatCall("POST", base+"/conversations", "", map[string]any{"message": "  "}, siteOrigin); r.Code != 422 || errCode(r) != "empty_message" {
		t.Fatalf("empty: %d %s", r.Code, r.Raw)
	}
	if r := e.chatCall("POST", base+"/conversations", "", map[string]any{"message": "hi", "email": "nope"}, siteOrigin); r.Code != 422 || errCode(r) != "invalid_email" {
		t.Fatalf("bad email: %d %s", r.Code, r.Raw)
	}
	r := e.chatCall("POST", base+"/conversations", "", start, siteOrigin)
	if r.Code != 201 || str(r.JSON, "token") == "" || str(r.JSON, "conversation_id") == "" {
		t.Fatalf("start: %d %s", r.Code, r.Raw)
	}
	cid, token := str(r.JSON, "conversation_id"), str(r.JSON, "token")
	conv := base + "/conversations/" + cid

	// Nothing readable is stored in clear.
	var plain int
	e.must(e.pool.QueryRow(ctx, `SELECT count(*) FROM chat_messages WHERE position(convert_to('Arusha','UTF8') in body_enc)>0`).Scan(&plain))
	if plain != 0 {
		t.Fatal("a message body is stored in clear")
	}

	// The token is the session: a wrong token, another chat's id and no token are all 404.
	for _, tk := range []string{"", "wrong", token + "x"} {
		if r := e.chatCall("GET", conv+"/messages", tk, nil, siteOrigin); r.Code != 404 {
			t.Fatalf("token %q: %d", tk, r.Code)
		}
	}
	if r := e.chatCall("GET", conv+"/stream", "wrong", nil, siteOrigin); r.Code != 404 {
		t.Fatalf("stream wrong token: %d", r.Code)
	}
	if r := e.chatCall("POST", conv+"/messages", token, map[string]any{"body": "Also: refunds?"}, siteOrigin); r.Code != 201 {
		t.Fatalf("visitor message: %d %s", r.Code, r.Raw)
	}
	if r := e.chatCall("POST", conv+"/messages", token, map[string]any{"body": "hi"}, "https://evil.example"); r.Code != 403 {
		t.Fatalf("visitor message from another origin: %d", r.Code)
	}

	// Dashboard: the conversation is in the inbox with 2 unread, nothing sensitive leaks into the list.
	list := e.admOK(admin, "GET", wsBase+"/chat/conversations", nil)
	it := items(list)
	if len(it) != 1 || it[0]["unread"].(float64) != 2 || it[0]["visitor"] != "Neema" || strings.Contains(list.Raw, "visitor@example.org") || it[0]["online"] != true {
		t.Fatalf("list: %s", list.Raw)
	}
	if other := e.admOK(admin, "GET", "/admin/v1/w/other/live/chat/conversations", nil); len(items(other)) != 0 {
		t.Fatal("another workspace sees the conversation")
	}
	if r := e.adm(admin, "GET", "/admin/v1/w/other/live/chat/conversations/"+cid, nil); r.Code != 404 {
		t.Fatalf("cross-workspace read: %d", r.Code)
	}
	detail := e.admOK(admin, "GET", wsBase+"/chat/conversations/"+cid, nil)
	if msgs := detail.JSON["messages"].([]any); len(msgs) != 2 || detail.JSON["visitor_email"] != "visitor@example.org" {
		t.Fatalf("detail: %s", detail.Raw)
	}
	if it := items(e.admOK(admin, "GET", wsBase+"/chat/conversations", nil)); it[0]["unread"].(float64) != 0 {
		t.Fatal("opening the conversation should mark it read")
	}

	// An answer reaches an open page: marked delivered, never emailed.
	rep := e.admOK(admin, "POST", wsBase+"/chat/conversations/"+cid+"/messages", map[string]any{"body": "Yes, we deliver to Arusha."})
	if str(rep.JSON, "id") == "" {
		t.Fatalf("reply: %s", rep.Raw)
	}
	poll := e.chatCall("GET", conv+"/messages", token, nil, siteOrigin)
	got := poll.JSON["items"].([]any)
	last := got[len(got)-1].(map[string]any)
	if poll.Code != 200 || len(got) != 3 || last["sender"] != "agent" || last["body"] != "Yes, we deliver to Arusha." || last["agent"] != "admin" || strings.Contains(poll.Raw, "@") {
		t.Fatalf("poll: %d %s", poll.Code, poll.Raw)
	}
	again := e.chatCall("GET", conv+"/messages?after="+poll.JSON["cursor"].(string), token, nil, siteOrigin)
	if len(again.JSON["items"].([]any)) != 0 {
		t.Fatalf("cursor repeats messages: %s", again.Raw)
	}
	e.backdateChat(cid, "visitor_seen_at", "created_at")
	if n, err := e.svc.SendChatEmails(ctx); err != nil || n != 0 {
		t.Fatalf("a delivered answer was emailed: %d %v", n, err)
	}

	// The visitor leaves; the next answer goes by email, once.
	e.admOK(admin, "POST", wsBase+"/chat/conversations/"+cid+"/messages", map[string]any{"body": "Our refund policy is on the site."})
	if n, _ := e.svc.SendChatEmails(ctx); n != 0 {
		t.Fatalf("emailed before the grace period: %d", n)
	}
	e.backdateChat(cid, "visitor_seen_at", "created_at")
	n, err := e.svc.SendChatEmails(ctx)
	if err != nil || n != 1 {
		t.Fatalf("follow-up: %d %v", n, err)
	}
	to := e.box.MAC("recipient", []byte("visitor@example.org"))
	if c := e.count(`SELECT count(*) FROM messages WHERE source='ui' AND recipient_hash=$1`, to); c != 1 {
		t.Fatalf("queued messages: %d", c)
	}
	if n, _ := e.svc.SendChatEmails(ctx); n != 0 {
		t.Fatalf("emailed twice: %d", n)
	}

	// A visitor who left no address is never emailed, and an answer is not marked as sent.
	e.resetChatLimits()
	r2 := e.chatCall("POST", base+"/conversations", "", map[string]any{"message": "Anyone there?"}, siteOrigin)
	cid2 := str(r2.JSON, "conversation_id")
	e.admOK(admin, "POST", wsBase+"/chat/conversations/"+cid2+"/messages", map[string]any{"body": "Yes."})
	e.backdateChat(cid2, "visitor_seen_at", "created_at")
	if n, _ := e.svc.SendChatEmails(ctx); n != 0 {
		t.Fatalf("no address, yet emailed: %d", n)
	}
	// ...until they leave one.
	if r := e.chatCall("PUT", base+"/conversations/"+cid2+"/contact", str(r2.JSON, "token"), map[string]any{"name": "Asha", "email": "asha@example.org"}, siteOrigin); r.Code != 200 {
		t.Fatalf("contact: %d %s", r.Code, r.Raw)
	}
	if n, _ := e.svc.SendChatEmails(ctx); n != 1 {
		t.Fatalf("after leaving an address: %d", n)
	}

	// Live streams. The visitor's page gets an answer written while it listens, and a resume
	// from the last event id repeats nothing; the dashboard hears a visitor's message.
	go func() {
		time.Sleep(120 * time.Millisecond)
		e.admOK(admin, "POST", wsBase+"/chat/conversations/"+cid+"/messages", map[string]any{"body": "Live answer"})
	}()
	sr := e.chatCall("GET", conv+"/stream", token, nil, siteOrigin)
	if sr.Code != 200 || !strings.HasPrefix(sr.Hdr.Get("Content-Type"), "text/event-stream") || !strings.Contains(sr.Raw, "event: message") || !strings.Contains(sr.Raw, "Live answer") {
		t.Fatalf("visitor stream: %d %v %s", sr.Code, sr.Hdr, sr.Raw)
	}
	if strings.Contains(sr.Raw, "@") {
		t.Fatalf("the stream shows a staff address: %s", sr.Raw)
	}
	var lastEvent string
	for _, l := range strings.Split(sr.Raw, "\n") {
		if strings.HasPrefix(l, "id: ") {
			lastEvent = strings.TrimPrefix(l, "id: ")
		}
	}
	if lastEvent == "" {
		t.Fatalf("no event id: %s", sr.Raw)
	}
	resume := e.raw("GET", conv+"/stream", map[string]string{"Authorization": "Bearer " + token, "Origin": siteOrigin, "Last-Event-ID": lastEvent}, nil)
	if strings.Contains(resume.Raw, "event: message") {
		t.Fatalf("resume repeated messages: %s", resume.Raw)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		e.chatCall("POST", conv+"/messages", token, map[string]any{"body": "Heard by the dashboard"}, siteOrigin)
	}()
	dr := e.raw("GET", wsBase+"/chat/conversations/stream", map[string]string{"Cookie": admin.cookie}, nil)
	if dr.Code != 200 || !strings.Contains(dr.Raw, "event: message") || !strings.Contains(dr.Raw, "Heard by the dashboard") {
		t.Fatalf("dashboard stream: %d %s", dr.Code, dr.Raw)
	}

	// Close, reopen by the visitor, delete.
	e.admOK(admin, "PATCH", wsBase+"/chat/conversations/"+cid, map[string]any{"state": "closed"})
	if it := items(e.admOK(admin, "GET", wsBase+"/chat/conversations?state=closed", nil)); len(it) != 1 {
		t.Fatalf("closed view: %v", it)
	}
	e.chatCall("POST", conv+"/messages", token, map[string]any{"body": "One more thing"}, siteOrigin)
	if it := items(e.admOK(admin, "GET", wsBase+"/chat/conversations?state=closed", nil)); len(it) != 0 {
		t.Fatal("a new visitor message should reopen the conversation")
	}
	e.admOK(admin, "DELETE", wsBase+"/chat/conversations/"+cid, nil)
	if r := e.chatCall("GET", conv+"/messages", token, nil, siteOrigin); r.Code != 404 {
		t.Fatalf("deleted conversation still readable: %d", r.Code)
	}
}

// backdateChat moves a conversation's last-seen time and its answers into the past.
func (e *env) backdateChat(cid, seenCol, msgCol string) {
	e.t.Helper()
	_, err := e.pool.Exec(context.Background(), `UPDATE chat_conversations SET `+seenCol+`=now()-interval '5 minutes' WHERE id=$1`, cid)
	e.must(err)
	_, err = e.pool.Exec(context.Background(), `UPDATE chat_messages SET `+msgCol+`=now()-interval '5 minutes' WHERE conversation_id=$1 AND sender='agent' AND delivered_at IS NULL`, cid)
	e.must(err)
}

func TestChatLimitsSpamAndWidgetRules(t *testing.T) {
	e := setup(t)
	e.svc.Opts.ChatEnabled = true
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")
	w := e.admOK(admin, "POST", wsBase+"/chat/widgets", map[string]any{"name": "Site", "allowed_origins": []string{siteOrigin}})
	base := "/v1/chat/" + str(w.JSON, "id")

	// Rules for widgets themselves.
	for _, bad := range []map[string]any{
		{"name": ""},
		{"name": "x", "allowed_origins": []string{"ftp://x"}},
		{"name": "x", "links": []map[string]string{{"label": "bad", "url": "javascript:alert(1)"}}},
		{"name": "x", "reply_from": "nope"},
	} {
		if r := e.adm(admin, "POST", wsBase+"/chat/widgets", bad); r.Code != 400 {
			t.Fatalf("%v: %d %s", bad, r.Code, r.Raw)
		}
	}
	if r := e.adm(admin, "POST", wsBase+"/chat/widgets", map[string]any{"name": "Site"}); r.Code != 409 || errCode(r) != "chat_exists" {
		t.Fatalf("duplicate: %d %s", r.Code, r.Raw)
	}

	// Spam is refused (there is no quarantine for chat).
	spam := "Make money fast: http://a.example http://b.example http://c.example viagra casino bit.ly/y"
	if r := e.chatCall("POST", base+"/conversations", "", map[string]any{"message": spam}, siteOrigin); r.Code != 422 || errCode(r) != "spam_rejected" {
		t.Fatalf("spam: %d %s", r.Code, r.Raw)
	}
	// Per-address limit: three new conversations a minute.
	e.resetChatLimits()
	for i := 0; i < 3; i++ {
		if r := e.chatCall("POST", base+"/conversations", "", map[string]any{"message": "hello " + strings.Repeat("x", i)}, siteOrigin); r.Code != 201 {
			t.Fatalf("conversation %d: %d %s", i, r.Code, r.Raw)
		}
	}
	r := e.chatCall("POST", base+"/conversations", "", map[string]any{"message": "one too many"}, siteOrigin)
	if r.Code != 429 || r.Hdr.Get("Retry-After") == "" {
		t.Fatalf("limit: %d %v", r.Code, r.Hdr)
	}

	// A disabled chat is invisible.
	id := str(w.JSON, "id")
	e.admOK(admin, "PUT", wsBase+"/chat/widgets/"+id, map[string]any{"name": "Site", "allowed_origins": []string{siteOrigin}, "enabled": false})
	if r := e.chatCall("GET", base+"/config", "", nil, siteOrigin); r.Code != 404 {
		t.Fatalf("disabled config: %d", r.Code)
	}
	e.admOK(admin, "DELETE", wsBase+"/chat/widgets/"+id, nil)
	if e.count(`SELECT count(*) FROM chat_conversations`) != 0 {
		t.Fatal("deleting a chat should delete its conversations")
	}
}

package test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const wsBase = "/admin/v1/w/sacas/sandbox"

func (e *env) verifiedDomain(admin session, project, mode, name string, bulk bool) string {
	e.t.Helper()
	r := e.admOK(admin, "POST", "/admin/v1/w/"+project+"/"+mode+"/domains", map[string]any{"name": name, "purpose": "both", "bulk": bulk})
	id, txt := str(r.JSON, "id"), str(r.JSON, "txt")
	dns := e.srv.Resolver.(fakeDNS)
	dns.txt[name] = []string{txt, "v=spf1 mx -all"}
	dns.txt["dkim._domainkey."+name] = []string{"v=DKIM1; k=rsa; p=MIIBIjANBgkqh"}
	dns.txt["_dmarc."+name] = []string{"v=DMARC1; p=none"}
	dns.mx[name] = []string{"mail.example.com."}
	r = e.admOK(admin, "POST", "/admin/v1/w/"+project+"/"+mode+"/domains/"+id+"/check", nil)
	for _, rec := range r.JSON["records"].([]any) {
		m := rec.(map[string]any)
		if m["key"] != "return_path" && m["pass"] != true {
			e.t.Fatalf("record %v did not pass: %v", m["key"], m)
		}
	}
	return id
}

func newDNS() fakeDNS {
	return fakeDNS{txt: map[string][]string{}, mx: map[string][]string{"notify.example.com": {"mail.example.com."}}, ptr: map[string][]string{}}
}

func TestWorkspaceScopeIsolation(t *testing.T) {
	e := setup(t)
	e.provision("sacas")
	e.provision("other")
	admin := e.user("admin@njiwa.co.tz", "admin")

	if r := e.adm(admin, "GET", "/admin/v1/w/nope/sandbox/context", nil); r.Code != 404 || errCode(r) != "project_not_found" {
		t.Fatalf("unknown project: %d %s", r.Code, r.Raw)
	}
	if r := e.adm(admin, "GET", "/admin/v1/w/sacas/staging/context", nil); r.Code != 404 {
		t.Fatalf("unknown mode: %d", r.Code)
	}
	if r := e.raw("GET", wsBase+"/context", nil, nil); r.Code != 401 {
		t.Fatalf("unauthenticated: %d", r.Code)
	}
	// A message of "other" is invisible from the sacas workspace.
	ko := e.provision("third")
	r := e.post(ko.sbx, idem("iso"), otpBody("user@example.org"))
	if r.Code != 202 {
		t.Fatalf("send: %d %s", r.Code, r.Raw)
	}
	otherID := str(r.JSON, "id")
	if r := e.adm(admin, "GET", wsBase+"/messages/"+otherID, nil); r.Code != 404 {
		t.Fatalf("cross-project message visible: %d", r.Code)
	}
	if n := len(items(e.admOK(admin, "GET", wsBase+"/messages", nil))); n != 0 {
		t.Fatalf("sacas sees %d messages of other projects", n)
	}
	if n := len(items(e.admOK(admin, "GET", "/admin/v1/w/third/sandbox/messages", nil))); n != 1 {
		t.Fatalf("third sees %d messages", n)
	}
	// Senders are per environment.
	for _, it := range items(e.admOK(admin, "GET", wsBase+"/senders", nil)) {
		if it["email"] != liveFrom {
			t.Fatalf("unexpected sender %v", it)
		}
	}
	// Keys of another project cannot be revoked through this scope.
	keys := items(e.admOK(admin, "GET", "/admin/v1/w/third/sandbox/keys", nil))
	if r := e.adm(admin, "POST", wsBase+"/keys/"+str(keys[0], "prefix")+"/revoke", nil); r.Code != 404 {
		t.Fatalf("cross-project revoke: %d", r.Code)
	}
}

func TestWorkspaceDomainsSendersAndChecklist(t *testing.T) {
	e := setup(t)
	e.srv.Resolver = newDNS()
	ctx := context.Background()
	e.must(e.svc.CreateProject(ctx, "sacas"))
	admin := e.user("admin@njiwa.co.tz", "admin")

	r := e.admOK(admin, "GET", wsBase+"/checklist", nil)
	if r.JSON["complete"] != false {
		t.Fatal("checklist complete on an empty project")
	}
	// Sender refused before its domain is verified.
	if r := e.adm(admin, "POST", wsBase+"/senders", map[string]any{"email": "hello@notify.example.com"}); r.Code != 409 || errCode(r) != "domain_not_verified" {
		t.Fatalf("sender before domain: %d %s", r.Code, r.Raw)
	}
	d := e.admOK(admin, "POST", wsBase+"/domains", map[string]any{"name": "notify.example.com", "purpose": "sending"})
	domainID := str(d.JSON, "id")
	if !strings.HasPrefix(str(d.JSON, "txt"), "njiwa-mail-verify=") {
		t.Fatalf("ownership record: %v", d.JSON)
	}
	if r := e.adm(admin, "POST", wsBase+"/domains", map[string]any{"name": "notify.example.com", "purpose": "sending"}); r.Code != 409 {
		t.Fatalf("duplicate domain: %d", r.Code)
	}
	// Detail lists the records before any check, with short host labels.
	det := e.admOK(admin, "GET", wsBase+"/domains/"+domainID, nil)
	found := map[string]string{}
	for _, rec := range det.JSON["records"].([]any) {
		m := rec.(map[string]any)
		found[m["key"].(string)] = m["short_host"].(string)
	}
	if found["dkim"] != "dkim._domainkey" || found["dmarc"] != "_dmarc" || found["ownership"] != "@" {
		t.Fatalf("short hosts: %v", found)
	}
	// Two SPF records fail with an explanation.
	dns := e.srv.Resolver.(fakeDNS)
	dns.txt["notify.example.com"] = []string{str(d.JSON, "txt"), "v=spf1 mx -all", "v=spf1 a -all"}
	r = e.admOK(admin, "POST", wsBase+"/domains/"+domainID+"/check", nil)
	for _, rec := range r.JSON["records"].([]any) {
		m := rec.(map[string]any)
		if m["key"] == "spf" && (m["pass"] != false || !strings.Contains(m["hint"].(string), "Two SPF")) {
			t.Fatalf("two SPF records: %v", m)
		}
	}
	dns.txt["notify.example.com"] = []string{str(d.JSON, "txt"), "v=spf1 mx -all"}
	dns.txt["dkim._domainkey.notify.example.com"] = []string{"v=DKIM1; k=rsa; p=MIIB"}
	dns.txt["_dmarc.notify.example.com"] = []string{"v=DMARC1; p=none"}
	e.admOK(admin, "POST", wsBase+"/domains/"+domainID+"/check", nil)

	s := e.admOK(admin, "POST", wsBase+"/senders", map[string]any{"email": "hello@notify.example.com", "display_name": "Hello"})
	senders := items(e.admOK(admin, "GET", wsBase+"/senders", nil))
	if len(senders) != 1 || senders[0]["status"] != "verified" || senders[0]["is_default"] != true {
		t.Fatalf("senders: %v", senders)
	}
	// Domain delete blocked while used, and names the user.
	if r := e.adm(admin, "DELETE", wsBase+"/domains/"+domainID, nil); r.Code != 409 || !strings.Contains(r.Raw, "hello@notify.example.com") {
		t.Fatalf("delete in-use domain: %d %s", r.Code, r.Raw)
	}
	e.admOK(admin, "DELETE", wsBase+"/senders/"+str(s.JSON, "id"), nil)
	e.admOK(admin, "DELETE", wsBase+"/domains/"+domainID, nil)

	steps := e.admOK(admin, "GET", wsBase+"/checklist", nil).JSON["steps"].([]any)
	if steps[1].(map[string]any)["done"] != true || steps[2].(map[string]any)["done"] != false {
		t.Fatalf("checklist after delete: %v", steps)
	}
}

func TestComposeIdempotencySuppressionAndCopy(t *testing.T) {
	e := setup(t)
	e.srv.Resolver = newDNS()
	e.provision("sacas")
	admin := e.user("admin@njiwa.co.tz", "admin")
	msg := map[string]any{"from": liveFrom, "to": "asha@example.org", "subject": "Habari", "html": "<p>Hello <b>Asha</b></p>"}
	hdr := map[string]string{"Cookie": admin.cookie, "X-CSRF-Token": admin.csrf, "Idempotency-Key": "ui-compose-0001"}
	r1 := e.raw("POST", wsBase+"/compose", hdr, msg)
	r2 := e.raw("POST", wsBase+"/compose", hdr, msg)
	if r1.Code != 200 || r2.Code != 200 || str(r1.JSON, "id") != str(r2.JSON, "id") {
		t.Fatalf("double click must not send twice: %d %s / %d %s", r1.Code, r1.Raw, r2.Code, r2.Raw)
	}
	id := str(r1.JSON, "id")
	if n := e.count(`SELECT count(*) FROM messages WHERE source='ui'`); n != 1 {
		t.Fatalf("ui messages: %d", n)
	}
	e.runWorker()
	if st := e.state(id); st != "captured" {
		t.Fatalf("sandbox compose: %s", st)
	}
	m := e.admOK(admin, "GET", wsBase+"/messages/"+id, nil)
	if m.JSON["subject"] != "Habari" || m.JSON["source"] != "ui" || m.JSON["recipient"] == "asha@example.org" || m.JSON["has_copy"] != true {
		t.Fatalf("message detail: %v", m.JSON)
	}
	cp := e.admOK(admin, "GET", wsBase+"/messages/"+id+"/copy", nil)
	if cp.JSON["to"] != "asha@example.org" || !strings.Contains(str(cp.JSON, "text"), "Hello Asha") {
		t.Fatalf("sent copy / plain text version: %v", cp.JSON)
	}
	// Suppressed recipients are named before sending and refused when sending.
	e.admOK(admin, "POST", wsBase+"/suppressions", map[string]any{"email": "blocked@example.org", "reason": "asked"})
	chk := e.admOK(admin, "POST", wsBase+"/recipients/check", map[string]any{"emails": []string{"blocked@example.org", "ok@example.org", "bad"}})
	if b := chk.JSON["blocked"].([]any); len(b) != 2 {
		t.Fatalf("blocked: %v", b)
	}
	hdr["Idempotency-Key"] = "ui-compose-0002"
	msg["to"] = "blocked@example.org"
	if r := e.raw("POST", wsBase+"/compose", hdr, msg); r.Code != 422 || errCode(r) != "recipient_suppressed" {
		t.Fatalf("suppressed compose: %d %s", r.Code, r.Raw)
	}
	// Retry is refused for a captured message.
	if r := e.adm(admin, "POST", wsBase+"/messages/"+id+"/retry", nil); r.Code != 409 {
		t.Fatalf("retry of captured: %d", r.Code)
	}
	// Drafts: save, list, delete.
	d := e.admOK(admin, "POST", wsBase+"/drafts", map[string]any{"subject": "Draft one", "to": "x@example.org"})
	if l := items(e.admOK(admin, "GET", wsBase+"/drafts", nil)); len(l) != 1 || l[0]["subject"] != "Draft one" {
		t.Fatalf("drafts: %v", l)
	}
	if g := e.admOK(admin, "GET", wsBase+"/drafts/"+str(d.JSON, "id"), nil); g.JSON["to"] != "x@example.org" {
		t.Fatalf("draft: %v", g.JSON)
	}
	e.admOK(admin, "DELETE", wsBase+"/drafts/"+str(d.JSON, "id"), nil)
	// Scheduled: send now.
	hdr["Idempotency-Key"] = "ui-compose-0003"
	msg["to"], msg["send_at"] = "later@example.org", time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339)
	sch := e.raw("POST", wsBase+"/compose", hdr, msg)
	if str(sch.JSON, "status") != "scheduled" {
		t.Fatalf("scheduled: %s", sch.Raw)
	}
	if l := items(e.admOK(admin, "GET", wsBase+"/messages?state=scheduled", nil)); len(l) != 1 {
		t.Fatalf("scheduled list: %v", l)
	}
	e.admOK(admin, "POST", wsBase+"/messages/"+str(sch.JSON, "id")+"/send-now", nil)
	if st := e.state(str(sch.JSON, "id")); st != "queued" {
		t.Fatalf("send now: %s", st)
	}
}

func TestInboxReplyAutomationAndTempInbox(t *testing.T) {
	e := setup(t)
	e.srv.Resolver = newDNS()
	e.provision("sacas")
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")

	if r := e.adm(admin, "POST", wsBase+"/mailboxes", map[string]any{"local_part": "help", "domain": "unverified.example"}); r.Code != 409 {
		t.Fatalf("mailbox on unverified domain: %d", r.Code)
	}
	mb := e.admOK(admin, "POST", wsBase+"/mailboxes", map[string]any{"local_part": "help", "domain": inDomain, "display_name": "Help"})
	mbID := str(mb.JSON, "id")

	// An auto-reply rule from a published template.
	tpl := e.admOK(admin, "POST", wsBase+"/templates", map[string]any{"slug": "thanks"})
	tplID := str(tpl.JSON, "id")
	e.admOK(admin, "PUT", wsBase+"/templates/"+tplID+"/draft", map[string]any{
		"variable_schema": []map[string]any{{"name": "subject", "type": "string", "required": true, "max_len": 300}},
		"content":         map[string]any{"en": map[string]any{"subject": "Re: {{.subject}}", "text": "We received your message about {{.subject}}.", "html": "<p>We received it.</p>"}}})
	e.admOK(admin, "POST", wsBase+"/templates/"+tplID+"/versions/1/publish", nil)
	rule := e.admOK(admin, "POST", wsBase+"/automations", map[string]any{"name": "Thanks", "mailbox_id": mbID, "action": "auto_reply", "template_id": tplID})
	ruleID := str(rule.JSON, "id")
	e.admOK(admin, "PATCH", wsBase+"/automations/"+ruleID, map[string]any{"enabled": true})
	label := e.admOK(admin, "POST", wsBase+"/automations", map[string]any{"name": "Label", "action": "label", "target": "support"})
	e.admOK(admin, "PATCH", wsBase+"/automations/"+str(label.JSON, "id"), map[string]any{"enabled": true})

	addr := e.listener("127.0.0.1/32")
	to := "help@" + inDomain
	e.must(smtpSend(addr, "asha@customer.example", []string{to}, inboundMail(to)))
	e.must(smtpSend(addr, "asha@customer.example", []string{to}, strings.Replace(inboundMail(to), "reply-1@", "reply-2@", 1)))
	auto := strings.Replace(strings.Replace(inboundMail(to), "reply-1@", "reply-3@", 1), "MIME-Version", "Auto-Submitted: auto-replied\nMIME-Version", 1)
	e.must(smtpSend(addr, "robot@customer.example", []string{to}, auto))
	e.process()

	list := items(e.admOK(admin, "GET", wsBase+"/inbox?mailbox="+mbID+"&unread=1", nil))
	if len(list) != 3 || !strings.Contains(str(list[0], "snippet"), "nahitaji msaada") {
		t.Fatalf("inbox: %v", list)
	}
	if u := e.admOK(admin, "GET", wsBase+"/inbox/unread", nil); u.JSON["total"] != float64(3) {
		t.Fatalf("unread: %v", u.JSON)
	}
	// One auto-reply per sender per day; none to automatic mail; labels applied.
	if n := e.count(`SELECT count(*) FROM messages WHERE source='automation'`); n != 1 {
		t.Fatalf("auto-replies: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM automation_runs WHERE result='skipped'`); n != 2 {
		t.Fatalf("skipped runs: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM inbound_messages WHERE 'support' = ANY(labels)`); n != 3 {
		t.Fatalf("labels: %d", n)
	}
	// Opening marks read; reader links the automation that handled it.
	first := str(list[len(list)-1], "id")
	open := e.admOK(admin, "GET", wsBase+"/inbox/"+first, nil)
	if open.JSON["text"] == "" || len(open.JSON["automation_runs"].([]any)) == 0 {
		t.Fatalf("open: %v", open.JSON)
	}
	if u := e.admOK(admin, "GET", wsBase+"/inbox/unread", nil); u.JSON["total"] != float64(2) {
		t.Fatalf("unread after open: %v", u.JSON)
	}
	// Dry-run test of a rule against a stored message.
	tr := e.admOK(admin, "POST", wsBase+"/automations/"+ruleID+"/test", map[string]any{"inbound_id": first})
	if tr.JSON["result"] != "would_skip" {
		t.Fatalf("dry run (already answered today): %v", tr.JSON)
	}
	// Reply is threaded.
	hdr := map[string]string{"Cookie": admin.cookie, "X-CSRF-Token": admin.csrf, "Idempotency-Key": "ui-reply-00001"}
	rep := e.raw("POST", wsBase+"/compose", hdr, map[string]any{"from": liveFrom, "to": "asha@customer.example", "subject": "Re: help",
		"text": "Karibu", "in_reply_to": first})
	if rep.Code != 200 {
		t.Fatalf("reply: %d %s", rep.Code, rep.Raw)
	}
	e.runWorker()
	raw, err := e.svc.AdminCapture(ctx, str(rep.JSON, "id"))
	e.must(err)
	if !strings.Contains(string(raw), "In-Reply-To: <reply-1@customer.example>") || strings.Contains(string(raw), "Auto-Submitted") {
		t.Fatalf("reply headers:\n%s", raw)
	}
	// Archive, block sender, delete.
	e.admOK(admin, "PATCH", wsBase+"/inbox/"+first, map[string]any{"archived": true})
	if l := items(e.admOK(admin, "GET", wsBase+"/inbox?archived=1", nil)); len(l) != 1 {
		t.Fatalf("archived: %d", len(l))
	}
	e.admOK(admin, "POST", wsBase+"/inbox/"+first+"/block", nil)
	e.must(smtpSend(addr, "asha@customer.example", []string{to}, strings.Replace(inboundMail(to), "reply-1@", "reply-9@", 1)))
	e.process()
	if n := e.count(`SELECT count(*) FROM inbound_messages`); n != 3 {
		t.Fatalf("blocked sender stored: %d", n)
	}
	e.admOK(admin, "DELETE", wsBase+"/inbox/"+first, nil)

	// Temporary inbox: create, receive, expire.
	tmp := e.admOK(admin, "POST", wsBase+"/temp-inboxes", map[string]any{"ttl": "1h"})
	taddr := str(tmp.JSON, "address")
	e.must(smtpSend(addr, "x@customer.example", []string{taddr}, inboundMail(taddr)))
	e.process()
	if n := e.count(`SELECT count(*) FROM inbound_messages m JOIN inbound_addresses a ON a.id=m.address_id WHERE a.temporary`); n != 1 {
		t.Fatalf("temp inbox mail: %d", n)
	}
	_, err = e.pool.Exec(ctx, `UPDATE inbound_addresses SET expires_at=now() - interval '1 second' WHERE temporary`)
	e.must(err)
	e.must(e.svc.ExpireTempInboxes(ctx))
	if n := e.count(`SELECT count(*) FROM inbound_addresses WHERE temporary`); n != 0 {
		t.Fatalf("expired temp inbox remains")
	}
	// Mailbox delete removes its mail.
	e.admOK(admin, "DELETE", wsBase+"/mailboxes/"+mbID, nil)
	if n := e.count(`SELECT count(*) FROM inbound_messages`); n != 0 {
		t.Fatalf("mailbox mail remains: %d", n)
	}
}

func arfMail(messageID string) string {
	return `From: fbl@isp.example
To: fbl@notify.example.com
Subject: Abuse report
Message-ID: <arf-` + messageID[:8] + `@isp.example>
MIME-Version: 1.0
Content-Type: multipart/report; report-type=feedback-report; boundary="b"

--b
Content-Type: text/plain

This is an abuse report.
--b
Content-Type: message/feedback-report

Feedback-Type: abuse
User-Agent: ISP-FBL/1.0
Version: 1
Original-Rcpt-To: redacted

--b
Content-Type: text/rfc822-headers

X-DM-Message-ID: ` + messageID + `

--b--
`
}

func TestBulkCampaignUnsubscribeAndComplaintPause(t *testing.T) {
	e := setup(t)
	e.srv.Resolver = newDNS()
	e.svc.Opts.PublicURL = "https://mail.example.com"
	e.provision("sacas")
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")

	body := map[string]any{"name": "Launch", "sender_email": "news@news.example.com", "subject": "Hi {{name}}", "html": "<p>Hello {{name}}</p>",
		"recipients": "email,name\nasha@example.org,Asha\nasha@example.org,Asha\nbad-address,X\nbaraka@example.org,Baraka\n"}
	if r := e.adm(admin, "POST", wsBase+"/campaigns", body); r.Code != 409 || errCode(r) != "bulk_domain_required" {
		t.Fatalf("bulk without bulk domain: %d %s", r.Code, r.Raw)
	}
	e.verifiedDomain(admin, "sacas", "sandbox", "news.example.com", true)
	e.admOK(admin, "POST", wsBase+"/senders", map[string]any{"email": "news@news.example.com"})

	chk := e.admOK(admin, "POST", wsBase+"/campaigns/check", map[string]any{"recipients": body["recipients"]})
	if chk.JSON["valid"] != float64(2) || len(chk.JSON["duplicates"].([]any)) != 1 || len(chk.JSON["invalid"].([]any)) != 1 {
		t.Fatalf("recipient check: %v", chk.JSON)
	}
	c := e.admOK(admin, "POST", wsBase+"/campaigns", body)
	cid := str(c.JSON, "id")
	e.admOK(admin, "POST", wsBase+"/campaigns/"+cid+"/start", nil)
	n, err := e.svc.PumpCampaigns(ctx)
	e.must(err)
	if n != 2 {
		t.Fatalf("pumped %d", n)
	}
	// A restart (second pump) never sends twice.
	n, _ = e.svc.PumpCampaigns(ctx)
	if c := e.count(`SELECT count(*) FROM messages WHERE campaign_id=$1`, cid); c != 2 || n != 0 {
		t.Fatalf("messages %d, second pump %d", c, n)
	}
	e.runWorker()
	var mid string
	e.must(e.pool.QueryRow(ctx, `SELECT id::text FROM messages WHERE campaign_id=$1 ORDER BY created_at LIMIT 1`, cid).Scan(&mid))
	raw, err := e.svc.AdminCapture(ctx, mid)
	e.must(err)
	s := string(raw)
	if !strings.Contains(s, "List-Unsubscribe: <https://mail.example.com/v1/unsubscribe/") || !strings.Contains(s, "List-Unsubscribe-Post: List-Unsubscribe=One-Click") ||
		!strings.Contains(s, "Hello Asha") && !strings.Contains(s, "Hello Baraka") {
		t.Fatalf("bulk message:\n%s", s)
	}
	i := strings.Index(s, "/v1/unsubscribe/")
	token := s[i+len("/v1/unsubscribe/") : i+len("/v1/unsubscribe/")+strings.IndexAny(s[i+len("/v1/unsubscribe/"):], ">\r\n")]
	if r := e.raw("POST", "/v1/unsubscribe/"+token+"x", nil, nil); r.Code != 404 {
		t.Fatalf("forged token: %d", r.Code)
	}
	if r := e.roundTrip(httptest.NewRequest("POST", "/v1/unsubscribe/"+token, strings.NewReader("List-Unsubscribe=One-Click"))); r.Code != 200 {
		t.Fatalf("unsubscribe: %d %s", r.Code, r.Raw)
	}
	// Next campaign skips the unsubscribed address; transactional mail is unaffected.
	chk = e.admOK(admin, "POST", wsBase+"/campaigns/check", map[string]any{"recipients": "asha@example.org\nbaraka@example.org"})
	if chk.JSON["valid"] != float64(1) || len(chk.JSON["unsubscribed"].([]any)) != 1 {
		t.Fatalf("after unsubscribe: %v", chk.JSON)
	}
	det := e.admOK(admin, "GET", wsBase+"/campaigns/"+cid, nil)
	if det.JSON["unsubscribed"] != float64(1) || det.JSON["accepted"] != float64(2) {
		t.Fatalf("campaign totals: %v", det.JSON)
	}

	// A complaint pauses a sending campaign and suppresses the recipient.
	c2 := e.admOK(admin, "POST", wsBase+"/campaigns", map[string]any{"name": "Second", "sender_email": "news@news.example.com", "subject": "Again",
		"text": "Again", "recipients": "c1@example.org\nc2@example.org\nc3@example.org\nc4@example.org\nc5@example.org\nc6@example.org\nc7@example.org\nc8@example.org\nc9@example.org\nc10@example.org\nc11@example.org\nc12@example.org\nc13@example.org\nc14@example.org\nc15@example.org\nc16@example.org", "rate_per_minute": 4})
	c2id := str(c2.JSON, "id")
	e.admOK(admin, "POST", wsBase+"/campaigns/"+c2id+"/start", nil)
	_, err = e.svc.PumpCampaigns(ctx)
	e.must(err)
	e.must(e.pool.QueryRow(ctx, `SELECT id::text FROM messages WHERE campaign_id=$1 LIMIT 1`, c2id).Scan(&mid))
	addr := e.listener("127.0.0.1/32")
	e.must(smtpSend(addr, "fbl@isp.example", []string{"fbl@notify.example.com"}, arfMail(mid)))
	e.process()
	det = e.admOK(admin, "GET", wsBase+"/campaigns/"+c2id, nil)
	if det.JSON["state"] != "paused" || det.JSON["complaints"] != float64(1) {
		t.Fatalf("complaint did not pause: %v", det.JSON)
	}
	if n := e.count(`SELECT count(*) FROM suppressions WHERE source='complaint'`); n != 1 {
		t.Fatalf("complaint suppression: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM alerts WHERE kind='campaign_paused'`); n != 1 {
		t.Fatalf("alert: %d", n)
	}
	if l := items(e.admOK(admin, "GET", wsBase+"/bounces?type=complaint", nil)); len(l) != 1 {
		t.Fatalf("complaints list: %v", l)
	}
}

func TestMailServerSettingsSecrets(t *testing.T) {
	e := setup(t)
	owner := e.user("owner@njiwa.co.tz", "owner")
	admin := e.user("admin@njiwa.co.tz", "admin")
	in := map[string]any{"smtp_host": "mail.example.com", "smtp_port": 587, "smtp_tls_mode": "starttls", "smtp_username": "njiwa@example.com",
		"smtp_password": "very-secret-password", "test_sender": "hello@notify.example.com", "mailcow_api_url": "https://mail.example.com", "mailcow_api_key": "KEY-123"}
	r := e.admOK(admin, "PUT", "/admin/v1/mail-server", in)
	if strings.Contains(r.Raw, "very-secret") || strings.Contains(r.Raw, "KEY-123") || r.JSON["smtp_password_set"] != true || r.JSON["mailcow_api_key_set"] != true {
		t.Fatalf("secrets returned: %s", r.Raw)
	}
	if n := e.count(`SELECT count(*) FROM mail_server WHERE smtp_password_enc::text LIKE '%very-secret%'`); n != 0 {
		t.Fatal("password stored in clear")
	}
	// Saving again without a password keeps the stored one.
	delete(in, "smtp_password")
	delete(in, "mailcow_api_key")
	r = e.admOK(owner, "PUT", "/admin/v1/mail-server", in)
	if r.JSON["smtp_password_set"] != true {
		t.Fatal("password lost on save")
	}
	cfg, sender, _, ok, err := e.svc.StoredSMTP(context.Background())
	if err != nil || !ok || cfg.Password != "very-secret-password" || sender != "hello@notify.example.com" {
		t.Fatalf("stored smtp: %v %v %v", ok, err, sender)
	}
	if r := e.adm(owner, "PUT", "/admin/v1/mail-server", map[string]any{"smtp_host": "mail.example.com", "smtp_tls_mode": "none"}); r.Code != 400 {
		t.Fatalf("plaintext SMTP accepted: %d", r.Code)
	}
}

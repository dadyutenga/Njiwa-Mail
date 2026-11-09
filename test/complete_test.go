package test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/ditronics-tz/mail/internal/secure"
)

func TestRawSendScheduleCancelAndBatch(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	k := e.provision("rawapp")
	rawKey, _, err := e.svc.CreateKey(ctx, "rawapp", "sandbox", []string{"emails:send_raw"}, "raw", 0)
	e.must(err)
	if r := e.post(k.sbx, idem("raw-no-scope"), map[string]any{"from": liveFrom, "to": testRcpt, "subject": "Hi", "text": "body"}); r.Code != 403 {
		t.Fatalf("template key must not raw-send: %d %s", r.Code, r.Raw)
	}
	body := map[string]any{"from": liveFrom, "to": testRcpt, "subject": "Hello", "text": "plain", "html": "<p>plain</p>", "priority": "high"}
	ok := e.do("POST", "/v1/emails", rawKey, idem("raw-ok"), body)
	if ok.Code != 202 || str(ok.JSON, "status") != "queued" {
		t.Fatalf("raw send: %d %s", ok.Code, ok.Raw)
	}
	if r := e.do("POST", "/v1/emails/"+str(ok.JSON, "id")+"/cancel", rawKey, "", nil); r.Code != 200 {
		t.Fatalf("cancel: %d %s", r.Code, r.Raw)
	}
	when := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	sched := e.do("POST", "/v1/emails", rawKey, idem("raw-later"), map[string]any{"from": liveFrom, "to": testRcpt, "subject": "Later", "text": "wait", "send_at": when})
	if sched.Code != 202 || str(sched.JSON, "status") != "scheduled" {
		t.Fatalf("schedule: %d %s", sched.Code, sched.Raw)
	}
	if n := e.runWorker(); n != 0 {
		t.Fatalf("scheduled message was claimed: %d", n)
	}
	pdf := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4"))
	withFile := e.do("POST", "/v1/emails", rawKey, idem("raw-file"), map[string]any{
		"from": liveFrom, "to": testRcpt, "cc": []string{"cc@example.org"}, "subject": "File", "text": "see attached",
		"attachments": []map[string]string{{"filename": "a.pdf", "content_type": "application/pdf", "content": pdf}},
	})
	if withFile.Code != 202 {
		t.Fatalf("attachment: %d %s", withFile.Code, withFile.Raw)
	}
	batch := e.do("POST", "/v1/emails/batch", rawKey, "", map[string]any{"messages": []any{
		map[string]any{"idempotency_key": idem("b1"), "from": liveFrom, "to": testRcpt, "subject": "One", "text": "1"},
		map[string]any{"idempotency_key": idem("b1"), "from": liveFrom, "to": "other@example.org", "subject": "Two", "text": "2"},
	}})
	if batch.Code != 200 {
		t.Fatalf("batch: %d %s", batch.Code, batch.Raw)
	}
	results, _ := batch.JSON["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("batch results: %s", batch.Raw)
	}
	second := results[1].(map[string]any)
	if int(second["status"].(float64)) != 409 {
		t.Fatalf("second batch item should conflict: %v", second)
	}
}

func TestDomainGateAndSearch(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	_ = e.provision("dom")
	id, token, err := e.svc.CreateDomain(ctx, "dom", "notify.example.com", "sending")
	e.must(err)
	if token == "" || id == "" {
		t.Fatal("missing domain token")
	}
	k, _, err := e.svc.CreateKey(ctx, "dom", "live", []string{"emails:send"}, "live", 0)
	e.must(err)
	e.must(e.svc.AddSender(ctx, "dom", "live", liveFrom, "App"))
	r := e.post(k, idem("blocked-domain"), otpBody(testRcpt))
	if r.Code != 403 || errCode(r) != "domain_not_ready" {
		t.Fatalf("unverified domain should block live send: %d %s", r.Code, r.Raw)
	}
}

func TestTOTPAndPrivacy(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.svc.Opts.RequireTOTP = true
	if _, err := e.svc.CreateUser(ctx, "owner@example.com", adminPass, "owner"); err != nil {
		t.Fatal(err)
	}
	sess := e.login("owner@example.com")
	if r := e.adm(sess, "POST", "/admin/v1/projects", map[string]any{"name": "blocked"}); r.Code != 403 || errCode(r) != "totp_required" {
		t.Fatalf("owner without totp: %d %s", r.Code, r.Raw)
	}
	_ = e.provision("priv")
	k, _, err := e.svc.CreateKey(ctx, "priv", "sandbox", []string{"emails:send"}, "p", 0)
	e.must(err)
	acc := e.post(k, idem("priv"), otpBody(testRcpt))
	if acc.Code != 202 {
		t.Fatal(acc.Raw)
	}
	exp, err := e.svc.ExportRecipient(ctx, "priv", "sandbox", testRcpt)
	e.must(err)
	msgs, _ := exp["messages"].([]map[string]any)
	if len(msgs) != 1 {
		t.Fatalf("export: %#v", exp["messages"])
	}
	e.must(e.svc.DeleteRecipient(ctx, "priv", "sandbox", testRcpt))
	if e.count(`SELECT count(*) FROM messages`) != 0 {
		t.Fatal("recipient data was not deleted")
	}
}

func TestReencrypt(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	k := e.provision("enc")
	acc := e.post(k.sbx, idem("enc"), otpBody(testRcpt))
	if acc.Code != 202 {
		t.Fatal(acc.Raw)
	}
	next := make([]byte, 32)
	if _, err := rand.Read(next); err != nil {
		t.Fatal(err)
	}
	e.must(e.svc.Reencrypt(ctx, next))
	box, err := secure.New(next, []byte("hmac-key-hmac-key-hmac-key-hmac-key!!"))
	if err != nil {
		t.Fatal(err)
	}
	var id string
	var enc []byte
	if err := e.pool.QueryRow(ctx, `SELECT id::text, payload_enc FROM messages LIMIT 1`).Scan(&id, &enc); err != nil {
		t.Fatal(err)
	}
	pt, err := box.Open(enc, []byte(id))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pt), "otp") {
		t.Fatalf("reencrypted payload: %s", pt)
	}
}

package test

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/service"
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

// D02: Live mail and Live senders need a registered, verified, claimed
// domain on every path. There is no grandfathering of unregistered domains.
func TestDomainGateAndSearch(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	_ = e.provision("dom")
	const from = "hello@gate.example.com"
	k, _, err := e.svc.CreateKey(ctx, "dom", "live", []string{"emails:send"}, "live", 0)
	e.must(err)
	body := otpBody(testRcpt)
	body["from"] = from

	// Unregistered domain: no Live sender by CLI/admin path, no Live mail.
	if err := e.svc.AddSender(ctx, "dom", "live", from, "App"); errCodeOf(err) != "domain_not_registered" {
		t.Fatalf("live sender on unregistered domain: %v", err)
	}
	e.must(e.svc.AddSender(ctx, "dom", "sandbox", from, "App")) // sandbox never sends
	_, err = e.pool.Exec(ctx, `INSERT INTO sender_identities(project_id, environment_id, email, display_name)
SELECT p.id, en.id, $1, 'legacy' FROM projects p JOIN environments en ON en.project_id=p.id AND en.mode='live' WHERE p.name='dom'`, from)
	e.must(err) // a sender created before this gate existed
	if r := e.post(k, idem("unregistered"), body); r.Code != 403 || errCode(r) != "domain_not_registered" {
		t.Fatalf("unregistered domain must block live send: %d %s", r.Code, r.Raw)
	}

	// Registered but not verified.
	id, token, err := e.svc.CreateDomain(ctx, "dom", "Gate.Example.COM.", "sending")
	e.must(err)
	if token == "" || id == "" {
		t.Fatal("missing domain token")
	}
	if e.count(`SELECT count(*) FROM domains WHERE id=$1 AND name='gate.example.com'`, id) != 1 {
		t.Fatal("domain name not stored canonically")
	}
	if r := e.post(k, idem("unverified"), body); r.Code != 403 || errCode(r) != "domain_not_ready" {
		t.Fatalf("unverified domain should block live send: %d %s", r.Code, r.Raw)
	}
	// Ownership proven and claimed but DNS not passing: sender may be created, mail not sent.
	_, err = e.pool.Exec(ctx, `UPDATE domains SET verified_at=now() WHERE id=$1`, id)
	e.must(err)
	_, err = e.pool.Exec(ctx, `INSERT INTO domain_claims(name, project_id, state) SELECT 'gate.example.com', id, 'active' FROM projects WHERE name='dom'`)
	e.must(err)
	e.must(e.svc.AddSender(ctx, "dom", "live", from, "App"))
	if r := e.post(k, idem("nodns"), body); r.Code != 403 || errCode(r) != "domain_not_ready" {
		t.Fatalf("failing DNS should block live send: %d %s", r.Code, r.Raw)
	}
	_, err = e.pool.Exec(ctx, `UPDATE domains SET spf_ok=true, dkim_ok=true, dmarc_ok=true WHERE id=$1`, id)
	e.must(err)
	if r := e.post(k, idem("ready"), body); r.Code != 202 {
		t.Fatalf("ready domain: %d %s", r.Code, r.Raw)
	}
}

func TestPrivacy(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
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

func TestRejectedUploadsAndUnauthorizedAttachmentAccess(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.provision("files")
	e.provision("otherfiles")
	rawKey, _, err := e.svc.CreateKey(ctx, "files", "sandbox", []string{"emails:send_raw", "inbound:read"}, "raw", 0)
	e.must(err)
	otherKey, _, err := e.svc.CreateKey(ctx, "otherfiles", "sandbox", []string{"inbound:read"}, "raw", 0)
	e.must(err)
	sendOnly, _, err := e.svc.CreateKey(ctx, "files", "sandbox", []string{"emails:send"}, "send", 0)
	e.must(err)
	e.srv.EmailMaxBytes = 8 << 20

	pdf := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n"))
	rejects := []map[string]string{
		{"filename": "note.html", "content_type": "text/html", "content": base64.StdEncoding.EncodeToString([]byte("<html>"))},
		{"filename": "note.pdf.exe", "content_type": "application/pdf", "content": pdf},
		{"filename": "note.exe.pdf", "content_type": "application/pdf", "content": pdf},
		{"filename": "note.pdf", "content_type": "application/pdf", "content": base64.StdEncoding.EncodeToString([]byte("<html>not a pdf"))},
		{"filename": "../note.pdf", "content_type": "application/pdf", "content": pdf},
		{"filename": "note.txt", "content_type": "text/plain", "content": base64.StdEncoding.EncodeToString([]byte("hello\x00"))},
	}
	for i, att := range rejects {
		r := e.do("POST", "/v1/emails", rawKey, idem("bad-"+string(rune('a'+i))), map[string]any{
			"from": liveFrom, "to": testRcpt, "subject": "File", "text": "x",
			"attachments": []map[string]string{att},
		})
		if r.Code != 422 || errCode(r) != "invalid_attachment" {
			t.Fatalf("accepted unsafe upload %s: %d %s", att["filename"], r.Code, r.Raw)
		}
	}
	big := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("a"), (5<<20)+1))
	r := e.do("POST", "/v1/emails", rawKey, idem("too-big"), map[string]any{
		"from": liveFrom, "to": testRcpt, "subject": "File", "text": "x",
		"attachments": []map[string]string{{"filename": "note.txt", "content_type": "text/plain", "content": big}},
	})
	if r.Code != 422 || errCode(r) != "attachments_too_large" {
		t.Fatalf("oversized upload: %d %s", r.Code, r.Raw)
	}

	var projectID, envID string
	e.must(e.pool.QueryRow(ctx, `SELECT p.id::text, e.id::text FROM projects p JOIN environments e ON e.project_id=p.id WHERE p.name='files' AND e.mode='sandbox'`).Scan(&projectID, &envID))
	msgID := "11111111-1111-4111-8111-111111111111"
	attID := "22222222-2222-4222-8222-222222222222"
	spoolID := "33333333-3333-4333-8333-333333333333"
	rel := filepath.Join("attachments", msgID, "01-file")
	full := filepath.Join(e.svc.Opts.DataDir, rel)
	e.must(os.MkdirAll(filepath.Dir(full), 0o700))
	e.must(os.WriteFile(full, []byte("%PDF-1.4"), 0o600))
	e.must(e.pool.QueryRow(ctx, `INSERT INTO inbound_messages(id, project_id, environment_id, spool_id, envelope_from, envelope_to, raw_size, retention_expires_at)
VALUES ($1,$2,$3,$4,'a@b.example','in@in.example',8, now() + interval '1 day') RETURNING id`, msgID, projectID, envID, spoolID).Scan(&msgID))
	e.must(e.pool.QueryRow(ctx, `INSERT INTO inbound_attachments(id, inbound_message_id, filename, content_type, size, storage_path, sha256)
VALUES ($1,$2,'note.pdf','application/pdf',8,$3,'abc') RETURNING id`, attID, msgID, rel).Scan(&attID))

	if r := e.raw("GET", "/v1/inbound/"+msgID+"/attachments/"+attID, nil, nil); r.Code != 401 {
		t.Fatalf("anonymous download: %d", r.Code)
	}
	if r := e.do("GET", "/v1/inbound/"+msgID+"/attachments/"+attID, sendOnly, "", nil); r.Code != 403 {
		t.Fatalf("key without inbound:read: %d", r.Code)
	}
	if r := e.do("GET", "/v1/inbound/"+msgID+"/attachments/"+attID, otherKey, "", nil); r.Code != 404 {
		t.Fatalf("other project download: %d", r.Code)
	}
	if r := e.raw("GET", "/admin/v1/inbound/"+msgID+"/attachments/"+attID, nil, nil); r.Code != 401 {
		t.Fatalf("anonymous admin download: %d", r.Code)
	}
	ok := e.do("GET", "/v1/inbound/"+msgID+"/attachments/"+attID, rawKey, "", nil)
	if ok.Code != 200 || ok.Hdr.Get("X-Content-Type-Options") != "nosniff" || ok.Hdr.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("authorized download: %d %v", ok.Code, ok.Hdr)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE inbound_attachments SET storage_path='../../etc/passwd' WHERE id=$1`, attID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.OpenAttachment(ctx, envID, msgID, attID); err == nil {
		t.Fatal("storage path escaped the data directory")
	}
}

func TestInternationalAddress(t *testing.T) {
	got, ok := service.NormalizeEmail("User@Bücher.example")
	if !ok || got != "user@xn--bcher-kva.example" {
		t.Fatalf("idna address: %q %v", got, ok)
	}
	if _, ok := service.NormalizeEmail("not an address"); ok {
		t.Fatal("space accepted")
	}
}

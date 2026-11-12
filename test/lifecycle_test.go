package test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F10: new attachments are sealed on disk and downloaded decrypted; files
// written before the change are converted without breaking downloads.
func TestAttachmentsAreEncryptedAtRestAndLegacyOnesConverted(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.provision("sacas")
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.admOK(admin, "POST", "/admin/v1/inbound-addresses", map[string]any{"project": "sacas", "mode": "live", "local_part": "support"})
	inKey, _, err := e.svc.CreateKey(ctx, "sacas", "live", []string{"inbound:read"}, "in", 0)
	e.must(err)
	addr := e.listener("127.0.0.0/8")
	to := "support@" + inDomain
	e.must(smtpSend(addr, "asha@customer.example", []string{to}, inboundMail(to)))
	e.process()

	var id, aid, rel string
	var sealed bool
	e.must(e.pool.QueryRow(ctx, `SELECT a.inbound_message_id::text, a.id::text, a.storage_path, a.encrypted FROM inbound_attachments a`).Scan(&id, &aid, &rel, &sealed))
	if !sealed {
		t.Fatal("new attachment not marked encrypted")
	}
	onDisk, err := os.ReadFile(filepath.Join(e.svc.Opts.DataDir, rel))
	e.must(err)
	if bytes.Contains(onDisk, []byte("%PDF")) {
		t.Fatal("attachment stored in clear")
	}
	dl := e.do("GET", "/v1/inbound/"+id+"/attachments/"+aid, inKey, "", nil)
	if dl.Code != 200 || !strings.HasPrefix(dl.Raw, "%PDF-1.4") {
		t.Fatalf("download of a sealed attachment: %d %q", dl.Code, dl.Raw)
	}
	adl := e.adm(admin, "GET", "/admin/v1/inbound/"+id+"/attachments/"+aid, nil)
	if adl.Code != 200 || !strings.HasPrefix(adl.Raw, "%PDF-1.4") || adl.Hdr.Get("Content-Length") == "" {
		t.Fatalf("operator download: %d %q", adl.Code, adl.Raw)
	}
	// A swapped file (sealed for another attachment) is refused, not served.
	_, err = e.pool.Exec(ctx, `UPDATE inbound_attachments SET id='99999999-9999-4999-8999-999999999999' WHERE id=$1`, aid)
	e.must(err)
	if r := e.do("GET", "/v1/inbound/"+id+"/attachments/99999999-9999-4999-8999-999999999999", inKey, "", nil); r.Code == 200 {
		t.Fatal("ciphertext bound to another attachment id was served")
	}

	// Legacy plaintext file written before F10.
	var projectID, envID string
	e.must(e.pool.QueryRow(ctx, `SELECT project_id::text, environment_id::text FROM inbound_messages WHERE id=$1`, id).Scan(&projectID, &envID))
	legacyMsg, legacyAtt := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	lrel := filepath.Join("attachments", legacyMsg, "01-file")
	full := filepath.Join(e.svc.Opts.DataDir, lrel)
	e.must(os.MkdirAll(filepath.Dir(full), 0o700))
	e.must(os.WriteFile(full, []byte("%PDF-legacy"), 0o600))
	_, err = e.pool.Exec(ctx, `INSERT INTO inbound_messages(id, project_id, environment_id, spool_id, envelope_from, envelope_to, raw_size, retention_expires_at)
VALUES ($1,$2,$3,'33333333-3333-4333-8333-333333333333','a@b.example',$4,8, now() + interval '1 day')`, legacyMsg, projectID, envID, to)
	e.must(err)
	_, err = e.pool.Exec(ctx, `INSERT INTO inbound_attachments(id, inbound_message_id, filename, content_type, size, storage_path, sha256)
VALUES ($1,$2,'old.pdf','application/pdf',11,$3,'abc')`, legacyAtt, legacyMsg, lrel)
	e.must(err)
	if r := e.do("GET", "/v1/inbound/"+legacyMsg+"/attachments/"+legacyAtt, inKey, "", nil); r.Code != 200 || r.Raw != "%PDF-legacy" {
		t.Fatalf("legacy download before conversion: %d %q", r.Code, r.Raw)
	}
	n, err := e.svc.EncryptLegacyAttachments(ctx, 10)
	if err != nil || n != 1 {
		t.Fatalf("converted %d (%v), want 1", n, err)
	}
	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Fatal("plaintext legacy file still on disk")
	}
	if r := e.do("GET", "/v1/inbound/"+legacyMsg+"/attachments/"+legacyAtt, inKey, "", nil); r.Code != 200 || r.Raw != "%PDF-legacy" {
		t.Fatalf("legacy download after conversion: %d %q", r.Code, r.Raw)
	}
	if n, _ := e.svc.EncryptLegacyAttachments(ctx, 10); n != 0 {
		t.Fatalf("second run converted %d", n)
	}
}

// F10: delivery evidence and installer receipts age out on their own clocks,
// but never the receipt that proves an installation.
func TestEvidenceAndReceiptRetention(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	k := e.provision("ret")
	r := e.post(k.live, idem("ret1"), otpBody(testRcpt))
	if r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	id := str(r.JSON, "id")
	e.runWorker()
	_, err := e.pool.Exec(ctx, `UPDATE delivery_events SET created_at = now() - interval '200 days' WHERE message_id=$1`, id)
	e.must(err)
	_, err = e.pool.Exec(ctx, `INSERT INTO delivery_events(message_id, source, event, dedupe_key) VALUES ($1,'mta_log','remote_accepted','fresh-1')`, id)
	e.must(err)

	var profile string
	e.must(e.pool.QueryRow(ctx, `INSERT INTO transport_profiles(mode) VALUES ('managed_mta') RETURNING id::text`).Scan(&profile))
	ins := func(result string, verified bool, rollback, age string) {
		v := "NULL"
		if verified {
			v = "now()"
		}
		_, err := e.pool.Exec(ctx, `INSERT INTO installer_receipts(profile_id, plan_digest, action, result_code, verified_at, rollback_state, created_at)
VALUES ($1,'d','apply',$2,`+v+`,$3, now() - $4::interval)`, profile, result, rollback, age)
		e.must(err)
	}
	ins("ok", true, "none", "900 days")      // proves the installation: kept
	ins("failed", false, "none", "800 days") // old noise: deleted
	ins("ok", true, "rolled_back", "700 days")
	ins("ok", true, "none", "1000 days") // older proof, superseded: deleted
	ins("failed", false, "none", "5 days")

	res, err := e.svc.RunRetention(ctx)
	e.must(err)
	if res.DeliveryEvents != 1 {
		t.Fatalf("delivery events purged: %d", res.DeliveryEvents)
	}
	if e.count(`SELECT count(*) FROM delivery_events WHERE message_id=$1`, id) != 1 {
		t.Fatal("recent evidence removed")
	}
	if res.Receipts != 3 || e.count(`SELECT count(*) FROM installer_receipts`) != 2 {
		t.Fatalf("receipts purged %d, left %d", res.Receipts, e.count(`SELECT count(*) FROM installer_receipts`))
	}
	if e.count(`SELECT count(*) FROM installer_receipts WHERE result_code='ok' AND rollback_state='none' AND verified_at IS NOT NULL`) != 1 {
		t.Fatal("the receipt proving the installation was removed")
	}
	// Operator-tunable.
	e.must(e.svc.UpdateSettings(ctx, map[string]int{"delivery_events_retention_days": 1}))
	_, err = e.pool.Exec(ctx, `UPDATE delivery_events SET created_at = now() - interval '2 days'`)
	e.must(err)
	if res, _ := e.svc.RunRetention(ctx); res.DeliveryEvents != 1 {
		t.Fatalf("tuned retention purged %d", res.DeliveryEvents)
	}
}

// F10: data-subject requests for one message and for one recipient.
func TestExportAndDeletePerMessageAndRecipient(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	k := e.provision("dsr")
	r := e.post(k.live, idem("dsr1"), otpBody(testRcpt))
	id := str(r.JSON, "id")
	e.runWorker()
	_, err := e.pool.Exec(ctx, `INSERT INTO bounces(message_id, project_id, environment_id, recipient_hash, recipient_masked, type, dedupe_hash)
SELECT m.id, m.project_id, m.environment_id, m.recipient_hash, 't***@example.org', 'soft', '\x01' FROM messages m WHERE m.id=$1`, id)
	e.must(err)
	base := "/admin/v1/w/dsr/live"

	// One message: export is a download, metadata only.
	ex := e.adm(admin, "GET", base+"/messages/"+id+"/export", nil)
	if ex.Code != 200 || !strings.Contains(ex.Hdr.Get("Content-Disposition"), "attachment") || !strings.Contains(ex.Raw, id) {
		t.Fatalf("message export: %d %s", ex.Code, ex.Raw)
	}
	if strings.Contains(ex.Raw, "Your code") || strings.Contains(ex.Raw, testRcpt) {
		t.Fatal("export leaks the body or the full recipient address")
	}
	// An active message cannot be deleted.
	_, err = e.pool.Exec(ctx, `UPDATE messages SET state='queued' WHERE id=$1`, id)
	e.must(err)
	if res := e.adm(admin, "DELETE", base+"/messages/"+id, nil); res.Code != 409 || errCode(res) != "message_active" {
		t.Fatalf("delete active: %d %s", res.Code, res.Raw)
	}
	_, err = e.pool.Exec(ctx, `UPDATE messages SET state='submitted' WHERE id=$1`, id)
	e.must(err)
	// Another project cannot reach it.
	e.provision("dsr2")
	if res := e.adm(admin, "DELETE", "/admin/v1/w/dsr2/live/messages/"+id, nil); res.Code != 404 {
		t.Fatalf("cross-project delete: %d", res.Code)
	}
	e.admOK(admin, "DELETE", base+"/messages/"+id, nil)
	if e.count(`SELECT count(*) FROM messages WHERE id=$1`, id) != 0 || e.count(`SELECT count(*) FROM bounces`) != 0 || e.count(`SELECT count(*) FROM delivery_events WHERE message_id=$1`, id) != 0 {
		t.Fatal("message rows remain")
	}
	if e.count(`SELECT count(*) FROM audit_log WHERE action IN ('privacy.delete_message','privacy.export_message')`) != 2 {
		t.Fatal("not audited")
	}

	// One recipient: JSON and CSV export, then delete including inbound files.
	r = e.post(k.live, idem("dsr3"), otpBody(testRcpt))
	if r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	e.runWorker()
	js := e.admOK(admin, "POST", base+"/privacy/export", map[string]any{"Email": testRcpt})
	if len(js.JSON["messages"].([]any)) != 1 || len(js.JSON["delivery_events"].([]any)) < 1 {
		t.Fatalf("recipient export: %s", js.Raw)
	}
	csv := e.adm(admin, "POST", base+"/privacy/export?format=csv", map[string]any{"Email": testRcpt})
	if csv.Code != 200 || !strings.HasPrefix(csv.Hdr.Get("Content-Type"), "text/csv") || !strings.HasPrefix(csv.Raw, "kind,id,message_id,when,state_or_event,detail") ||
		!strings.Contains(csv.Raw, "message,") || !strings.Contains(csv.Raw, "delivery_event,") {
		t.Fatalf("csv: %d %s", csv.Code, csv.Raw)
	}

	// An inbound message from this address with an attachment on disk.
	var projectID, envID string
	e.must(e.pool.QueryRow(ctx, `SELECT p.id::text, e.id::text FROM projects p JOIN environments e ON e.project_id=p.id WHERE p.name='dsr' AND e.mode='live'`).Scan(&projectID, &envID))
	msg, att := "44444444-4444-4444-8444-444444444444", "55555555-5555-4555-8555-555555555555"
	dir := filepath.Join(e.svc.Opts.DataDir, "attachments", msg)
	e.must(os.MkdirAll(dir, 0o700))
	e.must(os.WriteFile(filepath.Join(dir, "01-x"), []byte("secret"), 0o600))
	_, err = e.pool.Exec(ctx, `INSERT INTO inbound_messages(id, project_id, environment_id, spool_id, envelope_from, envelope_to, raw_size, retention_expires_at)
VALUES ($1,$2,$3,'66666666-6666-4666-8666-666666666666',$4,'in@in.example',8, now() + interval '1 day')`, msg, projectID, envID, testRcpt)
	e.must(err)
	_, err = e.pool.Exec(ctx, `INSERT INTO inbound_attachments(id, inbound_message_id, filename, content_type, size, storage_path, sha256)
VALUES ($1,$2,'a','text/plain',6,$3,'x')`, att, msg, filepath.Join("attachments", msg, "01-x"))
	e.must(err)
	e.admOK(admin, "POST", base+"/privacy/delete", map[string]any{"Email": testRcpt})
	if e.count(`SELECT count(*) FROM messages WHERE environment_id=$1`, envID) != 0 || e.count(`SELECT count(*) FROM inbound_messages`) != 0 {
		t.Fatal("recipient rows remain")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("inbound attachment files of the recipient were left on disk")
	}
}

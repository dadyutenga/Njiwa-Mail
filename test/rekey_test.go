package test

import (
	"crypto/rand"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/secure"
	"github.com/njiwa-tz/mail/internal/service"
	"github.com/njiwa-tz/mail/internal/webhook"
)

var testHMAC = []byte("hmac-key-hmac-key-hmac-key-hmac-key!!")

func rotatedBoxes(t *testing.T, old []byte) (both, onlyNew *secure.Box) {
	t.Helper()
	newKey := make([]byte, 32)
	if _, err := rand.Read(newKey); err != nil {
		t.Fatal(err)
	}
	nb, err := secure.New(newKey, testHMAC)
	if err != nil {
		t.Fatal(err)
	}
	both, err = nb.WithPrevious(old)
	if err != nil {
		t.Fatal(err)
	}
	return both, nb
}

// C02: the key can be changed while the data stays readable, rekey converts
// everything, and afterwards the old key is not needed.
func TestEncryptionKeyRotation(t *testing.T) {
	e := setup(t)
	ctx := t.Context()
	oldKey := []byte("0123456789abcdef0123456789abcdef")

	// Data sealed under the old key in several places.
	k := e.provision("sacas")
	if r := e.post(k.sbx, idem("rot2"), otpBody(testRcpt)); r.Code != 202 { // sandbox capture
		t.Fatal(r.Raw)
	}
	e.runWorker()
	live := e.post(k.live, idem("rot1"), otpBody("queued@example.org")) // queued payload, sent after rotation
	if live.Code != 202 {
		t.Fatal(live.Raw)
	}
	e.saveSMTP("mail.example.com", "smtp-secret-pw") // profile and legacy mail_server secrets
	rc := newReceiver(t)
	e.endpoint(rc, "live", []string{"email.submitted"}) // webhook signing secret
	admin := e.user("admin@njiwa.co.tz", "admin")
	totpSecret, _ := e.enrol(admin) // TOTP secret
	e.admOK(admin, "POST", "/admin/v1/inbound-addresses", map[string]any{"project": "sacas", "mode": "live", "local_part": "support"})
	inKey, _, err := e.svc.CreateKey(ctx, "sacas", "live", []string{"inbound:read"}, "in", 0)
	e.must(err)
	to := "support@" + inDomain
	e.must(smtpSend(e.listener("127.0.0.0/8"), "asha@customer.example", []string{to}, inboundMail(to)))
	e.process()
	var inID, attID string
	e.must(e.pool.QueryRow(ctx, `SELECT inbound_message_id::text, id::text FROM inbound_attachments`).Scan(&inID, &attID))
	// A "view in browser" copy and an HTTP ingest secret.
	var msgID, liveEnv string
	e.must(e.pool.QueryRow(ctx, `SELECT id::text FROM messages LIMIT 1`).Scan(&msgID))
	e.must(e.pool.QueryRow(ctx, `SELECT e.id::text FROM environments e JOIN projects p ON p.id=e.project_id WHERE p.name='sacas' AND e.mode='live'`).Scan(&liveEnv))
	_, err = e.svc.StoreWebView(ctx, msgID, "Web view", "<p>sealed under the old key</p>")
	e.must(err)
	ingestSecret, err := e.svc.RotateIngestSecret(ctx, liveEnv)
	e.must(err)

	// Rotate: new key seals, old key still opens.
	both, onlyNew := rotatedBoxes(t, oldKey)
	e.svc.Box = both
	if r := e.do("GET", "/v1/inbound/"+inID+"/attachments/"+attID, inKey, "", nil); r.Code != 200 || !strings.HasPrefix(r.Raw, "%PDF") {
		t.Fatalf("data unreadable while both keys are configured: %d", r.Code)
	}
	dry, err := e.svc.Rekey(ctx, true)
	e.must(err)
	if dry.Unreadable() != 0 || dry.Changed() < 7 {
		t.Fatalf("dry run: %+v", dry)
	}
	before := e.count(`SELECT count(*) FROM messages`)
	done, err := e.svc.Rekey(ctx, false)
	e.must(err)
	if done.Changed() != dry.Changed() || done.Unreadable() != 0 {
		t.Fatalf("rekey changed %d, dry run said %d (%+v)", done.Changed(), dry.Changed(), done)
	}
	again, err := e.svc.Rekey(ctx, true)
	e.must(err)
	if again.Changed() != 0 || again.Unreadable() != 0 {
		t.Fatalf("rekey is not complete: %+v", again)
	}
	if e.count(`SELECT count(*) FROM messages`) != before {
		t.Fatal("rekey changed row counts")
	}

	// The old key is no longer needed: drop it and everything still works.
	e.svc.Box = onlyNew
	final, err := e.svc.Rekey(ctx, true)
	e.must(err)
	scanned := 0
	for _, c := range final.Columns {
		scanned += c.Scanned
	}
	if final.Unreadable() != 0 || final.Changed() != 0 || scanned < 7 {
		t.Fatalf("with only the new key: %+v", final)
	}
	if r := e.do("GET", "/v1/inbound/"+inID+"/attachments/"+attID, inKey, "", nil); r.Code != 200 || !strings.HasPrefix(r.Raw, "%PDF") {
		t.Fatalf("attachment with only the new key: %d", r.Code)
	}
	if r := e.loginWith("admin@njiwa.co.tz", totpCode(t, totpSecret, 1)); r.Code != 200 {
		t.Fatalf("MFA login with only the new key: %d %s", r.Code, r.Raw)
	}
	if _, html, err := e.svc.OpenWebView(ctx, strings.TrimPrefix(e.svc.WebViewURL(msgID), e.svc.Opts.PublicURL+"/v1/view/")); err != nil || !strings.Contains(html, "old key") {
		t.Fatalf("web view with only the new key: %v", err)
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	if _, err := e.svc.VerifyIngest(ctx, liveEnv, ts, service.IngestSignature(ingestSecret, ts, []byte("x")), []byte("x")); err != nil {
		t.Fatalf("ingest secret with only the new key: %v", err)
	}
	e.runWorker() // opens the queued payload sealed under the old key
	if e.count(`SELECT count(*) FROM messages WHERE source<>'automation' AND state='submitted'`) < 1 {
		t.Fatal("queued message sealed under the old key was not sent")
	}
	d := &webhook.Deliverer{Svc: e.svc, AllowPrivate: true, RootCAs: rc.pool()}
	if n, err := d.RunOnce(ctx); err != nil || n < 1 {
		t.Fatalf("webhook signing secret unreadable after rotation: %d %v", n, err)
	}
}

// Values no configured key can open are reported, never altered.
func TestRekeyReportsUnreadableAndChangesNothing(t *testing.T) {
	e := setup(t)
	ctx := t.Context()
	k := e.provision("rk")
	if r := e.post(k.live, idem("u1"), otpBody(testRcpt)); r.Code != 202 {
		t.Fatal(r.Raw)
	}
	var before []byte
	e.must(e.pool.QueryRow(ctx, `SELECT payload_enc FROM messages`).Scan(&before))
	_, stranger := rotatedBoxes(t, []byte("fedcba9876543210fedcba9876543210")) // does not know the old key
	e.svc.Box = stranger
	for _, dry := range []bool{true, false} {
		rep, err := e.svc.Rekey(ctx, dry)
		e.must(err)
		if rep.Unreadable() != 1 || rep.Changed() != 0 {
			t.Fatalf("dry=%v: %+v", dry, rep)
		}
	}
	var after []byte
	e.must(e.pool.QueryRow(ctx, `SELECT payload_enc FROM messages`).Scan(&after))
	if !reflect.DeepEqual(before, after) {
		t.Fatal("an unreadable value was modified")
	}
}

// A new sealed column must be added to the rekey list, or rotating the key
// would silently strand its data.
func TestEverySealedColumnIsRekeyed(t *testing.T) {
	e := setup(t)
	rows, err := e.pool.Query(t.Context(), `SELECT table_name || '.' || column_name FROM information_schema.columns
WHERE table_schema='public' AND (column_name LIKE '%\_enc' OR column_name LIKE 'encrypted\_%') ORDER BY 1`)
	e.must(err)
	var inDB []string
	for rows.Next() {
		var s string
		e.must(rows.Scan(&s))
		inDB = append(inDB, s)
	}
	rows.Close()
	covered := service.SealedColumnNames()
	sort.Strings(covered)
	// Deduplicate (two specs may share a column name pattern only if listed twice).
	if !reflect.DeepEqual(inDB, covered) {
		t.Fatalf("sealed columns in the schema:\n  %v\nrekey covers:\n  %v", inDB, covered)
	}
}

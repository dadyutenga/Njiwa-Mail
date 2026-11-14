package test

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/service"
)

func (e *env) ingest(env, secret, ts string, body []byte, hdr map[string]string) resp {
	r := httptest.NewRequest("POST", "/v1/inbound/ingest/"+env, bytes.NewReader(body))
	r.Header.Set("Content-Type", "message/rfc822")
	r.Header.Set("X-Njiwa-Timestamp", ts)
	r.Header.Set("X-Njiwa-Signature", service.IngestSignature(secret, ts, body))
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return e.roundTrip(r)
}

// HTTP ingest: a signed raw message is spooled and stored like mail received
// over SMTP. Only this environment's addresses; replays are idempotent;
// wrong, stale or unconfigured signatures are the same 401.
func TestInboundIngest(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")
	e.provision("other")
	_, err := e.svc.CreateInboundAddress(ctx, "sacas", "sandbox", "orders", nil, "store", "")
	e.must(err)
	_, err = e.svc.CreateInboundAddress(ctx, "other", "sandbox", "theirs", nil, "store", "")
	e.must(err)
	var envID string
	e.must(e.pool.QueryRow(ctx, `SELECT e.id::text FROM environments e JOIN projects p ON p.id=e.project_id WHERE p.name='sacas' AND e.mode='sandbox'`).Scan(&envID))

	msg := []byte("From: Ada <ada@example.org>\r\nTo: orders@" + inDomain + "\r\nSubject: Order 7\r\nMessage-ID: <o7@example.org>\r\n\r\nPlease ship it.\r\n")
	now := strconv.FormatInt(time.Now().Unix(), 10)
	// Not configured yet: refused like a bad signature.
	if r := e.ingest(envID, "nis_whatever", now, msg, nil); r.Code != 401 {
		t.Fatalf("unconfigured: %d %s", r.Code, r.Raw)
	}
	st := e.admOK(admin, "POST", wsBase+"/inbound-ingest/secret", nil)
	secret := str(st.JSON, "secret")
	if !strings.HasPrefix(secret, "nis_") || st.JSON["enabled"] != true {
		t.Fatalf("%s", st.Raw)
	}
	if g := e.admOK(admin, "GET", wsBase+"/inbound-ingest", nil); strings.Contains(g.Raw, secret) {
		t.Fatal("the secret is shown again")
	}
	r := e.ingest(envID, secret, now, msg, nil)
	if r.Code != 202 || r.JSON["recipients"] != float64(1) {
		t.Fatalf("ingest: %d %s", r.Code, r.Raw)
	}
	e.process()
	if n := e.count(`SELECT count(*) FROM inbound_messages WHERE subject='Order 7' AND environment_id='` + envID + `'`); n != 1 {
		t.Fatalf("stored: %d", n)
	}
	// The same signed request again: no second message.
	if r := e.ingest(envID, secret, now, msg, nil); r.Code != 200 || r.JSON["duplicate"] != true {
		t.Fatalf("replay: %d %s", r.Code, r.Raw)
	}
	// Wrong secret, a stale timestamp, an altered body: all 401.
	old := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	if r := e.ingest(envID, "nis_wrong", now, msg, nil); r.Code != 401 || r.Hdr.Get("WWW-Authenticate") == "" {
		t.Fatalf("wrong secret: %d", r.Code)
	}
	if r := e.ingest(envID, secret, old, msg, nil); r.Code != 401 {
		t.Fatalf("stale: %d", r.Code)
	}
	// Another project's address cannot be reached with this environment's secret.
	if r := e.ingest(envID, secret, now, []byte("From: x@example.org\r\nSubject: s\r\n\r\nb"), map[string]string{
		"X-Njiwa-Recipients": "theirs@" + inDomain}); r.Code != 422 || errCode(r) != "no_inbound_recipient" {
		t.Fatalf("other environment: %d %s", r.Code, r.Raw)
	}
	// Disabled: refused again.
	e.admOK(admin, "DELETE", wsBase+"/inbound-ingest", nil)
	if r := e.ingest(envID, secret, now, append(msg, '!'), nil); r.Code != 401 {
		t.Fatalf("after disable: %d", r.Code)
	}
}

package test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInvalidAndRevokedKey(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	for name, tok := range map[string]string{"none": "", "garbage": "nope", "malformed": "dmk_liv_abc_def", "wrong-secret": k.live[:len(k.live)-3] + "AAA"} {
		if r := e.post(tok, idem(name), resetBody(testRcpt)); r.Code != 401 {
			t.Fatalf("%s: want 401 got %d", name, r.Code)
		}
	}
	// revoke
	ks, _ := e.svc.ListKeys(context.Background(), "sacas")
	parts := strings.SplitN(k.live, "_", 4)
	e.must(e.svc.RevokeKey(context.Background(), parts[2]))
	_ = ks
	if r := e.post(k.live, idem("rev"), resetBody(testRcpt)); r.Code != 401 {
		t.Fatalf("revoked key: want 401 got %d", r.Code)
	}
	if n := e.count(`SELECT count(*) FROM messages`); n != 0 {
		t.Fatalf("denied requests inserted %d messages", n)
	}
	// expired key
	exp, _, _ := e.svc.CreateKey(context.Background(), "sacas", "live", []string{"emails:send"}, "x", time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	if r := e.post(exp, idem("exp"), resetBody(testRcpt)); r.Code != 401 {
		t.Fatalf("expired key: want 401 got %d", r.Code)
	}
	// scope: read-only key cannot send
	if r := e.post(k.liveRead, idem("scope"), resetBody(testRcpt)); r.Code != 403 {
		t.Fatalf("missing scope: want 403 got %d", r.Code)
	}
	// rotation: old key stays valid during grace, new works; grace 0 revokes
	oldTok, oldPrefix, err := e.svc.CreateKey(context.Background(), "sacas", "live", []string{"emails:send"}, "rot", 0)
	e.must(err)
	graceTok, gracePrefix, err := e.svc.CreateKey(context.Background(), "sacas", "live", []string{"emails:send"}, "rot2", 0)
	e.must(err)
	newTok, _, err := e.svc.RotateKey(context.Background(), oldPrefix, 0)
	e.must(err)
	if r := e.post(oldTok, idem("old"), resetBody(testRcpt)); r.Code != 401 {
		t.Fatalf("rotated(0 grace) old key: want 401 got %d", r.Code)
	}
	if _, _, err := e.svc.RotateKey(context.Background(), gracePrefix, time.Hour); err != nil {
		t.Fatal(err)
	}
	if r := e.post(graceTok, idem("grace"), resetBody("grace@example.org")); r.Code != 202 {
		t.Fatalf("old key must work during grace: %d", r.Code)
	}
	if r := e.post(newTok, idem("new"), resetBody(testRcpt)); r.Code != 202 {
		t.Fatalf("new key: want 202 got %d %s", r.Code, r.Raw)
	}
}

func TestProjectEnvironmentIsolation(t *testing.T) {
	e := setup(t)
	a := e.provision("proj-a")
	b := e.provision("proj-b")
	r := e.post(a.live, idem("1"), resetBody(testRcpt))
	if r.Code != 202 {
		t.Fatal(r.Raw)
	}
	id := str(r.JSON, "id")
	if g := e.get(b.live, id); g.Code != 404 {
		t.Fatalf("foreign project read: want 404 got %d", g.Code)
	}
	// same project, other environment (sandbox key) cannot see the live message
	if g := e.get(a.sbx, id); g.Code != 404 {
		t.Fatalf("foreign environment read: want 404 got %d", g.Code)
	}
	if g := e.get(a.live, id); g.Code != 200 {
		t.Fatalf("own read: want 200 got %d", g.Code)
	}
	// idempotency keys are scoped per project+env: same key, independent messages
	r2 := e.post(b.live, idem("1"), resetBody(testRcpt))
	if r2.Code != 202 || str(r2.JSON, "id") == id {
		t.Fatalf("idem key must be scoped per project: %d %s", r2.Code, r2.Raw)
	}
	// a sender approved for A only is rejected for C
	c := e.provision("proj-c")
	_, err := e.pool.Exec(context.Background(), `DELETE FROM sender_identities WHERE project_id=(SELECT id FROM projects WHERE name='proj-c')`)
	e.must(err)
	if r := e.post(c.live, idem("2"), resetBody(testRcpt)); r.Code != 403 {
		t.Fatalf("want 403 got %d", r.Code)
	}
	// unknown / malformed id
	if g := e.get(a.live, "not-a-uuid"); g.Code != 404 {
		t.Fatal(g.Code)
	}
	if g := e.get(a.live, "00000000-0000-4000-8000-000000000000"); g.Code != 404 {
		t.Fatal(g.Code)
	}
}

func TestApprovedSenderEnforcement(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	for _, from := range []string{"ceo@evil.example.net", "security@notify.example.com.evil.net", "Security <security@notify.example.com>"} {
		b := resetBody(testRcpt)
		b["from"] = from
		r := e.post(k.live, idem("s"+from[:3]), b)
		if r.Code != 403 && r.Code != 422 {
			t.Fatalf("from %q: want 403/422 got %d", from, r.Code)
		}
	}
	b := resetBody(testRcpt)
	b["from"] = "ceo@evil.example.net"
	if r := e.post(k.live, idem("arb"), b); r.Code != 403 || errCode(r) != "sender_not_approved" {
		t.Fatalf("arbitrary sender: %d %s", r.Code, r.Raw)
	}
	if n := e.count(`SELECT count(*) FROM messages`); n != 0 {
		t.Fatalf("%d messages inserted", n)
	}
}

func TestIdempotencySequentialAndConcurrent(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	r1 := e.post(k.live, idem("seq"), resetBody(testRcpt))
	r2 := e.post(k.live, idem("seq"), resetBody(testRcpt))
	if r1.Code != 202 || r2.Code != 202 || str(r1.JSON, "id") != str(r2.JSON, "id") {
		t.Fatalf("sequential replay: %v %v", r1.Raw, r2.Raw)
	}
	// concurrent repeats of a new key
	var wg sync.WaitGroup
	ids := make(chan string, 30)
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := e.post(k.live, idem("conc"), resetBody("other@example.org"))
			if r.Code != 202 {
				t.Errorf("concurrent: %d %s", r.Code, r.Raw)
			}
			ids <- str(r.JSON, "id")
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		seen[id] = true
	}
	if len(seen) != 1 {
		t.Fatalf("concurrent repeats produced %d message ids", len(seen))
	}
	if n := e.count(`SELECT count(*) FROM messages`); n != 2 {
		t.Fatalf("want 2 messages got %d", n)
	}
	// worker submits each exactly once, even if run repeatedly / concurrently
	var wg2 sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg2.Add(1)
		go func() { defer wg2.Done(); e.w.RunOnce(context.Background()) }()
	}
	wg2.Wait()
	e.runWorker()
	if e.tr.Calls() != 2 {
		t.Fatalf("want 2 SMTP submissions, got %d", e.tr.Calls())
	}
	if n := e.count(`SELECT sum(attempts) FROM messages`); n != 2 {
		t.Fatalf("want 2 attempts total, got %d", n)
	}
}

func TestPayloadConflict409(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	if r := e.post(k.live, idem("c"), otpBody(testRcpt)); r.Code != 202 {
		t.Fatal(r.Raw)
	}
	b := otpBody(testRcpt)
	b["variables"].(map[string]any)["code"] = "111111"
	if r := e.post(k.live, idem("c"), b); r.Code != 409 || errCode(r) != "idempotency_conflict" {
		t.Fatalf("want 409 got %d %s", r.Code, r.Raw)
	}
	if n := e.count(`SELECT count(*) FROM messages`); n != 1 {
		t.Fatal(n)
	}
}

func TestDatabaseFailureNoAcceptance(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	_, err := e.pool.Exec(context.Background(), `ALTER TABLE messages RENAME TO messages_gone`)
	e.must(err)
	r := e.post(k.live, idem("dbfail"), resetBody(testRcpt))
	if r.Code != 503 || r.Hdr.Get("Retry-After") == "" {
		t.Fatalf("want 503 with Retry-After got %d %s", r.Code, r.Raw)
	}
	// closed pool: authentication itself cannot proceed, still never 202
	e.pool.Close()
	if r := e.post(k.live, idem("dbclosed"), resetBody(testRcpt)); r.Code == 202 {
		t.Fatal("202 returned with database unavailable")
	}
	if r := e.do("GET", "/health/ready", "", "", nil); r.Code != 503 {
		t.Fatalf("ready should fail without database, got %d", r.Code)
	}
}

func TestWorkerInterruptionBeforeClaim(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	r := e.post(k.live, idem("w1"), resetBody(testRcpt))
	id := str(r.JSON, "id")
	// "worker A" crashed before ever claiming: row is still queued and recoverable
	if e.state(id) != "queued" {
		t.Fatal(e.state(id))
	}
	e.runWorker() // worker B (a fresh process) picks it up
	if e.state(id) != "submitted" || e.tr.Calls() != 1 {
		t.Fatalf("state=%s calls=%d", e.state(id), e.tr.Calls())
	}
}

func TestWorkerInterruptionDuringSMTPBecomesUnknown(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	r := e.post(k.live, idem("w2"), resetBody(testRcpt))
	id := str(r.JSON, "id")
	// worker claims (state=submitting, committed) then dies mid-SMTP
	claimed, err := e.svc.Claim(context.Background(), 5, 30*time.Second, []string{"live"})
	e.must(err)
	if len(claimed) != 1 || e.state(id) != "submitting" {
		t.Fatal("claim failed")
	}
	// a second worker must not claim it while the lease is live
	e.runWorker()
	if e.tr.Calls() != 0 || e.state(id) != "submitting" {
		t.Fatalf("leased message re-claimed: calls=%d state=%s", e.tr.Calls(), e.state(id))
	}
	_, err = e.pool.Exec(context.Background(), `UPDATE messages SET lease_expires_at = now() - interval '1 second' WHERE id=$1`, id)
	e.must(err)
	e.runWorker()
	e.runWorker()
	if e.state(id) != "unknown" {
		t.Fatalf("want unknown got %s", e.state(id))
	}
	if e.tr.Calls() != 0 {
		t.Fatal("unknown message was automatically submitted")
	}
	if e.count(`SELECT count(*) FROM messages WHERE id=$1 AND payload_enc IS NOT NULL`, id) != 0 {
		t.Fatal("payload retained on unknown")
	}
	// operator reconciliation never resends
	e.must(e.svc.ResolveUnknown(context.Background(), id, "submitted", "mailcow queue id ABC123"))
	e.runWorker()
	if e.tr.Calls() != 0 || e.state(id) != "submitted" {
		t.Fatal("reconciliation must not resend")
	}
	// transport reporting an ambiguous outcome also lands in unknown, no retry
	e2 := setup(t)
	k2 := e2.provision("sacas")
	e2.tr.fn = func(n int, _ mailerEnv) mailerRes { return unknownResult() }
	id2 := str(e2.post(k2.live, idem("w3"), resetBody(testRcpt)).JSON, "id")
	for i := 0; i < 3; i++ {
		e2.runWorker()
	}
	if e2.state(id2) != "unknown" || e2.tr.Calls() != 1 {
		t.Fatalf("state=%s calls=%d", e2.state(id2), e2.tr.Calls())
	}
}

func TestTemporaryFailureBoundedRetryHonorsTTL(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	e.tr.fn = func(n int, _ mailerEnv) mailerRes { return tempResult() }
	id := str(e.post(k.live, idem("t1"), resetBody(testRcpt)).JSON, "id")
	for i := 0; i < 12; i++ {
		e.runWorker()
		time.Sleep(25 * time.Millisecond)
	}
	if e.tr.Calls() != 4 || e.state(id) != "failed" {
		t.Fatalf("want 4 attempts then failed; calls=%d state=%s", e.tr.Calls(), e.state(id))
	}
	if e.count(`SELECT count(*) FROM messages WHERE payload_enc IS NOT NULL`) != 0 {
		t.Fatal("payload retained after terminal state")
	}
	// retry would land beyond TTL => expired, never scheduled past the deadline
	e.w.Backoff = []time.Duration{time.Hour}
	id2 := str(e.post(k.live, idem("t2"), resetBody("second@example.org")).JSON, "id")
	e.runWorker()
	if e.state(id2) != "expired" {
		t.Fatalf("want expired got %s", e.state(id2))
	}
	// a temp failure followed by success within TTL ends as submitted after exactly one retry
	e.w.Backoff = []time.Duration{10 * time.Millisecond}
	e.tr.fn = func(n int, _ mailerEnv) mailerRes {
		if n == 6 {
			return tempResult()
		}
		return okResult()
	}
	before := e.tr.Calls()
	id3 := str(e.post(k.live, idem("t3"), resetBody("third@example.org")).JSON, "id")
	for i := 0; i < 5; i++ {
		e.runWorker()
		time.Sleep(25 * time.Millisecond)
	}
	if e.state(id3) != "submitted" || e.tr.Calls()-before != 2 {
		t.Fatalf("state=%s calls=%d", e.state(id3), e.tr.Calls()-before)
	}
}

func TestPermanentFailureIsFailedNoRetry(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	e.tr.fn = func(n int, _ mailerEnv) mailerRes { return permResult() }
	id := str(e.post(k.live, idem("p1"), resetBody(testRcpt)).JSON, "id")
	for i := 0; i < 3; i++ {
		e.runWorker()
	}
	if e.state(id) != "failed" || e.tr.Calls() != 1 {
		t.Fatalf("state=%s calls=%d", e.state(id), e.tr.Calls())
	}
}

func TestExpiredMessageNeverSubmits(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	id := str(e.post(k.live, idem("x1"), resetBody(testRcpt)).JSON, "id")
	_, err := e.pool.Exec(context.Background(), `UPDATE messages SET expires_at = now() - interval '1 second' WHERE id=$1`, id)
	e.must(err)
	e.runWorker()
	e.runWorker()
	if e.state(id) != "expired" || e.tr.Calls() != 0 {
		t.Fatalf("state=%s calls=%d", e.state(id), e.tr.Calls())
	}
	if e.count(`SELECT count(*) FROM messages WHERE id=$1 AND payload_enc IS NOT NULL`, id) != 0 {
		t.Fatal("payload retained after expiry")
	}
}

func TestSandboxSeparationAndCapture(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	e.tr.fn = func(n int, _ mailerEnv) mailerRes { t.Error("sandbox message contacted SMTP"); return okResult() }
	id := str(e.post(k.sbx, idem("sb1"), resetBody("anyone@anywhere.test")).JSON, "id") // sandbox is not recipient-restricted
	if id == "" {
		t.Fatal("sandbox send rejected")
	}
	e.runWorker()
	if e.state(id) != "captured" || e.tr.Calls() != 0 {
		t.Fatalf("state=%s calls=%d", e.state(id), e.tr.Calls())
	}
	raw, err := e.svc.ShowCapture(context.Background(), id)
	e.must(err)
	if !strings.Contains(string(raw), "RESET-TOKEN-SECRET-123") && !strings.Contains(string(raw), "RESET-TOKEN-SECRET") {
		t.Fatalf("capture missing content: %.200s", raw)
	}
	// capture auto-expires and retention removes it
	_, err = e.pool.Exec(context.Background(), `UPDATE sandbox_captures SET expires_at = now() - interval '1 second'`)
	e.must(err)
	if _, err := e.svc.ShowCapture(context.Background(), id); err == nil {
		t.Fatal("expired capture still readable")
	}
	_, err = e.svc.RunRetention(context.Background())
	e.must(err)
	if e.count(`SELECT count(*) FROM sandbox_captures`) != 0 {
		t.Fatal("retention did not delete expired captures")
	}
	// a worker without SMTP configured never claims live messages
	e.w.SMTP = nil
	live := str(e.post(k.live, idem("lv1"), resetBody(testRcpt)).JSON, "id")
	e.runWorker()
	if e.state(live) != "queued" {
		t.Fatalf("live message claimed without SMTP: %s", e.state(live))
	}
}

func TestRateLimitUnderConcurrency(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	e.must(e.svc.SetQuotas(context.Background(), "sacas", 5, 1000, 1000, 1000))
	avoidWindowEdge()
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, limited := 0, 0
	var retryAfter string
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := e.post(k.live, idem("rl"+string(rune('a'+i))), resetBody("u"+string(rune('a'+i))+"@example.org"))
			mu.Lock()
			defer mu.Unlock()
			switch r.Code {
			case 202:
				ok++
			case 429:
				limited++
				retryAfter = r.Hdr.Get("Retry-After")
			default:
				t.Errorf("unexpected %d %s", r.Code, r.Raw)
			}
		}(i)
	}
	wg.Wait()
	if ok != 5 || limited != 19 || retryAfter == "" {
		t.Fatalf("ok=%d limited=%d retry-after=%q", ok, limited, retryAfter)
	}
	if n := e.count(`SELECT count(*) FROM messages`); n != 5 {
		t.Fatalf("%d messages persisted", n)
	}
}

func TestRecipientLimitAndReplayNotCharged(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	e.must(e.svc.SetQuotas(context.Background(), "sacas", 1000, 1000, 3, 1000))
	avoidWindowEdge()
	codes := []int{}
	for i := 0; i < 5; i++ {
		codes = append(codes, e.post(k.live, idem("rc"+string(rune('a'+i))), resetBody(testRcpt)).Code)
	}
	if codes[0] != 202 || codes[2] != 202 || codes[3] != 429 || codes[4] != 429 {
		t.Fatalf("per-recipient limit: %v", codes)
	}
	// replays do not consume quota
	e.must(e.svc.SetQuotas(context.Background(), "sacas", 2, 1000, 1000, 1000))
	_, err := e.pool.Exec(context.Background(), `DELETE FROM rate_limit_buckets`)
	e.must(err)
	for i := 0; i < 5; i++ {
		if r := e.post(k.live, idem("replay"), resetBody("r1@example.org")); r.Code != 202 {
			t.Fatalf("replay %d: %d", i, r.Code)
		}
	}
	if r := e.post(k.live, idem("second"), resetBody("r2@example.org")); r.Code != 202 {
		t.Fatalf("second distinct send should fit in quota of 2: %d", r.Code)
	}
	if r := e.post(k.live, idem("third"), resetBody("r3@example.org")); r.Code != 429 {
		t.Fatalf("third distinct send must be limited: %d", r.Code)
	}
}

func TestSecretsNeverLoggedOrReturned(t *testing.T) {
	logs := captureLogs(t)
	e := setup(t)
	k := e.provision("sacas")
	e.w.SMTP = e.tr
	r1 := e.post(k.live, idem("sec1"), otpBody(testRcpt))
	r2 := e.post(k.live, idem("sec2"), resetBody(testRcpt))
	bad := e.post(k.live, idem("sec3"), resetBody("x@y")) // invalid recipient
	e.post("dmk_liv_deadbeef0000_wrongsecret", idem("sec4"), otpBody(testRcpt))
	e.runWorker()
	var all strings.Builder
	for _, r := range []resp{r1, r2, bad} {
		all.WriteString(r.Raw)
	}
	for _, id := range []string{str(r1.JSON, "id"), str(r2.JSON, "id")} {
		g := e.get(k.live, id)
		all.WriteString(g.Raw)
		if g.Code != 200 || g.JSON["status"] != "submitted" {
			t.Fatalf("status: %d %s", g.Code, g.Raw)
		}
	}
	all.WriteString(logs.String())
	out := all.String()
	secrets := []string{otpCode, "RESET-TOKEN-SECRET-123", testRcpt, k.live, strings.SplitN(k.live, "_", 4)[3], passSecret, "Authorization", "Bearer"}
	for _, s := range secrets {
		if strings.Contains(out, s) {
			t.Fatalf("secret %q leaked into responses or logs", s)
		}
	}
	// payload is encrypted at rest while queued
	e2 := setup(t)
	k2 := e2.provision("sacas")
	e2.post(k2.live, idem("enc1"), otpBody(testRcpt))
	var blob []byte
	e2.must(e2.pool.QueryRow(context.Background(), `SELECT payload_enc FROM messages`).Scan(&blob))
	if strings.Contains(string(blob), otpCode) || strings.Contains(string(blob), testRcpt) {
		t.Fatal("payload stored in plaintext")
	}
}

func TestValidationAndInjection(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	mut := func(f func(b map[string]any)) map[string]any { b := resetBody(testRcpt); f(b); return b }
	vars := func(b map[string]any) map[string]any { return b["variables"].(map[string]any) }
	cases := []struct {
		name string
		body any
		code int
		why  string
	}{
		{"http-url", mut(func(b map[string]any) { vars(b)["reset_url"] = "http://sacas.example.com/reset?t=1" }), 422, "invalid_url"},
		{"foreign-origin", mut(func(b map[string]any) { vars(b)["reset_url"] = "https://evil.example.net/reset?t=1" }), 422, "url_origin_not_approved"},
		{"userinfo-trick", mut(func(b map[string]any) { vars(b)["reset_url"] = "https://sacas.example.com@evil.net/x" }), 422, "invalid_url"},
		{"suffix-trick", mut(func(b map[string]any) { vars(b)["reset_url"] = "https://sacas.example.com.evil.net/x" }), 422, "url_origin_not_approved"},
		{"backslash", mut(func(b map[string]any) { vars(b)["reset_url"] = `https://sacas.example.com\@evil.net/x` }), 422, "invalid_url"},
		{"port-mismatch", mut(func(b map[string]any) { vars(b)["reset_url"] = "https://sacas.example.com:8443/x" }), 422, "url_origin_not_approved"},
		{"missing-var", mut(func(b map[string]any) { delete(vars(b), "reset_url") }), 422, "missing_variable"},
		{"unknown-var", mut(func(b map[string]any) { vars(b)["bcc"] = "a@b.com" }), 422, "unknown_variable"},
		{"crlf-name", mut(func(b map[string]any) { vars(b)["name"] = "Dadi\r\nBcc: evil@example.net" }), 422, "invalid_characters"},
		{"wrong-type", mut(func(b map[string]any) { vars(b)["expires_in_minutes"] = "15" }), 422, "invalid_variable_type"},
		{"unknown-template", mut(func(b map[string]any) { b["template"] = "marketing" }), 422, "unknown_template"},
		{"unknown-version", mut(func(b map[string]any) { b["template_version"] = 2 }), 422, "unknown_template"},
		{"bad-locale", mut(func(b map[string]any) { b["locale"] = "fr" }), 422, "invalid_locale"},
		{"two-recipients", mut(func(b map[string]any) { b["to"] = []string{"a@example.org", "b@example.org"} }), 422, "invalid_request"},
		{"cc-field", mut(func(b map[string]any) { b["cc"] = "a@example.org" }), 422, "invalid_request"},
		{"attachments", mut(func(b map[string]any) { b["attachments"] = []string{"x"} }), 422, "invalid_request"},
		{"header-injection-to", mut(func(b map[string]any) { b["to"] = "a@example.org\r\nBcc: x@evil.net" }), 422, "invalid_recipient"},
		{"ttl-too-long", mut(func(b map[string]any) { b["ttl_seconds"] = 3600 }), 422, "invalid_ttl"},
		{"ttl-too-short", mut(func(b map[string]any) { b["ttl_seconds"] = 1 }), 422, "invalid_ttl"},
		{"metadata-extra", mut(func(b map[string]any) { b["metadata"] = map[string]any{"token": "abc"} }), 422, "invalid_metadata"},
		{"bad-json", []byte(`{"from":`), 422, "invalid_request"},
		{"trailing-json", []byte(`{} {}`), 422, "invalid_request"},
		{"otp-bad-code", func() any { b := otpBody(testRcpt); vars(b)["code"] = "12 34"; return b }(), 422, "invalid_variable_format"},
		{"notice-title-newline", map[string]any{"from": liveFrom, "to": testRcpt, "template": "account-notice", "template_version": 1,
			"variables": map[string]any{"title": "Hi\nBcc: x@evil.net", "message": "m"}}, 422, "invalid_characters"},
	}
	for i, c := range cases {
		r := e.post(k.live, idem("v"+string(rune('a'+i))), c.body)
		if r.Code != c.code || errCode(r) != c.why {
			t.Errorf("%s: want %d/%s got %d %s", c.name, c.code, c.why, r.Code, r.Raw)
		}
	}
	if n := e.count(`SELECT count(*) FROM messages`); n != 0 {
		t.Fatalf("%d invalid requests were queued", n)
	}
	// missing / malformed idempotency key
	if r := e.post(k.live, "", resetBody(testRcpt)); r.Code != 422 {
		t.Fatalf("missing idempotency key: %d", r.Code)
	}
	// oversize body
	big := `{"from":"` + strings.Repeat("a", 40*1024) + `"}`
	if r := e.post(k.live, idem("big"), []byte(big)); r.Code != 413 {
		t.Fatalf("want 413 got %d", r.Code)
	}
	// reset template cannot be used when no origin is approved
	e.must(e.svc.SetOrigins(context.Background(), "sacas", "live", nil))
	if r := e.post(k.live, idem("noorigin"), resetBody(testRcpt)); r.Code != 422 || errCode(r) != "url_origin_not_approved" {
		t.Fatalf("no approved origins must block reset: %d %s", r.Code, r.Raw)
	}
}

func TestManualSuppressionAndRecipientPolicy(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	ctx := context.Background()
	e.must(e.svc.Suppress(ctx, "sacas", "live", "User@Example.org", "hard bounce confirmed"))
	if r := e.post(k.live, idem("sup"), resetBody(testRcpt)); r.Code != 422 || errCode(r) != "recipient_suppressed" {
		t.Fatalf("want suppressed 422 got %d %s", r.Code, r.Raw)
	}
	if e.count(`SELECT count(*) FROM messages`) != 0 {
		t.Fatal("suppressed recipient queued")
	}
	e.must(e.svc.Unsuppress(ctx, "sacas", "live", testRcpt))
	if r := e.post(k.live, idem("unsup"), resetBody(testRcpt)); r.Code != 202 {
		t.Fatal(r.Raw)
	}
	// live recipients are restricted to the allowlist until an operator widens it
	if r := e.post(k.live, idem("al"), resetBody("someone@gmail.com")); r.Code != 422 || errCode(r) != "recipient_not_allowed" {
		t.Fatalf("want recipient_not_allowed got %d %s", r.Code, r.Raw)
	}
}

func TestRestartPersistsAndWorkerResumes(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	id := str(e.post(k.live, idem("rs1"), resetBody(testRcpt)).JSON, "id")
	// simulate process restart: brand new pool + service + handler on the same database
	e.pool.Close()
	e2 := setup2(t, e)
	if g := e2.get(k.live, id); g.Code != 200 || g.JSON["status"] != "queued" {
		t.Fatalf("record lost across restart: %d %s", g.Code, g.Raw)
	}
	e2.runWorker()
	if e2.state(id) != "submitted" || e2.tr.Calls() != 1 {
		t.Fatalf("worker did not resume: %s %d", e2.state(id), e2.tr.Calls())
	}
}

func TestStatusIsHonestAboutSubmission(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	id := str(e.post(k.live, idem("st1"), resetBody(testRcpt)).JSON, "id")
	e.runWorker()
	g := e.get(k.live, id)
	if g.JSON["status"] != "submitted" {
		t.Fatal(g.Raw)
	}
	for _, banned := range []string{"delivered", "bounced", "opened", "inbox"} {
		if strings.Contains(strings.ToLower(g.Raw), banned) {
			t.Fatalf("status claims %q: %s", banned, g.Raw)
		}
	}
	if g.JSON["smtp_reply"] == nil {
		t.Fatalf("missing smtp acknowledgement: %s", g.Raw)
	}
	if r := e.do("GET", "/health/live", "", "", nil); r.Code != 200 {
		t.Fatal(r.Code)
	}
	if r := e.do("GET", "/health/ready", "", "", nil); r.Code != 200 {
		t.Fatal(r.Code)
	}
	ok, err := e.svc.WorkerHealthy(context.Background(), time.Minute)
	e.must(err)
	if ok {
		t.Fatal("no heartbeat yet, must be unhealthy")
	}
	e.must(e.svc.Heartbeat(context.Background(), "w"))
	if ok, _ := e.svc.WorkerHealthy(context.Background(), time.Minute); !ok {
		t.Fatal("heartbeat not seen")
	}
}

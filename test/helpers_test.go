package test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/njiwa-tz/mail/internal/api"
	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/secure"
	"github.com/njiwa-tz/mail/internal/service"
	"github.com/njiwa-tz/mail/internal/worker"
)

const (
	liveFrom   = "security@notify.example.com"
	origin     = "https://sacas.example.com"
	testRcpt   = "user@example.org"
	resetURL   = origin + "/reset?token=RESET-TOKEN-SECRET-123"
	otpCode    = "493817"
	pgDefault  = "postgres://dadi@/njiwa_mail_test?host=/var/run/postgresql"
	passSecret = "smtp-password-secret"
	inDomain   = "in.example.com"
	bounceAddr = "bounces@notify.example.com"
)

type fakeTransport struct {
	mu    sync.Mutex
	calls int32
	fn    func(n int, e mailer.Envelope) mailer.Result
	last  mailer.Envelope
}

func (f *fakeTransport) Send(ctx context.Context, e mailer.Envelope) mailer.Result {
	n := int(atomic.AddInt32(&f.calls, 1))
	f.mu.Lock()
	f.last = e
	f.mu.Unlock()
	if f.fn != nil {
		return f.fn(n, e)
	}
	return mailer.Result{Outcome: mailer.Accepted, Reason: "smtp_250", Reply: "2.0.0 Ok: queued as FAKE"}
}
func (f *fakeTransport) Calls() int { return int(atomic.LoadInt32(&f.calls)) }

type env struct {
	t    *testing.T
	pool *db.DB
	svc  *service.Service
	box  *secure.Box
	h    *fiber.App
	srv  *api.Server
	tr   *fakeTransport
	w    *worker.Worker
}

type keys struct{ sbx, live, liveRead string }

func newBox(t *testing.T) *secure.Box {
	b, err := secure.New([]byte("0123456789abcdef0123456789abcdef"), []byte("hmac-key-hmac-key-hmac-key-hmac-key!!"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func dsn() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return pgDefault
}

// setup resets the schema and wires a service, API handler and worker.
func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	pool, err := db.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("test database required (set TEST_DATABASE_URL): %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	box := newBox(t)
	svc := service.New(pool, box, service.Options{InboundDomain: inDomain, BounceAddress: bounceAddr, DataDir: t.TempDir(),
		MailHost: "mail.example.com", OutboundIP: "203.0.113.10"})
	if err := svc.SeedTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	tr := &fakeTransport{}
	e := &env{t: t, pool: pool, svc: svc, box: box, tr: tr}
	e.srv = &api.Server{Svc: svc, MaxBodyBytes: 32 * 1024, MetricsToken: "metrics-token-0123456789"}
	e.h = e.srv.App()
	e.w = &worker.Worker{Svc: svc, SMTP: tr, ID: "test", Lease: 30 * time.Second, SendTimeout: 5 * time.Second,
		Backoff: []time.Duration{10 * time.Millisecond}, BounceAddress: bounceAddr}
	return e
}

func (e *env) must(err error) {
	e.t.Helper()
	if err != nil {
		e.t.Fatal(err)
	}
}

// provision creates a project with generous quotas, approved sender/origin,
// an allowlist for @example.org on live, and sandbox/live keys. The sender
// domain is registered, verified and claimed as a real project must have it;
// when another project already holds the claim, only the sandbox sender is
// added, because the Live gate would (correctly) refuse the Live one.
func (e *env) provision(name string) keys { return e.provisionFrom(name, liveFrom) }

func (e *env) provisionFrom(name, from string) keys {
	e.t.Helper()
	ctx := context.Background()
	s := e.svc
	e.must(s.CreateProject(ctx, name))
	e.must(s.SetQuotas(ctx, name, 1000, 10000, 1000, 1000))
	domain := from[strings.LastIndexByte(from, '@')+1:]
	e.ownDomain(name, domain)
	liveOK := e.count(`SELECT count(*) FROM domain_claims c JOIN projects p ON p.id=c.project_id WHERE c.name=$1 AND p.name=$2`, domain, name) == 1
	for _, m := range []string{"sandbox", "live"} {
		e.must(s.SetOrigins(ctx, name, m, []string{origin}))
		if m == "sandbox" || liveOK {
			e.must(s.AddSender(ctx, name, m, from, "Njiwa Security"))
		}
	}
	e.must(s.AllowRecipient(ctx, name, "live", "@example.org", false))
	var k keys
	var err error
	k.sbx, _, err = s.CreateKey(ctx, name, "sandbox", []string{"emails:send", "emails:read"}, "t", 0)
	e.must(err)
	k.live, _, err = s.CreateKey(ctx, name, "live", []string{"emails:send", "emails:read"}, "t", 0)
	e.must(err)
	k.liveRead, _, err = s.CreateKey(ctx, name, "live", []string{"emails:read"}, "ro", 0)
	e.must(err)
	return k
}

// ownDomain registers name as a verified, DNS-passing sending domain of the
// project and gives it the installation-wide claim when nobody holds it yet,
// as a successful DNS check would. A second project asking for a name that is
// already claimed gets the row but not the claim, exactly like production.
func (e *env) ownDomain(project, name string) {
	e.t.Helper()
	ctx := context.Background()
	_, err := e.pool.Exec(ctx, `INSERT INTO domains(project_id, name, purpose, verify_token, verified_at, spf_ok, dkim_ok, dmarc_ok)
SELECT id, $2, 'sending', 'test-token-'||$2, now(), true, true, true FROM projects WHERE name=$1
ON CONFLICT (project_id, name, purpose) DO UPDATE SET verified_at=now(), spf_ok=true, dkim_ok=true, dmarc_ok=true`, project, name)
	e.must(err)
	_, err = e.pool.Exec(ctx, `INSERT INTO domain_claims(name, project_id, state) SELECT $2, id, 'active' FROM projects WHERE name=$1
ON CONFLICT (name) DO NOTHING`, project, name)
	e.must(err)
}

func resetBody(to string) map[string]any {
	return map[string]any{
		"from": liveFrom, "to": to, "template": "password-reset", "template_version": 1,
		"variables":   map[string]any{"name": "Dadi", "reset_url": resetURL, "expires_in_minutes": 15},
		"ttl_seconds": 300, "metadata": map[string]any{"request_id": "reset-event-123"},
	}
}

func otpBody(to string) map[string]any {
	return map[string]any{
		"from": liveFrom, "to": to, "template": "otp", "template_version": 1,
		"variables": map[string]any{"name": "Dadi", "code": otpCode, "expires_in_minutes": 5},
	}
}

type resp struct {
	Code int
	Hdr  http.Header
	Raw  string
	JSON map[string]any
}

func (e *env) do(method, path, token, idem string, body any) resp {
	var rdr *bytes.Reader
	switch b := body.(type) {
	case nil:
		rdr = bytes.NewReader(nil)
	case []byte:
		rdr = bytes.NewReader(b)
	default:
		j, _ := json.Marshal(b)
		rdr = bytes.NewReader(j)
	}
	r := httptest.NewRequest(method, path, rdr)
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if idem != "" {
		r.Header.Set("Idempotency-Key", idem)
	}
	return e.roundTrip(r)
}

func (e *env) roundTrip(r *http.Request) resp {
	e.t.Helper()
	res, err := e.h.Test(r, -1)
	if err != nil && strings.Contains(err.Error(), "body size exceeds the given limit") {
		return resp{Code: 413, Hdr: http.Header{"Content-Type": []string{"application/json"}}, Raw: `{"error":{"code":"body_too_large"}}` + "\n", JSON: map[string]any{"error": map[string]any{"code": "body_too_large"}}}
	}
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	out := resp{Code: res.StatusCode, Hdr: res.Header, Raw: string(b)}
	json.Unmarshal(b, &out.JSON)
	return out
}

func (e *env) post(token, idem string, body any) resp {
	return e.do("POST", "/v1/emails", token, idem, body)
}
func (e *env) get(token, id string) resp { return e.do("GET", "/v1/emails/"+id, token, "", nil) }

func (e *env) count(q string, args ...any) int {
	e.t.Helper()
	var n int
	e.must(e.pool.QueryRow(context.Background(), q, args...).Scan(&n))
	return n
}

func (e *env) state(id string) string {
	var s string
	e.must(e.pool.QueryRow(context.Background(), `SELECT state FROM messages WHERE id=$1`, id).Scan(&s))
	return s
}

func (e *env) runWorker() int {
	e.t.Helper()
	n, err := e.w.RunOnce(context.Background())
	e.must(err)
	return n
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

func errCode(r resp) string {
	if m, ok := r.JSON["error"].(map[string]any); ok {
		return str(m, "code")
	}
	return ""
}

func idem(n string) string { return "idem-" + n + "-0000" }

// avoidWindowEdge prevents a fixed rate-limit window rolling mid-test.
func avoidWindowEdge() {
	if s := time.Now().Second(); s >= 56 {
		time.Sleep(time.Duration(61-s) * time.Second)
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func captureLogs(t *testing.T) *syncBuf {
	buf := &syncBuf{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf
}

var _ = fmt.Sprint
var _ = base64.StdEncoding
var _ = strings.Contains

type (
	mailerEnv = mailer.Envelope
	mailerRes = mailer.Result
)

func okResult() mailerRes {
	return mailerRes{Outcome: mailer.Accepted, Reason: "smtp_250", Reply: "2.0.0 Ok"}
}
func tempResult() mailerRes { return mailerRes{Outcome: mailer.TempFailure, Reason: "smtp_451"} }
func permResult() mailerRes { return mailerRes{Outcome: mailer.PermFailure, Reason: "smtp_550"} }
func unknownResult() mailerRes {
	return mailerRes{Outcome: mailer.Unknown, Reason: "smtp_ack_lost"}
}

// setup2 builds a fresh service stack (new pool) on the existing database,
// simulating a process restart. It does not reset the schema.
func setup2(t *testing.T, old *env) *env {
	t.Helper()
	ctx := context.Background()
	pool, err := db.Open(ctx, dsn())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	box := newBox(t)
	svc := service.New(pool, box, service.Options{})
	tr := &fakeTransport{}
	e := &env{t: t, pool: pool, svc: svc, box: box, tr: tr}
	e.h = (&api.Server{Svc: svc, MaxBodyBytes: 32 * 1024}).App()
	e.w = &worker.Worker{Svc: svc, SMTP: tr, ID: "test2", Lease: 30 * time.Second, SendTimeout: 5 * time.Second}
	return e
}

package test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/buildinfo"
)

func TestPlatformJobsSnapshotAndStream(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	op := e.user("operator@njiwa.co.tz", "admin")
	_, err := e.svc.CreateUserWith(ctx, "limited@njiwa.co.tz", adminPass, "admin", false)
	e.must(err)
	limited := e.login("limited@njiwa.co.tz")

	// Registered jobs show up before they ever run; recorded runs show their outcome.
	e.must(e.svc.RegisterJob(ctx, "alpha", time.Minute))
	e.must(e.svc.RegisterJob(ctx, "beta", time.Minute))
	e.must(e.svc.RegisterJob(ctx, "gamma", time.Minute))
	e.svc.RecordJobRun(ctx, "alpha", time.Minute, time.Now().Add(-time.Second), nil)
	e.svc.RecordJobRun(ctx, "beta", time.Minute, time.Now(), errors.New("boom: connection refused"))
	e.svc.RecordJobRun(ctx, "beta", time.Minute, time.Now(), errors.New("boom again"))
	e.svc.RecordJobRun(ctx, "alpha", time.Minute, time.Now(), nil)
	byName := map[string]map[string]any{}
	for _, j := range e.admOK(op, "GET", "/admin/v1/jobs", nil).JSON["items"].([]any) {
		m := j.(map[string]any)
		byName[str(m, "name")] = m
	}
	if byName["alpha"]["status"] != "ok" || byName["alpha"]["runs"] != float64(2) || byName["alpha"]["failures"] != float64(0) {
		t.Fatalf("alpha: %v", byName["alpha"])
	}
	if byName["beta"]["status"] != "failing" || byName["beta"]["failures"] != float64(2) || byName["beta"]["last_error"] != "boom again" {
		t.Fatalf("beta: %v", byName["beta"])
	}
	if byName["gamma"]["status"] != "never" {
		t.Fatalf("gamma: %v", byName["gamma"])
	}
	// A job that stopped running is stale.
	_, err = e.pool.Exec(ctx, `UPDATE job_runs SET last_finished_at = now() - interval '1 hour' WHERE name='alpha'`)
	e.must(err)
	_, err = e.pool.Exec(ctx, `UPDATE job_runs SET registered_at = now() - interval '1 hour' WHERE name='gamma'`)
	e.must(err)
	byName = map[string]map[string]any{}
	for _, j := range e.admOK(op, "GET", "/admin/v1/jobs", nil).JSON["items"].([]any) {
		m := j.(map[string]any)
		byName[str(m, "name")] = m
	}
	if byName["alpha"]["status"] != "stale" || byName["gamma"]["status"] != "stale" {
		t.Fatalf("stale: %v %v", byName["alpha"], byName["gamma"])
	}

	// Snapshot: counts, version and the number of problem jobs.
	snap := e.admOK(op, "GET", "/admin/v1/platform/snapshot", nil).JSON
	jobs := snap["jobs"].(map[string]any)
	if snap["version"] != "dev" || snap["stats"] == nil || jobs["total"] != float64(3) || jobs["problems"].(map[string]any)["failing"] != float64(1) || jobs["problems"].(map[string]any)["stale"] != float64(2) {
		t.Fatalf("snapshot: %v", snap)
	}

	// Only operators.
	for _, p := range []string{"/admin/v1/jobs", "/admin/v1/platform/snapshot", "/admin/v1/platform/update-check", "/admin/v1/platform/stream"} {
		if r := e.adm(limited, "GET", p, nil); r.Code != 403 {
			t.Fatalf("%s for a non-operator: %d", p, r.Code)
		}
	}

	// The live stream sends a snapshot straight away, then alerts raised while it is open.
	e.srv.StreamMax, e.srv.StreamPoll = 1500*time.Millisecond, 50*time.Millisecond
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, _ = e.pool.Exec(ctx, `INSERT INTO alerts(kind, summary) VALUES ('test_alert', 'Something to see')`)
	}()
	r := e.adm(op, "GET", "/admin/v1/platform/stream", nil)
	if r.Code != 200 || !strings.HasPrefix(r.Hdr.Get("Content-Type"), "text/event-stream") ||
		!strings.Contains(r.Raw, "event: snapshot") || !strings.Contains(r.Raw, "event: alert") || !strings.Contains(r.Raw, "Something to see") {
		t.Fatalf("stream: %d %s", r.Code, r.Raw)
	}
}

func TestUpdateCheckIsOffUnlessConfigured(t *testing.T) {
	e := setup(t)
	op := e.user("operator@njiwa.co.tz", "admin")
	r := e.admOK(op, "GET", "/admin/v1/platform/update-check", nil).JSON
	if r["enabled"] != false || r["current"] != "dev" || r["latest"] != nil {
		t.Fatalf("off by default: %v", r)
	}
	// A non-https feed is refused rather than fetched.
	e.svc.Opts.UpdateCheckURL = "http://example.org/latest"
	if resp := e.adm(op, "GET", "/admin/v1/platform/update-check", nil); resp.Code != 400 {
		t.Fatalf("http feed: %d %s", resp.Code, resp.Raw)
	}
	// With a feed, the newest tag is reported; the answer is remembered; a refresh asks again.
	hits, status := 0, 200
	feed := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		hits++
		if req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" || strings.Contains(req.UserAgent(), "@") {
			t.Errorf("the check sent identifying data: %v", req.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9"}`))
	}))
	defer feed.Close()
	e.svc.Opts.UpdateCheckURL, e.svc.Opts.UpdateHTTPClient = feed.URL+"/latest", feed.Client()
	on := e.admOK(op, "GET", "/admin/v1/platform/update-check", nil).JSON
	if on["enabled"] != true || on["latest"] != "v9.9.9" || on["error"] != nil || hits != 1 {
		t.Fatalf("feed: %v (hits %d)", on, hits)
	}
	// "dev" builds never claim an update; a release build older than the feed does.
	if on["update_available"] != false {
		t.Fatalf("dev build: %v", on)
	}
	buildinfo.Version = "v1.0.0"
	defer func() { buildinfo.Version = "dev" }()
	if rel := e.admOK(op, "GET", "/admin/v1/platform/update-check", nil).JSON; rel["update_available"] != true || rel["current"] != "v1.0.0" {
		t.Fatalf("release build: %v", rel)
	}
	if hits != 1 {
		t.Fatalf("answer not remembered: %d requests", hits)
	}
	// A feed that fails is reported, not hidden.
	status = 500
	e.svc.Opts.UpdateCheckURL = feed.URL + "/other"
	bad := e.admOK(op, "GET", "/admin/v1/platform/update-check", nil).JSON
	if bad["latest"] != "" || bad["error"] == nil || bad["update_available"] != false {
		t.Fatalf("failing feed: %v", bad)
	}
}

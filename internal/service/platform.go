package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/njiwa-tz/mail/internal/buildinfo"
)

// Platform views for the installation operator: what each scheduled job last did, a snapshot of
// the platform's health for a live view, and an optional check for a newer release.

// RegisterJob records that the scheduler runs a job every interval, so a job that never gets to
// run still shows up in the list.
func (s *Service) RegisterJob(ctx context.Context, name string, interval time.Duration) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO job_runs(name, interval_seconds) VALUES ($1,$2)
ON CONFLICT (name) DO UPDATE SET interval_seconds=EXCLUDED.interval_seconds`, name, int(interval.Seconds()))
	return err
}

// RecordJobRun stores the outcome of one run of a job.
func (s *Service) RecordJobRun(ctx context.Context, name string, interval time.Duration, started time.Time, runErr error) {
	msg := ""
	if runErr != nil {
		msg = clean(runErr.Error(), 300)
	}
	_, _ = s.Pool.Exec(ctx, `INSERT INTO job_runs(name, interval_seconds, last_started_at, last_finished_at, last_ok, last_error, last_duration_ms, runs, failures)
VALUES ($1,$2,$3,now(),$4,$5,$6,1,CASE WHEN $4 THEN 0 ELSE 1 END)
ON CONFLICT (name) DO UPDATE SET interval_seconds=EXCLUDED.interval_seconds, last_started_at=EXCLUDED.last_started_at, last_finished_at=now(),
  last_ok=EXCLUDED.last_ok, last_error=EXCLUDED.last_error, last_duration_ms=EXCLUDED.last_duration_ms,
  runs=job_runs.runs+1, failures=job_runs.failures+CASE WHEN EXCLUDED.last_ok THEN 0 ELSE 1 END`,
		name, int(interval.Seconds()), started, runErr == nil, msg, int(time.Since(started).Milliseconds()))
}

// jobStatus: never (registered, no run yet), ok, failing (the last run failed) or stale (it has
// not finished a run in three intervals, so the scheduler is probably down or stuck).
func jobStatus(interval int, finished *time.Time, ok *bool, registered time.Time, now time.Time) string {
	grace := time.Duration(3*interval)*time.Second + time.Minute
	if finished == nil {
		if now.Sub(registered) > grace {
			return "stale"
		}
		return "never"
	}
	if now.Sub(*finished) > grace {
		return "stale"
	}
	if ok != nil && !*ok {
		return "failing"
	}
	return "ok"
}

// ListJobs lists the scheduled jobs with their last run and a status.
func (s *Service) ListJobs(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.maps(ctx, `SELECT name, interval_seconds, registered_at, last_started_at, last_finished_at, last_ok, last_error, last_duration_ms, runs, failures
FROM job_runs ORDER BY name`)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for _, r := range rows {
		interval, _ := r["interval_seconds"].(int32)
		reg, _ := r["registered_at"].(time.Time)
		var fin *time.Time
		if t, ok := r["last_finished_at"].(time.Time); ok {
			fin = &t
		}
		var ok *bool
		if b, has := r["last_ok"].(bool); has {
			ok = &b
		}
		r["status"] = jobStatus(int(interval), fin, ok, reg, now)
	}
	return rows, nil
}

// PlatformSnapshot is what the live platform view shows: queue and delivery counts, the workers
// that are alive, and how many scheduled jobs are failing or stale.
func (s *Service) PlatformSnapshot(ctx context.Context) (map[string]any, error) {
	st, err := s.Stats(ctx)
	if err != nil {
		return nil, err
	}
	jobs, err := s.ListJobs(ctx)
	if err != nil {
		return nil, err
	}
	bad := map[string]int{}
	for _, j := range jobs {
		if st, _ := j["status"].(string); st == "failing" || st == "stale" {
			bad[st]++
		}
	}
	return map[string]any{"at": time.Now().UTC(), "version": buildinfo.Version, "stats": st, "jobs": map[string]any{"total": len(jobs), "problems": bad}}, nil
}

// AlertsSince returns alerts raised after id, for the live view.
func (s *Service) AlertsSince(ctx context.Context, afterID int64) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT id, kind, summary, created_at FROM alerts WHERE id > $1 ORDER BY id LIMIT 50`, afterID)
}

// LatestAlertID is where a live view starts, so it shows only what happens while it is open.
func (s *Service) LatestAlertID(ctx context.Context) int64 {
	var id int64
	_ = s.Pool.QueryRow(ctx, `SELECT COALESCE(max(id),0) FROM alerts`).Scan(&id)
	return id
}

// ---------------------------------------------------------------- update check

type updateCache struct {
	mu      sync.Mutex
	at      time.Time
	latest  string
	url     string
	lastErr string
}

var updates updateCache

// UpdateCheck asks the configured release feed for the newest version. It is off unless
// UPDATE_CHECK_URL is set, sends no data about the installation (a plain GET with a generic
// client name) and remembers the answer for six hours; refresh forces one new request at most
// once a minute.
func (s *Service) UpdateCheck(ctx context.Context, refresh bool) (map[string]any, error) {
	feed := strings.TrimSpace(s.Opts.UpdateCheckURL)
	out := map[string]any{"current": buildinfo.Version, "enabled": feed != ""}
	if feed == "" {
		return out, nil
	}
	u, err := url.Parse(feed)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, badRequest("UPDATE_CHECK_URL must be an https address")
	}
	updates.mu.Lock()
	defer updates.mu.Unlock()
	age := time.Since(updates.at)
	if updates.url != feed || age > 6*time.Hour || (refresh && age > time.Minute) {
		updates.url = feed
		updates.at = time.Now()
		updates.latest, updates.lastErr = "", ""
		tag, err := fetchLatestTag(ctx, feed, s.Opts.UpdateHTTPClient)
		if err != nil {
			updates.lastErr = clean(err.Error(), 200)
		}
		updates.latest = tag
	}
	out["checked_at"] = updates.at.UTC()
	out["latest"] = updates.latest
	if updates.lastErr != "" {
		out["error"] = updates.lastErr
	}
	out["update_available"] = updates.latest != "" && buildinfo.Version != "dev" && updates.latest != buildinfo.Version
	return out, nil
}

func fetchLatestTag(ctx context.Context, feed string, client *http.Client) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", feed, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "njiwa-mail-update-check")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("the release feed could not be reached")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", errors.New("the release feed answered " + http.StatusText(resp.StatusCode))
	}
	var v struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v); err != nil || v.Tag == "" || len(v.Tag) > 64 {
		return "", errors.New("the release feed has no tag_name")
	}
	return v.Tag, nil
}

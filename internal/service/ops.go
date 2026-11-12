package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/installation"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------- settings

// settingDefaults lists every operator-tunable setting with its default and
// allowed range. Values are stored as JSON numbers in the settings table.
var settingDefaults = map[string]struct{ def, min, max int }{
	"inbound_retention_days":      {30, 1, 365},
	"bounce_alert_min_volume":     {20, 1, 100000}, // submissions in the last hour before a rate is judged
	"bounce_alert_hard_percent":   {5, 1, 100},     // hard bounces / submissions, last hour
	"bounce_alert_policy_percent": {2, 1, 100},     // policy bounces / submissions, last hour
	"webhook_auto_disable_after":  {20, 3, 1000},   // consecutive failed attempts
	"login_attempts_per_account":  {10, 3, 1000},   // per 15 minutes
	"login_attempts_per_ip":       {30, 3, 10000},  // per 15 minutes
	"default_send_per_minute":     {30, 1, 100000}, // applied to new projects
	"default_send_per_day":        {500, 1, 10000000},
	"require_mfa":                 {0, 0, 1}, // 1 = every admin must use a second factor (enrol on next sign-in)
}

func (s *Service) settingInt(ctx context.Context, key string) int {
	d := settingDefaults[key]
	var raw []byte
	if err := s.Pool.QueryRow(ctx, `SELECT value FROM settings WHERE key=$1`, key).Scan(&raw); err != nil {
		return d.def
	}
	var n int
	if json.Unmarshal(raw, &n) != nil || n < d.min || n > d.max {
		return d.def
	}
	return n
}

// Settings returns every setting with its effective value.
func (s *Service) Settings(ctx context.Context) (map[string]int, error) {
	out := map[string]int{}
	for k := range settingDefaults {
		out[k] = s.settingInt(ctx, k)
	}
	return out, nil
}

func (s *Service) UpdateSettings(ctx context.Context, in map[string]int) error {
	for k, v := range in {
		d, ok := settingDefaults[k]
		if !ok {
			return badRequest("unknown setting " + k)
		}
		if v < d.min || v > d.max {
			return badRequest(fmt.Sprintf("%s must be between %d and %d", k, d.min, d.max))
		}
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	for k, v := range in {
		if _, err := tx.Exec(ctx, `INSERT INTO settings(key, value) VALUES ($1, to_jsonb($2::int))
ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()`, k, v); err != nil {
			return unavailable("settings", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable("commit", err)
	}
	return nil
}

// ---------------------------------------------------------------- alerts

// raiseAlert records an operator alert and logs it. The same kind is not
// raised again while an unacknowledged alert of that kind is under an hour old.
func raiseAlert(ctx context.Context, tx *db.Tx, kind, summary string, details map[string]any) error {
	dj, _ := json.Marshal(details)
	tag, err := tx.Exec(ctx, `INSERT INTO alerts(kind, summary, details) SELECT $1,$2,$3
WHERE NOT EXISTS (SELECT 1 FROM alerts WHERE kind=$1 AND acknowledged_at IS NULL AND created_at > now() - interval '1 hour')`, kind, summary, dj)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		slog.Warn("ALERT", "kind", kind, "summary", summary)
	}
	return nil
}

func (s *Service) ListAlerts(ctx context.Context, openOnly bool, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT id, kind, summary, details, created_at, acknowledged_at, acknowledged_by FROM alerts
WHERE (NOT $1 OR acknowledged_at IS NULL) ORDER BY id DESC`, openOnly)
}

func (s *Service) AckAlert(ctx context.Context, id int64, by string) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE alerts SET acknowledged_at=now(), acknowledged_by=$2 WHERE id=$1 AND acknowledged_at IS NULL`, id, by)
	if err != nil {
		return unavailable("alert_ack", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

// BounceRates is the last hour of bounce activity against submissions.
type BounceRates struct {
	Submitted int     `json:"submitted_last_hour"`
	Hard      int     `json:"hard_last_hour"`
	Policy    int     `json:"policy_last_hour"`
	Soft      int     `json:"soft_last_hour"`
	HardPct   float64 `json:"hard_percent"`
	PolicyPct float64 `json:"policy_percent"`
}

func (s *Service) BounceRates(ctx context.Context) (BounceRates, error) {
	var r BounceRates
	err := s.Pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM messages WHERE submitted_at > now() - interval '1 hour'),
 count(*) FILTER (WHERE type='hard'), count(*) FILTER (WHERE type='policy'), count(*) FILTER (WHERE type='soft')
FROM bounces WHERE received_at > now() - interval '1 hour'`).Scan(&r.Submitted, &r.Hard, &r.Policy, &r.Soft)
	if r.Submitted > 0 {
		r.HardPct = float64(r.Hard) * 100 / float64(r.Submitted)
		r.PolicyPct = float64(r.Policy) * 100 / float64(r.Submitted)
	}
	return r, err
}

// CheckBounceRates raises an alert when the hard- or policy-bounce rate over
// the last hour passes its threshold. Run by the scheduler.
func (s *Service) CheckBounceRates(ctx context.Context) error {
	r, err := s.BounceRates(ctx)
	if err != nil {
		return err
	}
	if r.Submitted < s.settingInt(ctx, "bounce_alert_min_volume") {
		return nil
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if r.HardPct >= float64(s.settingInt(ctx, "bounce_alert_hard_percent")) {
		if err := raiseAlert(ctx, tx, "hard_bounce_rate", "Hard-bounce rate over the last hour is above the threshold",
			map[string]any{"submitted": r.Submitted, "hard": r.Hard, "percent": r.HardPct}); err != nil {
			return err
		}
	}
	if r.PolicyPct >= float64(s.settingInt(ctx, "bounce_alert_policy_percent")) {
		if err := raiseAlert(ctx, tx, "policy_bounce_rate", "Policy-bounce rate over the last hour is above the threshold: check DNS and IP reputation",
			map[string]any{"submitted": r.Submitted, "policy": r.Policy, "percent": r.PolicyPct}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ---------------------------------------------------------------- overview and metrics

// Stats is the operational snapshot behind the Overview page and /metrics.
type Stats struct {
	Counts             map[string]int `json:"counts"`
	SentToday          int            `json:"sent_today"` // submitted since midnight Africa/Dar_es_Salaam
	QueueDepth         int            `json:"queue_depth"`
	OldestQueuedAgeSec float64        `json:"oldest_queued_age_seconds"`
	WebhookBacklog     int            `json:"webhook_backlog"`
	WebhookDead        int            `json:"webhook_dead"`
	InboundSpool       int            `json:"inbound_spool_pending"`
	InboundFailed      int            `json:"inbound_spool_failed"`
	Bounces            BounceRates    `json:"bounces"`
	BounceTotals       map[string]int `json:"bounce_totals"`
	OpenAlerts         int            `json:"open_alerts"`
	Workers            []WorkerBeat   `json:"workers"`
	DNSOK              *bool          `json:"dns_ok"` // nil = never checked
}

type WorkerBeat struct {
	Role     string    `json:"role"`
	Instance string    `json:"instance"`
	LastSeen time.Time `json:"last_seen"`
	Healthy  bool      `json:"healthy"`
}

func (s *Service) Stats(ctx context.Context) (*Stats, error) {
	st := &Stats{Counts: map[string]int{}, BounceTotals: map[string]int{"hard": 0, "soft": 0, "policy": 0, "unknown": 0}, Workers: []WorkerBeat{}}
	for _, c := range consoleStates {
		st.Counts[c] = 0
	}
	rows, err := s.Pool.Query(ctx, `SELECT 'm:' || state, count(*) FROM messages GROUP BY state
UNION ALL SELECT 'b:' || type, count(*) FROM bounces GROUP BY type`)
	if err != nil {
		return nil, unavailable("stats_counts", err)
	}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			rows.Close()
			return nil, unavailable("stats_scan", err)
		}
		if strings.HasPrefix(k, "m:") {
			st.Counts[k[2:]] = n
		} else {
			st.BounceTotals[k[2:]] = n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, unavailable("stats_rows", err)
	}
	st.QueueDepth = st.Counts["queued"] + st.Counts["retry_wait"]
	err = s.Pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM messages WHERE submitted_at >= date_trunc('day', now() AT TIME ZONE 'Africa/Dar_es_Salaam') AT TIME ZONE 'Africa/Dar_es_Salaam'),
 COALESCE((SELECT extract(epoch FROM now() - min(created_at)) FROM messages WHERE state IN ('queued','retry_wait')), 0)::float8,
 (SELECT count(*) FROM webhook_deliveries WHERE state IN ('pending','sending')),
 (SELECT count(*) FROM webhook_deliveries WHERE state='dead'),
 (SELECT count(*) FROM inbound_spool WHERE state IN ('pending','processing')),
 (SELECT count(*) FROM inbound_spool WHERE state='failed'),
 (SELECT count(*) FROM alerts WHERE acknowledged_at IS NULL),
 (SELECT bool_and(pass) FROM (SELECT DISTINCT ON (domain, record_type) pass FROM dns_checks ORDER BY domain, record_type, checked_at DESC) x)`).
		Scan(&st.SentToday, &st.OldestQueuedAgeSec, &st.WebhookBacklog, &st.WebhookDead, &st.InboundSpool, &st.InboundFailed, &st.OpenAlerts, &st.DNSOK)
	if err != nil {
		return nil, unavailable("stats", err)
	}
	if st.Bounces, err = s.BounceRates(ctx); err != nil {
		return nil, unavailable("stats_bounces", err)
	}
	wr, err := s.Pool.Query(ctx, `SELECT role, worker_id, last_seen FROM worker_heartbeats WHERE last_seen > now() - interval '1 hour' ORDER BY role, worker_id`)
	if err != nil {
		return nil, unavailable("stats_workers", err)
	}
	defer wr.Close()
	for wr.Next() {
		var b WorkerBeat
		if err := wr.Scan(&b.Role, &b.Instance, &b.LastSeen); err != nil {
			return nil, unavailable("stats_workers_scan", err)
		}
		b.Healthy = time.Since(b.LastSeen) <= time.Minute
		st.Workers = append(st.Workers, b)
	}
	return st, wr.Err()
}

// Metrics renders Stats in the Prometheus text exposition format.
func (s *Service) Metrics(ctx context.Context) (string, error) {
	st, err := s.Stats(ctx)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	gauge := func(name, help string) { fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name) }
	keys := func(m map[string]int) []string {
		ks := make([]string, 0, len(m))
		for k := range m {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return ks
	}
	gauge("njiwa_mail_messages", "Messages by state (within metadata retention).")
	for _, k := range keys(st.Counts) {
		fmt.Fprintf(&b, "njiwa_mail_messages{state=%q} %d\n", k, st.Counts[k])
	}
	gauge("njiwa_mail_queue_depth", "Messages waiting for a worker (queued + retry_wait).")
	fmt.Fprintf(&b, "njiwa_mail_queue_depth %d\n", st.QueueDepth)
	gauge("njiwa_mail_oldest_queued_age_seconds", "Age of the oldest waiting message.")
	fmt.Fprintf(&b, "njiwa_mail_oldest_queued_age_seconds %.0f\n", st.OldestQueuedAgeSec)
	gauge("njiwa_mail_bounces", "Bounces by type (within metadata retention).")
	for _, k := range keys(st.BounceTotals) {
		fmt.Fprintf(&b, "njiwa_mail_bounces{type=%q} %d\n", k, st.BounceTotals[k])
	}
	gauge("njiwa_mail_webhook_backlog", "Webhook deliveries pending or in flight.")
	fmt.Fprintf(&b, "njiwa_mail_webhook_backlog %d\n", st.WebhookBacklog)
	gauge("njiwa_mail_webhook_dead", "Webhook deliveries that exhausted their retries.")
	fmt.Fprintf(&b, "njiwa_mail_webhook_dead %d\n", st.WebhookDead)
	gauge("njiwa_mail_inbound_spool", "Inbound messages accepted but not yet parsed, by state.")
	fmt.Fprintf(&b, "njiwa_mail_inbound_spool{state=\"pending\"} %d\nnjiwa_mail_inbound_spool{state=\"failed\"} %d\n", st.InboundSpool, st.InboundFailed)
	gauge("njiwa_mail_open_alerts", "Unacknowledged operator alerts.")
	fmt.Fprintf(&b, "njiwa_mail_open_alerts %d\n", st.OpenAlerts)
	gauge("njiwa_mail_role_up", "1 when a role instance has sent a heartbeat within the last minute.")
	for _, w := range st.Workers {
		up := 0
		if w.Healthy {
			up = 1
		}
		fmt.Fprintf(&b, "njiwa_mail_role_up{role=%q,instance=%q} %d\n", w.Role, w.Instance, up)
	}
	if err := s.deliveryMetrics(ctx, &b, gauge); err != nil {
		return "", err
	}
	return b.String(), nil
}

// deliveryMetrics adds transport health, the MTA's view of delivery and the
// managed certificate expiry (F08). Every value is derived from rows the
// application already keeps, so any API or worker instance reports the same.
func (s *Service) deliveryMetrics(ctx context.Context, b *strings.Builder, gauge func(name, help string)) error {
	st, err := s.InstallationState(ctx)
	if err != nil {
		return err
	}
	rd, _ := st["readiness"].(installation.Readiness)
	ready, failing := 0, 0
	if rd.Status == installation.Ready {
		ready = 1
	}
	var problems []string
	var certNotAfter *time.Time
	for _, step := range rd.Steps {
		if step.ID == "live_delivery" {
			failing = 1
		}
		if step.Required && step.ProblemCode != "" {
			problems = append(problems, step.ProblemCode)
		}
	}
	gauge("njiwa_mail_installation_ready", "1 when every required installation readiness step is ready.")
	fmt.Fprintf(b, "njiwa_mail_installation_ready{mode=%q} %d\n", rd.Mode, ready)
	gauge("njiwa_mail_installation_problem", "Required readiness steps with a problem, by problem code.")
	sort.Strings(problems)
	for _, p := range problems {
		fmt.Fprintf(b, "njiwa_mail_installation_problem{code=%q} 1\n", p)
	}
	gauge("njiwa_mail_transport_failing", "1 while recent Live sends cannot reach the mail server (TRANSPORT_FAILING).")
	fmt.Fprintf(b, "njiwa_mail_transport_failing %d\n", failing)

	// Connection-level retries in the last 15 minutes by reason. A managed
	// Postfix with its DKIM signer down answers 454, so smtp_454 tracks
	// signer availability.
	gauge("njiwa_mail_transport_retries_15m", "Live send attempts that could not reach or were refused by the mail server in the last 15 minutes, by reason.")
	rows, err := s.Pool.Query(ctx, `SELECT r, count(ev.id) FROM unnest($1::text[]) r
LEFT JOIN message_events ev ON ev.reason_code = r AND ev.event_type='retry_scheduled' AND ev.created_at > now() - interval '15 minutes'
GROUP BY r ORDER BY r`, transportDownReasons)
	if err != nil {
		return unavailable("metrics_retries", err)
	}
	for rows.Next() {
		var r string
		var n int
		if err := rows.Scan(&r, &n); err != nil {
			rows.Close()
			return unavailable("metrics_retries", err)
		}
		fmt.Fprintf(b, "njiwa_mail_transport_retries_15m{reason=%q} %d\n", r, n)
	}
	rows.Close()

	var lastSubmitAge *float64
	var liveBacklog int
	if err := s.Pool.QueryRow(ctx, `SELECT
 (SELECT extract(epoch FROM now() - max(m.submitted_at)) FROM messages m JOIN environments e ON e.id=m.environment_id WHERE e.mode='live')::float8,
 (SELECT count(*) FROM messages m JOIN environments e ON e.id=m.environment_id WHERE e.mode='live' AND m.state IN ('queued','retry_wait','submitting','scheduled'))`).
		Scan(&lastSubmitAge, &liveBacklog); err != nil {
		return unavailable("metrics_live", err)
	}
	if lastSubmitAge != nil {
		gauge("njiwa_mail_last_live_submission_age_seconds", "Seconds since a Live message was last handed to the mail server.")
		fmt.Fprintf(b, "njiwa_mail_last_live_submission_age_seconds %.0f\n", *lastSubmitAge)
	}
	gauge("njiwa_mail_live_backlog", "Live messages Njiwa still has to hand over (queued, retrying, sending, scheduled).")
	fmt.Fprintf(b, "njiwa_mail_live_backlog %d\n", liveBacklog)
	if s.Opts.LiveQueueMax > 0 {
		gauge("njiwa_mail_live_backlog_limit", "LIVE_QUEUE_MAX: new Live mail is refused with 503 queue_full at this backlog.")
		fmt.Fprintf(b, "njiwa_mail_live_backlog_limit %d\n", s.Opts.LiveQueueMax)
	}

	// What the mail server reported afterwards (mta-log collector, DSNs).
	gauge("njiwa_mail_delivery_events_1h", "Delivery evidence recorded in the last hour, by event (deferred, remote_accepted, bounced, expired_in_queue, ...).")
	ev, err := s.Pool.Query(ctx, `SELECT e, count(d.id) FROM unnest(ARRAY['submitted','deferred','remote_accepted','bounced','complaint','expired_in_queue']) e
LEFT JOIN delivery_events d ON d.event = e AND d.observed_at > now() - interval '1 hour' GROUP BY e ORDER BY e`)
	if err != nil {
		return unavailable("metrics_evidence", err)
	}
	for ev.Next() {
		var e string
		var n int
		if err := ev.Scan(&e, &n); err != nil {
			ev.Close()
			return unavailable("metrics_evidence", err)
		}
		fmt.Fprintf(b, "njiwa_mail_delivery_events_1h{event=%q} %d\n", e, n)
	}
	ev.Close()

	var raw []byte
	if s.Pool.QueryRow(ctx, `SELECT config_json FROM transport_profiles WHERE is_active AND mode='managed_mta'`).Scan(&raw) == nil {
		var na struct {
			NotAfter *time.Time `json:"client_cert_not_after"`
		}
		_ = json.Unmarshal(raw, &na)
		certNotAfter = na.NotAfter
	}
	if certNotAfter != nil {
		gauge("njiwa_mail_client_cert_expiry_seconds", "Seconds until the worker's managed submission certificate expires (negative when expired).")
		fmt.Fprintf(b, "njiwa_mail_client_cert_expiry_seconds %.0f\n", time.Until(*certNotAfter).Seconds())
	}
	return nil
}

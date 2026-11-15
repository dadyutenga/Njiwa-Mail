package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strings"
	"time"
)

// ReportQuery selects a period of one environment's mail. To is exclusive.
// The comparison period is the same length, just before From.
type ReportQuery struct {
	From, To         time.Time
	Template, Domain string
}

// ParseReportRange reads from/to (RFC 3339 or YYYY-MM-DD); the default is
// the last 30 days. A period longer than 400 days is refused.
func ParseReportRange(from, to string) (time.Time, time.Time, error) {
	parse := func(v string) (time.Time, error) {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t, nil
		}
		return time.Parse("2006-01-02", v)
	}
	end := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	start := end.Add(-30 * 24 * time.Hour)
	var err error
	if to != "" {
		if end, err = parse(to); err != nil {
			return start, end, badRequest("to must be a date (YYYY-MM-DD) or RFC 3339 time")
		}
	}
	if from != "" {
		if start, err = parse(from); err != nil {
			return start, end, badRequest("from must be a date (YYYY-MM-DD) or RFC 3339 time")
		}
	}
	if !start.Before(end) || end.Sub(start) > 400*24*time.Hour {
		return start, end, badRequest("choose a period of up to 400 days, from before to")
	}
	return start, end, nil
}

// reportTotals are counts for one period. "submitted" means the mail server
// accepted the message; remote_accepted means the recipient's server did
// (where the mail server log or a DSN says so). Neither is "delivered".
const reportTotalsSQL = `SELECT
  count(*) AS messages,
  count(*) FILTER (WHERE m.state='submitted') AS submitted,
  count(*) FILTER (WHERE m.state IN ('failed','expired')) AS failed,
  count(*) FILTER (WHERE m.state='unknown') AS unknown,
  count(*) FILTER (WHERE m.bounce_state='hard') AS hard_bounces,
  count(*) FILTER (WHERE m.bounce_state='soft') AS soft_bounces,
  count(*) FILTER (WHERE EXISTS (SELECT 1 FROM delivery_events d WHERE d.message_id=m.id AND d.event='remote_accepted')) AS remote_accepted,
  count(*) FILTER (WHERE EXISTS (SELECT 1 FROM bounces b WHERE b.message_id=m.id AND b.type='complaint')) AS complaints
FROM messages m
WHERE m.environment_id=$1 AND m.created_at >= $2 AND m.created_at < $3
  AND ($4='' OR m.template=$4)
  AND ($5='' OR split_part(m.sender,'@',2)=$5)`

// mailboxProviderSQL groups a recipient's domain into the provider people
// know. Domains hosted elsewhere (a company on Google Workspace) count as
// "other": only the address's own domain is known, never its MX.
const mailboxProviderSQL = `CASE
  WHEN d IN ('gmail.com','googlemail.com') THEN 'Gmail'
  WHEN d IN ('outlook.com','hotmail.com','live.com','msn.com','hotmail.co.uk','outlook.fr','live.co.uk') THEN 'Outlook'
  WHEN d IN ('yahoo.com','ymail.com','yahoo.co.uk','yahoo.fr','rocketmail.com','aol.com') THEN 'Yahoo'
  WHEN d IN ('icloud.com','me.com','mac.com') THEN 'iCloud'
  WHEN d IN ('proton.me','protonmail.com','pm.me') THEN 'Proton'
  WHEN d IN ('gmx.com','gmx.net','gmx.de','mail.com') THEN 'GMX'
  WHEN d IN ('yandex.com','yandex.ru') THEN 'Yandex'
  WHEN d IN ('zoho.com','zohomail.com') THEN 'Zoho'
  ELSE 'Other' END`

// Report is the analytics page: totals with the previous period, a daily
// series, timing percentiles, and results by mailbox provider.
func (s *Service) Report(ctx context.Context, sc *Scope, q ReportQuery) (map[string]any, error) {
	args := func(from, to time.Time) []any { return []any{sc.EnvID, from, to, q.Template, q.Domain} }
	cur, err := s.one(ctx, reportTotalsSQL, args(q.From, q.To)...)
	if err != nil {
		return nil, err
	}
	prevFrom := q.From.Add(-q.To.Sub(q.From))
	prev, err := s.one(ctx, reportTotalsSQL, args(prevFrom, q.From)...)
	if err != nil {
		return nil, err
	}
	change := map[string]any{}
	for k, v := range cur {
		c, p := toF(v), toF(prev[k])
		if p == 0 {
			change[k] = nil // no base to compare against
		} else {
			change[k] = (c - p) / p * 100
		}
	}
	daily, err := s.maps(ctx, `SELECT to_char(date_trunc('day', m.created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS day,
  count(*) AS messages,
  count(*) FILTER (WHERE m.state='submitted') AS submitted,
  count(*) FILTER (WHERE m.state IN ('failed','expired')) AS failed,
  count(*) FILTER (WHERE m.bounce_state='hard') AS hard_bounces,
  count(*) FILTER (WHERE m.bounce_state='soft') AS soft_bounces
FROM messages m WHERE m.environment_id=$1 AND m.created_at >= $2 AND m.created_at < $3
  AND ($4='' OR m.template=$4) AND ($5='' OR split_part(m.sender,'@',2)=$5)
GROUP BY 1 ORDER BY 1`, args(q.From, q.To)...)
	if err != nil {
		return nil, err
	}
	timing, err := s.one(ctx, `SELECT
  percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM m.submitted_at - coalesce(m.send_at, m.created_at))) AS submit_p50,
  percentile_cont(0.9) WITHIN GROUP (ORDER BY extract(epoch FROM m.submitted_at - coalesce(m.send_at, m.created_at))) AS submit_p90,
  percentile_cont(0.99) WITHIN GROUP (ORDER BY extract(epoch FROM m.submitted_at - coalesce(m.send_at, m.created_at))) AS submit_p99,
  count(m.submitted_at) AS submit_samples
FROM messages m WHERE m.environment_id=$1 AND m.created_at >= $2 AND m.created_at < $3 AND m.submitted_at IS NOT NULL
  AND ($4='' OR m.template=$4) AND ($5='' OR split_part(m.sender,'@',2)=$5)`, args(q.From, q.To)...)
	if err != nil {
		return nil, err
	}
	remote, err := s.one(ctx, `SELECT
  percentile_cont(0.5) WITHIN GROUP (ORDER BY x) AS remote_p50,
  percentile_cont(0.9) WITHIN GROUP (ORDER BY x) AS remote_p90,
  percentile_cont(0.99) WITHIN GROUP (ORDER BY x) AS remote_p99,
  count(*) AS remote_samples
FROM (SELECT extract(epoch FROM min(d.observed_at) - coalesce(m.send_at, m.created_at)) AS x
  FROM messages m JOIN delivery_events d ON d.message_id=m.id AND d.event='remote_accepted'
  WHERE m.environment_id=$1 AND m.created_at >= $2 AND m.created_at < $3
    AND ($4='' OR m.template=$4) AND ($5='' OR split_part(m.sender,'@',2)=$5)
  GROUP BY m.id, m.send_at, m.created_at) t`, args(q.From, q.To)...)
	if err != nil {
		return nil, err
	}
	for k, v := range remote {
		timing[k] = v
	}
	providers, err := s.maps(ctx, `SELECT `+mailboxProviderSQL+` AS provider, count(*) AS messages,
  count(*) FILTER (WHERE state='submitted') AS submitted,
  count(*) FILTER (WHERE state IN ('failed','expired')) AS failed,
  count(*) FILTER (WHERE bounce_state='hard') AS hard_bounces,
  count(*) FILTER (WHERE remote) AS remote_accepted,
  count(*) FILTER (WHERE complaint) AS complaints
FROM (SELECT lower(split_part(m.recipient_masked,'@',2)) AS d, m.state, m.bounce_state,
    EXISTS (SELECT 1 FROM delivery_events e WHERE e.message_id=m.id AND e.event='remote_accepted') AS remote,
    EXISTS (SELECT 1 FROM bounces b WHERE b.message_id=m.id AND b.type='complaint') AS complaint
  FROM messages m WHERE m.environment_id=$1 AND m.created_at >= $2 AND m.created_at < $3
    AND ($4='' OR m.template=$4) AND ($5='' OR split_part(m.sender,'@',2)=$5)) x
GROUP BY 1 ORDER BY 2 DESC`, args(q.From, q.To)...)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"from": q.From, "to": q.To, "previous_from": prevFrom,
		"totals": cur, "previous": prev, "change_percent": change,
		"daily": daily, "timing_seconds": timing, "providers": providers,
		"notes": []string{
			"submitted: the mail server accepted the message. remote_accepted: the recipient's mail server accepted it, where the mail server log or a report says so. Neither means it reached the inbox.",
			"Providers are worked out from the address's domain; a company domain hosted by Google or Microsoft counts as Other.",
		},
	}, nil
}

func toF(v any) float64 {
	switch x := v.(type) {
	case int64:
		return float64(x)
	case int32:
		return float64(x)
	case int:
		return float64(x)
	case float64:
		return x
	}
	return 0
}

// ReportCSV renders one table of a report as CSV: daily, providers or totals.
func ReportCSV(rep map[string]any, table string) ([]byte, error) {
	var rows []map[string]any
	var cols []string
	switch table {
	case "", "daily":
		cols = []string{"day", "messages", "submitted", "failed", "hard_bounces", "soft_bounces"}
		rows, _ = rep["daily"].([]map[string]any)
	case "providers":
		cols = []string{"provider", "messages", "submitted", "remote_accepted", "failed", "hard_bounces", "complaints"}
		rows, _ = rep["providers"].([]map[string]any)
	case "totals":
		cols = []string{"metric", "this_period", "previous_period", "change_percent"}
		cur, _ := rep["totals"].(map[string]any)
		prev, _ := rep["previous"].(map[string]any)
		ch, _ := rep["change_percent"].(map[string]any)
		for _, k := range []string{"messages", "submitted", "remote_accepted", "failed", "unknown", "hard_bounces", "soft_bounces", "complaints"} {
			c := ""
			if v, ok := ch[k].(float64); ok {
				c = fmt.Sprintf("%.1f", v)
			}
			rows = append(rows, map[string]any{"metric": k, "this_period": cur[k], "previous_period": prev[k], "change_percent": c})
		}
	default:
		return nil, badRequest("table is daily, providers or totals")
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write(cols)
	for _, r := range rows {
		rec := make([]string, len(cols))
		for i, c := range cols {
			if r[c] != nil {
				rec[i] = csvCell(fmt.Sprint(r[c]))
			}
		}
		_ = w.Write(rec)
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// csvCell stops a spreadsheet from running a cell as a formula, but keeps
// a negative number (a fall in a percentage) as a number.
func csvCell(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		if _, err := fmt.Sscanf(v, "%f", new(float64)); err == nil {
			return v // a negative number is data, not a formula
		}
		return "'" + v
	}
	return v
}

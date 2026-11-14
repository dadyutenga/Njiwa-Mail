package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ---------------------------------------------------------------- announcements

// CreateAnnouncement broadcasts a message to every user's inbox.
func (s *Service) CreateAnnouncement(ctx context.Context, by, title, body, link string) (string, error) {
	title = clean(strings.TrimSpace(title), 200)
	if title == "" {
		return "", badRequest("give the announcement a title")
	}
	link = strings.TrimSpace(link)
	if link != "" {
		u, err := url.Parse(link)
		// Same-site paths or https links only: never javascript: or data:.
		if err != nil || !(strings.HasPrefix(link, "/") && !strings.HasPrefix(link, "//") || u.Scheme == "https") {
			return "", badRequest("the link must be a path such as /p/project/domains or an https:// address")
		}
	}
	var id string
	err := s.Pool.QueryRow(ctx, `INSERT INTO announcements(title, body, link, created_by) VALUES ($1,$2,$3,$4) RETURNING id::text`,
		title, clean(body, 4000), clean(link, 500), by).Scan(&id)
	if err != nil {
		return "", unavailable("announcement", err)
	}
	return id, nil
}

func (s *Service) ListAnnouncements(ctx context.Context, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT a.id::text AS id, a.title, a.body, a.link, a.created_by, a.created_at, a.retracted_at,
  (SELECT count(*) FROM inbox_state i WHERE i.kind='announcement' AND i.item_id=a.id::text AND i.read_at IS NOT NULL) AS read_by
FROM announcements a ORDER BY a.created_at DESC`)
}

// RetractAnnouncement removes it from every inbox; it stays listed for operators.
func (s *Service) RetractAnnouncement(ctx context.Context, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE announcements SET retracted_at=now() WHERE id=$1 AND retracted_at IS NULL`, id)
	if err != nil {
		return unavailable("announcement", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

// ---------------------------------------------------------------- daily report

// GenerateDailyReport records the report for one UTC day, once. It covers
// every project and environment: messages created that day by state, hard
// bounces, complaints, inbound mail, and alerts still open.
func (s *Service) GenerateDailyReport(ctx context.Context, day time.Time) (bool, error) {
	start := day.UTC().Truncate(24 * time.Hour)
	end := start.Add(24 * time.Hour)
	rows, err := s.maps(ctx, `SELECT p.name AS project, e.mode,
  count(m.id) AS messages,
  count(m.id) FILTER (WHERE m.state='submitted') AS submitted,
  count(m.id) FILTER (WHERE m.state IN ('failed','expired')) AS failed,
  count(m.id) FILTER (WHERE m.state='unknown') AS unknown,
  count(m.id) FILTER (WHERE m.bounce_state='hard') AS hard_bounces,
  count(m.id) FILTER (WHERE EXISTS (SELECT 1 FROM bounces b WHERE b.message_id=m.id AND b.type='complaint')) AS complaints,
  (SELECT count(*) FROM inbound_messages i WHERE i.environment_id=e.id AND i.received_at >= $1 AND i.received_at < $2) AS inbound
FROM environments e JOIN projects p ON p.id=e.project_id
LEFT JOIN messages m ON m.environment_id=e.id AND m.created_at >= $1 AND m.created_at < $2
GROUP BY p.name, e.mode, e.id ORDER BY p.name, e.mode`, start, end)
	if err != nil {
		return false, err
	}
	var open int64
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM alerts WHERE acknowledged_at IS NULL`).Scan(&open); err != nil {
		return false, err
	}
	var active []map[string]any
	totals := map[string]int64{}
	for _, r := range rows {
		if toF(r["messages"]) == 0 && toF(r["inbound"]) == 0 {
			continue
		}
		active = append(active, r)
		for _, k := range []string{"messages", "submitted", "failed", "unknown", "hard_bounces", "complaints", "inbound"} {
			totals[k] += int64(toF(r[k]))
		}
	}
	if active == nil {
		active = []map[string]any{}
	}
	data, _ := json.Marshal(map[string]any{"day": start.Format("2006-01-02"), "totals": totals, "projects": active, "open_alerts": open})
	tag, err := s.Pool.Exec(ctx, `INSERT INTO daily_reports(day, data) VALUES ($1,$2) ON CONFLICT (day) DO NOTHING`, start, data)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ---------------------------------------------------------------- inbox

// Notifications is what one user should see: announcements, daily reports and (for
// installation operators) open alerts, newest first, without what they
// dismissed.
func (s *Service) Notifications(ctx context.Context, u *AdminUser) (map[string]any, error) {
	items, err := s.maps(ctx, `SELECT * FROM (
  SELECT 'announcement' AS kind, a.id::text AS id, a.title, a.body, a.link, a.created_at, NULL::jsonb AS data
    FROM announcements a WHERE a.retracted_at IS NULL AND a.created_at > now() - interval '90 days'
  UNION ALL
  SELECT 'daily_report', to_char(r.day,'YYYY-MM-DD'), 'Daily report for ' || to_char(r.day,'YYYY-MM-DD'), '', '', r.created_at, r.data
    FROM daily_reports r WHERE r.day > now() - interval '14 days'
  UNION ALL
  SELECT 'alert', x.id::text, x.summary, x.kind, '', x.created_at, x.details
    FROM alerts x WHERE $2 AND x.acknowledged_at IS NULL
) n
LEFT JOIN LATERAL (SELECT st.read_at, st.dismissed_at FROM inbox_state st WHERE st.user_id=$1 AND st.kind=n.kind AND st.item_id=n.id) st ON true
WHERE st.dismissed_at IS NULL
ORDER BY n.created_at DESC LIMIT 100`, u.ID, u.Operator)
	if err != nil {
		return nil, err
	}
	unread := 0
	for _, it := range items {
		delete(it, "dismissed_at")
		it["read"] = it["read_at"] != nil
		if it["read_at"] == nil {
			unread++
		}
	}
	return map[string]any{"items": items, "unread": unread}, nil
}

// MarkNotification records "read" or "dismiss" for one item and this user only.
func (s *Service) MarkNotification(ctx context.Context, u *AdminUser, kind, id, action string) error {
	switch kind {
	case "announcement", "daily_report", "alert":
	default:
		return newErr(404, "not_found")
	}
	if id == "" || len(id) > 64 {
		return newErr(404, "not_found")
	}
	if kind == "alert" && !u.Operator {
		return newErr(404, "not_found")
	}
	set := "read_at=coalesce(inbox_state.read_at, now())"
	cols := "read_at"
	switch action {
	case "read":
	case "dismiss":
		set += ", dismissed_at=coalesce(inbox_state.dismissed_at, now())"
		cols = "read_at, dismissed_at"
	default:
		return newErr(404, "not_found")
	}
	vals := "now()"
	if action == "dismiss" {
		vals = "now(), now()"
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO inbox_state(user_id, kind, item_id, `+cols+`) VALUES ($1,$2,$3,`+vals+`)
ON CONFLICT (user_id, kind, item_id) DO UPDATE SET `+set, u.ID, kind, id)
	return opErr(err)
}

// MarkAllNotificationsRead marks everything currently in the user's inbox as read.
func (s *Service) MarkAllNotificationsRead(ctx context.Context, u *AdminUser) error {
	in, err := s.Notifications(ctx, u)
	if err != nil {
		return err
	}
	for _, it := range in["items"].([]map[string]any) {
		if it["read"] == true {
			continue
		}
		if err := s.MarkNotification(ctx, u, fmt.Sprint(it["kind"]), fmt.Sprint(it["id"]), "read"); err != nil {
			return err
		}
	}
	return nil
}

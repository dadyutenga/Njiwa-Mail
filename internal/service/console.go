package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Console views are metadata only. They never return payloads, recipients,
// sandbox captures, or API key secrets.

var consoleStates = []string{
	"queued", "scheduled", "submitting", "submitted", "retry_wait", "failed", "unknown", "expired", "captured", "cancelled",
}

type ConsoleOverview struct {
	Database string         `json:"database"`
	Worker   ConsoleWorker  `json:"worker"`
	Projects int            `json:"projects"`
	Messages int            `json:"messages"`
	Counts   map[string]int `json:"counts"`
}

type ConsoleWorker struct {
	Healthy  bool       `json:"healthy"`
	LastSeen *time.Time `json:"last_seen,omitempty"`
}

type ConsoleMessage struct {
	ID              string            `json:"id"`
	State           string            `json:"state"`
	Template        string            `json:"template"`
	TemplateVersion int               `json:"template_version"`
	Project         string            `json:"project"`
	Mode            string            `json:"mode"`
	Attempts        int               `json:"attempts"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	ExpiresAt       time.Time         `json:"expires_at"`
	SubmittedAt     *time.Time        `json:"submitted_at,omitempty"`
	CompletedAt     *time.Time        `json:"completed_at,omitempty"`
	IdempotencyKey  string            `json:"idempotency_key"`
	SMTPReply       string            `json:"smtp_reply,omitempty"`
	Metadata        map[string]string `json:"metadata"`
	Events          []Event           `json:"events,omitempty"`
}

type ConsoleEnv struct {
	Mode               string `json:"mode"`
	Enabled            bool   `json:"enabled"`
	RestrictRecipients bool   `json:"restrict_recipients"`
	OriginCount        int    `json:"origin_count"`
	AllowlistCount     int    `json:"allowlist_count"`
	SenderCount        int    `json:"sender_count"`
}

type ConsoleProject struct {
	Name               string       `json:"name"`
	Enabled            bool         `json:"enabled"`
	SendPerMinute      int          `json:"send_per_minute"`
	SendPerDay         int          `json:"send_per_day"`
	RecipientPerMinute int          `json:"recipient_per_minute"`
	RecipientPerHour   int          `json:"recipient_per_hour"`
	Environments       []ConsoleEnv `json:"environments"`
}

func validConsoleState(state string) bool {
	if state == "" {
		return true
	}
	for _, s := range consoleStates {
		if s == state {
			return true
		}
	}
	return false
}

func (s *Service) ConsoleOverview(ctx context.Context) (*ConsoleOverview, error) {
	out := &ConsoleOverview{
		Database: "ready",
		Counts:   map[string]int{},
	}
	for _, st := range consoleStates {
		out.Counts[st] = 0
	}
	if err := s.Ready(ctx); err != nil {
		return nil, unavailable("console_ready", err)
	}
	rows, err := s.Pool.Query(ctx, `SELECT state, count(*) FROM messages GROUP BY state`)
	if err != nil {
		return nil, unavailable("console_counts", err)
	}
	defer rows.Close()
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, unavailable("console_counts_scan", err)
		}
		out.Counts[st] = n
		out.Messages += n
	}
	if err := rows.Err(); err != nil {
		return nil, unavailable("console_counts_rows", err)
	}
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM projects`).Scan(&out.Projects); err != nil {
		return nil, unavailable("console_projects", err)
	}
	var last *time.Time
	err = s.Pool.QueryRow(ctx, `SELECT max(last_seen) FROM worker_heartbeats`).Scan(&last)
	if err != nil {
		return nil, unavailable("console_worker", err)
	}
	out.Worker.LastSeen = last
	if last != nil && time.Since(*last) <= time.Minute {
		out.Worker.Healthy = true
	}
	return out, nil
}

func (s *Service) ConsoleMessages(ctx context.Context, state string, limit int) ([]ConsoleMessage, error) {
	if !validConsoleState(state) {
		return nil, newErr(422, "invalid_request")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.Pool.Query(ctx, `
SELECT m.id::text, m.state, m.template, m.template_version, p.name, e.mode, m.attempts,
       m.created_at, m.updated_at, m.expires_at, m.submitted_at, m.completed_at, m.idempotency_key
FROM messages m
JOIN projects p ON p.id = m.project_id
JOIN environments e ON e.id = m.environment_id
WHERE ($1 = '' OR m.state = $1)
ORDER BY m.created_at DESC
LIMIT $2`, state, limit)
	if err != nil {
		return nil, unavailable("console_messages", err)
	}
	defer rows.Close()
	out := []ConsoleMessage{}
	for rows.Next() {
		var m ConsoleMessage
		if err := rows.Scan(&m.ID, &m.State, &m.Template, &m.TemplateVersion, &m.Project, &m.Mode, &m.Attempts,
			&m.CreatedAt, &m.UpdatedAt, &m.ExpiresAt, &m.SubmittedAt, &m.CompletedAt, &m.IdempotencyKey); err != nil {
			return nil, unavailable("console_messages_scan", err)
		}
		m.Metadata = map[string]string{}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, unavailable("console_messages_rows", err)
	}
	return out, nil
}

func (s *Service) ConsoleMessage(ctx context.Context, id string) (*ConsoleMessage, error) {
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	var m ConsoleMessage
	var meta []byte
	var reply *string
	err := s.Pool.QueryRow(ctx, `
SELECT m.id::text, m.state, m.template, m.template_version, p.name, e.mode, m.attempts,
       m.created_at, m.updated_at, m.expires_at, m.submitted_at, m.completed_at,
       m.idempotency_key, m.smtp_reply, m.metadata
FROM messages m
JOIN projects p ON p.id = m.project_id
JOIN environments e ON e.id = m.environment_id
WHERE m.id = $1`, id).Scan(&m.ID, &m.State, &m.Template, &m.TemplateVersion, &m.Project, &m.Mode, &m.Attempts,
		&m.CreatedAt, &m.UpdatedAt, &m.ExpiresAt, &m.SubmittedAt, &m.CompletedAt, &m.IdempotencyKey, &reply, &meta)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, newErr(404, "not_found")
	}
	if err != nil {
		return nil, unavailable("console_message", err)
	}
	if reply != nil {
		m.SMTPReply = *reply
	}
	m.Metadata = map[string]string{}
	if len(meta) > 0 {
		_ = json.Unmarshal(meta, &m.Metadata)
	}
	rows, err := s.Pool.Query(ctx, `SELECT event_type, reason_code, created_at FROM message_events WHERE message_id=$1 ORDER BY id`, id)
	if err != nil {
		return nil, unavailable("console_events", err)
	}
	defer rows.Close()
	m.Events = []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Type, &e.Reason, &e.At); err != nil {
			return nil, unavailable("console_events_scan", err)
		}
		m.Events = append(m.Events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, unavailable("console_events_rows", err)
	}
	return &m, nil
}

func (s *Service) ConsoleProjects(ctx context.Context) ([]ConsoleProject, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT p.name, p.enabled, p.send_per_minute, p.send_per_day, p.recipient_per_minute, p.recipient_per_hour,
       e.mode, e.enabled, e.restrict_recipients,
       coalesce(cardinality(e.approved_origins), 0),
       coalesce(cardinality(e.recipient_allowlist), 0),
       (SELECT count(*) FROM sender_identities s WHERE s.environment_id = e.id)
FROM projects p
JOIN environments e ON e.project_id = p.id
ORDER BY p.name, e.mode`)
	if err != nil {
		return nil, unavailable("console_project_list", err)
	}
	defer rows.Close()
	byName := map[string]*ConsoleProject{}
	order := []string{}
	for rows.Next() {
		var name string
		var p ConsoleProject
		var env ConsoleEnv
		if err := rows.Scan(&name, &p.Enabled, &p.SendPerMinute, &p.SendPerDay, &p.RecipientPerMinute, &p.RecipientPerHour,
			&env.Mode, &env.Enabled, &env.RestrictRecipients, &env.OriginCount, &env.AllowlistCount, &env.SenderCount); err != nil {
			return nil, unavailable("console_project_scan", err)
		}
		cur, ok := byName[name]
		if !ok {
			p.Name = name
			p.Environments = []ConsoleEnv{}
			byName[name] = &p
			cur = byName[name]
			order = append(order, name)
		}
		cur.Environments = append(cur.Environments, env)
	}
	if err := rows.Err(); err != nil {
		return nil, unavailable("console_project_rows", err)
	}
	out := []ConsoleProject{}
	for _, name := range order {
		out = append(out, *byName[name])
	}
	return out, nil
}

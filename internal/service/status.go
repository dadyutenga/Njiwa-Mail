package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/njiwa-tz/mail/internal/db"
	"regexp"
	"time"
)

type Event struct {
	Type   string    `json:"type"`
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at"`
}

type Status struct {
	ID              string            `json:"id"`
	Status          string            `json:"status"`
	Template        string            `json:"template"`
	TemplateVersion int               `json:"template_version"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	ExpiresAt       time.Time         `json:"expires_at"`
	SubmittedAt     *time.Time        `json:"submitted_at,omitempty"`
	CompletedAt     *time.Time        `json:"completed_at,omitempty"`
	Attempts        int               `json:"attempts"`
	SMTPReply       string            `json:"smtp_reply,omitempty"`
	Metadata        map[string]string `json:"metadata"`
	Events          []Event           `json:"events"`
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// GetStatus returns metadata only: never variables, tokens or recipient.
// The query is always scoped by project AND environment.
func (s *Service) GetStatus(ctx context.Context, p *Principal, id string) (*Status, error) {
	if !p.HasScope(ScopeRead) {
		return nil, newErr(403, "missing_scope")
	}
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	var st Status
	var meta []byte
	var reply *string
	err := s.Pool.QueryRow(ctx, `
SELECT id::text, state, template, template_version, created_at, updated_at, expires_at, submitted_at, completed_at, attempts, smtp_reply, metadata
FROM messages WHERE id=$1 AND project_id=$2 AND environment_id=$3`, id, p.ProjectID, p.EnvironmentID).
		Scan(&st.ID, &st.Status, &st.Template, &st.TemplateVersion, &st.CreatedAt, &st.UpdatedAt, &st.ExpiresAt,
			&st.SubmittedAt, &st.CompletedAt, &st.Attempts, &reply, &meta)
	if errors.Is(err, db.ErrNoRows) {
		return nil, newErr(404, "not_found")
	}
	if err != nil {
		return nil, unavailable("status", err)
	}
	if reply != nil {
		st.SMTPReply = *reply
	}
	st.Metadata = map[string]string{}
	json.Unmarshal(meta, &st.Metadata)
	rows, err := s.Pool.Query(ctx, `SELECT event_type, reason_code, created_at FROM message_events WHERE message_id=$1 ORDER BY id`, id)
	if err != nil {
		return nil, unavailable("events", err)
	}
	defer rows.Close()
	st.Events = []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Type, &e.Reason, &e.At); err != nil {
			return nil, unavailable("events_scan", err)
		}
		st.Events = append(st.Events, e)
	}
	if rows.Err() != nil {
		return nil, unavailable("events_rows", rows.Err())
	}
	return &st, nil
}

// Ready checks critical local dependencies: database reachability and schema.
func (s *Service) Ready(ctx context.Context) error {
	var n int
	return s.Pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n)
}

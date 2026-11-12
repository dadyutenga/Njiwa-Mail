package service

import (
	"context"
	"time"
)

// E10: helpers for the mail queue controls. The queue itself lives in Postfix
// and is reached through the host agent; this side only ties queue ids to our
// messages and leaves an evidence trail.

// MessagesByQueueID maps Postfix queue ids to the Njiwa-Mail message they carry.
func (s *Service) MessagesByQueueID(ctx context.Context, queueIDs []string) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	if len(queueIDs) == 0 {
		return out, nil
	}
	rows, err := s.maps(ctx, `SELECT DISTINCT ON (m.mta_queue_id) m.mta_queue_id AS queue_id, m.id::text AS message_id, p.name AS project, e.mode, m.template
FROM messages m JOIN projects p ON p.id=m.project_id JOIN environments e ON e.id=m.environment_id
WHERE m.mta_queue_id = ANY($1) ORDER BY m.mta_queue_id, m.submitted_at DESC NULLS LAST`, queueIDs)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if q, _ := r["queue_id"].(string); q != "" {
			out[q] = r
		}
	}
	return out, nil
}

// NoteQueueAction records an operator's queue action as delivery evidence on the
// message that carries the queue id (if we know it), so the message timeline
// explains why mail the server accepted never arrived.
func (s *Service) NoteQueueAction(ctx context.Context, queueID, action string) error {
	var id string
	err := s.Pool.QueryRow(ctx, `SELECT id::text FROM messages WHERE mta_queue_id=$1 ORDER BY submitted_at DESC NULLS LAST LIMIT 1`, queueID).Scan(&id)
	if err != nil {
		return nil // not one of ours (or already purged): nothing to annotate
	}
	reason := map[string]string{
		"hold":    "held in the mail server queue by an operator",
		"release": "released in the mail server queue by an operator",
		"requeue": "re-queued in the mail server by an operator",
		"delete":  "removed from the mail server queue by an operator; it will not be delivered",
	}[action]
	if reason == "" {
		return nil
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	if err := addEvidence(ctx, tx, id, "operator", "operator_resolved", "", "", reason, queueID, dedupe("queue_action", queueID, action, time.Now().Format(time.RFC3339Nano))); err != nil {
		return unavailable("evidence", err)
	}
	return tx.Commit(ctx)
}

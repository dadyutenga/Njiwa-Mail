package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
)

// F10: data-subject requests. Export is metadata only (never bodies or
// variables); delete removes the rows and every file they own.

// ExportRecipient returns everything stored about one recipient address in a
// project environment: message metadata with its delivery evidence, bounces,
// inbound mail from that address, and whether it is suppressed.
func (s *Service) ExportRecipient(ctx context.Context, project, mode, email string) (map[string]any, error) {
	e, ok := NormalizeEmail(email)
	if !ok {
		return nil, badRequest("invalid email address")
	}
	_, eid, err := s.envID(ctx, project, mode)
	if err != nil {
		return nil, notFound(err)
	}
	hash := s.Box.MAC("recipient", []byte(e))
	msgs, err := s.maps(ctx, `SELECT id::text AS id, state, template, source, sender, bounce_state, created_at FROM messages WHERE environment_id=$1 AND recipient_hash=$2 ORDER BY created_at`, eid, hash)
	if err != nil {
		return nil, err
	}
	events, err := s.maps(ctx, `SELECT d.message_id::text AS message_id, d.source, d.event, d.smtp_status, d.remote_host, d.safe_reason, d.observed_at
FROM delivery_events d JOIN messages m ON m.id=d.message_id WHERE m.environment_id=$1 AND m.recipient_hash=$2 ORDER BY d.observed_at, d.id`, eid, hash)
	if err != nil {
		return nil, err
	}
	bounces, err := s.maps(ctx, `SELECT id::text AS id, message_id::text AS message_id, type, status_code, received_at FROM bounces
WHERE environment_id=$1 AND recipient_hash=$2 ORDER BY received_at`, eid, hash)
	if err != nil {
		return nil, err
	}
	inb, err := s.maps(ctx, `SELECT id::text AS id, subject, received_at FROM inbound_messages WHERE environment_id=$1 AND lower(envelope_from)=$2`, eid, e)
	if err != nil {
		return nil, err
	}
	var suppressed int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM suppressions WHERE environment_id=$1 AND recipient_hash=$2`, eid, hash).Scan(&suppressed); err != nil {
		return nil, unavailable("privacy_export", err)
	}
	return map[string]any{"email_masked": maskEmail(e), "exported_at": time.Now().UTC(), "suppressed": suppressed > 0,
		"messages": msgs, "delivery_events": events, "bounces": bounces, "inbound": inb}, nil
}

// RecipientExportCSV flattens an export into one CSV: a row per record with
// its kind, so it opens in a spreadsheet.
func RecipientExportCSV(x map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"kind", "id", "message_id", "when", "state_or_event", "detail"})
	cell := func(v any) string {
		switch t := v.(type) {
		case nil:
			return ""
		case time.Time:
			return t.UTC().Format(time.RFC3339)
		default:
			return csvSafe(fmt.Sprint(t))
		}
	}
	rows := func(kind, key string, f func(m map[string]any) []string) {
		list, _ := x[key].([]map[string]any)
		for _, m := range list {
			_ = w.Write(append([]string{kind}, f(m)...))
		}
	}
	rows("message", "messages", func(m map[string]any) []string {
		return []string{cell(m["id"]), "", cell(m["created_at"]), cell(m["state"]), cell(m["template"])}
	})
	rows("delivery_event", "delivery_events", func(m map[string]any) []string {
		return []string{"", cell(m["message_id"]), cell(m["observed_at"]), cell(m["event"]), cell(m["smtp_status"]) + " " + cell(m["safe_reason"])}
	})
	rows("bounce", "bounces", func(m map[string]any) []string {
		return []string{cell(m["id"]), cell(m["message_id"]), cell(m["received_at"]), cell(m["type"]), cell(m["status_code"])}
	})
	rows("inbound", "inbound", func(m map[string]any) []string {
		return []string{cell(m["id"]), "", cell(m["received_at"]), "received", cell(m["subject"])}
	})
	w.Flush()
	return buf.Bytes(), w.Error()
}

// csvSafe stops spreadsheet formula injection from stored text.
func csvSafe(v string) string {
	if v != "" && (v[0] == '=' || v[0] == '+' || v[0] == '-' || v[0] == '@' || v[0] == '\t' || v[0] == '\r') {
		return "'" + v
	}
	return v
}

// DeleteRecipient removes every stored row about one recipient in a project
// environment, including the attachment files of inbound mail from them.
func (s *Service) DeleteRecipient(ctx context.Context, project, mode, email string) error {
	e, ok := NormalizeEmail(email)
	if !ok {
		return badRequest("invalid email address")
	}
	_, eid, err := s.envID(ctx, project, mode)
	if err != nil {
		return notFound(err)
	}
	hash := s.Box.MAC("recipient", []byte(e))
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	// Bounces keep only a hash and a masked address, but they are still the person's data.
	if _, err := tx.Exec(ctx, `DELETE FROM bounces WHERE environment_id=$1 AND recipient_hash=$2`, eid, hash); err != nil {
		return unavailable("privacy_bounces", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM bounces WHERE message_id IN (SELECT id FROM messages WHERE environment_id=$1 AND recipient_hash=$2)`, eid, hash); err != nil {
		return unavailable("privacy_bounces", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM messages WHERE environment_id=$1 AND recipient_hash=$2`, eid, hash); err != nil {
		return unavailable("privacy_messages", err)
	}
	rows, err := tx.Query(ctx, `DELETE FROM inbound_messages WHERE environment_id=$1 AND lower(envelope_from)=$2 RETURNING id::text`, eid, e)
	if err != nil {
		return unavailable("privacy_inbound", err)
	}
	ids, err := db.CollectStrings(rows)
	if err != nil {
		return unavailable("privacy_inbound", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM suppressions WHERE environment_id=$1 AND recipient_hash=$2`, eid, hash); err != nil {
		return unavailable("privacy_suppressions", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable("commit", err)
	}
	s.removeAttachmentDirs(ids)
	return nil
}

func (s *Service) removeAttachmentDirs(messageIDs []string) {
	if s.Opts.DataDir == "" {
		return
	}
	for _, id := range messageIDs {
		if uuidRe.MatchString(id) {
			os.RemoveAll(s.attachmentDir(id))
		}
	}
}

// ExportMessage is the operator's record of one message: metadata, events,
// delivery evidence and bounces. It never contains the body or variables.
func (s *Service) ExportMessage(ctx context.Context, sc *Scope, id string) (map[string]any, error) {
	m, err := s.WSMessage(ctx, sc, id)
	if err != nil {
		return nil, err
	}
	m["exported_at"] = time.Now().UTC()
	return m, nil
}

// DeleteMessage removes one finished message and everything attached to it
// (events, delivery evidence, bounces, sent copy, captures). A message that is
// still queued, retrying, being submitted or waiting for reconciliation cannot
// be deleted; cancel or resolve it first.
func (s *Service) DeleteMessage(ctx context.Context, sc *Scope, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM messages WHERE id=$1 AND environment_id=$2 FOR UPDATE`, id, sc.EnvID).Scan(&state); err != nil {
		return notFound(err)
	}
	switch state {
	case "queued", "retry_wait", "submitting", "scheduled", "unknown":
		return newErr(409, "message_active")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM bounces WHERE message_id=$1`, id); err != nil {
		return unavailable("message_delete", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM messages WHERE id=$1`, id); err != nil {
		return unavailable("message_delete", err)
	}
	return tx.Commit(ctx)
}

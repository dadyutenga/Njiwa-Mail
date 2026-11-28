package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
)

// Projects are archived first (hidden, nothing can send or receive for them,
// restorable) and deleted for good on request or after the grace period.

// ArchiveGrace is how long an archived project can be restored before the
// scheduler deletes it.
const ArchiveGrace = 30 * 24 * time.Hour

// ProjectTables lists every table that refers to a project or one of its
// environments, and how deleting a project removes its rows: "explicit" here,
// or by a foreign key that cascades. A schema test fails when a new table is
// missing.
var ProjectTables = map[string]string{
	"api_keys": "explicit", "automation_rules": "cascade", "blocked_senders": "cascade", "bounces": "explicit",
	"bulk_unsubscribes": "cascade", "campaigns": "cascade", "chat_bans": "explicit", "chat_conversations": "cascade",
	"chat_messages": "cascade", "chat_snippets": "cascade", "chat_widgets": "cascade", "contact_lists": "cascade",
	"contact_segments": "cascade", "contacts": "cascade", "dns_changes": "cascade", "dns_connections": "cascade",
	"dns_oauth_states": "cascade", "dns_setups": "cascade", "domain_claims": "cascade", "domains": "explicit",
	"drafts": "cascade", "environments": "explicit", "form_filters": "cascade", "form_submissions": "cascade",
	"forms": "cascade", "inbound_addresses": "explicit", "inbound_ingest": "cascade", "inbound_messages": "explicit",
	"messages": "explicit", "monthly_usage": "cascade", "newsletter_issues": "cascade", "project_members": "cascade", "report_schedules": "cascade",
	"sender_identities": "explicit", "suppressions": "explicit", "template_styles": "cascade", "templates": "explicit",
	"webhook_endpoints": "explicit",
}

const projectEnvs = `(SELECT id FROM environments WHERE project_id=$1)`

// projectDeletes run in this order: rows that others point to go last.
var projectDeletes = []string{
	`INSERT INTO removed_inbound_files SELECT id FROM inbound_messages WHERE project_id=$1 ON CONFLICT DO NOTHING`,
	`DELETE FROM inbound_messages WHERE project_id=$1`,
	`DELETE FROM messages WHERE project_id=$1`,
	`DELETE FROM suppressions WHERE project_id=$1`,
	`DELETE FROM bounces WHERE project_id=$1 OR environment_id IN ` + projectEnvs,
	`DELETE FROM chat_bans WHERE environment_id IN ` + projectEnvs,
	`DELETE FROM inbound_addresses WHERE project_id=$1`,
	`DELETE FROM webhook_endpoints WHERE project_id=$1`,
	`DELETE FROM api_keys WHERE project_id=$1`,
	`DELETE FROM sender_identities WHERE project_id=$1`,
	`DELETE FROM environments WHERE project_id=$1`,
	`DELETE FROM templates WHERE project_id=$1`,
	`DELETE FROM domains WHERE project_id=$1`,
	`DELETE FROM projects WHERE id=$1`,
}

const liveQueuedSQL = `SELECT count(*) FROM messages m JOIN environments e ON e.id=m.environment_id
WHERE m.project_id=$1 AND e.mode='live' AND m.state IN ('queued','scheduled','submitting','retry_wait')`

type lockedProject struct {
	ID       string
	Archived bool
}

func lockProject(ctx context.Context, tx *db.Tx, name string) (p lockedProject, err error) {
	err = tx.QueryRow(ctx, `SELECT id::text, archived_at IS NOT NULL FROM projects WHERE name=$1 FOR UPDATE`, name).Scan(&p.ID, &p.Archived)
	if errors.Is(err, db.ErrNoRows) {
		return p, newErr(404, "project_not_found")
	}
	if err != nil {
		return p, unavailable("project_lock", err)
	}
	return p, nil
}

func refuseQueued(ctx context.Context, tx *db.Tx, id string) error {
	var n int
	if err := tx.QueryRow(ctx, liveQueuedSQL, id).Scan(&n); err != nil {
		return unavailable("project_queue", err)
	}
	if n > 0 {
		return &Error{Status: 409, Code: "project_has_queued_mail",
			Message: fmt.Sprintf("%d Live messages of this project are still waiting to be sent. Let them go out or cancel them first.", n)}
	}
	return nil
}

func confirmName(name, confirm string) error {
	if confirm != name {
		return &Error{Status: 400, Code: "confirmation_mismatch", Message: "type the project name exactly to confirm"}
	}
	return nil
}

// ProjectRole of a project named, whether or not it is archived.
func (s *Service) ProjectRoleByName(ctx context.Context, u *AdminUser, name string) (string, error) {
	var id string
	err := s.Pool.QueryRow(ctx, `SELECT id::text FROM projects WHERE name=$1`, name).Scan(&id)
	if errors.Is(err, db.ErrNoRows) {
		return "", newErr(404, "project_not_found")
	}
	if err != nil {
		return "", unavailable("project_role", err)
	}
	return s.ProjectRole(ctx, u, id)
}

// ProjectRemoval counts what deleting the project would remove.
func (s *Service) ProjectRemoval(ctx context.Context, name string) (map[string]any, error) {
	m, err := s.one(ctx, `SELECT
 (SELECT count(*) FROM api_keys WHERE project_id=p.id) AS api_keys,
 (SELECT count(*) FROM sender_identities WHERE project_id=p.id) AS senders,
 (SELECT count(*) FROM domains WHERE project_id=p.id) AS domains,
 (SELECT count(*) FROM domain_claims WHERE project_id=p.id) AS domain_claims,
 (SELECT count(*) FROM templates WHERE project_id=p.id) AS templates,
 (SELECT count(*) FROM contacts WHERE project_id=p.id) AS contacts,
 (SELECT count(*) FROM messages WHERE project_id=p.id) AS messages,
 (SELECT count(*) FROM inbound_messages WHERE project_id=p.id) AS received_messages,
 (SELECT count(*) FROM inbound_attachments a JOIN inbound_messages i ON i.id=a.inbound_message_id WHERE i.project_id=p.id) AS attachments,
 (SELECT count(*) FROM webhook_endpoints WHERE project_id=p.id) AS webhooks,
 (SELECT count(*) FROM forms WHERE project_id=p.id) AS forms,
 (SELECT count(*) FROM chat_widgets WHERE project_id=p.id) AS chats,
 (SELECT count(*) FROM messages q JOIN environments e ON e.id=q.environment_id
   WHERE q.project_id=p.id AND e.mode='live' AND q.state IN ('queued','scheduled','submitting','retry_wait')) AS queued_live
FROM projects p WHERE p.name=$1`, name)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// RenameProject changes the project's name, which is also its address in the
// dashboard. Keys, domains and mail are tied to the project, not the name.
func (s *Service) RenameProject(ctx context.Context, name, to string) (string, error) {
	to = strings.TrimSpace(to)
	if !projectNameRe(to) {
		return "", badRequest("project name must be 1-64 letters, digits, dashes or underscores")
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE projects SET name=$2 WHERE name=$1 AND archived_at IS NULL`, name, to)
	if err != nil {
		if strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate") {
			return "", newErr(409, "project_exists")
		}
		return "", unavailable("project_rename", err)
	}
	if tag.RowsAffected() == 0 {
		return "", newErr(404, "project_not_found")
	}
	return to, nil
}

// RevokeProjectKeys revokes every usable API key of the project.
func (s *Service) RevokeProjectKeys(ctx context.Context, name string) (int64, error) {
	tag, err := s.Pool.Exec(ctx, `UPDATE api_keys SET revoked_at=now() WHERE revoked_at IS NULL
AND project_id=(SELECT id FROM projects WHERE name=$1)`, name)
	if err != nil {
		return 0, unavailable("project_revoke_keys", err)
	}
	return tag.RowsAffected(), nil
}

// ArchiveProject hides the project and stops everything it does. It is
// refused while Live mail is waiting, and returns when it will be deleted.
func (s *Service) ArchiveProject(ctx context.Context, name, confirm string) (time.Time, error) {
	var until time.Time
	if err := confirmName(name, confirm); err != nil {
		return until, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return until, unavailable("project_archive", err)
	}
	defer tx.Rollback(ctx)
	p, err := lockProject(ctx, tx, name)
	if err != nil {
		return until, err
	}
	if p.Archived {
		return until, &Error{Status: 409, Code: "project_archived", Message: "this project is already archived"}
	}
	if err := refuseQueued(ctx, tx, p.ID); err != nil {
		return until, err
	}
	if err := tx.QueryRow(ctx, `UPDATE projects SET archived_at=now(), purge_after=now() + make_interval(secs => $2::float8),
  enabled_before_archive=enabled, enabled=false WHERE id=$1 RETURNING purge_after`, p.ID, ArchiveGrace.Seconds()).Scan(&until); err != nil {
		return until, unavailable("project_archive", err)
	}
	return until, opErr(tx.Commit(ctx))
}

// RestoreProject brings an archived project back as it was. Revoked keys stay revoked.
func (s *Service) RestoreProject(ctx context.Context, name string) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE projects SET archived_at=NULL, purge_after=NULL, enabled=COALESCE(enabled_before_archive, true),
  enabled_before_archive=NULL WHERE name=$1 AND archived_at IS NOT NULL`, name)
	if err != nil {
		return unavailable("project_restore", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "project_not_found")
	}
	return nil
}

// DeleteProject permanently removes an archived project and everything in it,
// releases its domain claims, and deletes its attachment files. The audit log
// keeps its history.
func (s *Service) DeleteProject(ctx context.Context, name, confirm string) error {
	if err := confirmName(name, confirm); err != nil {
		return err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("project_delete", err)
	}
	defer tx.Rollback(ctx)
	p, err := lockProject(ctx, tx, name)
	if err != nil {
		return err
	}
	if !p.Archived {
		return &Error{Status: 409, Code: "project_not_archived", Message: "archive the project first"}
	}
	if err := refuseQueued(ctx, tx, p.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SET LOCAL njiwa.deleting_project = 'on'`); err != nil {
		return unavailable("project_delete", err)
	}
	for _, q := range projectDeletes {
		if _, err := tx.Exec(ctx, q, p.ID); err != nil {
			return unavailable("project_delete", fmt.Errorf("%s: %w", q, err))
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable("project_delete", err)
	}
	// Best effort now; the scheduler retries whatever is left.
	_, _ = s.PurgeRemovedInboundFiles(context.WithoutCancel(ctx))
	return nil
}

// PurgeArchivedProjects deletes the projects whose grace period has ended.
func (s *Service) PurgeArchivedProjects(ctx context.Context) (int, error) {
	rows, err := s.Pool.Query(ctx, `SELECT name FROM projects WHERE archived_at IS NOT NULL AND purge_after <= now() ORDER BY purge_after LIMIT 20`)
	if err != nil {
		return 0, err
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return 0, err
		}
		names = append(names, n)
	}
	rows.Close()
	done := 0
	var failed error
	for _, n := range names {
		if err := s.DeleteProject(ctx, n, n); err != nil {
			failed = err
			continue
		}
		done++
		_ = s.Audit(ctx, Actor{Name: "scheduler"}, "project.delete", "project", n, map[string]any{"reason": "grace_period_ended"})
	}
	return done, failed
}

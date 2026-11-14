package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/secure"
)

// SendFormDigests tells the addresses a form nominates about new
// submissions: "immediate" on the scheduler's next minute, "hourly" and
// "daily" in one message per period. Quarantined and rejected submissions are
// never announced. The mail goes from the form's reply sender (or a sender of
// the environment), through the queue like any other message; if there is no
// sender the submissions stay pending and it is tried again.
func (s *Service) SendFormDigests(ctx context.Context) (int, error) {
	if !s.Opts.FormsEnabled {
		return 0, nil
	}
	rows, err := s.Pool.Query(ctx, `SELECT f.id::text, f.name, f.project_id::text, f.environment_id::text, p.name, e.mode, f.digest, f.notify_emails, f.reply_from, f.last_digest_at
FROM forms f JOIN projects p ON p.id=f.project_id JOIN environments e ON e.id=f.environment_id
WHERE f.enabled AND f.digest<>'none' AND cardinality(f.notify_emails)>0
  AND EXISTS (SELECT 1 FROM form_submissions x WHERE x.form_id=f.id AND x.notified_at IS NULL AND x.action IN ('allow','flag'))`)
	if err != nil {
		return 0, err
	}
	type job struct {
		id, name, projectID, envID, project, mode, digest, replyFrom string
		to                                                           []string
		last                                                         *time.Time
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.name, &j.projectID, &j.envID, &j.project, &j.mode, &j.digest, &j.to, &j.replyFrom, &j.last); err != nil {
			rows.Close()
			return 0, err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	sent := 0
	for _, j := range jobs {
		every := map[string]time.Duration{"immediate": 0, "hourly": time.Hour, "daily": 24 * time.Hour}[j.digest]
		if j.last != nil && time.Since(*j.last) < every {
			continue
		}
		n, err := s.sendFormDigest(ctx, j.id, j.name, j.projectID, j.envID, j.project, j.mode, j.digest, j.replyFrom, j.to)
		if err != nil {
			slog.Warn("form digest failed", "form", j.id, "err", err)
			continue
		}
		sent += n
	}
	return sent, nil
}

func (s *Service) sendFormDigest(ctx context.Context, formID, name, projectID, envID, project, mode, digest, replyFrom string, to []string) (int, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id::text, received_at, sender_masked, action, spam_score FROM form_submissions
WHERE form_id=$1 AND notified_at IS NULL AND action IN ('allow','flag') ORDER BY received_at LIMIT 200 FOR UPDATE SKIP LOCKED`, formID)
	if err != nil {
		return 0, err
	}
	type sub struct {
		id, sender, action string
		at                 time.Time
		score              int
	}
	var subs []sub
	for rows.Next() {
		var x sub
		if err := rows.Scan(&x.id, &x.at, &x.sender, &x.action, &x.score); err != nil {
			rows.Close()
			return 0, err
		}
		subs = append(subs, x)
	}
	rows.Close()
	if len(subs) == 0 {
		return 0, nil
	}
	var from, fromName string
	err = tx.QueryRow(ctx, `SELECT email, display_name FROM sender_identities WHERE environment_id=$1
ORDER BY (email=$2) DESC, is_default DESC, created_at LIMIT 1`, envID, replyFrom).Scan(&from, &fromName)
	if errors.Is(err, db.ErrNoRows) {
		return 0, errors.New("no sender address in this environment")
	}
	if err != nil {
		return 0, err
	}
	var b strings.Builder
	subject := fmt.Sprintf("%d new submission(s): %s", len(subs), name)
	fmt.Fprintf(&b, "%d new submission(s) to the form %q in %s (%s).\n\n", len(subs), name, project, mode)
	if len(subs) == 1 && digest == "immediate" {
		if fields, err := s.submissionFields(ctx, subs[0].id); err == nil {
			msg := fields["message"]
			if len(msg) > 500 {
				msg = msg[:500] + "…"
			}
			subject = "New message from " + subs[0].sender + ": " + name
			fmt.Fprintf(&b, "From: %s <%s>\n%s\n\n", fields["name"], fields["email"], msg)
		}
	}
	for _, x := range subs {
		flag := ""
		if x.action == "flag" {
			flag = "  (flagged as possible spam)"
		}
		fmt.Fprintf(&b, "- %s  %s  score %d%s\n", x.at.UTC().Format("2006-01-02 15:04"), x.sender, x.score, flag)
	}
	if pu := strings.TrimRight(s.Opts.PublicURL, "/"); pu != "" {
		fmt.Fprintf(&b, "\nOpen them: %s/p/%s/forms/submissions\n", pu, project)
	}
	text := b.String()
	for _, addr := range to {
		id := secure.NewUUID()
		body, _ := json.Marshal(Payload{From: from, FromName: fromName, To: addr, Template: "raw", Raw: true, Subject: clean(subject, 300), Text: text})
		sealed, err := s.Box.Seal(body, []byte(id))
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO messages (id, project_id, environment_id, template, template_version, state, payload_enc,
recipient_hash, request_hmac, idempotency_key, expires_at, locale, priority, source, sender, recipient_masked)
VALUES ($1,$2,$3,'raw',0,'queued',$4,$5,$6,$7, now() + interval '1 day', 'en', 'normal', 'automation', $8, $9)`,
			id, projectID, envID, sealed, s.Box.MAC("recipient", []byte(addr)), s.Box.MAC("request", body), "form-digest-"+id, from, maskEmail(addr)); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO message_events(message_id, event_type) VALUES ($1,'accepted')`, id); err != nil {
			return 0, err
		}
	}
	ids := make([]string, len(subs))
	for i, x := range subs {
		ids[i] = x.id
	}
	if _, err := tx.Exec(ctx, `UPDATE form_submissions SET notified_at=now() WHERE id = ANY($1::uuid[])`, ids); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE forms SET last_digest_at=now() WHERE id=$1`, formID); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(subs), nil
}

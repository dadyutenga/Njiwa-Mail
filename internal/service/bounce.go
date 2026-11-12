package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"github.com/njiwa-tz/mail/internal/db"
	"strings"
)

// BounceReport is one recipient of a parsed delivery status notification.
type BounceReport struct {
	// MessageID is OUR message id, taken from X-DM-Message-ID or Message-ID in
	// the returned headers. Empty when the DSN could not be correlated.
	MessageID  string
	Recipient  string // Final-Recipient address
	Action     string // failed, delayed, ...
	Status     string // enhanced status code, e.g. 5.1.1
	Diagnostic string
	// DSNID identifies the notification itself (its Message-ID, or a digest of
	// the raw message) so that redelivery of the same DSN is recognised.
	DSNID string
	// Complaint marks a feedback-loop (ARF) report: the recipient marked our message as spam.
	Complaint bool
}

// ClassifyBounce maps an enhanced status code to a bounce type.
//
//	5.1.x  bad mailbox or domain            hard    (recipient is suppressed)
//	4.x.x  temporary                        soft    (Mailcow keeps retrying)
//	5.7.x  policy, blocked, authentication  policy  (alert; never suppress)
//	other  including 5.2.x mailbox full     unknown (operator review)
func ClassifyBounce(status string) string {
	p := strings.Split(strings.TrimSpace(status), ".")
	if len(p) != 3 {
		return "unknown"
	}
	switch {
	case p[0] == "4":
		return "soft"
	case p[0] == "5" && p[1] == "1":
		return "hard"
	case p[0] == "5" && p[1] == "7":
		return "policy"
	}
	return "unknown"
}

// RecordBounce stores the reports from one DSN and completes its spool row.
//
// Handling is idempotent: a report already seen changes nothing. A recipient
// is suppressed only for a hard bounce that correlates to one of our messages
// AND names that message's own recipient, so a forged or misrouted DSN cannot
// suppress an arbitrary address. Policy bounces never suppress: they mean our
// DNS or reputation is wrong, not the recipient.
func (s *Service) RecordBounce(ctx context.Context, it SpoolItem, reports []BounceReport) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	verpID, verp := s.verifyVERP(ctx, it.To)
	for _, r := range reports {
		how := "none"
		switch {
		case verp == verpValid:
			r.MessageID, how = verpID, "verp" // the envelope we signed beats any header
		case verp == verpInvalid:
			r.MessageID, how = "", "verp_invalid" // forged or damaged: trust nothing from the headers either
		case uuidRe.MatchString(r.MessageID):
			how = "header"
		}
		if err := s.recordOne(ctx, tx, r, how); err != nil {
			return err
		}
	}
	if err := finishSpool(ctx, tx, it.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) recordOne(ctx context.Context, tx *db.Tx, r BounceReport, correlatedBy string) error {
	typ := ClassifyBounce(r.Status)
	if r.Action == "delayed" && typ != "soft" {
		typ = "unknown"
	}
	if r.Complaint {
		typ = "complaint"
	}
	var rcptHash []byte
	masked := ""
	if e, ok := NormalizeEmail(strings.ToLower(strings.TrimSpace(r.Recipient))); ok {
		rcptHash, masked = s.Box.MAC("recipient", []byte(e)), maskEmail(e)
	}

	var msgID, pid, eid *string
	var campaignID *string
	verified := false
	if uuidRe.MatchString(r.MessageID) {
		var p, e, mm string
		var mh []byte
		err := tx.QueryRow(ctx, `SELECT project_id::text, environment_id::text, recipient_hash, recipient_masked, campaign_id::text FROM messages WHERE id=$1 FOR UPDATE`, r.MessageID).Scan(&p, &e, &mh, &mm, &campaignID)
		if err == nil {
			msgID, pid, eid = &r.MessageID, &p, &e
			verified = rcptHash != nil && bytes.Equal(mh, rcptHash)
			if r.Complaint && rcptHash == nil {
				// Feedback loops usually redact the recipient; the report is
				// about this message, so it is about this message's recipient.
				rcptHash, masked, verified = mh, mm, true
			}
		} else if !errors.Is(err, db.ErrNoRows) {
			return err
		}
	}

	h := sha256.New()
	for _, part := range []string{r.DSNID, r.MessageID, strings.ToLower(r.Recipient), r.Status} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	var bounceID string
	if msgID == nil && correlatedBy != "verp_invalid" {
		correlatedBy = "none"
	}
	err := tx.QueryRow(ctx, `INSERT INTO bounces(message_id, project_id, environment_id, recipient_hash, recipient_masked, type, status_code, diagnostic, dedupe_hash, correlated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (dedupe_hash) DO NOTHING RETURNING id::text`,
		msgID, pid, eid, rcptHash, masked, typ, clean(r.Status, 20), clean(r.Diagnostic, 500), h.Sum(nil), correlatedBy).Scan(&bounceID)
	if errors.Is(err, db.ErrNoRows) {
		return nil // this DSN was already processed
	}
	if err != nil {
		return err
	}

	suppressed := false
	if msgID != nil {
		if typ == "hard" && verified {
			tag, err := tx.Exec(ctx, `INSERT INTO suppressions(project_id, environment_id, recipient_hash, recipient_masked, reason, source, bounce_id)
VALUES ($1,$2,$3,$4,$5,'hard_bounce',$6) ON CONFLICT (environment_id, recipient_hash) DO NOTHING`,
				*pid, *eid, rcptHash, masked, "hard bounce "+clean(r.Status, 20), bounceID)
			if err != nil {
				return err
			}
			suppressed = tag.RowsAffected() > 0
		}
		if typ == "complaint" && verified {
			tag, err := tx.Exec(ctx, `INSERT INTO suppressions(project_id, environment_id, recipient_hash, recipient_masked, reason, source, bounce_id)
VALUES ($1,$2,$3,$4,'spam complaint','complaint',$5) ON CONFLICT (environment_id, recipient_hash) DO NOTHING`, *pid, *eid, rcptHash, masked, bounceID)
			if err != nil {
				return err
			}
			suppressed = tag.RowsAffected() > 0
		}
		if campaignID != nil && (typ == "hard" || typ == "complaint") {
			if err := checkCampaignHealth(ctx, tx, *campaignID, typ); err != nil {
				return err
			}
		}
		if typ == "hard" || typ == "soft" {
			// hard is final; soft never downgrades a hard state.
			if _, err := tx.Exec(ctx, `UPDATE messages SET bounce_state=$2, updated_at=now() WHERE id=$1 AND (bounce_state='none' OR $2='hard')`, *msgID, typ); err != nil {
				return err
			}
		}
		ev := "bounced"
		if typ == "complaint" {
			ev = "complaint"
		}
		if err := addEvidence(ctx, tx, *msgID, "dsn", ev, clean(r.Status, 20), "", typ+" "+clean(r.Diagnostic, 160), "", dedupe("dsn", bounceID)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO message_events(message_id, event_type, reason_code) VALUES ($1,'bounced',$2)`,
			*msgID, typ+":"+clean(r.Status, 20)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT dm_emit($1::uuid, 'email.bounced', jsonb_build_object('message_id', $2::text, 'bounce_id', $3::text,
  'type', $4::text, 'status', $5::text, 'suppressed', $6::bool))`, *eid, *msgID, bounceID, typ, clean(r.Status, 20), suppressed); err != nil {
			return err
		}
	}
	if typ == "policy" {
		return raiseAlert(ctx, tx, "policy_bounce", "A recipient server rejected mail for policy reasons: check SPF, DKIM, DMARC, PTR and blocklists",
			map[string]any{"bounce_id": bounceID, "status": clean(r.Status, 20)})
	}
	return nil
}

func (s *Service) ListBounces(ctx context.Context, typ string, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT b.id::text AS id, b.message_id::text AS message_id, p.name AS project, e.mode, b.recipient_masked AS recipient,
  b.type, b.status_code, b.diagnostic, b.correlated_by, b.received_at
FROM bounces b LEFT JOIN projects p ON p.id=b.project_id LEFT JOIN environments e ON e.id=b.environment_id
WHERE ($1='' OR b.type=$1) ORDER BY b.received_at DESC`, typ)
}

package service

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"regexp"
	"strings"

	"github.com/njiwa-tz/mail/internal/db"
)

// Delivery evidence (backlog F01, E08). Each row says what was observed about
// a message after Njiwa handed it over, and where that came from.

var (
	queuedAsRe = regexp.MustCompile(`(?i)queued as ([0-9A-Za-z]{6,32})`)
	// postfix/smtp[123]: 92C4A8CB48: to=<...>, relay=mx.example.net[192.0.2.1]:25, ..., dsn=2.0.0, status=sent (250 2.0.0 OK)
	mtaLineRe = regexp.MustCompile(`postfix(?:/[a-z0-9_-]+)*/(?:smtp|lmtp|error|virtual|local)\[\d+\]: ([0-9A-Za-z]{6,32}): (.*)$`)
	relayRe   = regexp.MustCompile(`(?:^|, )relay=([^,\s]+)`)
	dsnRe     = regexp.MustCompile(`(?:^|, )dsn=(\d\.\d{1,3}\.\d{1,3})`)
	statusRe  = regexp.MustCompile(`(?:^|, )status=(sent|deferred|bounced|expired)(?: \((.*)\))?`)
	// postfix/qmgr[123]: 92C4A8CB48: from=<...>, status=expired, returned to sender
	expiredRe = regexp.MustCompile(`postfix(?:/[a-z0-9_-]+)*/qmgr\[\d+\]: ([0-9A-Za-z]{6,32}): from=<[^>]*>, status=expired`)
	emailRe   = regexp.MustCompile(`[^\s<>()"',;:]+@[^\s<>()"',;:]+`)
)

// QueueIDFromReply extracts the MTA queue id from a 250 reply ("queued as X").
func QueueIDFromReply(reply string) string {
	if m := queuedAsRe.FindStringSubmatch(reply); m != nil {
		return m[1]
	}
	return ""
}

// safeText removes addresses and bounds length, so evidence never stores
// recipient data or long remote text.
func safeText(s string, n int) string {
	s = emailRe.ReplaceAllString(s, "<address>")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		s = s[:n]
	}
	return s
}

func dedupe(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:16])
}

func addEvidence(ctx context.Context, tx *db.Tx, messageID, source, event, status, host, reason, queueID, key string) error {
	_, err := tx.Exec(ctx, `INSERT INTO delivery_events(message_id, source, event, smtp_status, remote_host, safe_reason, mta_queue_id, dedupe_key)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (dedupe_key) DO NOTHING`, messageID, source, event, status, host, safeText(reason, 200), queueID, key)
	return err
}

// MTALogEvent is one parsed Postfix delivery line.
type MTALogEvent struct {
	QueueID, Relay, DSN, Status, Text, Line string
}

// ParseMTALine parses one Postfix log line; ok is false for anything that is
// not a delivery status line.
func ParseMTALine(line string) (MTALogEvent, bool) {
	if x := expiredRe.FindStringSubmatch(line); x != nil {
		return MTALogEvent{QueueID: x[1], DSN: "4.4.7", Status: "expired", Text: "the message expired in the mail queue", Line: line}, true
	}
	m := mtaLineRe.FindStringSubmatch(line)
	if m == nil {
		return MTALogEvent{}, false
	}
	rest := m[2]
	st := statusRe.FindStringSubmatch(rest)
	d := dsnRe.FindStringSubmatch(rest)
	if st == nil || d == nil {
		return MTALogEvent{}, false
	}
	ev := MTALogEvent{QueueID: m[1], DSN: d[1], Status: st[1], Text: st[2], Line: line}
	if r := relayRe.FindStringSubmatch(rest); r != nil {
		ev.Relay = r[1]
		if i := strings.IndexByte(ev.Relay, '['); i > 0 {
			ev.Relay = ev.Relay[:i] // keep the host name, drop the address
		}
	}
	return ev, true
}

// IngestMTALog reads Postfix log lines and records delivery evidence for the
// messages they correlate to (by MTA queue id, within the last 7 days, newest
// first, since queue ids are eventually reused). Unrelated lines and unknown
// queue ids are ignored. It returns how many events were recorded.
func (s *Service) IngestMTALog(ctx context.Context, r io.Reader) (int, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		ev, ok := ParseMTALine(sc.Text())
		if !ok {
			continue
		}
		recorded, err := s.recordMTAEvent(ctx, ev)
		if err != nil {
			return n, err
		}
		if recorded {
			n++
		}
	}
	return n, sc.Err()
}

func (s *Service) recordMTAEvent(ctx context.Context, ev MTALogEvent) (bool, error) {
	var id, env string
	err := s.Pool.QueryRow(ctx, `SELECT id::text, environment_id::text FROM messages WHERE mta_queue_id=$1 AND submitted_at > now() - interval '7 days'
ORDER BY submitted_at DESC LIMIT 1`, ev.QueueID).Scan(&id, &env)
	if err != nil {
		return false, nil // not one of ours (or too old)
	}
	event := map[string]string{"sent": "remote_accepted", "deferred": "deferred", "bounced": "bounced", "expired": "expired_in_queue"}[ev.Status]
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `INSERT INTO delivery_events(message_id, source, event, smtp_status, remote_host, safe_reason, mta_queue_id, dedupe_key)
VALUES ($1,'mta_log',$2,$3,$4,$5,$6,$7) ON CONFLICT (dedupe_key) DO NOTHING`, id, event, ev.DSN, safeText(ev.Relay, 120), safeText(ev.Text, 200), ev.QueueID, dedupe("mta", ev.Line))
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if event == "bounced" || event == "expired_in_queue" {
		// The MTA gave up: the message is a bounce even when no DSN comes back.
		if _, err := tx.Exec(ctx, `UPDATE messages SET bounce_state='hard', updated_at=now() WHERE id=$1 AND $2 LIKE '5.%'`, id, ev.DSN); err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec(ctx, `SELECT dm_emit($1::uuid, $2, jsonb_build_object('message_id', $3::text, 'event', $4::text, 'status', $5::text))`,
		env, "email.delivery_"+event, id, event, ev.DSN); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// DeliveryEvidence lists the evidence for one message and a plain summary.
// The summary never claims inbox delivery.
func (s *Service) DeliveryEvidence(ctx context.Context, messageID string) ([]map[string]any, string, error) {
	rows, err := s.maps(ctx, `SELECT source, event, smtp_status, remote_host, safe_reason AS reason, mta_queue_id, observed_at
FROM delivery_events WHERE message_id=$1 ORDER BY observed_at, id`, messageID)
	if err != nil {
		return nil, "", err
	}
	summary := "No delivery evidence yet."
	for _, r := range rows {
		switch r["event"] {
		case "submitted":
			summary = "Accepted by your mail server."
			if q, _ := r["mta_queue_id"].(string); q != "" {
				summary = "Accepted by your mail server (queue ID " + q + ")."
			}
		case "deferred":
			summary = "The recipient's server has not accepted it yet; the mail server keeps retrying."
		case "remote_accepted":
			summary = "The recipient's mail server accepted it. This does not show whether it reached the inbox."
		case "bounced":
			summary = "The message bounced."
		case "complaint":
			summary = "The recipient reported it as spam."
		case "expired_in_queue":
			summary = "The mail server gave up retrying."
		case "operator_resolved":
			reason, _ := r["reason"].(string)
			summary = "An operator recorded the outcome: " + reason + "."
		}
	}
	return rows, summary, nil
}

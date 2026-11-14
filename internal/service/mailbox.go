package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
)

// Mailboxes are inbound_addresses on the configured inbound domain or on a
// verified receiving domain of the project. Temporary inboxes are mailboxes
// with an expiry; they and their mail are removed when they expire.

func (s *Service) receivingDomainOK(ctx context.Context, sc *Scope, domain string) error {
	if domain == "" {
		return badRequest("choose a domain")
	}
	if domain == s.Opts.InboundDomain && domain != "" {
		return nil
	}
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM domains WHERE project_id=$1 AND name=$2 AND purpose IN ('receiving','both') AND verified_at IS NOT NULL)`,
		sc.ProjectID, domain).Scan(&ok)
	if err != nil {
		return unavailable("domain_lookup", err)
	}
	if !ok {
		return &Error{Status: 409, Code: "domain_not_receiving", Message: domain + " is not a verified receiving domain of this project"}
	}
	return nil
}

type MailboxInput struct {
	LocalPart   string `json:"local_part"`
	Domain      string `json:"domain"`
	DisplayName string `json:"display_name"`
	CatchAll    bool   `json:"catch_all"`
}

func (s *Service) CreateMailbox(ctx context.Context, sc *Scope, in MailboxInput) (string, error) {
	domain := strings.ToLower(strings.TrimSpace(in.Domain))
	if err := s.receivingDomainOK(ctx, sc, domain); err != nil {
		return "", err
	}
	local := strings.ToLower(strings.TrimSpace(in.LocalPart))
	if in.CatchAll {
		local = "*"
	} else if !localPartRe.MatchString(local) {
		return "", badRequest("the address may use letters, digits, dot, dash, underscore or plus")
	}
	var id string
	err := s.Pool.QueryRow(ctx, `INSERT INTO inbound_addresses(project_id, environment_id, domain, local_part, route, display_name)
VALUES ($1,$2,$3,$4,'store',$5) ON CONFLICT (domain, local_part) DO NOTHING RETURNING id::text`,
		sc.ProjectID, sc.EnvID, domain, local, clean(strings.TrimSpace(in.DisplayName), 100)).Scan(&id)
	if errors.Is(err, db.ErrNoRows) {
		return "", &Error{Status: 409, Code: "address_exists", Message: local + "@" + domain + " already exists"}
	}
	if err != nil {
		return "", unavailable("mailbox_create", err)
	}
	return id, nil
}

func (s *Service) ListMailboxes(ctx context.Context, sc *Scope, temporary bool, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT a.id::text AS id, a.local_part || '@' || a.domain AS address, a.local_part, a.domain, a.display_name,
  a.enabled, a.route, a.forward_to, a.local_part='*' AS catch_all, a.expires_at, a.created_at,
  (SELECT count(*) FROM inbound_messages m WHERE m.address_id=a.id AND m.read_at IS NULL AND m.archived_at IS NULL) AS unread,
  (SELECT count(*) FROM inbound_messages m WHERE m.address_id=a.id) AS messages,
  (SELECT max(received_at) FROM inbound_messages m WHERE m.address_id=a.id) AS last_received_at,
  (SELECT count(*) FROM automation_rules r WHERE r.address_id=a.id) AS automations
FROM inbound_addresses a WHERE a.environment_id=$1 AND a.temporary=$2 ORDER BY a.domain, a.local_part`, sc.EnvID, temporary)
}

func (s *Service) SetMailboxEnabled(ctx context.Context, sc *Scope, id string, enabled bool) error {
	if err := s.InScope(ctx, sc, "inbound_addresses", id); err != nil {
		return err
	}
	_, err := s.Pool.Exec(ctx, `UPDATE inbound_addresses SET enabled=$2 WHERE id=$1`, id, enabled)
	return opErr(err)
}

// DeleteMailbox deletes the address, its stored mail and attachments.
func (s *Service) DeleteMailbox(ctx context.Context, sc *Scope, id string) error {
	if err := s.InScope(ctx, sc, "inbound_addresses", id); err != nil {
		return err
	}
	return s.deleteAddresses(ctx, `id=$1`, id)
}

func (s *Service) deleteAddresses(ctx context.Context, where string, args ...any) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `DELETE FROM inbound_messages WHERE address_id IN (SELECT id FROM inbound_addresses WHERE `+where+`) RETURNING id::text`, args...)
	if err != nil {
		return unavailable("mailbox_mail_delete", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if _, err := tx.Exec(ctx, `DELETE FROM inbound_addresses WHERE `+where, args...); err != nil {
		return unavailable("mailbox_delete", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable("commit", err)
	}
	for _, id := range ids {
		os.RemoveAll(s.attachmentDir(id))
	}
	return nil
}

// ---------------------------------------------------------------- temporary inboxes

var tempTTLs = map[string]time.Duration{"1h": time.Hour, "1d": 24 * time.Hour, "7d": 7 * 24 * time.Hour}

func (s *Service) CreateTempInbox(ctx context.Context, sc *Scope, ttl string) (map[string]any, error) {
	d, ok := tempTTLs[ttl]
	if !ok {
		return nil, badRequest("lifetime must be 1h, 1d or 7d")
	}
	if s.Opts.InboundDomain == "" {
		return nil, &Error{Status: 409, Code: "inbound_not_configured", Message: "temporary inboxes need INBOUND_DOMAIN on the server"}
	}
	var active, limit int
	if err := s.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM inbound_addresses WHERE environment_id=$1 AND temporary AND expires_at > now()),
  (SELECT temp_inbox_limit FROM projects WHERE id=$2)`, sc.EnvID, sc.ProjectID).Scan(&active, &limit); err != nil {
		return nil, unavailable("temp_count", err)
	}
	if active >= limit {
		return nil, &Error{Status: 409, Code: "temp_inbox_limit", Message: "this project already has its maximum number of active temporary inboxes; delete one first"}
	}
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		return nil, unavailable("random", err)
	}
	local := "tmp-" + hex.EncodeToString(b)
	var id string
	err := s.Pool.QueryRow(ctx, `INSERT INTO inbound_addresses(project_id, environment_id, domain, local_part, route, temporary, expires_at)
VALUES ($1,$2,$3,$4,'store',true, now() + make_interval(secs => $5::float8)) RETURNING id::text`,
		sc.ProjectID, sc.EnvID, s.Opts.InboundDomain, local, d.Seconds()).Scan(&id)
	if err != nil {
		return nil, unavailable("temp_create", err)
	}
	return map[string]any{"id": id, "address": local + "@" + s.Opts.InboundDomain}, nil
}

// ExpireTempInboxes deletes expired temporary inboxes and their mail.
func (s *Service) ExpireTempInboxes(ctx context.Context) error {
	return s.deleteAddresses(ctx, `temporary AND expires_at <= now()`)
}

// ---------------------------------------------------------------- inbox

type InboxFilter struct {
	Mailbox, Query, Label string
	Unread, Archived      bool
}

func (s *Service) Inbox(ctx context.Context, sc *Scope, f InboxFilter, pg Page) ([]map[string]any, bool, error) {
	if f.Mailbox != "" && !uuidRe.MatchString(f.Mailbox) {
		return nil, false, badRequest("unknown mailbox")
	}
	items, more, err := s.page(ctx, pg, `SELECT `+inboundCols+`, m.read_at, m.archived_at, to_jsonb(m.labels) AS labels,
  a.local_part || '@' || a.domain AS mailbox, a.temporary
FROM inbound_messages m LEFT JOIN inbound_addresses a ON a.id=m.address_id
WHERE m.environment_id=$1 AND ($2='' OR m.address_id=NULLIF($2,'')::uuid) AND (NOT $3 OR m.read_at IS NULL)
  AND (CASE WHEN $4 THEN m.archived_at IS NOT NULL ELSE m.archived_at IS NULL END)
  AND ($5='' OR m.subject ILIKE '%' || $5 || '%' OR m.header_from ILIKE '%' || $5 || '%' OR m.search_tsv @@ plainto_tsquery('simple', $5))
  AND ($6='' OR $6 = ANY(m.labels))
ORDER BY m.received_at DESC`, sc.EnvID, f.Mailbox, f.Unread, f.Archived, f.Query, f.Label)
	if err != nil || len(items) == 0 {
		return items, more, err
	}
	s.addSnippets(ctx, items)
	return items, more, nil
}

// addSnippets decrypts the start of each text body for the list view.
func (s *Service) addSnippets(ctx context.Context, items []map[string]any) {
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it["id"].(string))
	}
	rows, err := s.Pool.Query(ctx, `SELECT id::text, text_enc FROM inbound_messages WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return
	}
	defer rows.Close()
	snip := map[string]string{}
	for rows.Next() {
		var id string
		var enc []byte
		if rows.Scan(&id, &enc) != nil || enc == nil {
			continue
		}
		if pt, err := s.Box.Open(enc, []byte("inbound-text:"+id)); err == nil {
			t := strings.Join(strings.Fields(string(pt)), " ")
			if r := []rune(t); len(r) > 140 {
				t = string(r[:140]) + "…"
			}
			snip[id] = t
		}
	}
	for _, it := range items {
		it["snippet"] = snip[it["id"].(string)]
	}
}

// UnreadCounts returns unread counts per mailbox and in total.
func (s *Service) UnreadCounts(ctx context.Context, sc *Scope) (map[string]any, error) {
	rows, err := s.maps(ctx, `SELECT address_id::text AS mailbox, count(*) AS unread FROM inbound_messages
WHERE environment_id=$1 AND read_at IS NULL AND archived_at IS NULL GROUP BY address_id`, sc.EnvID)
	if err != nil {
		return nil, err
	}
	total := int64(0)
	per := map[string]any{}
	for _, r := range rows {
		n, _ := r["unread"].(int64)
		total += n
		if mb, ok := r["mailbox"].(string); ok {
			per[mb] = n
		}
	}
	return map[string]any{"total": total, "mailboxes": per}, nil
}

// OpenInbox returns one message with bodies and marks it read.
func (s *Service) OpenInbox(ctx context.Context, sc *Scope, id string) (map[string]any, error) {
	m, err := s.GetInbound(ctx, sc.EnvID, id, true)
	if err != nil {
		return nil, err
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE inbound_messages SET read_at=COALESCE(read_at, now()) WHERE id=$1 AND environment_id=$2`, id, sc.EnvID)
	extra, err := s.one(ctx, `SELECT m.read_at, m.archived_at, to_jsonb(m.labels) AS labels, m.in_reply_to_message_id::text AS in_reply_to,
  a.local_part || '@' || a.domain AS mailbox, a.id::text AS mailbox_id,
  (SELECT si.email FROM sender_identities si WHERE si.environment_id=m.environment_id AND split_part(si.email,'@',2)=a.domain LIMIT 1) AS reply_sender,
  EXISTS (SELECT 1 FROM blocked_senders b WHERE b.environment_id=m.environment_id AND b.sender=lower(m.envelope_from)) AS sender_blocked
FROM inbound_messages m LEFT JOIN inbound_addresses a ON a.id=m.address_id WHERE m.id=$1`, id)
	if err != nil {
		return nil, err
	}
	for k, v := range extra {
		m[k] = v
	}
	if m["automation_runs"], err = s.maps(ctx, `SELECT r.rule_id::text AS rule_id, ar.name, r.result, r.detail, r.at, r.message_id::text AS message_id
FROM automation_runs r JOIN automation_rules ar ON ar.id=r.rule_id WHERE r.inbound_id=$1 AND NOT r.dry_run ORDER BY r.at`, id); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Service) UpdateInbox(ctx context.Context, sc *Scope, id string, read, archived *bool) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE inbound_messages SET
  read_at = CASE WHEN $3::bool IS NULL THEN read_at WHEN $3 THEN COALESCE(read_at, now()) ELSE NULL END,
  archived_at = CASE WHEN $4::bool IS NULL THEN archived_at WHEN $4 THEN COALESCE(archived_at, now()) ELSE NULL END
WHERE id=$1 AND environment_id=$2`, id, sc.EnvID, read, archived)
	if err != nil {
		return unavailable("inbox_update", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

func (s *Service) DeleteInbox(ctx context.Context, sc *Scope, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM inbound_messages WHERE id=$1 AND environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return unavailable("inbox_delete", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	os.RemoveAll(s.attachmentDir(id))
	return nil
}

// BlockSender stops storing mail from the message's envelope sender in this
// environment, and archives what is already there from that sender.
func (s *Service) BlockSender(ctx context.Context, sc *Scope, id string) (string, error) {
	if !uuidRe.MatchString(id) {
		return "", newErr(404, "not_found")
	}
	var from string
	if err := s.Pool.QueryRow(ctx, `SELECT lower(envelope_from) FROM inbound_messages WHERE id=$1 AND environment_id=$2`, id, sc.EnvID).Scan(&from); err != nil {
		return "", notFound(err)
	}
	if from == "" {
		return "", badRequest("this message has no sender address to block")
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO blocked_senders(environment_id, sender) VALUES ($1,$2) ON CONFLICT DO NOTHING`, sc.EnvID, from); err != nil {
		return "", unavailable("block", err)
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE inbound_messages SET archived_at=COALESCE(archived_at, now()) WHERE environment_id=$1 AND lower(envelope_from)=$2`, sc.EnvID, from)
	return from, nil
}

func (s *Service) ListBlocked(ctx context.Context, sc *Scope) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT sender, created_at FROM blocked_senders WHERE environment_id=$1 ORDER BY created_at DESC`, sc.EnvID)
}

func (s *Service) Unblock(ctx context.Context, sc *Scope, sender string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM blocked_senders WHERE environment_id=$1 AND sender=lower($2)`, sc.EnvID, sender)
	return opErr(err)
}

func (s *Service) blocked(ctx context.Context, tx *db.Tx, envID, from string) (bool, error) {
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM blocked_senders WHERE environment_id=$1 AND sender=lower($2)`, envID, from).Scan(&one)
	if errors.Is(err, db.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// InboxEvent is one new inbound message for the live stream.
type InboxEvent struct {
	ID         string    `json:"id"`
	From       string    `json:"from"`
	Subject    string    `json:"subject"`
	AddressID  string    `json:"address_id,omitempty"`
	ReceivedAt time.Time `json:"received_at"`
}

// InboxSince returns messages received in the environment after the cursor
// (received_at, id), oldest first, at most limit.
func (s *Service) InboxSince(ctx context.Context, envID string, after time.Time, afterID string, limit int) ([]InboxEvent, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id::text, coalesce(header_from,''), coalesce(subject,''), coalesce(address_id::text,''), received_at
FROM inbound_messages WHERE environment_id=$1 AND (received_at, id::text) > ($2, $3)
ORDER BY received_at, id LIMIT $4`, envID, after, afterID, limit)
	if err != nil {
		return nil, unavailable("inbox_stream", err)
	}
	defer rows.Close()
	var out []InboxEvent
	for rows.Next() {
		var e InboxEvent
		if err := rows.Scan(&e.ID, &e.From, &e.Subject, &e.AddressID, &e.ReceivedAt); err != nil {
			return nil, unavailable("inbox_stream", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

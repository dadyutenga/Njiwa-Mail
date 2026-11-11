package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/njiwa-tz/mail/internal/db"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/secure"
)

// Inbound mail arrives from Mailcow at the private SMTP listener. The raw
// message is written to disk and recorded in inbound_spool before the listener
// answers 250; parsing happens afterwards so a parser fault cannot lose mail.

const (
	KindInbound = "inbound"
	KindBounce  = "bounce"

	spoolMaxAttempts = 5
)

var localPartRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9._+-]{0,62}[a-z0-9])?$`)

// clean makes a string safe for PostgreSQL text and JSON: valid UTF-8, no NUL.
func clean(s string, max int) string {
	s = strings.ReplaceAll(strings.ToValidUTF8(s, "�"), "\x00", "")
	if max > 0 && len(s) > max {
		s = strings.ToValidUTF8(s[:max], "")
	}
	return s
}

func splitAddr(addr string) (local, domain string, ok bool) {
	a := strings.ToLower(strings.TrimSpace(addr))
	at := strings.LastIndexByte(a, '@')
	if at < 1 || at == len(a)-1 {
		return "", "", false
	}
	return a[:at], a[at+1:], true
}

type inboundRoute struct {
	AddressID, ProjectID, EnvironmentID string
	EndpointID                          *string
	Route, ForwardTo                    string
}

// route finds the enabled inbound address for a recipient: an exact local
// part wins over the domain wildcard.
func (s *Service) route(ctx context.Context, addr string) (*inboundRoute, error) {
	local, domain, ok := splitAddr(addr)
	if !ok {
		return nil, nil
	}
	var r inboundRoute
	err := s.Pool.QueryRow(ctx, `SELECT a.id::text, a.project_id::text, a.environment_id::text, a.webhook_endpoint_id::text, a.route, a.forward_to
FROM inbound_addresses a JOIN environments e ON e.id=a.environment_id JOIN projects p ON p.id=a.project_id
WHERE a.domain=$1 AND a.local_part IN ($2,'*') AND a.enabled AND e.enabled AND p.enabled
ORDER BY (a.local_part='*') LIMIT 1`, domain, local).Scan(&r.AddressID, &r.ProjectID, &r.EnvironmentID, &r.EndpointID, &r.Route, &r.ForwardTo)
	if errors.Is(err, db.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ResolveRecipient classifies an SMTP recipient: KindBounce for the bounce
// address, KindInbound for an enabled inbound address, "" for unknown (the
// listener rejects those at RCPT TO so Mailcow bounces them).
func (s *Service) ResolveRecipient(ctx context.Context, addr string) (string, error) {
	a := strings.ToLower(strings.TrimSpace(addr))
	if s.Opts.BounceAddress != "" && a == s.Opts.BounceAddress {
		return KindBounce, nil
	}
	local, domain, ok := splitAddr(a)
	if !ok || s.Opts.InboundDomain == "" || domain != s.Opts.InboundDomain {
		return "", nil
	}
	// The bounce mailbox may reach us through a Mailcow alias onto the relayed
	// inbound domain (bounces@notify.<domain> -> bounces@in.<domain>).
	if bl, _, ok := splitAddr(s.Opts.BounceAddress); ok && local == bl {
		return KindBounce, nil
	}
	r, err := s.route(ctx, a)
	if err != nil || r == nil {
		return "", err
	}
	return KindInbound, nil
}

// SpoolFile returns the absolute path for a spooled raw message.
func (s *Service) SpoolFile(id string) string {
	return filepath.Join(s.Opts.DataDir, "spool", id+".eml")
}

func (s *Service) attachmentDir(messageID string) string {
	return filepath.Join(s.Opts.DataDir, "attachments", messageID)
}

// Spool records a raw message already written (and synced) to SpoolFile(id).
func (s *Service) Spool(ctx context.Context, id, kind, from string, to []string, size int) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO inbound_spool(id, kind, envelope_from, envelope_to, path, size) VALUES ($1,$2,$3,$4,$5,$6)`,
		id, kind, clean(strings.ToLower(from), 320), to, filepath.Join("spool", id+".eml"), size)
	return err
}

type SpoolItem struct {
	ID, Kind, From string
	To             []string
	Size, Attempts int
}

// ClaimSpool leases raw messages for parsing. A lease that expired (the parser
// process died) is claimed again, up to a bounded number of attempts.
func (s *Service) ClaimSpool(ctx context.Context, limit int, lease time.Duration) ([]SpoolItem, error) {
	rows, err := s.Pool.Query(ctx, `
WITH c AS (SELECT id FROM inbound_spool
  WHERE state='pending' OR (state='processing' AND lease_expires_at < now())
  ORDER BY received_at LIMIT $1 FOR UPDATE SKIP LOCKED)
UPDATE inbound_spool sp SET state='processing', attempts=sp.attempts+1, lease_expires_at=now() + make_interval(secs => $2::float8)
FROM c WHERE sp.id=c.id RETURNING sp.id::text, sp.kind, sp.envelope_from, sp.envelope_to, sp.size, sp.attempts`, limit, lease.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SpoolItem
	for rows.Next() {
		var it SpoolItem
		if err := rows.Scan(&it.ID, &it.Kind, &it.From, &it.To, &it.Size, &it.Attempts); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// FailSpool records a parse failure. The raw file is kept for operator review
// until retention; after the final attempt the row stays in state failed.
func (s *Service) FailSpool(ctx context.Context, it SpoolItem, code string) error {
	state := "pending"
	if it.Attempts >= spoolMaxAttempts {
		state = "failed"
		slog.Error("inbound message could not be parsed; raw file kept", "spool", it.ID, "reason", code)
	}
	_, err := s.Pool.Exec(ctx, `UPDATE inbound_spool SET state=$2, error_code=$3, lease_expires_at=NULL WHERE id=$1 AND state='processing'`, it.ID, state, code)
	return err
}

func finishSpool(ctx context.Context, tx *db.Tx, id string) error {
	_, err := tx.Exec(ctx, `UPDATE inbound_spool SET state='done', error_code='', lease_expires_at=NULL WHERE id=$1`, id)
	return err
}

type ParsedAttachment struct {
	Filename, ContentType string
	Content               []byte
}

type ParsedInbound struct {
	HeaderFrom, Subject, Text, HTML string
	Headers                         map[string][]string
	SpamScore                       *float64
	Attachments                     []ParsedAttachment
}

// StoreInbound persists a parsed message once per recipient that still routes
// to an enabled address, writes attachments outside any web root under
// generated names, emits inbound.received and completes the spool row, all in
// one transaction. It is idempotent per (spool, recipient).
func (s *Service) StoreInbound(ctx context.Context, it SpoolItem, p *ParsedInbound) (stored int, err error) {
	hj, _ := json.Marshal(p.Headers)
	retention := time.Duration(s.settingInt(ctx, "inbound_retention_days")) * 24 * time.Hour
	var dirs []string
	defer func() {
		if err != nil {
			for _, d := range dirs {
				os.RemoveAll(d)
			}
		}
	}()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	for _, rcpt := range it.To {
		r, err := s.route(ctx, rcpt)
		if err != nil {
			return 0, err
		}
		if r == nil {
			continue // address was disabled between RCPT and parsing
		}
		id := secure.NewUUID()
		var textEnc, htmlEnc []byte
		if p.Text != "" {
			if textEnc, err = s.Box.Seal([]byte(p.Text), []byte("inbound-text:"+id)); err != nil {
				return 0, err
			}
		}
		if p.HTML != "" {
			if htmlEnc, err = s.Box.Seal([]byte(p.HTML), []byte("inbound-html:"+id)); err != nil {
				return 0, err
			}
		}
		tag, err := tx.Exec(ctx, `INSERT INTO inbound_messages(id, project_id, environment_id, address_id, spool_id, envelope_from, envelope_to,
  header_from, subject, text_enc, html_enc, headers, spam_score, raw_size, retention_expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14, now() + make_interval(secs => $15::float8))
ON CONFLICT (spool_id, envelope_to) DO NOTHING`,
			id, r.ProjectID, r.EnvironmentID, r.AddressID, it.ID, it.From, strings.ToLower(rcpt),
			clean(p.HeaderFrom, 500), clean(p.Subject, 1000), textEnc, htmlEnc, hj, p.SpamScore, it.Size, retention.Seconds())
		if err != nil {
			return 0, err
		}
		if tag.RowsAffected() == 0 {
			continue // already stored by an earlier, interrupted attempt
		}
		if len(p.Attachments) > 0 {
			dir := s.attachmentDir(id)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return 0, err
			}
			dirs = append(dirs, dir)
			for i, a := range p.Attachments {
				name := fmt.Sprintf("%02d-%s", i, secure.NewUUID()) // generated: the sender's filename never touches the filesystem
				if err := os.WriteFile(filepath.Join(dir, name), a.Content, 0o600); err != nil {
					return 0, err
				}
				sum := sha256.Sum256(a.Content)
				if _, err := tx.Exec(ctx, `INSERT INTO inbound_attachments(inbound_message_id, filename, content_type, size, storage_path, sha256) VALUES ($1,$2,$3,$4,$5,$6)`,
					id, clean(a.Filename, 255), clean(a.ContentType, 200), len(a.Content), filepath.Join("attachments", id, name), hex.EncodeToString(sum[:])); err != nil {
					return 0, err
				}
			}
		}
		ref, dmID := headerOne(p.Headers, "In-Reply-To"), headerOne(p.Headers, "X-DM-Message-ID")
		if _, err := tx.Exec(ctx, `UPDATE inbound_messages SET search_tsv=to_tsvector('simple', $2),
in_reply_to_message_id=(SELECT id FROM messages WHERE smtp_message_id=$3 OR id::text=$4 LIMIT 1) WHERE id=$1`,
			id, clean(p.Subject+" "+p.HeaderFrom+" "+p.Text, 8000), strings.Trim(ref, "<> "), dmID); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `SELECT dm_emit($1::uuid, 'inbound.received', jsonb_build_object('inbound_id', $2::text, 'address_id', $3::text,
  'attachment_count', $4::int, 'raw_size', $5::int), $6::uuid)`, r.EnvironmentID, id, r.AddressID, len(p.Attachments), it.Size, r.EndpointID); err != nil {
			return 0, err
		}
		if r.Route == "forward" && r.ForwardTo != "" {
			if err := s.queueForward(ctx, tx, r, p); err != nil {
				return 0, err
			}
		}
		stored++
	}
	if err := finishSpool(ctx, tx, it.ID); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	os.Remove(s.SpoolFile(it.ID)) // parsed copy is authoritative now
	return stored, nil
}

// inboundRetention removes inbound bodies and attachments past their
// retention period, then old metadata and finished or abandoned spool entries.
func (s *Service) inboundRetention(ctx context.Context) (purged, spool int64, err error) {
	rows, err := s.Pool.Query(ctx, `UPDATE inbound_messages SET text_enc=NULL, html_enc=NULL, bodies_purged_at=now()
WHERE bodies_purged_at IS NULL AND retention_expires_at < now() RETURNING id::text`)
	if err != nil {
		return 0, 0, err
	}
	ids, err := db.CollectStrings(rows)
	if err != nil {
		return 0, 0, err
	}
	for _, id := range ids {
		if s.Opts.DataDir != "" {
			os.RemoveAll(s.attachmentDir(id))
		}
	}
	if len(ids) > 0 {
		if _, err := s.Pool.Exec(ctx, `DELETE FROM inbound_attachments WHERE inbound_message_id = ANY($1::uuid[])`, ids); err != nil {
			return 0, 0, err
		}
	}
	if _, err := s.Pool.Exec(ctx, `DELETE FROM inbound_messages WHERE bodies_purged_at IS NOT NULL AND received_at < now() - make_interval(secs => $1::float8)`,
		s.Opts.MetadataRetention.Seconds()); err != nil {
		return 0, 0, err
	}
	days := s.settingInt(ctx, "inbound_retention_days")
	rows, err = s.Pool.Query(ctx, `DELETE FROM inbound_spool WHERE (state='done' AND received_at < now() - interval '1 day')
   OR (state='failed' AND received_at < now() - make_interval(days => $1)) RETURNING id::text`, days)
	if err != nil {
		return 0, 0, err
	}
	sids, err := db.CollectStrings(rows)
	if err != nil {
		return 0, 0, err
	}
	for _, id := range sids {
		if s.Opts.DataDir != "" {
			os.Remove(s.SpoolFile(id))
		}
	}
	return int64(len(ids)), int64(len(sids)), nil
}

// ---------------------------------------------------------------- reads

const inboundCols = `m.id::text AS id, m.address_id::text AS address_id, m.envelope_from, m.envelope_to, m.header_from, m.subject,
  m.spam_score, m.raw_size, m.received_at, m.retention_expires_at, m.bodies_purged_at,
  (SELECT count(*) FROM inbound_attachments a WHERE a.inbound_message_id=m.id) AS attachment_count`

// ListInbound lists inbound messages. envID "" lists every environment (admin
// only); project and mode filter by name.
func headerOne(h map[string][]string, name string) string {
	for k, v := range h {
		if strings.EqualFold(k, name) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

// queueForward records a raw queued message to the address's forward target.
// It uses the environment's first approved sender. No sender means the copy is skipped.
func (s *Service) queueForward(ctx context.Context, tx *db.Tx, r *inboundRoute, p *ParsedInbound) error {
	var from, name string
	err := tx.QueryRow(ctx, `SELECT email, display_name FROM sender_identities WHERE environment_id=$1 LIMIT 1`, r.EnvironmentID).Scan(&from, &name)
	if errors.Is(err, db.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	to, ok := NormalizeEmail(r.ForwardTo)
	if !ok {
		return nil
	}
	id := secure.NewUUID()
	body, _ := json.Marshal(Payload{From: from, FromName: name, To: to, Template: "raw", Raw: true, Subject: "Fwd: " + clean(p.Subject, 200), Text: p.Text, HTML: p.HTML})
	sealed, err := s.Box.Seal(body, []byte(id))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO messages (id, project_id, environment_id, template, template_version, state, payload_enc,
recipient_hash, request_hmac, idempotency_key, expires_at, locale, priority)
VALUES ($1,$2,$3,'raw',0,'queued',$4,$5,$6,$7, now() + interval '1 day', 'en', 'normal')`,
		id, r.ProjectID, r.EnvironmentID, sealed, s.Box.MAC("recipient", []byte(to)), s.Box.MAC("request", body), "fwd-"+id)
	return err
}

func (s *Service) ListInbound(ctx context.Context, envID, project, mode, query string, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT `+inboundCols+`, p.name AS project, e.mode, m.in_reply_to_message_id::text AS in_reply_to
FROM inbound_messages m JOIN projects p ON p.id=m.project_id JOIN environments e ON e.id=m.environment_id
WHERE ($1='' OR m.environment_id=NULLIF($1,'')::uuid) AND ($2='' OR p.name=$2) AND ($3='' OR e.mode=$3)
  AND ($4='' OR m.subject ILIKE '%' || $4 || '%' OR m.header_from ILIKE '%' || $4 || '%' OR m.search_tsv @@ plainto_tsquery('simple', $4))
ORDER BY m.received_at DESC`, envID, project, mode, query)
}

// GetInbound returns one inbound message. Bodies are decrypted only when
// withBodies is set (API keys with inbound:read, and admin or owner users).
func (s *Service) GetInbound(ctx context.Context, envID, id string, withBodies bool) (map[string]any, error) {
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	m, err := s.one(ctx, `SELECT `+inboundCols+`, m.headers, p.name AS project, e.mode
FROM inbound_messages m JOIN projects p ON p.id=m.project_id JOIN environments e ON e.id=m.environment_id
WHERE m.id=$1 AND ($2='' OR m.environment_id=NULLIF($2,'')::uuid)`, id, envID)
	if err != nil {
		return nil, err
	}
	atts, err := s.maps(ctx, `SELECT id::text AS id, filename, content_type, size, sha256 FROM inbound_attachments WHERE inbound_message_id=$1 ORDER BY storage_path`, id)
	if err != nil {
		return nil, err
	}
	m["attachments"] = atts
	if !withBodies {
		return m, nil
	}
	var textEnc, htmlEnc []byte
	if err := s.Pool.QueryRow(ctx, `SELECT text_enc, html_enc FROM inbound_messages WHERE id=$1`, id).Scan(&textEnc, &htmlEnc); err != nil {
		return nil, notFound(err)
	}
	m["text"], m["html"] = "", ""
	if textEnc != nil {
		if pt, err := s.Box.Open(textEnc, []byte("inbound-text:"+id)); err == nil {
			m["text"] = string(pt)
		}
	}
	if htmlEnc != nil {
		if pt, err := s.Box.Open(htmlEnc, []byte("inbound-html:"+id)); err == nil {
			m["html"] = string(pt)
		}
	}
	return m, nil
}

type AttachmentFile struct {
	Path, Filename, ContentType string
	Size                        int
}

// OpenAttachment resolves an attachment to its file on disk, scoped to the
// environment when envID is set.
func (s *Service) OpenAttachment(ctx context.Context, envID, messageID, attachmentID string) (*AttachmentFile, error) {
	if !uuidRe.MatchString(messageID) || !uuidRe.MatchString(attachmentID) {
		return nil, newErr(404, "not_found")
	}
	var f AttachmentFile
	var rel string
	err := s.Pool.QueryRow(ctx, `SELECT a.storage_path, a.filename, a.content_type, a.size FROM inbound_attachments a
JOIN inbound_messages m ON m.id=a.inbound_message_id
WHERE a.id=$1 AND m.id=$2 AND ($3='' OR m.environment_id=NULLIF($3,'')::uuid)`, attachmentID, messageID, envID).Scan(&rel, &f.Filename, &f.ContentType, &f.Size)
	if err != nil {
		return nil, notFound(err)
	}
	if s.Opts.DataDir == "" {
		return nil, newErr(404, "not_found")
	}
	full, err := attachmentPath(s.Opts.DataDir, rel)
	if err != nil {
		return nil, newErr(404, "not_found")
	}
	f.Path = full
	return &f, nil
}

// attachmentPath resolves a stored relative path and refuses anything that
// would leave the data directory.
func attachmentPath(root, rel string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
		return "", errors.New("path escapes data dir")
	}
	full := filepath.Join(root, rel)
	prefix := root + string(os.PathSeparator)
	if full != root && !strings.HasPrefix(full, prefix) {
		return "", errors.New("path escapes data dir")
	}
	return full, nil
}

// ---------------------------------------------------------------- addresses

// CreateInboundAddress adds local@<inbound domain>; local "*" catches every
// address under the domain not claimed by an exact entry.
func (s *Service) CreateInboundAddress(ctx context.Context, project, mode, local string, endpointID *string, route, forwardTo string) (string, error) {
	if s.Opts.InboundDomain == "" {
		return "", newErr(409, "inbound_not_configured")
	}
	local = strings.ToLower(strings.TrimSpace(local))
	if local != "*" && !localPartRe.MatchString(local) {
		return "", badRequest("local part must be letters, digits, dot, dash, underscore or plus, or * for a wildcard")
	}
	if route == "" {
		route = "store"
	}
	if route != "store" && route != "webhook" && route != "forward" {
		return "", badRequest("route must be store, webhook or forward")
	}
	if route == "forward" {
		if _, ok := NormalizeEmail(forwardTo); !ok {
			return "", badRequest("forward_to must be an email address")
		}
	}
	pid, eid, err := s.envID(ctx, project, mode)
	if err != nil {
		return "", newErr(404, "not_found")
	}
	if err := s.checkEndpoint(ctx, eid, endpointID); err != nil {
		return "", err
	}
	var id string
	err = s.Pool.QueryRow(ctx, `INSERT INTO inbound_addresses(project_id, environment_id, domain, local_part, webhook_endpoint_id, route, forward_to)
VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (domain, local_part) DO NOTHING RETURNING id::text`, pid, eid, s.Opts.InboundDomain, local, endpointID, route, strings.ToLower(forwardTo)).Scan(&id)
	if errors.Is(err, db.ErrNoRows) {
		return "", newErr(409, "address_exists")
	}
	if err != nil {
		return "", unavailable("address_create", err)
	}
	return id, nil
}

func (s *Service) checkEndpoint(ctx context.Context, envID string, endpointID *string) error {
	if endpointID == nil || *endpointID == "" {
		return nil
	}
	if !uuidRe.MatchString(*endpointID) {
		return badRequest("unknown webhook endpoint")
	}
	var one int
	err := s.Pool.QueryRow(ctx, `SELECT 1 FROM webhook_endpoints WHERE id=$1 AND environment_id=$2`, *endpointID, envID).Scan(&one)
	if errors.Is(err, db.ErrNoRows) {
		return badRequest("webhook endpoint must belong to the same project environment")
	}
	return notFound(err)
}

// UpdateInboundAddress enables or disables an address and sets or clears
// (empty string) its webhook endpoint.
func (s *Service) UpdateInboundAddress(ctx context.Context, id string, enabled *bool, endpointID *string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	var eid string
	if err := s.Pool.QueryRow(ctx, `SELECT environment_id::text FROM inbound_addresses WHERE id=$1`, id).Scan(&eid); err != nil {
		return notFound(err)
	}
	if err := s.checkEndpoint(ctx, eid, endpointID); err != nil {
		return err
	}
	_, err := s.Pool.Exec(ctx, `UPDATE inbound_addresses SET enabled=COALESCE($2,enabled),
webhook_endpoint_id=CASE WHEN $3::text IS NULL THEN webhook_endpoint_id ELSE NULLIF($3,'')::uuid END WHERE id=$1`, id, enabled, endpointID)
	if err != nil {
		return unavailable("address_update", err)
	}
	return nil
}

func (s *Service) DeleteInboundAddress(ctx context.Context, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM inbound_addresses WHERE id=$1`, id)
	if err != nil {
		return unavailable("address_delete", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

func (s *Service) ListInboundAddresses(ctx context.Context, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT a.id::text AS id, a.local_part || '@' || a.domain AS address, a.local_part, a.domain, a.enabled,
  a.route, a.forward_to, a.webhook_endpoint_id::text AS webhook_endpoint_id, p.name AS project, e.mode, a.created_at
FROM inbound_addresses a JOIN projects p ON p.id=a.project_id JOIN environments e ON e.id=a.environment_id
ORDER BY a.domain, a.local_part`)
}

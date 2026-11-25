package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
)

// AuditFilter narrows the audit log. Every field is optional.
type AuditFilter struct {
	Actor  string     // exact actor
	Action string     // action prefix, such as "user." or "domain.create"
	Query  string     // substring of actor, action, target type or target id
	From   *time.Time // at >= From
	To     *time.Time // at < To
}

// MaxAuditExport caps one export so a download never exhausts the server.
const MaxAuditExport = 50000

func likeEscape(v string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(v)
}

func (f AuditFilter) where() (string, []any) {
	pattern := ""
	if f.Query != "" {
		pattern = "%" + likeEscape(f.Query) + "%"
	}
	action := ""
	if f.Action != "" {
		action = likeEscape(f.Action) + "%"
	}
	return `WHERE ($1='' OR actor=$1)
  AND ($2='' OR action LIKE $2 ESCAPE '\')
  AND ($3='' OR actor ILIKE $3 ESCAPE '\' OR action ILIKE $3 ESCAPE '\' OR target_type ILIKE $3 ESCAPE '\' OR target_id ILIKE $3 ESCAPE '\')
  AND ($4::timestamptz IS NULL OR at >= $4)
  AND ($5::timestamptz IS NULL OR at < $5)`, []any{f.Actor, action, pattern, f.From, f.To}
}

const auditColumns = `id, seq, at, user_id::text AS user_id, api_key_id::text AS api_key_id, actor, action, target_type, target_id, details, encode(hash, 'hex') AS hash`

// SealAudit chains every committed unsealed entry (audit_seal in a short transaction of
// its own). Entries written inside a caller's transaction become visible only when it
// commits, so readers seal first as well.
func (s *Service) SealAudit(ctx context.Context) error {
	for {
		var n int
		if err := s.Pool.QueryRow(ctx, `SELECT audit_seal()`).Scan(&n); err != nil {
			return unavailable("audit_seal", err)
		}
		if n < 10000 {
			return nil
		}
	}
}

// ListAuditFiltered pages through the audit log, newest first.
func (s *Service) ListAuditFiltered(ctx context.Context, f AuditFilter, pg Page) ([]map[string]any, bool, error) {
	if err := s.SealAudit(ctx); err != nil {
		return nil, false, err
	}
	where, args := f.where()
	return s.page(ctx, pg, `SELECT `+auditColumns+` FROM audit_log `+where+` ORDER BY seq DESC`, args...)
}

// AuditEntry is one row of an export.
type AuditEntry struct {
	Seq        int64          `json:"seq"`
	At         time.Time      `json:"at"`
	UserID     string         `json:"user_id,omitempty"`
	APIKeyID   string         `json:"api_key_id,omitempty"`
	Actor      string         `json:"actor"`
	Action     string         `json:"action"`
	TargetType string         `json:"target_type"`
	TargetID   string         `json:"target_id"`
	Details    map[string]any `json:"details"`
	Hash       string         `json:"hash"`
	PrevHash   string         `json:"prev_hash"`
}

// ExportAudit returns the matching entries oldest first, at most MaxAuditExport.
// truncated reports that more entries matched.
func (s *Service) ExportAudit(ctx context.Context, f AuditFilter) (out []AuditEntry, truncated bool, err error) {
	if err := s.SealAudit(ctx); err != nil {
		return nil, false, err
	}
	where, args := f.where()
	rows, err := s.Pool.Query(ctx, `SELECT seq, at, coalesce(user_id::text,''), coalesce(api_key_id::text,''), actor, action,
  target_type, target_id, details, encode(hash,'hex'), encode(prev_hash,'hex')
FROM audit_log `+where+` ORDER BY seq LIMIT $6`, append(args, MaxAuditExport+1)...)
	if err != nil {
		return nil, false, unavailable("audit_export", err)
	}
	defer rows.Close()
	for rows.Next() {
		var e AuditEntry
		var details []byte
		if err := rows.Scan(&e.Seq, &e.At, &e.UserID, &e.APIKeyID, &e.Actor, &e.Action, &e.TargetType, &e.TargetID,
			&details, &e.Hash, &e.PrevHash); err != nil {
			return nil, false, unavailable("audit_export", err)
		}
		if json.Unmarshal(details, &e.Details) != nil || e.Details == nil {
			e.Details = map[string]any{}
		}
		e.At = e.At.UTC()
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, false, unavailable("audit_export", err)
	}
	if len(out) > MaxAuditExport {
		return out[:MaxAuditExport], true, nil
	}
	return out, false, nil
}

// AuditIntegrity is the result of walking the hash chain.
type AuditIntegrity struct {
	OK         bool      `json:"ok"`
	Entries    int64     `json:"entries"`
	FirstSeq   int64     `json:"first_seq"`
	LastSeq    int64     `json:"last_seq"`
	HeadHash   string    `json:"head_hash"`
	AnchorSeq  int64     `json:"anchor_seq"`
	AnchorHash string    `json:"anchor_hash"`
	CheckedAt  time.Time `json:"checked_at"`
	// When OK is false: the first entry that does not verify, and why.
	BrokenSeq int64  `json:"broken_seq,omitempty"`
	Problem   string `json:"problem,omitempty"`
}

// VerifyAudit recomputes every hash and link. It detects an entry that was changed,
// inserted or removed anywhere in the chain, including the newest ones (via the head).
func (s *Service) VerifyAudit(ctx context.Context) (AuditIntegrity, error) {
	r := AuditIntegrity{CheckedAt: time.Now().UTC()}
	if err := s.SealAudit(ctx); err != nil {
		return r, err
	}
	var head, anchor []byte
	var headSeq int64
	err := s.Pool.QueryRow(ctx, `SELECT seq, hash, anchor_seq, anchor_hash FROM audit_chain_head`).Scan(&headSeq, &head, &r.AnchorSeq, &anchor)
	if errors.Is(err, db.ErrNoRows) {
		return r, unavailable("audit_verify", errors.New("audit chain head missing"))
	}
	if err != nil {
		return r, unavailable("audit_verify", err)
	}
	r.HeadHash, r.AnchorHash = hex.EncodeToString(head), hex.EncodeToString(anchor)
	var broken *int64
	var problem *string
	var first, last *int64
	var lastHash []byte
	err = s.Pool.QueryRow(ctx, `WITH c AS (
  SELECT seq, hash,
    hash = audit_entry_hash(prev_hash, seq, at, user_id, api_key_id, actor, action, target_type, target_id, details) AS hash_ok,
    prev_hash = coalesce(lag(hash) OVER w, $1) AS link_ok,
    seq = coalesce(lag(seq) OVER w, $2) + 1 AS seq_ok
  FROM audit_log WHERE seq IS NOT NULL WINDOW w AS (ORDER BY seq))
SELECT count(*), min(seq), max(seq), (array_agg(hash ORDER BY seq DESC))[1],
  min(seq) FILTER (WHERE NOT (hash_ok AND link_ok AND seq_ok)),
  (array_agg(CASE WHEN NOT seq_ok THEN 'entries are missing before this one'
                  WHEN NOT link_ok THEN 'the link to the previous entry does not match'
                  ELSE 'the entry was changed after it was written' END ORDER BY seq)
     FILTER (WHERE NOT (hash_ok AND link_ok AND seq_ok)))[1]
FROM c`, anchor, r.AnchorSeq).Scan(&r.Entries, &first, &last, &lastHash, &broken, &problem)
	if err != nil {
		return r, unavailable("audit_verify", err)
	}
	if first != nil {
		r.FirstSeq, r.LastSeq = *first, *last
	}
	switch {
	case broken != nil:
		r.BrokenSeq, r.Problem = *broken, *problem
	case r.Entries == 0 && headSeq != r.AnchorSeq:
		r.BrokenSeq, r.Problem = r.AnchorSeq+1, "every entry after the retention anchor is missing"
	case r.Entries > 0 && (r.LastSeq != headSeq || hex.EncodeToString(lastHash) != r.HeadHash):
		r.BrokenSeq, r.Problem = r.LastSeq+1, "the newest entries are missing"
	default:
		r.OK = true
	}
	return r, nil
}

// purgeAudit deletes entries older than the retention period, oldest first, and moves the
// chain anchor to the last deleted entry so the rest still verifies. It records what it did.
func (s *Service) purgeAudit(ctx context.Context, days int) (int64, error) {
	if err := s.SealAudit(ctx); err != nil {
		return 0, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL njiwa.audit_retention = 'on'`); err != nil {
		return 0, err
	}
	var cut *int64
	if err := tx.QueryRow(ctx, `SELECT max(seq) FROM audit_log WHERE at < now() - make_interval(days => $1)`, days).Scan(&cut); err != nil {
		return 0, err
	}
	if cut == nil {
		return 0, nil
	}
	var anchor []byte
	if err := tx.QueryRow(ctx, `SELECT hash FROM audit_log WHERE seq=$1`, *cut).Scan(&anchor); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM audit_log WHERE seq <= $1`, *cut)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE audit_chain_head SET anchor_seq=$1, anchor_hash=$2, updated_at=now()`, *cut, anchor); err != nil {
		return 0, err
	}
	if err := audit(ctx, tx, Actor{Name: "system"}, "audit.retention_purge", "audit_log", "", map[string]any{
		"deleted": tag.RowsAffected(), "through_seq": *cut, "retention_days": days, "anchor_hash": hex.EncodeToString(anchor),
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), s.SealAudit(ctx)
}

package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/secure"
)

// Live chat extras: a signed visitor identity, bans, canned replies and analytics.

// ---------------------------------------------------------------- signed identity

// ChatIdentityHash is what a website's own server computes and puts in the page so the chat can
// trust a visitor's email: the hex HMAC-SHA256 of the lower-cased email, keyed with the chat's
// identity secret. A browser never sees the secret.
func ChatIdentityHash(secret, email string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(m.Sum(nil))
}

func (s *Service) newIdentitySecret(widgetID string) (string, []byte, error) {
	secret, err := randToken(32)
	if err != nil {
		return "", nil, unavailable("chat_identity", err)
	}
	sealed, err := s.Box.Seal([]byte(secret), []byte("chat-identity:"+widgetID))
	if err != nil {
		return "", nil, unavailable("seal", err)
	}
	return secret, sealed, nil
}

// RotateChatIdentitySecret makes a new identity secret and returns it once. Signatures made with the
// old secret stop working for new conversations at once.
func (s *Service) RotateChatIdentitySecret(ctx context.Context, sc *Scope, id string) (string, error) {
	if err := s.chatOn(); err != nil {
		return "", err
	}
	if !uuidRe.MatchString(id) {
		return "", errChatFound
	}
	secret, sealed, err := s.newIdentitySecret(id)
	if err != nil {
		return "", err
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE chat_widgets SET identity_secret_enc=$3 WHERE id=$1 AND environment_id=$2`, id, sc.EnvID, sealed)
	if err != nil {
		return "", unavailable("chat_identity", err)
	}
	if tag.RowsAffected() == 0 {
		return "", errChatFound
	}
	return secret, nil
}

// checkChatIdentity reports whether the visitor's signature is valid. No signature is "not
// verified"; a wrong signature, or one without an email, is refused outright so a mistake on the
// website shows up instead of silently losing verification.
func (s *Service) checkChatIdentity(ctx context.Context, widgetID, email, identity string) (bool, error) {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return false, nil
	}
	bad := &Error{Status: 422, Code: "identity_invalid", Message: "the signed identity does not match the email address"}
	if email == "" {
		return false, bad
	}
	var enc []byte
	if err := s.Pool.QueryRow(ctx, `SELECT identity_secret_enc FROM chat_widgets WHERE id=$1`, widgetID).Scan(&enc); err != nil {
		return false, unavailable("chat_identity", err)
	}
	if len(enc) == 0 {
		return false, bad
	}
	pt, err := s.Box.Open(enc, []byte("chat-identity:"+widgetID))
	if err != nil {
		return false, unavailable("chat_identity", err)
	}
	want := ChatIdentityHash(string(pt), email)
	if !hmac.Equal([]byte(strings.ToLower(identity)), []byte(want)) {
		return false, bad
	}
	return true, nil
}

// ---------------------------------------------------------------- bans

// chatBanned reports whether an address (hash) or an email (hash) is banned in the environment.
func (s *Service) chatBanned(ctx context.Context, envID string, ipHash, emailHash []byte) (bool, error) {
	var banned bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM chat_bans WHERE environment_id=$1 AND
  (($2::bytea IS NOT NULL AND ip_hash=$2) OR ($3::bytea IS NOT NULL AND email_hash=$3)))`, envID, ipHash, emailHash).Scan(&banned); err != nil {
		return false, unavailable("chat_ban", err)
	}
	return banned, nil
}

// BanChatVisitor bans the address a conversation came from and the email the visitor gave, closes
// the conversation, and stops the visitor from writing in it or starting another.
func (s *Service) BanChatVisitor(ctx context.Context, sc *Scope, by, id, reason string) error {
	if err := s.chatOn(); err != nil {
		return err
	}
	if !uuidRe.MatchString(id) {
		return errChatFound
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	var ip, em []byte
	if err := tx.QueryRow(ctx, `SELECT ip_hash, email_hash FROM chat_conversations WHERE id=$1 AND environment_id=$2 FOR UPDATE`, id, sc.EnvID).Scan(&ip, &em); err != nil {
		if errors.Is(err, db.ErrNoRows) {
			return errChatFound
		}
		return unavailable("chat_ban", err)
	}
	reason = clean(strings.TrimSpace(reason), 200)
	if _, err := tx.Exec(ctx, `INSERT INTO chat_bans(environment_id, ip_hash, email_hash, reason, created_by) VALUES ($1,$2,$3,$4,$5)`, sc.EnvID, ip, em, reason, by); err != nil {
		return unavailable("chat_ban", err)
	}
	// Every conversation from the same address or email is marked, not only this one.
	if _, err := tx.Exec(ctx, `UPDATE chat_conversations SET banned_at=now(), state='closed'
WHERE environment_id=$1 AND (ip_hash=$2 OR ($3::bytea IS NOT NULL AND email_hash=$3))`, sc.EnvID, ip, em); err != nil {
		return unavailable("chat_ban", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable("commit", err)
	}
	return nil
}

// UnbanChatVisitor lifts the ban made for a conversation's address and email.
func (s *Service) UnbanChatVisitor(ctx context.Context, sc *Scope, id string) error {
	if err := s.chatOn(); err != nil {
		return err
	}
	if !uuidRe.MatchString(id) {
		return errChatFound
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	var ip, em []byte
	if err := tx.QueryRow(ctx, `SELECT ip_hash, email_hash FROM chat_conversations WHERE id=$1 AND environment_id=$2 FOR UPDATE`, id, sc.EnvID).Scan(&ip, &em); err != nil {
		if errors.Is(err, db.ErrNoRows) {
			return errChatFound
		}
		return unavailable("chat_ban", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chat_bans WHERE environment_id=$1 AND (ip_hash=$2 OR ($3::bytea IS NOT NULL AND email_hash=$3))`, sc.EnvID, ip, em); err != nil {
		return unavailable("chat_ban", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE chat_conversations SET banned_at=NULL WHERE environment_id=$1 AND (ip_hash=$2 OR ($3::bytea IS NOT NULL AND email_hash=$3))`, sc.EnvID, ip, em); err != nil {
		return unavailable("chat_ban", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable("commit", err)
	}
	return nil
}

// ---------------------------------------------------------------- canned replies

type ChatSnippetInput struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (in *ChatSnippetInput) check() error {
	in.Title = clean(strings.TrimSpace(in.Title), 60)
	in.Body = strings.TrimSpace(in.Body)
	if in.Title == "" || in.Body == "" {
		return badRequest("give the canned reply a title and a text")
	}
	if len(in.Body) > maxChatBody {
		return badRequest(fmt.Sprintf("a canned reply is at most %d characters", maxChatBody))
	}
	return nil
}

func (s *Service) ListChatSnippets(ctx context.Context, sc *Scope) ([]map[string]any, error) {
	if err := s.chatOn(); err != nil {
		return nil, err
	}
	return s.maps(ctx, `SELECT id::text AS id, title, body, created_at FROM chat_snippets WHERE environment_id=$1 ORDER BY title`, sc.EnvID)
}

func (s *Service) CreateChatSnippet(ctx context.Context, sc *Scope, in ChatSnippetInput) (string, error) {
	if err := s.chatOn(); err != nil {
		return "", err
	}
	if err := in.check(); err != nil {
		return "", err
	}
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM chat_snippets WHERE environment_id=$1`, sc.EnvID).Scan(&n); err != nil {
		return "", unavailable("chat_snippets", err)
	}
	if n >= 100 {
		return "", badRequest("at most 100 canned replies")
	}
	id := secure.NewUUID()
	tag, err := s.Pool.Exec(ctx, `INSERT INTO chat_snippets(id, project_id, environment_id, title, body) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (environment_id, title) DO NOTHING`,
		id, sc.ProjectID, sc.EnvID, in.Title, in.Body)
	if err != nil {
		return "", unavailable("chat_snippets", err)
	}
	if tag.RowsAffected() == 0 {
		return "", &Error{Status: 409, Code: "snippet_exists", Message: "a canned reply with this title exists"}
	}
	return id, nil
}

func (s *Service) UpdateChatSnippet(ctx context.Context, sc *Scope, id string, in ChatSnippetInput) error {
	if err := s.chatOn(); err != nil {
		return err
	}
	if !uuidRe.MatchString(id) {
		return errChatFound
	}
	if err := in.check(); err != nil {
		return err
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE chat_snippets SET title=$3, body=$4 WHERE id=$1 AND environment_id=$2`, id, sc.EnvID, in.Title, in.Body)
	if err != nil {
		if strings.Contains(err.Error(), "chat_snippets_environment_id_title_key") {
			return &Error{Status: 409, Code: "snippet_exists", Message: "a canned reply with this title exists"}
		}
		return unavailable("chat_snippets", err)
	}
	if tag.RowsAffected() == 0 {
		return errChatFound
	}
	return nil
}

func (s *Service) DeleteChatSnippet(ctx context.Context, sc *Scope, id string) error {
	if err := s.chatOn(); err != nil {
		return err
	}
	if !uuidRe.MatchString(id) {
		return errChatFound
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM chat_snippets WHERE id=$1 AND environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return unavailable("chat_snippets", err)
	}
	if tag.RowsAffected() == 0 {
		return errChatFound
	}
	return nil
}

// ---------------------------------------------------------------- analytics

// ChatAnalytics summarises the last `days` days (1 to 365): conversations, how many were answered, how
// long the first answer took, and a count per day. Times are for the first agent answer after the
// visitor's first message; a conversation nobody answered is counted as unanswered, not timed.
func (s *Service) ChatAnalytics(ctx context.Context, sc *Scope, days int) (map[string]any, error) {
	if err := s.chatOn(); err != nil {
		return nil, err
	}
	if days < 1 || days > 365 {
		days = 30
	}
	since := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	tot, err := s.one(ctx, `WITH f AS (
  SELECT c.id, c.created_at, c.identity_verified, c.banned_at IS NOT NULL AS banned,
    (SELECT min(m.created_at) FROM chat_messages m WHERE m.conversation_id=c.id AND m.sender='agent') AS first_reply
  FROM chat_conversations c WHERE c.environment_id=$1 AND c.created_at >= $2)
SELECT count(*) AS conversations, count(first_reply) AS answered, count(*) - count(first_reply) AS unanswered,
  count(*) FILTER (WHERE identity_verified) AS identified, count(*) FILTER (WHERE banned) AS banned,
  percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM first_reply - created_at)) FILTER (WHERE first_reply IS NOT NULL) AS first_answer_p50_seconds,
  percentile_cont(0.9) WITHIN GROUP (ORDER BY extract(epoch FROM first_reply - created_at)) FILTER (WHERE first_reply IS NOT NULL) AS first_answer_p90_seconds
FROM f`, sc.EnvID, since)
	if err != nil {
		return nil, err
	}
	msgs, err := s.one(ctx, `SELECT count(*) FILTER (WHERE sender='visitor') AS from_visitors, count(*) FILTER (WHERE sender='agent') AS from_agents,
  count(*) FILTER (WHERE sender='agent' AND emailed_at IS NOT NULL) AS answers_emailed
FROM chat_messages WHERE environment_id=$1 AND created_at >= $2`, sc.EnvID, since)
	if err != nil {
		return nil, err
	}
	daily, err := s.maps(ctx, `SELECT to_char(date_trunc('day', created_at), 'YYYY-MM-DD') AS day, count(*) AS conversations
FROM chat_conversations WHERE environment_id=$1 AND created_at >= $2 GROUP BY 1 ORDER BY 1`, sc.EnvID, since)
	if err != nil {
		return nil, err
	}
	return map[string]any{"days": days, "from": since, "totals": tot, "messages": msgs, "daily": daily,
		"notes": []string{"First-answer times run from the visitor's first message to the first answer from an agent. Unanswered conversations are counted, not timed."}}, nil
}

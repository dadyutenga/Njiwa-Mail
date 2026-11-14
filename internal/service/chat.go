package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/secure"
)

// Live chat: a widget on a website (one script tag), a conversation per
// visitor, answers typed in the dashboard and delivered to the open page in
// real time. If the visitor has left, an answer is sent to the email address
// they gave, through the normal sending pipeline.

const (
	maxChatBody        = 2000
	maxChatMessages    = 300
	maxChatLinks       = 8
	chatSeenEvery      = 10 * time.Second // how often an open page refreshes "last seen"
	chatEmailAfter     = 30 * time.Second // an answer not received by then may be emailed
	chatGoneAfter      = 60 * time.Second // the visitor's page has been silent this long
	chatEmailBatchSize = 50
)

var (
	errChatOff   = newErr(404, "chat_disabled")
	errChatFound = newErr(404, "not_found")
)

func (s *Service) chatOn() error {
	if !s.Opts.ChatEnabled {
		return errChatOff
	}
	return nil
}

// ---------------------------------------------------------------- widgets

type ChatLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type ChatWidgetInput struct {
	Name           string     `json:"name"`
	AllowedOrigins []string   `json:"allowed_origins"`
	Title          string     `json:"title"`
	Welcome        string     `json:"welcome"`
	OfflineMessage string     `json:"offline_message"`
	Announcement   string     `json:"announcement"`
	Links          []ChatLink `json:"links"`
	ReplyFrom      string     `json:"reply_from"`
	Enabled        *bool      `json:"enabled"`
}

func (in *ChatWidgetInput) check() error {
	in.Name = clean(strings.TrimSpace(in.Name), 80)
	if in.Name == "" {
		return badRequest("give the chat a name")
	}
	origins := []string{}
	for _, o := range in.AllowedOrigins {
		if strings.TrimSpace(o) == "" {
			continue
		}
		n := NormalizeOrigin(o)
		if n == "" {
			return badRequest("origins look like https://www.example.com")
		}
		origins = append(origins, n)
	}
	in.AllowedOrigins = origins
	in.Title = clean(strings.TrimSpace(in.Title), 80)
	in.Welcome = clean(strings.TrimSpace(in.Welcome), 500)
	in.OfflineMessage = clean(strings.TrimSpace(in.OfflineMessage), 500)
	in.Announcement = clean(strings.TrimSpace(in.Announcement), 500)
	if len(in.Links) > maxChatLinks {
		return badRequest("at most 8 links")
	}
	links := []ChatLink{}
	for _, l := range in.Links {
		l.Label, l.URL = clean(strings.TrimSpace(l.Label), 60), strings.TrimSpace(l.URL)
		if l.Label == "" && l.URL == "" {
			continue
		}
		u, err := url.Parse(l.URL)
		if l.Label == "" || err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || len(l.URL) > 500 {
			return badRequest("each link needs a label and an http(s) address")
		}
		links = append(links, l)
	}
	in.Links = links
	if in.ReplyFrom = strings.TrimSpace(in.ReplyFrom); in.ReplyFrom != "" {
		e, ok := NormalizeEmail(in.ReplyFrom)
		if !ok {
			return badRequest("reply-from must be an email address")
		}
		in.ReplyFrom = e
	}
	return nil
}

const chatWidgetCols = `name, allowed_origins, title, welcome, offline_message, announcement, links, reply_from, enabled`

func (s *Service) CreateChatWidget(ctx context.Context, sc *Scope, in ChatWidgetInput) (string, error) {
	if err := s.chatOn(); err != nil {
		return "", err
	}
	if err := in.check(); err != nil {
		return "", err
	}
	links, _ := json.Marshal(in.Links)
	enabled := in.Enabled == nil || *in.Enabled
	var id string
	err := s.Pool.QueryRow(ctx, `INSERT INTO chat_widgets(project_id, environment_id, `+chatWidgetCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (environment_id, name) DO NOTHING RETURNING id::text`,
		sc.ProjectID, sc.EnvID, in.Name, in.AllowedOrigins, in.Title, in.Welcome, in.OfflineMessage, in.Announcement, links, in.ReplyFrom, enabled).Scan(&id)
	if errors.Is(err, db.ErrNoRows) {
		return "", &Error{Status: 409, Code: "chat_exists", Message: "a chat with this name exists"}
	}
	if err != nil {
		return "", unavailable("chat_create", err)
	}
	return id, nil
}

func (s *Service) UpdateChatWidget(ctx context.Context, sc *Scope, id string, in ChatWidgetInput) error {
	if err := s.chatOn(); err != nil {
		return err
	}
	if !uuidRe.MatchString(id) {
		return errChatFound
	}
	if err := in.check(); err != nil {
		return err
	}
	links, _ := json.Marshal(in.Links)
	enabled := in.Enabled == nil || *in.Enabled
	tag, err := s.Pool.Exec(ctx, `UPDATE chat_widgets SET (`+chatWidgetCols+`) = ($3,$4,$5,$6,$7,$8,$9,$10,$11) WHERE id=$1 AND environment_id=$2`,
		id, sc.EnvID, in.Name, in.AllowedOrigins, in.Title, in.Welcome, in.OfflineMessage, in.Announcement, links, in.ReplyFrom, enabled)
	if err != nil {
		if strings.Contains(err.Error(), "chat_widgets_environment_id_name_key") {
			return &Error{Status: 409, Code: "chat_exists", Message: "a chat with this name exists"}
		}
		return unavailable("chat_update", err)
	}
	if tag.RowsAffected() == 0 {
		return errChatFound
	}
	return nil
}

func (s *Service) DeleteChatWidget(ctx context.Context, sc *Scope, id string) error {
	if err := s.chatOn(); err != nil {
		return err
	}
	if !uuidRe.MatchString(id) {
		return errChatFound
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM chat_widgets WHERE id=$1 AND environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return unavailable("chat_delete", err)
	}
	if tag.RowsAffected() == 0 {
		return errChatFound
	}
	return nil
}

func (s *Service) ListChatWidgets(ctx context.Context, sc *Scope) ([]map[string]any, error) {
	if err := s.chatOn(); err != nil {
		return nil, err
	}
	return s.maps(ctx, `SELECT w.id::text AS id, `+chatWidgetCols+`, w.created_at,
  (SELECT count(*) FROM chat_conversations c WHERE c.widget_id=w.id) AS conversations,
  (SELECT count(*) FROM chat_messages m JOIN chat_conversations c ON c.id=m.conversation_id
     WHERE c.widget_id=w.id AND m.sender='visitor' AND NOT m.seen_by_agent) AS unread
FROM chat_widgets w WHERE w.environment_id=$1 ORDER BY w.name`, sc.EnvID)
}

// ---------------------------------------------------------------- public side

type chatWidget struct {
	ID, EnvID, ProjectID string
	Origins              []string
}

func (s *Service) chatWidget(ctx context.Context, id string) (*chatWidget, error) {
	if !uuidRe.MatchString(id) {
		return nil, errChatFound
	}
	w := chatWidget{ID: id}
	err := s.Pool.QueryRow(ctx, `SELECT environment_id::text, project_id::text, allowed_origins FROM chat_widgets WHERE id=$1 AND enabled`, id).
		Scan(&w.EnvID, &w.ProjectID, &w.Origins)
	if errors.Is(err, db.ErrNoRows) {
		return nil, errChatFound
	}
	if err != nil {
		return nil, unavailable("chat_widget", err)
	}
	return &w, nil
}

// ChatOrigins is the allowed origins of an enabled chat, for CORS answers.
func (s *Service) ChatOrigins(ctx context.Context, id string) ([]string, error) {
	if err := s.chatOn(); err != nil {
		return nil, err
	}
	w, err := s.chatWidget(ctx, id)
	if err != nil {
		return nil, err
	}
	return w.Origins, nil
}

// checkChatOrigin refuses a browser page that is not on the allowlist. A
// client that sends neither Origin nor Referer is not a browser; the session
// token and the limits hold it.
func checkChatOrigin(origins []string, origin, referer string) error {
	o := NormalizeOrigin(origin)
	if o == "" {
		o = NormalizeOrigin(referer)
	}
	if o == "" {
		return nil
	}
	for _, a := range origins {
		if a == o {
			return nil
		}
	}
	return newErr(403, "origin_not_allowed")
}

// ChatConfig is what the widget shows before a conversation starts.
func (s *Service) ChatConfig(ctx context.Context, id string) (map[string]any, error) {
	if err := s.chatOn(); err != nil {
		return nil, err
	}
	if !uuidRe.MatchString(id) {
		return nil, errChatFound
	}
	var title, welcome, offline, announcement string
	var links []byte
	err := s.Pool.QueryRow(ctx, `SELECT title, welcome, offline_message, announcement, links FROM chat_widgets WHERE id=$1 AND enabled`, id).
		Scan(&title, &welcome, &offline, &announcement, &links)
	if errors.Is(err, db.ErrNoRows) {
		return nil, errChatFound
	}
	if err != nil {
		return nil, unavailable("chat_config", err)
	}
	var ls []ChatLink
	_ = json.Unmarshal(links, &ls)
	if ls == nil {
		ls = []ChatLink{}
	}
	return map[string]any{"title": title, "welcome": welcome, "offline_message": offline, "announcement": announcement, "links": ls}, nil
}

type ChatStart struct {
	WidgetID, Origin, Referer, IP string
	Name, Email, Message          string
}

type ChatStarted struct {
	ConversationID string `json:"conversation_id"`
	Token          string `json:"token"`
}

type chatVisitor struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func chatText(v string, max int) (string, error) {
	v = strings.TrimSpace(strings.ReplaceAll(v, "\r\n", "\n"))
	if v == "" {
		return "", &Error{Status: 422, Code: "empty_message", Message: "write a message"}
	}
	if len(v) > max {
		return "", &Error{Status: 422, Code: "message_too_long", Message: fmt.Sprintf("a message is at most %d characters", max)}
	}
	return v, nil
}

func (s *Service) chatIPKey(ip string) string {
	return hex.EncodeToString(s.Box.MAC("form-ip", []byte(cleanIP(ip)))[:8])
}

// StartChat opens a conversation with the visitor's first message and hands
// the page a session token. Like forms, it is limited per address, per chat
// and per workspace, scored for spam, and held to the workspace's filters.
func (s *Service) StartChat(ctx context.Context, p ChatStart) (*ChatStarted, error) {
	if err := s.chatOn(); err != nil {
		return nil, err
	}
	w, err := s.chatWidget(ctx, p.WidgetID)
	if err != nil {
		return nil, err
	}
	if err := checkChatOrigin(w.Origins, p.Origin, p.Referer); err != nil {
		return nil, err
	}
	ipk := s.chatIPKey(p.IP)
	for _, b := range []struct {
		key    string
		window time.Duration
		limit  int
	}{
		{"ch:new:" + w.ID + ":" + ipk + ":min", time.Minute, 3},
		{"ch:new:" + w.ID + ":" + ipk + ":hour", time.Hour, 10},
		{"ch:new:" + w.ID + ":min", time.Minute, 60},
		{"ch:ws:" + w.EnvID + ":min", time.Minute, 300},
	} {
		if err := s.formLimit(ctx, b.key, b.window, b.limit); err != nil {
			return nil, err
		}
	}
	msg, err := chatText(p.Message, maxChatBody)
	if err != nil {
		return nil, err
	}
	v := chatVisitor{Name: clean(strings.TrimSpace(p.Name), 80)}
	if strings.TrimSpace(p.Email) != "" {
		e, ok := NormalizeEmail(p.Email)
		if !ok {
			return nil, &Error{Status: 422, Code: "invalid_email", Message: "enter a valid email address or leave it empty"}
		}
		v.Email = e
	}
	if err := s.chatSpam(ctx, w.EnvID, msg, v, p.IP); err != nil {
		return nil, err
	}
	id, msgID := secure.NewUUID(), secure.NewUUID()
	token, err := randToken(32)
	if err != nil {
		return nil, unavailable("chat_token", err)
	}
	th := sha256.Sum256([]byte(token))
	venc, err := s.sealChatVisitor(id, v)
	if err != nil {
		return nil, err
	}
	benc, err := s.Box.Seal([]byte(msg), []byte("chat-msg:"+msgID))
	if err != nil {
		return nil, unavailable("seal", err)
	}
	var emailHash []byte
	if v.Email != "" {
		emailHash = s.Box.MAC("recipient", []byte(v.Email))
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO chat_conversations(id, widget_id, environment_id, token_hash, visitor_enc, email_hash, email_masked, ip_hash)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, id, w.ID, w.EnvID, th[:], venc, emailHash, maskedOrEmpty(v.Email), s.Box.MAC("form-ip", []byte(cleanIP(p.IP)))); err != nil {
		return nil, unavailable("chat_start", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO chat_messages(id, conversation_id, environment_id, sender, body_enc) VALUES ($1,$2,$3,'visitor',$4)`, msgID, id, w.EnvID, benc); err != nil {
		return nil, unavailable("chat_start", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, unavailable("commit", err)
	}
	return &ChatStarted{ConversationID: id, Token: token}, nil
}

func maskedOrEmpty(e string) string {
	if e == "" {
		return ""
	}
	return maskEmail(e)
}

func (s *Service) sealChatVisitor(id string, v chatVisitor) ([]byte, error) {
	b, _ := json.Marshal(v)
	enc, err := s.Box.Seal(b, []byte("chat-visitor:"+id))
	if err != nil {
		return nil, unavailable("seal", err)
	}
	return enc, nil
}

func (s *Service) openChatVisitor(id string, enc []byte) chatVisitor {
	var v chatVisitor
	if pt, err := s.Box.Open(enc, []byte("chat-visitor:"+id)); err == nil {
		_ = json.Unmarshal(pt, &v)
	}
	return v
}

// chatSpam applies the built-in score and the workspace's form filters.
// There is no quarantine for chat: anything that would be held is refused.
func (s *Service) chatSpam(ctx context.Context, envID, msg string, v chatVisitor, ip string) error {
	vf, err := s.getVerifier()
	if err != nil {
		return unavailable("chat_verify", err)
	}
	values := []string{msg}
	if v.Name != "" {
		values = append(values, v.Name)
	}
	res := scoreSubmission(values, v.Email, vf.IsDisposable, false)
	action := actionForScore(res.Score)
	filters, err := s.loadFilters(ctx, envID)
	if err != nil {
		return err
	}
	for _, fl := range filters {
		if matchFilter(fl, append(append([]string{}, values...), v.Email), v.Email, cleanIP(ip)) {
			action = maxAction(action, fl.Action)
		}
	}
	if actionRank[action] >= actionRank["quarantine"] {
		return &Error{Status: 422, Code: "spam_rejected", Message: "the message looks like spam and was not accepted"}
	}
	return nil
}

type chatConv struct {
	ID, WidgetID, EnvID, State string
	Origins                    []string
	Count                      int
}

// chatSession finds the conversation a token belongs to. A wrong token and an
// unknown conversation are the same answer.
func (s *Service) chatSession(ctx context.Context, widgetID, convID, token string) (*chatConv, error) {
	if err := s.chatOn(); err != nil {
		return nil, err
	}
	if !uuidRe.MatchString(widgetID) || !uuidRe.MatchString(convID) || token == "" {
		return nil, errChatFound
	}
	th := sha256.Sum256([]byte(token))
	c := chatConv{ID: convID, WidgetID: widgetID}
	err := s.Pool.QueryRow(ctx, `SELECT c.environment_id::text, c.state, w.allowed_origins,
  (SELECT count(*) FROM chat_messages m WHERE m.conversation_id=c.id)
FROM chat_conversations c JOIN chat_widgets w ON w.id=c.widget_id
WHERE c.id=$1 AND c.widget_id=$2 AND c.token_hash=$3 AND w.enabled`, convID, widgetID, th[:]).
		Scan(&c.EnvID, &c.State, &c.Origins, &c.Count)
	if errors.Is(err, db.ErrNoRows) {
		return nil, errChatFound
	}
	if err != nil {
		return nil, unavailable("chat_session", err)
	}
	return &c, nil
}

type ChatSend struct {
	WidgetID, ConversationID, Token, Origin, Referer, IP, Body string
}

// SendChatVisitor adds the visitor's message to their conversation.
func (s *Service) SendChatVisitor(ctx context.Context, p ChatSend) (string, error) {
	c, err := s.chatSession(ctx, p.WidgetID, p.ConversationID, p.Token)
	if err != nil {
		return "", err
	}
	if err := checkChatOrigin(c.Origins, p.Origin, p.Referer); err != nil {
		return "", err
	}
	for _, b := range []struct {
		key    string
		window time.Duration
		limit  int
	}{
		{"ch:msg:" + c.ID + ":min", time.Minute, 20},
		{"ch:msgip:" + c.WidgetID + ":" + s.chatIPKey(p.IP) + ":min", time.Minute, 60},
		{"ch:ws:" + c.EnvID + ":min", time.Minute, 300},
	} {
		if err := s.formLimit(ctx, b.key, b.window, b.limit); err != nil {
			return "", err
		}
	}
	if c.Count >= maxChatMessages {
		return "", &Error{Status: 422, Code: "conversation_full", Message: "this conversation is full; start a new one"}
	}
	body, err := chatText(p.Body, maxChatBody)
	if err != nil {
		return "", err
	}
	if err := s.chatSpam(ctx, c.EnvID, body, chatVisitor{}, p.IP); err != nil {
		return "", err
	}
	id := secure.NewUUID()
	enc, err := s.Box.Seal([]byte(body), []byte("chat-msg:"+id))
	if err != nil {
		return "", unavailable("seal", err)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO chat_messages(id, conversation_id, environment_id, sender, body_enc) VALUES ($1,$2,$3,'visitor',$4)`, id, c.ID, c.EnvID, enc); err != nil {
		return "", unavailable("chat_send", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE chat_conversations SET last_message_at=now(), visitor_seen_at=now(), state='open' WHERE id=$1`, c.ID); err != nil {
		return "", unavailable("chat_send", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", unavailable("commit", err)
	}
	return id, nil
}

// SetChatContact lets the visitor leave a name and an email address so an
// answer can reach them after they have left.
func (s *Service) SetChatContact(ctx context.Context, widgetID, convID, token, origin, referer, name, email string) error {
	c, err := s.chatSession(ctx, widgetID, convID, token)
	if err != nil {
		return err
	}
	if err := checkChatOrigin(c.Origins, origin, referer); err != nil {
		return err
	}
	if err := s.formLimit(ctx, "ch:contact:"+c.ID+":hour", time.Hour, 10); err != nil {
		return err
	}
	v := chatVisitor{Name: clean(strings.TrimSpace(name), 80)}
	var hash []byte
	if strings.TrimSpace(email) != "" {
		e, ok := NormalizeEmail(email)
		if !ok {
			return &Error{Status: 422, Code: "invalid_email", Message: "enter a valid email address or leave it empty"}
		}
		v.Email, hash = e, s.Box.MAC("recipient", []byte(e))
	}
	enc, err := s.sealChatVisitor(c.ID, v)
	if err != nil {
		return err
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE chat_conversations SET visitor_enc=$2, email_hash=$3, email_masked=$4 WHERE id=$1`, c.ID, enc, hash, maskedOrEmpty(v.Email)); err != nil {
		return unavailable("chat_contact", err)
	}
	return nil
}

// ChatMessage is one message as the visitor's page sees it.
type ChatMessage struct {
	ID     string    `json:"id"`
	Sender string    `json:"sender"`
	Body   string    `json:"body"`
	At     time.Time `json:"at"`
	Agent  string    `json:"agent,omitempty"`
}

// ChatCursor is "unixnano:id" of the last message a page has.
func ChatCursor(m ChatMessage) string { return strconv.FormatInt(m.At.UnixNano(), 10) + ":" + m.ID }

func parseChatCursor(c string) (time.Time, string) {
	if ns, id, ok := strings.Cut(c, ":"); ok {
		if n, err := strconv.ParseInt(ns, 10, 64); err == nil && uuidRe.MatchString(id) {
			return time.Unix(0, n).UTC(), id
		}
	}
	return time.Time{}, "00000000-0000-0000-0000-000000000000"
}

// ChatMessagesSince returns the conversation's messages after a cursor, oldest
// first, marks the answers it returns as received, and records that the
// visitor's page is still open (at most every 10 seconds).
func (s *Service) ChatMessagesSince(ctx context.Context, widgetID, convID, token, cursor string) ([]ChatMessage, string, error) {
	c, err := s.chatSession(ctx, widgetID, convID, token)
	if err != nil {
		return nil, cursor, err
	}
	at, afterID := parseChatCursor(cursor)
	if _, err := s.Pool.Exec(ctx, `UPDATE chat_conversations SET visitor_seen_at=now() WHERE id=$1 AND visitor_seen_at < now() - make_interval(secs => $2::float8)`, c.ID, chatSeenEvery.Seconds()); err != nil {
		return nil, cursor, unavailable("chat_seen", err)
	}
	rows, err := s.Pool.Query(ctx, `SELECT id::text, sender, agent_email, body_enc, created_at FROM chat_messages
WHERE conversation_id=$1 AND (created_at, id::text) > ($2, $3) ORDER BY created_at, id LIMIT 100`, c.ID, at, afterID)
	if err != nil {
		return nil, cursor, unavailable("chat_messages", err)
	}
	defer rows.Close()
	var out []ChatMessage
	var agentIDs []string
	for rows.Next() {
		var m ChatMessage
		var enc []byte
		var agent string
		if err := rows.Scan(&m.ID, &m.Sender, &agent, &enc, &m.At); err != nil {
			return nil, cursor, unavailable("chat_messages", err)
		}
		pt, err := s.Box.Open(enc, []byte("chat-msg:"+m.ID))
		if err != nil {
			continue
		}
		m.Body = string(pt)
		if m.Sender == "agent" {
			agentIDs = append(agentIDs, m.ID)
			m.Agent = chatAgentName(agent)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, cursor, unavailable("chat_messages", err)
	}
	rows.Close()
	if len(agentIDs) > 0 {
		if _, err := s.Pool.Exec(ctx, `UPDATE chat_messages SET delivered_at=now() WHERE id = ANY($1::uuid[]) AND delivered_at IS NULL`, agentIDs); err != nil {
			return nil, cursor, unavailable("chat_delivered", err)
		}
	}
	if len(out) > 0 {
		cursor = ChatCursor(out[len(out)-1])
	}
	return out, cursor, nil
}

// chatAgentName shows an agent by the part of their address before the @, so a
// visitor never sees a full staff address.
func chatAgentName(email string) string {
	if i := strings.IndexByte(email, '@'); i > 0 {
		return email[:i]
	}
	return ""
}

// ---------------------------------------------------------------- dashboard side

type ChatConversationFilter struct {
	WidgetID, State string
}

func (s *Service) ListChatConversations(ctx context.Context, sc *Scope, f ChatConversationFilter, pg Page) ([]map[string]any, bool, error) {
	if err := s.chatOn(); err != nil {
		return nil, false, err
	}
	if f.WidgetID != "" && !uuidRe.MatchString(f.WidgetID) {
		return nil, false, errChatFound
	}
	if f.State != "" && f.State != "open" && f.State != "closed" {
		return nil, false, badRequest("state is open or closed")
	}
	items, more, err := s.page(ctx, pg, `SELECT c.id::text AS id, c.widget_id::text AS widget_id, w.name AS widget, c.state, c.email_masked, c.created_at, c.last_message_at,
  c.visitor_seen_at, u.email AS assigned_to,
  (SELECT count(*) FROM chat_messages m WHERE m.conversation_id=c.id AND m.sender='visitor' AND NOT m.seen_by_agent) AS unread,
  (SELECT m.id::text FROM chat_messages m WHERE m.conversation_id=c.id ORDER BY m.created_at DESC, m.id DESC LIMIT 1) AS last_id
FROM chat_conversations c JOIN chat_widgets w ON w.id=c.widget_id LEFT JOIN users u ON u.id=c.assigned_to
WHERE c.environment_id=$1 AND ($2='' OR c.widget_id=NULLIF($2,'')::uuid) AND ($3='' OR c.state=$3)
ORDER BY c.last_message_at DESC`, sc.EnvID, f.WidgetID, f.State)
	if err != nil {
		return nil, false, err
	}
	for _, it := range items {
		id := fmt.Sprint(it["id"])
		var enc []byte
		if lid, ok := it["last_id"].(string); ok && lid != "" {
			if err := s.Pool.QueryRow(ctx, `SELECT body_enc FROM chat_messages WHERE id=$1`, lid).Scan(&enc); err == nil {
				if pt, err := s.Box.Open(enc, []byte("chat-msg:"+lid)); err == nil {
					p := string(pt)
					if len(p) > 120 {
						p = p[:120] + "…"
					}
					it["preview"] = p
				}
			}
		}
		delete(it, "last_id")
		var venc []byte
		if err := s.Pool.QueryRow(ctx, `SELECT visitor_enc FROM chat_conversations WHERE id=$1`, id).Scan(&venc); err == nil {
			it["visitor"] = s.openChatVisitor(id, venc).Name
		}
		seen, _ := it["visitor_seen_at"].(time.Time)
		it["online"] = time.Since(seen) < chatGoneAfter
	}
	return items, more, nil
}

// GetChatConversation returns the conversation with its messages and marks
// the visitor's messages as seen by an agent.
func (s *Service) GetChatConversation(ctx context.Context, sc *Scope, id string) (map[string]any, error) {
	if err := s.chatOn(); err != nil {
		return nil, err
	}
	if !uuidRe.MatchString(id) {
		return nil, errChatFound
	}
	m, err := s.one(ctx, `SELECT c.id::text AS id, c.widget_id::text AS widget_id, w.name AS widget, w.reply_from, c.state, c.email_masked, c.created_at,
  c.last_message_at, c.visitor_seen_at, c.assigned_to::text AS assigned_to_id, u.email AS assigned_to, c.visitor_enc
FROM chat_conversations c JOIN chat_widgets w ON w.id=c.widget_id LEFT JOIN users u ON u.id=c.assigned_to
WHERE c.id=$1 AND c.environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return nil, err
	}
	venc, _ := m["visitor_enc"].([]byte)
	delete(m, "visitor_enc")
	v := s.openChatVisitor(id, venc)
	m["visitor"], m["visitor_email"] = v.Name, v.Email
	seen, _ := m["visitor_seen_at"].(time.Time)
	m["online"] = time.Since(seen) < chatGoneAfter
	rows, err := s.Pool.Query(ctx, `SELECT id::text, sender, agent_email, body_enc, created_at, delivered_at, emailed_at FROM chat_messages
WHERE conversation_id=$1 ORDER BY created_at, id LIMIT $2`, id, maxChatMessages)
	if err != nil {
		return nil, unavailable("chat_messages", err)
	}
	defer rows.Close()
	msgs := []map[string]any{}
	for rows.Next() {
		var mid, sender, agent string
		var enc []byte
		var at time.Time
		var delivered, emailed *time.Time
		if err := rows.Scan(&mid, &sender, &agent, &enc, &at, &delivered, &emailed); err != nil {
			return nil, unavailable("chat_messages", err)
		}
		body := ""
		if pt, err := s.Box.Open(enc, []byte("chat-msg:"+mid)); err == nil {
			body = string(pt)
		}
		msgs = append(msgs, map[string]any{"id": mid, "sender": sender, "agent": agent, "body": body, "at": at, "delivered_at": delivered, "emailed_at": emailed})
	}
	if err := rows.Err(); err != nil {
		return nil, unavailable("chat_messages", err)
	}
	rows.Close()
	m["messages"] = msgs
	if _, err := s.Pool.Exec(ctx, `UPDATE chat_messages SET seen_by_agent=true WHERE conversation_id=$1 AND sender='visitor' AND NOT seen_by_agent`, id); err != nil {
		return nil, unavailable("chat_seen", err)
	}
	return m, nil
}

// ReplyChat adds an agent's answer. The visitor's open page receives it within
// seconds; if the page is gone, SendChatEmails mails it to the address the
// visitor left.
func (s *Service) ReplyChat(ctx context.Context, sc *Scope, user, id, body string) (string, error) {
	if err := s.chatOn(); err != nil {
		return "", err
	}
	if !uuidRe.MatchString(id) {
		return "", errChatFound
	}
	body, err := chatText(body, maxChatBody)
	if err != nil {
		return "", err
	}
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM chat_messages WHERE conversation_id=c.id) FROM chat_conversations c WHERE c.id=$1 AND c.environment_id=$2`, id, sc.EnvID).Scan(&n); err != nil {
		return "", notFound(err)
	}
	if n >= maxChatMessages {
		return "", &Error{Status: 422, Code: "conversation_full", Message: "this conversation is full"}
	}
	mid := secure.NewUUID()
	enc, err := s.Box.Seal([]byte(body), []byte("chat-msg:"+mid))
	if err != nil {
		return "", unavailable("seal", err)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO chat_messages(id, conversation_id, environment_id, sender, agent_email, body_enc, seen_by_agent) VALUES ($1,$2,$3,'agent',$4,$5,true)`,
		mid, id, sc.EnvID, user, enc); err != nil {
		return "", unavailable("chat_reply", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE chat_conversations SET last_message_at=now(), state='open' WHERE id=$1`, id); err != nil {
		return "", unavailable("chat_reply", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", unavailable("commit", err)
	}
	return mid, nil
}

// UpdateChatConversation sets the state and/or the assignee ("" unassigns).
func (s *Service) UpdateChatConversation(ctx context.Context, sc *Scope, id string, state, assignee *string) error {
	if err := s.chatOn(); err != nil {
		return err
	}
	if !uuidRe.MatchString(id) {
		return errChatFound
	}
	if state != nil && *state != "open" && *state != "closed" {
		return badRequest("state is open or closed")
	}
	if assignee != nil && *assignee != "" && !uuidRe.MatchString(*assignee) {
		return badRequest("unknown user")
	}
	if assignee != nil && *assignee != "" {
		var ok bool
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id=$1)`, *assignee).Scan(&ok); err != nil || !ok {
			return badRequest("unknown user")
		}
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE chat_conversations SET state=COALESCE($3, state),
  assigned_to = CASE WHEN $4::boolean THEN NULLIF($5,'')::uuid ELSE assigned_to END WHERE id=$1 AND environment_id=$2`,
		id, sc.EnvID, state, assignee != nil, derefStr(assignee))
	if err != nil {
		return unavailable("chat_update", err)
	}
	if tag.RowsAffected() == 0 {
		return errChatFound
	}
	return nil
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (s *Service) DeleteChatConversation(ctx context.Context, sc *Scope, id string) error {
	if err := s.chatOn(); err != nil {
		return err
	}
	if !uuidRe.MatchString(id) {
		return errChatFound
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM chat_conversations WHERE id=$1 AND environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return unavailable("chat_delete", err)
	}
	if tag.RowsAffected() == 0 {
		return errChatFound
	}
	return nil
}

// ChatEvent is one new visitor message for the dashboard's live stream.
type ChatEvent struct {
	ID           string    `json:"id"`
	Conversation string    `json:"conversation"`
	Widget       string    `json:"widget"`
	Visitor      string    `json:"visitor"`
	Preview      string    `json:"preview"`
	At           time.Time `json:"at"`
}

// ChatVisitorMessagesSince returns visitor messages after a (time, id) cursor,
// oldest first, for the dashboard stream.
func (s *Service) ChatVisitorMessagesSince(ctx context.Context, envID string, after time.Time, afterID string, limit int) ([]ChatEvent, error) {
	rows, err := s.Pool.Query(ctx, `SELECT m.id::text, c.id::text, w.name, c.email_masked, m.body_enc, m.created_at, c.visitor_enc FROM chat_messages m
JOIN chat_conversations c ON c.id=m.conversation_id JOIN chat_widgets w ON w.id=c.widget_id
WHERE m.environment_id=$1 AND m.sender='visitor' AND (m.created_at, m.id::text) > ($2, $3) ORDER BY m.created_at, m.id LIMIT $4`, envID, after, afterID, limit)
	if err != nil {
		return nil, unavailable("chat_stream", err)
	}
	defer rows.Close()
	var out []ChatEvent
	for rows.Next() {
		var e ChatEvent
		var masked string
		var enc, venc []byte
		if err := rows.Scan(&e.ID, &e.Conversation, &e.Widget, &masked, &enc, &e.At, &venc); err != nil {
			return nil, unavailable("chat_stream", err)
		}
		if pt, err := s.Box.Open(enc, []byte("chat-msg:"+e.ID)); err == nil {
			e.Preview = string(pt)
			if len(e.Preview) > 120 {
				e.Preview = e.Preview[:120] + "…"
			}
		}
		e.Visitor = s.openChatVisitor(e.Conversation, venc).Name
		if e.Visitor == "" {
			e.Visitor = masked
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- email follow-up

// SendChatEmails mails answers the visitor's page never received. An answer
// qualifies once it is older than 30 seconds, was not delivered to an open
// page, and the page has been silent for a minute; the visitor must have left
// an email address. Answers to one conversation go out in one message. It is
// "accepted for sending" like any mail: delivery is shown by the timeline.
func (s *Service) SendChatEmails(ctx context.Context) (int, error) {
	if !s.Opts.ChatEnabled {
		return 0, nil
	}
	rows, err := s.Pool.Query(ctx, `SELECT c.id::text, c.environment_id::text, e.project_id::text, p.name, e.mode, w.title, w.name, w.reply_from
FROM chat_conversations c JOIN chat_widgets w ON w.id=c.widget_id JOIN environments e ON e.id=c.environment_id JOIN projects p ON p.id=e.project_id
WHERE c.email_hash IS NOT NULL AND c.visitor_seen_at < now() - make_interval(secs => $1::float8)
  AND EXISTS (SELECT 1 FROM chat_messages m WHERE m.conversation_id=c.id AND m.sender='agent' AND m.delivered_at IS NULL AND m.emailed_at IS NULL
              AND m.created_at < now() - make_interval(secs => $2::float8))
ORDER BY c.last_message_at LIMIT $3`, chatGoneAfter.Seconds(), chatEmailAfter.Seconds(), chatEmailBatchSize)
	if err != nil {
		return 0, err
	}
	type job struct{ id, envID, projectID, project, mode, title, widget, replyFrom string }
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.envID, &j.projectID, &j.project, &j.mode, &j.title, &j.widget, &j.replyFrom); err != nil {
			rows.Close()
			return 0, err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	sent := 0
	for _, j := range jobs {
		n, err := s.emailChatConversation(ctx, j.id, &Scope{ProjectID: j.projectID, EnvID: j.envID, Project: j.project, Mode: j.mode}, firstNonEmpty(j.title, j.widget), j.replyFrom)
		if err != nil {
			slog.Warn("chat email failed", "conversation", j.id, "err", err)
			continue
		}
		sent += n
	}
	return sent, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (s *Service) emailChatConversation(ctx context.Context, convID string, sc *Scope, title, replyFrom string) (int, error) {
	var venc []byte
	if err := s.Pool.QueryRow(ctx, `SELECT visitor_enc FROM chat_conversations WHERE id=$1`, convID).Scan(&venc); err != nil {
		return 0, err
	}
	v := s.openChatVisitor(convID, venc)
	rows, err := s.Pool.Query(ctx, `SELECT id::text, body_enc, created_at FROM chat_messages
WHERE conversation_id=$1 AND sender='agent' AND delivered_at IS NULL AND emailed_at IS NULL AND created_at < now() - make_interval(secs => $2::float8)
ORDER BY created_at, id LIMIT 20`, convID, chatEmailAfter.Seconds())
	if err != nil {
		return 0, err
	}
	var ids, bodies []string
	for rows.Next() {
		var id string
		var enc []byte
		var at time.Time
		if err := rows.Scan(&id, &enc, &at); err != nil {
			rows.Close()
			return 0, err
		}
		pt, err := s.Box.Open(enc, []byte("chat-msg:"+id))
		if err != nil {
			continue
		}
		ids, bodies = append(ids, id), append(bodies, string(pt))
	}
	rows.Close()
	if len(ids) == 0 {
		return 0, nil
	}
	mark := func() error {
		_, err := s.Pool.Exec(ctx, `UPDATE chat_messages SET emailed_at=now() WHERE id = ANY($1::uuid[])`, ids)
		return err
	}
	if v.Email == "" {
		return 0, mark() // the visitor removed their address; nothing to send to
	}
	var from string
	err = s.Pool.QueryRow(ctx, `SELECT email FROM sender_identities WHERE environment_id=$1 ORDER BY (email=$2) DESC, is_default DESC, created_at LIMIT 1`, sc.EnvID, replyFrom).Scan(&from)
	if errors.Is(err, db.ErrNoRows) {
		return 0, errors.New("no sender address in this environment")
	}
	if err != nil {
		return 0, err
	}
	var b strings.Builder
	if v.Name != "" {
		fmt.Fprintf(&b, "Hello %s,\n\n", v.Name)
	}
	fmt.Fprintf(&b, "You left a chat with %s and we answered after you had gone:\n\n", title)
	for _, body := range bodies {
		fmt.Fprintf(&b, "%s\n\n", body)
	}
	idsKey := append([]string{}, ids...)
	sort.Strings(idsKey)
	key := sha256.Sum256([]byte(strings.Join(idsKey, ",")))
	_, err = s.Compose(ctx, sc, "chat", "chat-email-"+hex.EncodeToString(key[:12]), ComposeInput{From: from, To: v.Email, Subject: "Reply from " + title, Text: b.String()})
	var se *Error
	if err != nil && (!errors.As(err, &se) || se.Status >= 500) {
		return 0, err // try again next minute
	}
	if err != nil {
		slog.Warn("chat email not sent", "conversation", convID, "code", se.Code)
	}
	if merr := mark(); merr != nil {
		return 0, merr
	}
	if err != nil {
		return 0, nil
	}
	return len(ids), nil
}

// ChatStreamOpen limits how often one address may open a live stream for a chat.
func (s *Service) ChatStreamOpen(ctx context.Context, widgetID, ip string) error {
	if err := s.chatOn(); err != nil {
		return err
	}
	return s.formLimit(ctx, "ch:stream:"+widgetID+":"+s.chatIPKey(ip)+":min", time.Minute, 20)
}

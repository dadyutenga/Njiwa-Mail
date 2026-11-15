package api

import (
	"context"
	_ "embed"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/njiwa-tz/mail/internal/service"
)

//go:embed chat_widget.js
var chatWidgetJS string

// chatCORS answers a browser page on an allowed origin; others get no CORS
// headers, so their script cannot read an answer.
func (s *Server) chatCORS(c fiber.Ctx, widgetID string) {
	origin := c.Get("Origin")
	if origin == "" {
		return
	}
	origins, err := s.Svc.ChatOrigins(reqCtx(c), widgetID)
	if err != nil {
		return
	}
	n := service.NormalizeOrigin(origin)
	for _, o := range origins {
		if o == n && n != "" {
			c.Set("Access-Control-Allow-Origin", origin)
			c.Set("Vary", "Origin")
			c.Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
			c.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Last-Event-ID")
			c.Set("Access-Control-Max-Age", "600")
			return
		}
	}
}

func bearerToken(c fiber.Ctx) string {
	h := c.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func (s *Server) chatRoutes(app *fiber.App) {
	svc := s.Svc
	// ---- public: the widget on a website
	pub := func(method, path string, fn func(c fiber.Ctx, id string) (any, int, error)) {
		app.Add([]string{method}, path, func(c fiber.Ctx) error {
			id := c.Params("id")
			s.chatCORS(c, id)
			c.Set("Cache-Control", "no-store")
			out, status, err := fn(c, id)
			if err != nil {
				if e, ok := service.IsError(err); ok && e.RetryAfter > 0 {
					c.Set("Retry-After", strconv.Itoa(int(e.RetryAfter/time.Second)+1))
				}
				return writeErr(c, err)
			}
			return writeJSON(c, status, out)
		})
	}
	app.Options("/v1/chat/:id/*", func(c fiber.Ctx) error {
		s.chatCORS(c, c.Params("id"))
		return c.SendStatus(204)
	})
	app.Get("/v1/chat/:id/widget.js", func(c fiber.Ctx) error {
		if !svc.Opts.ChatEnabled {
			return writeErr(c, &service.Error{Status: 404, Code: "chat_disabled"})
		}
		c.Set(fiber.HeaderContentType, "application/javascript; charset=utf-8")
		c.Set("Cache-Control", "public, max-age=300")
		c.Set("X-Content-Type-Options", "nosniff")
		return c.SendString(chatWidgetJS)
	})
	pub("GET", "/v1/chat/:id/config", func(c fiber.Ctx, id string) (any, int, error) {
		out, err := svc.ChatConfig(reqCtx(c), id)
		return out, 200, err
	})
	pub("POST", "/v1/chat/:id/conversations", func(c fiber.Ctx, id string) (any, int, error) {
		var in struct {
			Name     string `json:"name"`
			Email    string `json:"email"`
			Message  string `json:"message"`
			Identity string `json:"identity"`
		}
		if err := decode(c, &in); err != nil {
			return nil, 0, err
		}
		out, err := svc.StartChat(reqCtx(c), service.ChatStart{WidgetID: id, Origin: c.Get("Origin"), Referer: c.Get("Referer"),
			IP: s.clientIP(c), Name: in.Name, Email: in.Email, Message: in.Message, Identity: in.Identity})
		return out, 201, err
	})
	pub("POST", "/v1/chat/:id/conversations/:cid/messages", func(c fiber.Ctx, id string) (any, int, error) {
		var in struct {
			Body string `json:"body"`
		}
		if err := decode(c, &in); err != nil {
			return nil, 0, err
		}
		mid, err := svc.SendChatVisitor(reqCtx(c), service.ChatSend{WidgetID: id, ConversationID: c.Params("cid"), Token: bearerToken(c),
			Origin: c.Get("Origin"), Referer: c.Get("Referer"), IP: s.clientIP(c), Body: in.Body})
		return map[string]string{"id": mid}, 201, err
	})
	pub("PUT", "/v1/chat/:id/conversations/:cid/contact", func(c fiber.Ctx, id string) (any, int, error) {
		var in struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		}
		if err := decode(c, &in); err != nil {
			return nil, 0, err
		}
		err := svc.SetChatContact(reqCtx(c), id, c.Params("cid"), bearerToken(c), c.Get("Origin"), c.Get("Referer"), in.Name, in.Email)
		return map[string]bool{"ok": true}, 200, err
	})
	pub("GET", "/v1/chat/:id/conversations/:cid/messages", func(c fiber.Ctx, id string) (any, int, error) {
		items, cursor, err := svc.ChatMessagesSince(reqCtx(c), id, c.Params("cid"), bearerToken(c), c.Query("after"))
		if items == nil {
			items = []service.ChatMessage{}
		}
		return map[string]any{"items": items, "cursor": cursor}, 200, err
	})
	app.Get("/v1/chat/:id/conversations/:cid/stream", func(c fiber.Ctx) error {
		id, cid, token := c.Params("id"), c.Params("cid"), bearerToken(c)
		s.chatCORS(c, id)
		cursor := c.Get("Last-Event-ID")
		if cursor == "" {
			cursor = c.Query("after")
		}
		if err := s.chatStreamLimit(c, id); err != nil {
			return writeErr(c, err)
		}
		// Check the session before switching to a stream, so a wrong token is a plain 404.
		first, cur, err := svc.ChatMessagesSince(reqCtx(c), id, cid, token, cursor)
		if err != nil {
			return writeErr(c, err)
		}
		cursor = cur
		return s.eventStream(c, func(cx context.Context) ([]sseEvent, error) {
			items := first
			first = nil
			if items == nil {
				var err error
				if items, cursor, err = svc.ChatMessagesSince(cx, id, cid, token, cursor); err != nil {
					return nil, err
				}
			}
			out := make([]sseEvent, 0, len(items))
			for _, m := range items {
				out = append(out, sseEvent{Name: "message", ID: service.ChatCursor(m), Data: m})
			}
			return out, nil
		})(c)
	})

	// ---- workspace: widgets and the conversation inbox
	base := "/admin/v1/w/:project/:mode"
	h := func(method, path string, fn wsFn) { route(app, method, base+path, s.ws(fn)) }
	ctx := reqCtx
	h("GET", "/chat/widgets", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.ListChatWidgets(ctx(c), sc)
		return map[string]any{"items": items}, err
	})
	h("POST", "/chat/widgets", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in service.ChatWidgetInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, secret, err := svc.CreateChatWidget(ctx(c), sc, in)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "chat_widget.create", "chat_widget", id, scoped(sc, map[string]any{"name": in.Name}))
		// The identity secret is shown this once.
		return map[string]string{"id": id, "identity_secret": secret}, nil
	})
	h("PUT", "/chat/widgets/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in service.ChatWidgetInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.UpdateChatWidget(ctx(c), sc, c.Params("id"), in); err != nil {
			return nil, err
		}
		s.audit(c, u, "chat_widget.update", "chat_widget", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("DELETE", "/chat/widgets/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteChatWidget(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "chat_widget.delete", "chat_widget", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("POST", "/chat/widgets/:id/identity-secret", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		secret, err := svc.RotateChatIdentitySecret(ctx(c), sc, c.Params("id"))
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "chat_widget.identity_secret", "chat_widget", c.Params("id"), scoped(sc, nil))
		return map[string]string{"identity_secret": secret}, nil
	})
	h("GET", "/chat/analytics", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		days, _ := strconv.Atoi(c.Query("days", "30"))
		return svc.ChatAnalytics(ctx(c), sc, days)
	})
	h("GET", "/chat/snippets", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.ListChatSnippets(ctx(c), sc)
		return map[string]any{"items": items}, err
	})
	h("POST", "/chat/snippets", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in service.ChatSnippetInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, err := svc.CreateChatSnippet(ctx(c), sc, in)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "chat_snippet.create", "chat_snippet", id, scoped(sc, map[string]any{"title": in.Title}))
		return map[string]string{"id": id}, nil
	})
	h("PUT", "/chat/snippets/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in service.ChatSnippetInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.UpdateChatSnippet(ctx(c), sc, c.Params("id"), in); err != nil {
			return nil, err
		}
		s.audit(c, u, "chat_snippet.update", "chat_snippet", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("DELETE", "/chat/snippets/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteChatSnippet(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "chat_snippet.delete", "chat_snippet", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("POST", "/chat/conversations/:id/ban", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Reason string `json:"reason"`
		}
		if len(c.Body()) > 0 {
			if err := decode(c, &in); err != nil {
				return nil, err
			}
		}
		if err := svc.BanChatVisitor(ctx(c), sc, u.Email, c.Params("id"), in.Reason); err != nil {
			return nil, err
		}
		s.audit(c, u, "chat_visitor.ban", "chat_conversation", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("DELETE", "/chat/conversations/:id/ban", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.UnbanChatVisitor(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "chat_visitor.unban", "chat_conversation", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("GET", "/chat/conversations", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.ListChatConversations(ctx(c), sc, service.ChatConversationFilter{WidgetID: c.Query("widget"), State: c.Query("state")}, pageOf(c))
		return list(c, items, more, err)
	})
	h("GET", "/chat/conversations/stream", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if !svc.Opts.ChatEnabled {
			return nil, &service.Error{Status: 404, Code: "chat_disabled"}
		}
		after, afterID := time.Now().UTC(), ""
		if last := c.Get("Last-Event-ID"); last != "" {
			if ns, id, ok := strings.Cut(last, ":"); ok {
				if n, err := strconv.ParseInt(ns, 10, 64); err == nil {
					after, afterID = time.Unix(0, n).UTC(), id
				}
			}
		}
		return s.eventStream(c, func(cx context.Context) ([]sseEvent, error) {
			items, err := svc.ChatVisitorMessagesSince(cx, sc.EnvID, after, afterID, 50)
			out := make([]sseEvent, 0, len(items))
			for _, it := range items {
				after, afterID = it.At, it.ID
				out = append(out, sseEvent{Name: "message", ID: strconv.FormatInt(it.At.UnixNano(), 10) + ":" + it.ID, Data: it})
			}
			return out, err
		}), nil
	})
	h("GET", "/chat/conversations/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.GetChatConversation(ctx(c), sc, c.Params("id"))
	})
	h("PATCH", "/chat/conversations/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			State      *string `json:"state"`
			AssignedTo *string `json:"assigned_to"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.UpdateChatConversation(ctx(c), sc, c.Params("id"), in.State, in.AssignedTo); err != nil {
			return nil, err
		}
		s.audit(c, u, "chat_conversation.update", "chat_conversation", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("DELETE", "/chat/conversations/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteChatConversation(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "chat_conversation.delete", "chat_conversation", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("POST", "/chat/conversations/:id/messages", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Body string `json:"body"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		mid, err := svc.ReplyChat(ctx(c), sc, u.Email, c.Params("id"), in.Body)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "chat_conversation.reply", "chat_conversation", c.Params("id"), scoped(sc, map[string]any{"message_id": mid}))
		return map[string]string{"id": mid}, nil
	})
}

// chatStreamLimit keeps one address from holding many open streams.
func (s *Server) chatStreamLimit(c fiber.Ctx, widgetID string) error {
	return s.Svc.ChatStreamOpen(reqCtx(c), widgetID, s.clientIP(c))
}

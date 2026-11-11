// Package api exposes the HTTP contract on Fiber.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/service"
)

type Server struct {
	Svc             *service.Service
	MaxBodyBytes    int64
	EmailMaxBytes   int64 // larger cap for POST /v1/emails so attachments fit; 0 uses MaxBodyBytes
	ConsolePassword string
	SessionTTL      time.Duration
	SMTPConfigured  bool
	EnvSMTP         *mailer.SMTPConfig // SMTP from the environment (nil when unset); used by the Mail server test

	// Admin API (/admin/v1).
	TrustProxy      bool             // take the client IP from X-Forwarded-For (set by our own reverse proxy)
	InsecureCookies bool             // local HTTP development only: drop the Secure cookie attribute
	MetricsToken    string           // bearer token for /metrics; empty disables the endpoint
	Resolver        service.Resolver // DNS resolver override for tests; nil = system resolver
}

const (
	adminCookie  = "dm_admin"
	csrfHeader   = "X-CSRF-Token"
	adminMaxBody = 2 << 20
)

// App builds the Fiber application: application API, admin API, and the legacy console.
func (s *Server) App() *fiber.App {
	// Ceiling only. Handlers enforce the live MaxBodyBytes / EmailMaxBytes
	// limits, which tests adjust after the app is built. Base64 attachments
	// make the JSON body larger than the decoded file.
	app := fiber.New(fiber.Config{
		BodyLimit:             16 << 20,
		ReadTimeout:           15 * time.Second,
		WriteTimeout:          30 * time.Second,
		IdleTimeout:           60 * time.Second,
		DisableStartupMessage: true,
		Immutable:             true,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			var fe *fiber.Error
			if errors.As(err, &fe) {
				if fe.Code == fiber.StatusRequestEntityTooLarge {
					return writeErr(c, &service.Error{Status: 413, Code: "body_too_large"})
				}
				c.Status(fe.Code)
				return c.SendString(fe.Message)
			}
			return writeErr(c, &service.Error{Status: 500, Code: "internal_error"})
		},
	})
	app.Use(s.middleware)

	app.Post("/v1/emails/batch", s.postBatch)
	app.Post("/v1/emails", s.postEmail)
	app.Post("/v1/emails/:id/cancel", s.cancelEmail)
	app.Get("/v1/emails/:id", s.getEmail)
	app.Get("/health/live", func(c *fiber.Ctx) error { return writeJSON(c, 200, map[string]string{"status": "ok"}) })
	app.Get("/health/ready", s.ready)

	app.Post("/console/login", s.consoleLogin)
	app.Post("/console/logout", s.consoleLogout)
	app.Get("/console/me", s.consoleMe)
	app.Get("/console/overview", s.consoleOverview)
	app.Get("/console/messages", s.consoleMessages)
	app.Get("/console/messages/:id", s.consoleMessage)
	app.Get("/console/projects", s.consoleProjects)
	app.Get("/console/keys", s.consoleKeys)

	s.appRoutes(app)
	s.adminRoutes(app)
	s.productRoutes(app)
	s.workspaceRoutes(app)
	return app
}

func (s *Server) middleware(c *fiber.Ctx) error {
	start := time.Now()
	c.Set("Cache-Control", "no-store")
	c.Set("X-Content-Type-Options", "nosniff")
	c.SetUserContext(c.Context())
	var err error
	defer func() {
		if p := recover(); p != nil {
			slog.Error("handler panic", "panic", p)
			if c.Response().StatusCode() == fiber.StatusOK && len(c.Response().Body()) == 0 {
				_ = writeErr(c, &service.Error{Status: 500, Code: "internal_error"})
			}
		}
		code := c.Response().StatusCode()
		var fe *fiber.Error
		if err != nil && errors.As(err, &fe) && code == fiber.StatusOK {
			code = fe.Code
		}
		route := c.Route().Path
		if route == "" {
			route = c.Path()
		}
		slog.Info("request", "method", c.Method(), "route", route, "status", code, "ms", time.Since(start).Milliseconds())
	}()
	err = c.Next()
	return err
}

func reqCtx(c *fiber.Ctx) context.Context {
	if ctx := c.UserContext(); ctx != nil {
		return ctx
	}
	return context.Background()
}

func writeJSON(c *fiber.Ctx, status int, v any) error {
	c.Set(fiber.HeaderContentType, "application/json")
	c.Status(status)
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		return err
	}
	return c.Send(buf.Bytes())
}

func writeErr(c *fiber.Ctx, err error) error {
	var e *service.Error
	if !errors.As(err, &e) {
		e = &service.Error{Status: 500, Code: "internal_error"}
	}
	if e.RetryAfter > 0 {
		secs := int(e.RetryAfter.Round(time.Second) / time.Second)
		if secs < 1 {
			secs = 1
		}
		c.Set(fiber.HeaderRetryAfter, strconv.Itoa(secs))
	}
	if e.Status == 401 {
		c.Set(fiber.HeaderWWWAuthenticate, `Bearer realm="njiwa-mail"`)
	}
	body := map[string]string{"code": e.Code}
	if e.Message != "" {
		body["message"] = e.Message
	}
	return writeJSON(c, e.Status, map[string]any{"error": body})
}

func (s *Server) emailLimit() int64 {
	limit := s.MaxBodyBytes
	if s.EmailMaxBytes > limit {
		limit = s.EmailMaxBytes
	}
	if limit <= 0 {
		limit = 32 * 1024
	}
	return limit
}

func (s *Server) postEmail(c *fiber.Ctx) error {
	p, err := s.Svc.Authenticate(reqCtx(c), c.Get("Authorization"))
	if err != nil {
		return writeErr(c, err)
	}
	if err := p.AllowIP(s.clientIP(c)); err != nil {
		return writeErr(c, err)
	}
	limit := s.emailLimit()
	n := int64(len(c.Body()))
	if cl := int64(c.Request().Header.ContentLength()); cl > n {
		n = cl
	}
	if n > limit {
		return writeErr(c, &service.Error{Status: 413, Code: "body_too_large"})
	}
	res, err := s.Svc.Accept(reqCtx(c), p, c.Get("Idempotency-Key"), append([]byte(nil), c.Body()...))
	if err != nil {
		return writeErr(c, err)
	}
	return writeJSON(c, 202, res)
}

func (s *Server) getEmail(c *fiber.Ctx) error {
	p, err := s.Svc.Authenticate(reqCtx(c), c.Get("Authorization"))
	if err != nil {
		return writeErr(c, err)
	}
	if err := p.AllowIP(s.clientIP(c)); err != nil {
		return writeErr(c, err)
	}
	st, err := s.Svc.GetStatus(reqCtx(c), p, c.Params("id"))
	if err != nil {
		return writeErr(c, err)
	}
	return writeJSON(c, 200, st)
}

func (s *Server) ready(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(reqCtx(c), 2*time.Second)
	defer cancel()
	if err := s.Svc.Ready(ctx); err != nil {
		return writeJSON(c, 503, map[string]string{"status": "unavailable"})
	}
	return writeJSON(c, 200, map[string]string{"status": "ready"})
}

func (s *Server) clientIP(c *fiber.Ctx) string {
	if s.TrustProxy {
		if xf := strings.TrimSpace(strings.Split(c.Get("X-Forwarded-For"), ",")[0]); net.ParseIP(xf) != nil {
			return xf
		}
	}
	addr := c.Context().RemoteAddr().String()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func forbidden() error { return &service.Error{Status: 403, Code: "forbidden"} }

type adminFn func(c *fiber.Ctx, u *service.AdminUser) (any, error)

// adm wraps an admin handler with session, CSRF and role enforcement.
func (s *Server) adm(minRole string, h adminFn) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tok, err := cookieVal(c, adminCookie)
		if err != nil {
			return writeErr(c, &service.Error{Status: 401, Code: "unauthenticated"})
		}
		u, err := s.Svc.Session(reqCtx(c), tok)
		if err != nil {
			return writeErr(c, err)
		}
		if c.Method() != http.MethodGet && c.Method() != http.MethodHead {
			if subtleMismatch(c.Get(csrfHeader), u.CSRF) {
				return writeErr(c, &service.Error{Status: 403, Code: "csrf_token_invalid"})
			}
		}
		if !u.Can(minRole) {
			return writeErr(c, forbidden())
		}
		out, err := h(c, u)
		if err != nil {
			return writeErr(c, err)
		}
		return writeOut(c, out)
	}
}

func writeOut(c *fiber.Ctx, out any) error {
	switch v := out.(type) {
	case nil:
		return writeJSON(c, 200, map[string]bool{"ok": true})
	case rawResponse:
		return v.write(c)
	default:
		return writeJSON(c, 200, out)
	}
}

func subtleMismatch(got, want string) bool {
	return subtleCompare(got, want) != 1
}

// rawResponse is a non-JSON response (attachment download, HTML view, cookie write).
type rawResponse struct {
	header map[string]string
	body   []byte
	file   string
}

func (rr rawResponse) write(c *fiber.Ctx) error {
	for k, v := range rr.header {
		if strings.EqualFold(k, "Set-Cookie") {
			c.Response().Header.Add("Set-Cookie", v)
			continue
		}
		c.Set(k, v)
	}
	if rr.file != "" {
		b, err := os.ReadFile(rr.file)
		if err != nil {
			return writeErr(c, &service.Error{Status: 404, Code: "not_found"})
		}
		c.Status(200)
		return c.Send(b)
	}
	c.Status(200)
	return c.Send(rr.body)
}

func download(f *service.AttachmentFile) rawResponse {
	// Always served as an opaque download: the sender's content type and
	// filename are data, never instructions to the browser.
	return rawResponse{file: f.Path, header: map[string]string{
		"Content-Type":            "application/octet-stream",
		"Content-Disposition":     mime.FormatMediaType("attachment", map[string]string{"filename": safeFilename(f.Filename)}),
		"Content-Length":          strconv.Itoa(f.Size),
		"Content-Security-Policy": "sandbox; default-src 'none'",
		"X-Content-Type-Options":  "nosniff",
	}}
}

func safeFilename(n string) string {
	n = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, n)
	if n = strings.Trim(n, ". "); n == "" {
		n = "attachment"
	}
	return n
}

func jsonDecoder(b []byte) *json.Decoder {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec
}

func decode(c *fiber.Ctx, v any) error {
	if len(c.Body()) > adminMaxBody {
		return &service.Error{Status: 400, Code: "invalid_request", Message: "request body is not valid JSON for this endpoint"}
	}
	dec := json.NewDecoder(bytes.NewReader(c.Body()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &service.Error{Status: 400, Code: "invalid_request", Message: "request body is not valid JSON for this endpoint"}
	}
	return nil
}

func pageOf(c *fiber.Ctx) service.Page {
	l, _ := strconv.Atoi(c.Query("limit"))
	o, _ := strconv.Atoi(c.Query("offset"))
	return service.Page{Limit: l, Offset: o}
}

func list(c *fiber.Ctx, items []map[string]any, more bool, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	pg := pageOf(c)
	if pg.Limit <= 0 || pg.Limit > 100 {
		pg.Limit = 50
		if l, _ := strconv.Atoi(c.Query("limit")); l > 100 {
			pg.Limit = 100
		}
	}
	if pg.Offset < 0 {
		pg.Offset = 0
	}
	return map[string]any{"items": items, "has_more": more, "limit": pg.Limit, "offset": pg.Offset}, nil
}

func timeParam(c *fiber.Ctx, name string) (*time.Time, error) {
	v := c.Query(name)
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, &service.Error{Status: 400, Code: "invalid_request", Message: name + " must be an RFC 3339 timestamp"}
	}
	return &t, nil
}

// audit records a completed admin write. A failure to record is logged loudly
// but does not turn an already-committed change into an error response.
func (s *Server) audit(c *fiber.Ctx, u *service.AdminUser, action, targetType, targetID string, details map[string]any) {
	if err := s.Svc.Audit(reqCtx(c), u.Actor(), action, targetType, targetID, details); err != nil {
		slog.Error("AUDIT WRITE FAILED", "action", action, "target_type", targetType, "target_id", targetID, "actor", u.Email)
	}
}

func cookieVal(c *fiber.Ctx, name string) (string, error) {
	v := c.Cookies(name)
	if v == "" {
		return "", http.ErrNoCookie
	}
	return v, nil
}

func setCookie(c *fiber.Ctx, ck *http.Cookie) {
	c.Response().Header.Add("Set-Cookie", ck.String())
}

func notFound(c *fiber.Ctx) error {
	c.Status(fiber.StatusNotFound)
	return c.SendString("404 page not found\n")
}

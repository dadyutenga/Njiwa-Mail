package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/njiwa-tz/mail/internal/service"
)

// setupHeader must be present on POST /admin/v1/setup. A page on another site
// cannot add it without a CORS preflight, which this server never allows.
const setupHeader = "X-Njiwa-Setup"

// sameSiteRequest refuses a request that a browser says came from another site.
func sameSiteRequest(c fiber.Ctx) bool {
	if c.Get(setupHeader) == "" || !strings.HasPrefix(strings.ToLower(c.Get("Content-Type")), "application/json") {
		return false
	}
	if c.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if o := c.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil || !strings.EqualFold(u.Hostname(), c.Hostname()) {
			return false
		}
	}
	return true
}

func (s *Server) setupRoutes(app *fiber.App) {
	app.Get("/admin/v1/setup/status", func(c fiber.Ctx) error {
		needs, token, err := s.Svc.NeedsSetup(reqCtx(c))
		if err != nil {
			return writeErr(c, err)
		}
		return writeJSON(c, 200, map[string]any{"needs_setup": needs, "token_required": token})
	})
	// Public only while the installation has no user: creates the organization
	// and the first operator, signs them in, and is closed from then on.
	app.Post("/admin/v1/setup", func(c fiber.Ctx) error {
		if !sameSiteRequest(c) {
			return writeErr(c, &service.Error{Status: 403, Code: "cross_site_request"})
		}
		var in service.FirstRunInput
		if len(c.Body()) > 4096 || json.Unmarshal(c.Body(), &in) != nil {
			return writeErr(c, &service.Error{Status: 400, Code: "invalid_json"})
		}
		email, err := s.Svc.FirstRun(reqCtx(c), in, s.clientIP(c))
		if err != nil {
			return writeErr(c, err)
		}
		tok, u, err := s.Svc.Login(reqCtx(c), email, in.Password, s.clientIP(c), c.Get("User-Agent"))
		if err != nil {
			return writeErr(c, err)
		}
		setCookie(c, &http.Cookie{Name: adminCookie, Value: tok, Path: "/admin", HttpOnly: true, Secure: !s.InsecureCookies,
			SameSite: http.SameSiteLaxMode, Expires: u.ExpiresAt})
		return writeJSON(c, 200, u)
	})
}

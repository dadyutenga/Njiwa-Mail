package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/njiwa-tz/mail/internal/service"
)

func (s *Server) setAdminCookie(c fiber.Ctx, tok string, exp time.Time) {
	setCookie(c, &http.Cookie{Name: adminCookie, Value: tok, Path: "/admin", HttpOnly: true, Secure: !s.InsecureCookies,
		SameSite: http.SameSiteLaxMode, Expires: exp})
}

// ssoBack sends the browser to the sign-in page with a result the page
// understands. The code is one of a fixed set, never provider text.
func ssoBack(c fiber.Ctx, key, val string) error {
	return c.Redirect().Status(fiber.StatusFound).To("/login?" + key + "=" + url.QueryEscape(val))
}

// ssoRoutes: sign-in with an OpenID Connect provider, and the operator's
// provider settings. The sign-in routes are public by design: start only
// redirects, the callback needs a one-time state, and complete needs a
// pending token and the account's authenticator code.
func (s *Server) ssoRoutes(app *fiber.App) {
	svc := s.Svc
	app.Get("/admin/v1/auth/sso/providers", func(c fiber.Ctx) error {
		items, err := svc.PublicSSOProviders(reqCtx(c))
		if err != nil {
			return writeErr(c, err)
		}
		return writeJSON(c, 200, map[string]any{"items": items})
	})
	app.Get("/admin/v1/auth/sso/callback", func(c fiber.Ctx) error {
		if c.Query("error") != "" {
			return ssoBack(c, "sso_error", "sso_failed")
		}
		res, err := svc.FinishSSO(reqCtx(c), c.Query("state"), c.Query("code"), s.clientIP(c), c.Get("User-Agent"))
		if err != nil {
			if e, ok := service.IsError(err); ok && e.Status == 429 {
				return ssoBack(c, "sso_error", "too_many_attempts")
			}
			return ssoBack(c, "sso_error", "sso_failed")
		}
		if res.Pending != "" {
			return ssoBack(c, "sso_pending", res.Pending)
		}
		s.setAdminCookie(c, res.Token, res.User.ExpiresAt)
		return c.Redirect().Status(fiber.StatusFound).To("/")
	})
	app.Post("/admin/v1/auth/sso/complete", func(c fiber.Ctx) error {
		var in struct {
			Pending string `json:"pending"`
			Code    string `json:"code"`
		}
		if len(c.Body()) > 4096 || json.Unmarshal(c.Body(), &in) != nil {
			return writeErr(c, &service.Error{Status: 401, Code: "sso_failed"})
		}
		tok, u, err := svc.CompleteSSO(reqCtx(c), in.Pending, in.Code, s.clientIP(c), c.Get("User-Agent"))
		if err != nil {
			return writeErr(c, err)
		}
		s.setAdminCookie(c, tok, u.ExpiresAt)
		return writeJSON(c, 200, u)
	})
	app.Get("/admin/v1/auth/sso/:id/start", func(c fiber.Ctx) error {
		to, err := svc.StartSSO(reqCtx(c), c.Params("id"), s.clientIP(c))
		if err != nil {
			code := "sso_unavailable"
			if e, ok := service.IsError(err); ok && (e.Code == "too_many_attempts" || e.Code == "public_url_missing") {
				code = e.Code
			}
			return ssoBack(c, "sso_error", code)
		}
		return c.Redirect().Status(fiber.StatusFound).To(to)
	})

	op := func(method, path string, fn adminFn) { route(app, method, path, s.adm(s.op(fn))) }
	op(fiber.MethodGet, "/admin/v1/sso-providers", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		items, err := svc.ListSSOProviders(reqCtx(c))
		return map[string]any{"items": items}, err
	})
	op(fiber.MethodPost, "/admin/v1/sso-providers", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		var in service.SSOProviderInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, err := svc.CreateSSOProvider(reqCtx(c), in)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "sso_provider.create", "sso_provider", id, map[string]any{"name": in.Name, "issuer": in.Issuer})
		return map[string]string{"id": id}, nil
	})
	op(fiber.MethodPut, "/admin/v1/sso-providers/:id", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		var in service.SSOProviderInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.UpdateSSOProvider(reqCtx(c), c.Params("id"), in); err != nil {
			return nil, err
		}
		s.audit(c, u, "sso_provider.update", "sso_provider", c.Params("id"), map[string]any{"name": in.Name, "secret_changed": in.ClientSecret != ""})
		return nil, nil
	})
	op(fiber.MethodDelete, "/admin/v1/sso-providers/:id", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		if err := svc.DeleteSSOProvider(reqCtx(c), c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "sso_provider.delete", "sso_provider", c.Params("id"), nil)
		return nil, nil
	})
}

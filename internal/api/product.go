package api

import (
	"encoding/json"

	"github.com/ditronics-tz/mail/internal/service"
	"github.com/gofiber/fiber/v2"
)

func (s *Server) productRoutes(app *fiber.App) {
	svc := s.Svc
	h := func(method, path, role string, fn adminFn) {
		app.Add(method, path, s.adm(role, fn))
	}
	const V, A = service.RoleAdmin, service.RoleAdmin
	h(fiber.MethodGet, "/admin/v1/analytics", V, func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, err := svc.Analytics(reqCtx(c), c.Query("project"), c.Query("mode"), c.Query("template"), c.Query("domain"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items}, nil
	})
	h(fiber.MethodGet, "/admin/v1/domains", V, func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListDomains(reqCtx(c), pageOf(c))
		return list(c, items, more, err)
	})
	h(fiber.MethodPost, "/admin/v1/domains", A, func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Project, Name, Purpose string
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, token, err := svc.CreateDomain(reqCtx(c), in.Project, in.Name, in.Purpose)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "domain.create", "domain", id, map[string]any{"name": in.Name, "purpose": in.Purpose})
		return map[string]string{"id": id, "txt": "ditronics-mail-verify=" + token}, nil
	})
	h(fiber.MethodPost, "/admin/v1/domains/:id/verify", A, func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		if err := svc.VerifyDomain(reqCtx(c), c.Params("id"), s.Resolver); err != nil {
			return nil, err
		}
		s.audit(c, u, "domain.verify", "domain", c.Params("id"), nil)
		return nil, nil
	})
	h(fiber.MethodPost, "/admin/v1/auth/totp", V, func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		url, err := svc.BeginTOTP(reqCtx(c), u.ID, u.Email)
		if err != nil {
			return nil, err
		}
		return map[string]string{"otpauth_url": url}, nil
	})
	h(fiber.MethodPost, "/admin/v1/auth/totp/confirm", V, func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Code string `json:"code"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		codes, err := svc.ConfirmTOTP(reqCtx(c), u.ID, in.Code)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "auth.totp_enable", "user", u.ID, nil)
		return map[string]any{"recovery_codes": codes}, nil
	})
	h(fiber.MethodGet, "/admin/v1/auth/sessions", V, func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, err := svc.ListSessions(reqCtx(c), u.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items}, nil
	})
	h(fiber.MethodDelete, "/admin/v1/auth/sessions/:id", V, func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		if err := svc.RevokeSession(reqCtx(c), u.ID, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "auth.session_revoke", "session", c.Params("id"), nil)
		return nil, nil
	})
	app.Post("/admin/v1/auth/forgot", func(c *fiber.Ctx) error {
		var in struct {
			Email string `json:"email"`
		}
		if err := decode(c, &in); err != nil {
			return writeErr(c, err)
		}
		_ = svc.RequestPasswordReset(reqCtx(c), in.Email)
		return writeJSON(c, 200, map[string]bool{"ok": true})
	})
	app.Post("/admin/v1/auth/reset", func(c *fiber.Ctx) error {
		var in struct {
			Token, Password string
		}
		if err := decode(c, &in); err != nil {
			return writeErr(c, err)
		}
		if err := svc.ResetPassword(reqCtx(c), in.Token, in.Password); err != nil {
			return writeErr(c, err)
		}
		return writeJSON(c, 200, map[string]bool{"ok": true})
	})
	h(fiber.MethodGet, "/admin/v1/alert-channels", V, func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, err := svc.ListAlertChannels(reqCtx(c))
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items}, nil
	})
	h(fiber.MethodPost, "/admin/v1/alert-channels", A, func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct{ Kind, Target string }
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.SaveAlertChannel(reqCtx(c), in.Kind, in.Target); err != nil {
			return nil, err
		}
		s.audit(c, u, "alert_channel.create", "alert_channel", in.Kind, nil)
		return nil, nil
	})
	h(fiber.MethodPost, "/admin/v1/privacy/export", A, func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct{ Project, Mode, Email string }
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		return svc.ExportRecipient(reqCtx(c), in.Project, in.Mode, in.Email)
	})
	h(fiber.MethodPost, "/admin/v1/privacy/delete", A, func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct{ Project, Mode, Email string }
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.DeleteRecipient(reqCtx(c), in.Project, in.Mode, in.Email); err != nil {
			return nil, err
		}
		s.audit(c, u, "privacy.delete", "recipient", "", map[string]any{"project": in.Project, "mode": in.Mode})
		return nil, nil
	})
	h(fiber.MethodGet, "/admin/v1/docs", V, func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		return map[string]any{"operations": v1Operations}, nil
	})
}

func (s *Server) postBatch(c *fiber.Ctx) error {
	p, err := s.Svc.Authenticate(reqCtx(c), c.Get("Authorization"))
	if err != nil {
		return writeErr(c, err)
	}
	if err := p.AllowIP(s.clientIP(c)); err != nil {
		return writeErr(c, err)
	}
	if len(c.Body()) > 6<<20 {
		return writeErr(c, &service.Error{Status: 422, Code: "invalid_request"})
	}
	var in struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if json.Unmarshal(c.Body(), &in) != nil {
		return writeErr(c, &service.Error{Status: 422, Code: "invalid_request"})
	}
	out, err := s.Svc.AcceptBatch(reqCtx(c), p, in.Messages)
	if err != nil {
		return writeErr(c, err)
	}
	return writeJSON(c, 200, map[string]any{"results": out})
}

func (s *Server) cancelEmail(c *fiber.Ctx) error {
	p, err := s.Svc.Authenticate(reqCtx(c), c.Get("Authorization"))
	if err != nil {
		return writeErr(c, err)
	}
	if err := p.AllowIP(s.clientIP(c)); err != nil {
		return writeErr(c, err)
	}
	if !p.HasScope(service.ScopeSend) && !p.HasScope(service.ScopeSendRaw) {
		return writeErr(c, &service.Error{Status: 403, Code: "missing_scope"})
	}
	if err := s.Svc.Cancel(reqCtx(c), p, c.Params("id")); err != nil {
		return writeErr(c, err)
	}
	return writeJSON(c, 200, map[string]string{"status": "cancelled"})
}

var v1Operations = []map[string]string{
	{"method": "POST", "path": "/v1/emails", "purpose": "Queue one template or raw email"},
	{"method": "POST", "path": "/v1/emails/batch", "purpose": "Queue up to 100 emails, each with its own result"},
	{"method": "POST", "path": "/v1/emails/{id}/cancel", "purpose": "Cancel a queued or scheduled email"},
	{"method": "GET", "path": "/v1/emails/{id}", "purpose": "Status and safe events"},
	{"method": "GET", "path": "/v1/inbound", "purpose": "List inbound mail, with q for search"},
	{"method": "GET", "path": "/v1/inbound/{id}", "purpose": "One inbound message"},
	{"method": "GET", "path": "/v1/inbound/{id}/attachments/{aid}", "purpose": "Download an attachment"},
	{"method": "GET", "path": "/v1/suppressions", "purpose": "List suppressions"},
	{"method": "POST", "path": "/v1/suppressions", "purpose": "Add a suppression"},
	{"method": "DELETE", "path": "/v1/suppressions/{id}", "purpose": "Remove a suppression"},
}

package api

import (
	"encoding/json"

	"github.com/gofiber/fiber/v3"
	"github.com/njiwa-tz/mail/internal/service"
)

func (s *Server) productRoutes(app *fiber.App) {
	svc := s.Svc
	h := func(method, path string, fn adminFn) {
		route(app, method, path, s.adm(fn))
	}
	h(fiber.MethodGet, "/admin/v1/analytics", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		items, err := svc.Analytics(reqCtx(c), c.Query("project"), c.Query("mode"), c.Query("template"), c.Query("domain"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items}, nil
	})
	// ---- the user's notification inbox, and operator announcements
	h(fiber.MethodGet, "/admin/v1/notifications", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		return svc.Notifications(reqCtx(c), u)
	})
	h(fiber.MethodPost, "/admin/v1/notifications/read-all", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		return nil, svc.MarkAllNotificationsRead(reqCtx(c), u)
	})
	h(fiber.MethodPost, "/admin/v1/notifications/:kind/:id/:action", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		return nil, svc.MarkNotification(reqCtx(c), u, c.Params("kind"), c.Params("id"), c.Params("action"))
	})
	op := func(method, path string, fn adminFn) { route(app, method, path, s.adm(s.op(fn))) }
	op(fiber.MethodGet, "/admin/v1/announcements", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListAnnouncements(reqCtx(c), pageOf(c))
		return list(c, items, more, err)
	})
	op(fiber.MethodPost, "/admin/v1/announcements", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct{ Title, Body, Link string }
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, err := svc.CreateAnnouncement(reqCtx(c), u.Email, in.Title, in.Body, in.Link)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "announcement.create", "announcement", id, map[string]any{"title": in.Title})
		return map[string]string{"id": id}, nil
	})
	op(fiber.MethodPost, "/admin/v1/announcements/:id/retract", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		if err := svc.RetractAnnouncement(reqCtx(c), c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "announcement.retract", "announcement", c.Params("id"), nil)
		return nil, nil
	})
	// ---- usage plans (operators)
	op(fiber.MethodGet, "/admin/v1/plans", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		items, err := svc.ListPlans(reqCtx(c))
		return map[string]any{"items": items}, err
	})
	op(fiber.MethodPost, "/admin/v1/plans", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		var in service.UsagePlanInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, err := svc.CreatePlan(reqCtx(c), in)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "plan.create", "plan", id, map[string]any{"name": in.Name})
		return map[string]string{"id": id}, nil
	})
	op(fiber.MethodPut, "/admin/v1/plans/:id", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		var in service.UsagePlanInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		n, err := svc.UpdatePlan(reqCtx(c), c.Params("id"), in)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "plan.update", "plan", c.Params("id"), map[string]any{"projects": n})
		return map[string]int{"projects_updated": n}, nil
	})
	op(fiber.MethodDelete, "/admin/v1/plans/:id", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		if err := svc.DeletePlan(reqCtx(c), c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "plan.delete", "plan", c.Params("id"), nil)
		return nil, nil
	})
	op(fiber.MethodPost, "/admin/v1/projects/:name/plan", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			PlanID string `json:"plan_id"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.AssignPlan(reqCtx(c), c.Params("name"), in.PlanID); err != nil {
			return nil, err
		}
		s.audit(c, u, "project.plan", "project", c.Params("name"), map[string]any{"plan_id": in.PlanID})
		return nil, nil
	})
	op(fiber.MethodGet, "/admin/v1/usage", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		items, err := svc.Usage(reqCtx(c))
		return map[string]any{"items": items}, err
	})
	h(fiber.MethodGet, "/admin/v1/domains", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListDomains(reqCtx(c), pageOf(c))
		return list(c, items, more, err)
	})
	h(fiber.MethodPost, "/admin/v1/domains", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
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
		return map[string]string{"id": id, "txt": "njiwa-mail-verify=" + token}, nil
	})
	h(fiber.MethodPost, "/admin/v1/domains/:id/verify", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		if err := svc.VerifyDomain(reqCtx(c), c.Params("id"), s.Resolver); err != nil {
			return nil, err
		}
		s.audit(c, u, "domain.verify", "domain", c.Params("id"), nil)
		return nil, nil
	})
	h(fiber.MethodGet, "/admin/v1/auth/sessions", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		items, err := svc.ListSessions(reqCtx(c), u.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items}, nil
	})
	h(fiber.MethodDelete, "/admin/v1/auth/sessions/:id", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		if err := svc.RevokeSession(reqCtx(c), u.ID, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "auth.session_revoke", "session", c.Params("id"), nil)
		return nil, nil
	})
	app.Post("/admin/v1/auth/forgot", func(c fiber.Ctx) error {
		var in struct {
			Email string `json:"email"`
		}
		if err := decode(c, &in); err != nil {
			return writeErr(c, err)
		}
		_ = svc.RequestPasswordReset(reqCtx(c), in.Email)
		return writeJSON(c, 200, map[string]bool{"ok": true})
	})
	app.Post("/admin/v1/auth/reset", func(c fiber.Ctx) error {
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
	h(fiber.MethodGet, "/admin/v1/alert-channels", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		items, err := svc.ListAlertChannels(reqCtx(c))
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items}, nil
	})
	h(fiber.MethodPost, "/admin/v1/alert-channels", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
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
	h(fiber.MethodPost, "/admin/v1/privacy/export", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct{ Project, Mode, Email string }
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		return svc.ExportRecipient(reqCtx(c), in.Project, in.Mode, in.Email)
	})
	h(fiber.MethodPost, "/admin/v1/privacy/delete", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
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
	h(fiber.MethodGet, "/admin/v1/docs", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		return map[string]any{"operations": v1Operations}, nil
	})
}

func (s *Server) postBatch(c fiber.Ctx) error {
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

func (s *Server) cancelEmail(c fiber.Ctx) error {
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

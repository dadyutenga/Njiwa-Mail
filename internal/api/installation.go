package api

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/service"
)

// Installation routes: installation-level readiness and the transport profile
// registry. These are operator-only (adm) and independent of any project.
func (s *Server) installationRoutes(app *fiber.App) {
	svc := s.Svc
	h := func(method, path string, fn adminFn) { app.Add(method, "/admin/v1/installation"+path, s.adm(fn)) }

	h("GET", "", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		st, err := svc.InstallationState(reqCtx(c))
		if err != nil {
			return nil, err
		}
		// When SMTP_HOST is set in the environment it wins at runtime; say so
		// instead of letting the saved profile look authoritative.
		st["environment_override"] = s.EnvSMTP != nil
		return st, nil
	})
	h("GET", "/transport-profiles", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		l, err := svc.ListTransportProfiles(reqCtx(c))
		if err != nil {
			return nil, err
		}
		return map[string]any{"profiles": l}, nil
	})
	h("POST", "/transport-profiles", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in service.TransportProfileInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		p, err := svc.CreateTransportProfile(reqCtx(c), u.Actor(), in)
		if err != nil {
			return nil, err
		}
		return p, nil
	})
	h("GET", "/transport-profiles/:id", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		return svc.TransportProfile(reqCtx(c), c.Params("id"))
	})
	h("PUT", "/transport-profiles/:id", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in service.TransportProfileInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		return svc.UpdateTransportProfile(reqCtx(c), u.Actor(), c.Params("id"), in)
	})
	h("DELETE", "/transport-profiles/:id", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		return nil, svc.RetireTransportProfile(reqCtx(c), u.Actor(), c.Params("id"))
	})
	// Connection diagnosis for one profile, recorded against its exact revision.
	h("POST", "/transport-profiles/:id/test", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		id := c.Params("id")
		cfg, sender, rev, err := svc.ProfileSMTP(reqCtx(c), id)
		if err != nil {
			return nil, err
		}
		tctx, cancel := context.WithTimeout(reqCtx(c), 25*time.Second)
		defer cancel()
		steps := mailer.Diagnose(tctx, cfg, sender)
		ok := true
		for _, st := range steps {
			ok = ok && (st.OK || (st.Skipped && st.Key == "sender" && sender == ""))
		}
		if err := svc.RecordInstallationCheck(reqCtx(c), id, rev, "connection", ok, service.ProblemCode("connection", steps), nil, false); err != nil {
			return nil, &service.Error{Status: 503, Code: "service_unavailable"}
		}
		s.audit(c, u, "transport.test", "transport_profile", id, map[string]any{"ok": ok, "revision": rev})
		return map[string]any{"ok": ok, "steps": steps, "config_revision": rev}, nil
	})
	h("GET", "/transport-profiles/:id/activation-plan", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		return svc.PlanTransportSwitch(reqCtx(c), c.Params("id"))
	})
	h("POST", "/transport-profiles/:id/activate", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			MigratePending bool `json:"migrate_pending"`
		}
		if len(c.Body()) > 0 {
			if err := decode(c, &in); err != nil {
				return nil, err
			}
		}
		return svc.ActivateTransport(reqCtx(c), u.Actor(), c.Params("id"), in.MigratePending)
	})
}

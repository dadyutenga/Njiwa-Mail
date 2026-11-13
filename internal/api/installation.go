package api

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/service"
)

// Installation routes: installation-level readiness and the transport profile
// registry. These are operator-only (adm) and independent of any project.
func (s *Server) installationRoutes(app *fiber.App) {
	svc := s.Svc
	h := func(method, path string, fn adminFn) {
		route(app, method, "/admin/v1/installation"+path, s.adm(s.op(fn)))
	}

	// Installation-wide domain claims (operator only).
	route(app, "GET", "/admin/v1/domain-claims", s.adm(s.op(func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		l, err := svc.DomainClaims(reqCtx(c))
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": l}, nil
	})))
	route(app, "POST", "/admin/v1/domain-claims/transfer", s.adm(s.op(func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Name    string `json:"name"`
			Project string `json:"project"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		return nil, svc.TransferDomainClaim(reqCtx(c), u.Actor(), in.Name, in.Project)
	})))
	// The managed mail host as the installer recorded it: receipts, delivery evidence flow,
	// signing domains without a key yet, and a staged DKIM rotation.
	h("GET", "/mail-host", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		receipts, err := svc.InstallerReceipts(reqCtx(c), 50)
		if err != nil {
			return nil, err
		}
		evidence, err := svc.EvidenceStatus(reqCtx(c))
		if err != nil {
			return nil, err
		}
		missing, err := svc.DomainsMissingKey(reqCtx(c))
		if err != nil {
			return nil, err
		}
		rotation, err := svc.RotationView(reqCtx(c), s.Resolver)
		if err != nil {
			return nil, err
		}
		return map[string]any{"receipts": receipts, "evidence": evidence, "domains_missing_key": missing,
			"suggested_commands": svc.SuggestedInstallerCommands(reqCtx(c)), "dkim_rotation": rotation}, nil
	})
	// What the latest read-only host discovery found (njiwa-mail discover -record).
	h("GET", "/discovery", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		return svc.Discovery(reqCtx(c))
	})
	h("GET", "", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		st, err := svc.InstallationState(reqCtx(c))
		if err != nil {
			return nil, err
		}
		// When SMTP_HOST is set in the environment it wins at runtime; say so
		// instead of letting the saved profile look authoritative.
		st["environment_override"] = s.EnvSMTP != nil
		return st, nil
	})
	// The operator's confirmed delivery-mode choice, persisted server-side.
	h("PUT", "/wizard", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			SelectedMode string `json:"selected_mode"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		st, err := svc.ChooseDeliveryMode(reqCtx(c), u.Actor(), in.SelectedMode)
		if err != nil {
			return nil, err
		}
		st["environment_override"] = s.EnvSMTP != nil
		return st, nil
	})
	h("GET", "/transport-profiles", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		l, err := svc.ListTransportProfiles(reqCtx(c))
		if err != nil {
			return nil, err
		}
		return map[string]any{"profiles": l}, nil
	})
	h("POST", "/transport-profiles", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
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
	h("GET", "/transport-profiles/:id", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		return svc.TransportProfile(reqCtx(c), c.Params("id"))
	})
	h("PUT", "/transport-profiles/:id", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		var in service.TransportProfileInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		return svc.UpdateTransportProfile(reqCtx(c), u.Actor(), c.Params("id"), in)
	})
	h("DELETE", "/transport-profiles/:id", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		return nil, svc.RetireTransportProfile(reqCtx(c), u.Actor(), c.Params("id"))
	})
	// What an existing SMTP server sends from (C07).
	h("PUT", "/transport-profiles/:id/sending-facts", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		var in service.SendingFactsInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		return svc.SetSendingFacts(reqCtx(c), u.Actor(), c.Params("id"), in)
	})
	// Connection diagnosis for one profile, recorded against its exact revision.
	h("POST", "/transport-profiles/:id/test", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
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
	h("GET", "/transport-profiles/:id/activation-plan", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		return svc.PlanTransportSwitch(reqCtx(c), c.Params("id"))
	})
	h("POST", "/transport-profiles/:id/activate", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
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

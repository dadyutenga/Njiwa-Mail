package api

import (
	"crypto/subtle"

	"github.com/gofiber/fiber/v3"
	"github.com/njiwa-tz/mail/internal/service"
)

// Application API on /v1: inbound mail and suppressions. Every
// request authenticates an API key and every query is scoped to that key's
// project AND environment.

type keyFn func(c fiber.Ctx, p *service.Principal) (any, error)

func (s *Server) key(scope string, h keyFn) fiber.Handler {
	return func(c fiber.Ctx) error {
		p, err := s.Svc.Authenticate(reqCtx(c), c.Get("Authorization"))
		if err != nil {
			return writeErr(c, err)
		}
		if err := p.AllowIP(s.clientIP(c)); err != nil {
			return writeErr(c, err)
		}
		if !p.HasScope(scope) {
			return writeErr(c, &service.Error{Status: 403, Code: "missing_scope"})
		}
		out, err := h(c, p)
		if err != nil {
			return writeErr(c, err)
		}
		return writeOut(c, out)
	}
}

func (s *Server) appRoutes(app *fiber.App) {
	svc := s.Svc
	app.Post("/v1/inbound/ingest/:env", s.ingestInbound)
	app.Get("/v1/inbound", s.key(service.ScopeInboundRead, func(c fiber.Ctx, p *service.Principal) (any, error) {
		items, more, err := svc.ListInbound(reqCtx(c), p.EnvironmentID, "", "", c.Query("q"), pageOf(c))
		return list(c, items, more, err)
	}))
	app.Get("/v1/inbound/:id", s.key(service.ScopeInboundRead, func(c fiber.Ctx, p *service.Principal) (any, error) {
		return svc.GetInbound(reqCtx(c), p.EnvironmentID, c.Params("id"), true)
	}))
	app.Get("/v1/inbound/:id/attachments/:aid", s.key(service.ScopeInboundRead, func(c fiber.Ctx, p *service.Principal) (any, error) {
		f, err := svc.OpenAttachment(reqCtx(c), p.EnvironmentID, c.Params("id"), c.Params("aid"))
		if err != nil {
			return nil, err
		}
		return download(f), nil
	}))

	actor := func(p *service.Principal) service.Actor {
		return service.Actor{KeyID: p.KeyID, Name: "key:" + p.KeyPrefix}
	}
	app.Get("/v1/suppressions", s.key(service.ScopeSuppressions, func(c fiber.Ctx, p *service.Principal) (any, error) {
		items, more, err := svc.KeySuppressions(reqCtx(c), p, pageOf(c))
		return list(c, items, more, err)
	}))
	app.Post("/v1/suppressions", s.key(service.ScopeSuppressions, func(c fiber.Ctx, p *service.Principal) (any, error) {
		var in struct {
			Email  string `json:"email"`
			Reason string `json:"reason"`
		}
		if err := decode(c, &in); err != nil {
			return nil, &service.Error{Status: 422, Code: "invalid_request"}
		}
		id, err := svc.KeySuppress(reqCtx(c), p, in.Email, in.Reason)
		if err != nil {
			return nil, err
		}
		svc.Audit(reqCtx(c), actor(p), "suppression.add", "suppression", id, nil)
		return map[string]string{"id": id}, nil
	}))
	// :id is a suppression id or the email address itself.
	app.Delete("/v1/suppressions/:id", s.key(service.ScopeSuppressions, func(c fiber.Ctx, p *service.Principal) (any, error) {
		if err := svc.KeyUnsuppress(reqCtx(c), p, c.Params("id")); err != nil {
			return nil, err
		}
		svc.Audit(reqCtx(c), actor(p), "suppression.remove", "suppression", "", nil)
		return nil, nil
	}))

	// Prometheus metrics: disabled (404) unless a token is configured.
	app.Get("/metrics", func(c fiber.Ctx) error {
		want := "Bearer " + s.MetricsToken
		if s.MetricsToken == "" || subtle.ConstantTimeCompare([]byte(c.Get("Authorization")), []byte(want)) != 1 {
			return notFound(c)
		}
		text, err := svc.Metrics(reqCtx(c))
		if err != nil {
			return writeErr(c, err)
		}
		c.Set(fiber.HeaderContentType, "text/plain; version=0.0.4; charset=utf-8")
		return c.SendString(text)
	})
}

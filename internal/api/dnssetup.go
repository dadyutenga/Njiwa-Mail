package api

import (
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/njiwa-tz/mail/internal/service"
)

// DNS setup routes (docs/DNS_SETUP.md). Everything is project-scoped and goes
// through ws/adm: a session is required, writes require the CSRF token, and
// the project is resolved from the path before any handler runs.

func (s *Server) dnsRoutes(app *fiber.App) {
	svc := s.Svc
	base := "/admin/v1/w/:project/:mode"
	h := func(method, path string, fn wsFn) { route(app, method, base+path, s.ws(fn)) }
	ctx := reqCtx

	h("GET", "/dns/capabilities", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.DNSCapabilities(), nil
	})
	h("GET", "/dns/connections", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.DNSConnections(ctx(c), sc)
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items}, nil
	})
	h("POST", "/dns/connections", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Provider string `json:"provider"`
			Token    string `json:"token"`
			Label    string `json:"label"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if in.Provider == "" {
			in.Provider = "cloudflare"
		}
		out, err := svc.ConnectDNSToken(ctx(c), sc, u.Actor(), in.Provider, in.Token, in.Label)
		if err != nil {
			return nil, err
		}
		return out, nil // the audit row is written by the service; the token never appears in it
	})
	h("PUT", "/dns/connections/:id/token", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Token string `json:"token"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		return svc.RotateDNSToken(ctx(c), sc, u.Actor(), c.Params("id"), in.Token)
	})
	h("DELETE", "/dns/connections/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return nil, svc.DisconnectDNS(ctx(c), sc, u.Actor(), c.Params("id"))
	})
	h("GET", "/dns/connections/:id/zones", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		zs, err := svc.DNSZones(ctx(c), sc, c.Params("id"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": zs}, nil
	})
	h("POST", "/dns/oauth/start", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			DomainID string `json:"domain_id"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		link, err := svc.StartDNSOAuth(ctx(c), sc, u.Actor(), in.DomainID)
		if err != nil {
			return nil, err
		}
		return map[string]string{"url": link}, nil
	})
	// The one fixed redirect URI. The browser arrives here from the provider
	// with the admin session cookie (a top-level GET). The state is single-use
	// and bound to the user who started the flow.
	route(app, "GET", "/admin/v1/dns/oauth/callback", s.adm(func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		if c.Query("error") != "" {
			return s.oauthBack("", "", "", "denied"), nil
		}
		project, domainID, conn, err := svc.CompleteDNSOAuth(ctx(c), u.Actor(), c.Query("code"), c.Query("state"))
		if err != nil {
			code := "failed"
			if e, ok := service.IsError(err); ok {
				code = e.Code
			}
			return s.oauthBack("", "", "", code), nil
		}
		return s.oauthBack(project, domainID, conn["id"].(string), ""), nil
	}))

	h("GET", "/domains/:id/dns-manual", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.ManualDNSRecords(ctx(c), sc, c.Params("id"))
	})
	h("GET", "/domains/:id/dns-setup", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.LatestDNSSetup(ctx(c), sc, c.Params("id"))
	})
	h("POST", "/domains/:id/dns-setup", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			ConnectionID string `json:"connection_id"`
			ZoneID       string `json:"zone_id"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		return svc.StartDNSSetup(ctx(c), sc, u.Actor(), c.Params("id"), in.ConnectionID, in.ZoneID, s.Resolver)
	})
	h("GET", "/dns-setups/:sid", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.SetupView(ctx(c), sc, c.Params("sid"))
	})
	h("POST", "/dns-setups/:sid/analyze", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.ReanalyzeDNSSetup(ctx(c), sc, u.Actor(), c.Params("sid"), s.Resolver)
	})
	h("POST", "/dns-setups/:sid/approve", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Digest      string   `json:"digest"`
			Acknowledge []string `json:"acknowledge"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		return svc.ApproveDNSSetup(ctx(c), sc, u.Actor(), c.Params("sid"), in.Digest, in.Acknowledge)
	})
	h("POST", "/dns-setups/:sid/apply", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.ApplyDNSSetup(ctx(c), sc, u.Actor(), c.Params("sid"), s.Resolver)
	})
	h("POST", "/dns-setups/:sid/verify", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.VerifyDNSSetup(ctx(c), sc, u.Actor(), c.Params("sid"), s.Resolver)
	})
	h("POST", "/dns-setups/:sid/rollback", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.RollbackDNSSetup(ctx(c), sc, u.Actor(), c.Params("sid"), s.Resolver)
	})
}

// oauthBack sends the browser back into the dashboard. Only the project name
// (path-escaped) and ids go into the URL; errors are fixed codes, never text.
func (s *Server) oauthBack(project, domainID, connID, errCode string) rawResponse {
	origin := strings.TrimRight(s.Svc.Opts.PublicURL, "/")
	path := "/app"
	q := url.Values{}
	if project != "" {
		path = "/p/" + url.PathEscape(project) + "/domains"
		if domainID != "" {
			path += "/" + url.PathEscape(domainID)
		}
		q.Set("dns_connection", connID)
	} else {
		q.Set("dns_error", errCode)
	}
	return rawResponse{status: 302, header: map[string]string{"Location": origin + path + "?" + q.Encode(), "Cache-Control": "no-store"}}
}

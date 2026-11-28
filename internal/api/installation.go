package api

import (
	"context"
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/njiwa-tz/mail/internal/installer/discover"
	"github.com/njiwa-tz/mail/internal/installer/managed"
	"github.com/njiwa-tz/mail/internal/mtaagent"
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
		commands, needs := svc.SuggestedInstallerCommands(reqCtx(c), c.Query("hostname"), c.Query("digest"))
		return map[string]any{"receipts": receipts, "evidence": evidence, "domains_missing_key": missing,
			"suggested_commands": commands, "commands_need": needs, "dkim_rotation": rotation}, nil
	})
	// Domain expansion on an installation the host agent already owns. The
	// operator reviews the exact digest; the agent refuses a different plan.
	// The host CLI remains the path when the agent is not configured.
	s.hostInstallRoutes(h)
	h("POST", "/domains/plan", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		domains, err := s.claimedDomainRequest(c)
		if err != nil {
			return nil, err
		}
		agent, err := s.domainAgent()
		if err != nil {
			return nil, err
		}
		pl, err := agent.PlanDomains(reqCtx(c), domains)
		if err != nil {
			return nil, domainAgentErr(err)
		}
		files := make([]map[string]string, 0, len(pl.Files))
		for _, f := range pl.Files {
			if f.Change != "unchanged" {
				files = append(files, map[string]string{"path": f.Path, "change": f.Change})
			}
		}
		return map[string]any{"digest": pl.Digest, "domains": pl.Params.Domains, "selector": pl.Params.Selector, "files": files}, nil
	})
	h("POST", "/domains/apply", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Domains []string `json:"domains"`
			Digest  string   `json:"digest"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if in.Digest == "" {
			return nil, &service.Error{Status: 422, Code: "digest_required"}
		}
		domains, err := s.onlyClaimed(reqCtx(c), in.Domains)
		if err != nil {
			return nil, err
		}
		agent, err := s.domainAgent()
		if err != nil {
			return nil, err
		}
		res, err := agent.ApplyDomains(reqCtx(c), domains, in.Digest)
		if err != nil {
			return nil, domainAgentErr(err)
		}
		if err := svc.RecordManagedDomainKeys(reqCtx(c), u.Actor(), res.ReceiptID, res.Digest, res.Selector, res.DKIMRecords); err != nil {
			return nil, err
		}
		return map[string]any{"receipt_id": res.ReceiptID, "digest": res.Digest, "selector": res.Selector, "dkim_records": res.DKIMRecords}, nil
	})
	// The ten-step setup journey, derived from server state (resumable).
	h("GET", "/setup", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		return svc.SetupJourney(reqCtx(c), c.Query("project"))
	})
	// What the latest read-only host discovery found (njiwa-mail discover -record).
	h("GET", "/discovery", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		return svc.Discovery(reqCtx(c))
	})
	h("POST", "/discovery", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		scan := s.DiscoverHost
		if agent, ok := s.MTA.(interface {
			Discover(context.Context) (*discover.Report, error)
		}); ok {
			scan = agent.Discover
		}
		if scan == nil {
			return nil, &service.Error{Status: 409, Code: "host_discovery_unavailable", Message: "Connect the host agent to scan this server. For local development, restart with go run .; alternatively run go run . discover -record from the project folder on the host."}
		}
		report, err := scan(reqCtx(c))
		if err != nil {
			return nil, mtaErr(err)
		}
		if report == nil {
			return nil, &service.Error{Status: 502, Code: "host_discovery_failed"}
		}
		needs := s.DiscoveryNeeds
		if needs.APIPort == 0 {
			needs = discover.DefaultNeeds()
		}
		recommendation := discover.Recommend(report, needs)
		report.Recommendation = &recommendation
		if err := svc.RecordDiscovery(reqCtx(c), u.Actor(), report); err != nil {
			return nil, err
		}
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
		steps, ok, rev, err := svc.CheckTransportProfile(reqCtx(c), id, s.SMTPRootCAs)
		if err != nil {
			return nil, err
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

// managedDomainAgent is the host agent's domain expansion. Queue control does
// not imply this; a client that only implements MTAAgent cannot onboard domains.
type managedDomainAgent interface {
	PlanDomains(ctx context.Context, domains []string) (*managed.Plan, error)
	ApplyDomains(ctx context.Context, domains []string, digest string) (*managed.DomainResult, error)
}

func (s *Server) domainAgent() (managedDomainAgent, error) {
	agent, ok := s.MTA.(managedDomainAgent)
	if s.MTA == nil || !ok {
		return nil, &service.Error{Status: 409, Code: "mta_agent_not_configured"}
	}
	return agent, nil
}

func (s *Server) claimedDomainRequest(c fiber.Ctx) ([]string, error) {
	var in struct {
		Domains []string `json:"domains"`
	}
	if err := decode(c, &in); err != nil {
		return nil, err
	}
	return s.onlyClaimed(reqCtx(c), in.Domains)
}

func (s *Server) onlyClaimed(ctx context.Context, domains []string) ([]string, error) {
	if len(domains) == 0 || len(domains) > 100 {
		return nil, &service.Error{Status: 400, Code: "invalid_domains"}
	}
	claimed, err := s.Svc.ClaimedDomains(ctx)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, d := range claimed {
		allowed[d] = true
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(domains))
	for _, d := range domains {
		name, good := service.NormalizeDomain(d)
		if !good || !allowed[name] || seen[name] {
			return nil, &service.Error{Status: 422, Code: "domain_not_claimed"}
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
}

func domainAgentErr(err error) error {
	var ae *mtaagent.AgentError
	if errors.As(err, &ae) && ae.Status == 409 {
		return &service.Error{Status: 409, Code: "managed_domain_plan_changed_or_blocked"}
	}
	return mtaErr(err)
}

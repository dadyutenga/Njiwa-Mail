package api

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/njiwa-tz/mail/internal/mtaagent"
	"github.com/njiwa-tz/mail/internal/service"
)

// hostInstaller is the host agent's installer. An agent started without
// -allow-install refuses every call.
type hostInstaller interface {
	InstallPlan(ctx context.Context, in mtaagent.InstallRequest) (*mtaagent.InstallPlan, error)
	InstallApply(ctx context.Context, in mtaagent.InstallRequest) error
	InstallRollback(ctx context.Context, in mtaagent.InstallRequest) error
	InstallJob(ctx context.Context) (*mtaagent.InstallJob, error)
}

var (
	installDigestRe  = regexp.MustCompile(`^[0-9a-f]{12,64}$`)
	installReceiptRe = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z$`)
)

func installErr(err error) error {
	var ae *mtaagent.AgentError
	switch {
	case errors.Is(err, mtaagent.ErrUnreachable):
		return &service.Error{Status: 503, Code: "mta_agent_unreachable", Message: "the host agent does not answer"}
	case errors.As(err, &ae) && (ae.Code == "host_install_disabled" || ae.Status == 404):
		return &service.Error{Status: 409, Code: "host_install_disabled",
			Message: "the host agent does not allow installing; start it with -allow-install, or use the commands on this page"}
	case errors.As(err, &ae):
		msg := ae.Message
		if msg == "" {
			msg = "the host refused: " + ae.Code
		}
		return &service.Error{Status: 409, Code: ae.Code, Message: msg}
	}
	return &service.Error{Status: 503, Code: "mta_agent_unreachable"}
}

func (s *Server) hostInstallRoutes(h func(string, string, adminFn)) {
	svc := s.Svc
	agent := func() (hostInstaller, error) {
		a, ok := s.MTA.(hostInstaller)
		if s.MTA == nil || !ok {
			return nil, &service.Error{Status: 409, Code: "mta_agent_not_configured", Message: "connect the host agent first (MTA_AGENT_SOCKET)"}
		}
		return a, nil
	}
	request := func(c fiber.Ctx) (hostInstaller, mtaagent.InstallRequest, error) {
		var out mtaagent.InstallRequest
		var in struct {
			Hostname     string `json:"hostname"`
			BounceDomain string `json:"bounce_domain"`
			Selector     string `json:"selector"`
			Digest       string `json:"digest"`
		}
		if err := decode(c, &in); err != nil {
			return nil, out, err
		}
		a, err := agent()
		if err != nil {
			return nil, out, err
		}
		f, err := svc.InstallerFacts(reqCtx(c), in.Hostname, in.BounceDomain, in.Selector)
		if err != nil {
			return nil, out, err
		}
		return a, mtaagent.InstallRequest{Hostname: f.Hostname, BounceDomain: f.BounceDomain, Domains: f.Domains, Selector: f.Selector,
			SubmissionBind: f.SubmissionBind, InboundRelay: f.InboundRelay, ComposeSubnet: f.ComposeSubnet, Digest: in.Digest}, nil
	}
	h("POST", "/host/plan", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		a, req, err := request(c)
		if err != nil {
			return nil, err
		}
		req.Digest = ""
		pl, err := a.InstallPlan(reqCtx(c), req)
		if err != nil {
			return nil, installErr(err)
		}
		s.audit(c, u, "installer.plan", "installer", pl.Digest, map[string]any{"hostname": req.Hostname, "blockers": len(pl.Blockers)})
		return pl, nil
	})
	h("POST", "/host/apply", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		a, req, err := request(c)
		if err != nil {
			return nil, err
		}
		if !installDigestRe.MatchString(req.Digest) {
			return nil, &service.Error{Status: 422, Code: "digest_required", Message: "approve the digest of the plan you reviewed"}
		}
		// Audited before it starts: what was approved is on record whatever happens.
		s.audit(c, u, "installer.apply_requested", "installer", req.Digest, map[string]any{"hostname": req.Hostname, "domains": req.Domains})
		if err := a.InstallApply(reqCtx(c), req); err != nil {
			return nil, installErr(err)
		}
		return map[string]any{"started": true}, nil
	})
	h("POST", "/host/rollback", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Hostname  string `json:"hostname"`
			ReceiptID string `json:"receipt_id"`
			Confirm   string `json:"confirm"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.Confirm) != "ROLL BACK" || (in.ReceiptID != "" && !installReceiptRe.MatchString(in.ReceiptID)) {
			return nil, &service.Error{Status: 422, Code: "confirmation_required", Message: "type ROLL BACK to confirm"}
		}
		a, err := agent()
		if err != nil {
			return nil, err
		}
		f, err := svc.InstallerFacts(reqCtx(c), in.Hostname, "", "")
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "installer.rollback_requested", "installer", in.ReceiptID, nil)
		if err := a.InstallRollback(reqCtx(c), mtaagent.InstallRequest{Hostname: f.Hostname, Domains: f.Domains, Selector: f.Selector,
			SubmissionBind: f.SubmissionBind, InboundRelay: f.InboundRelay, ComposeSubnet: f.ComposeSubnet, ReceiptID: in.ReceiptID}); err != nil {
			return nil, installErr(err)
		}
		return map[string]any{"started": true}, nil
	})
	// The guided flow, derived from what is recorded.
	h("GET", "/host/flow", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		return svc.HostSetupFlow(reqCtx(c))
	})
	// Progress. A finished job is recorded here or by WatchHost, once; the
	// certificate and key never leave the server side.
	h("GET", "/host/job", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		a, err := agent()
		if err != nil {
			return nil, err
		}
		j, err := a.InstallJob(reqCtx(c))
		if err != nil {
			return nil, installErr(err)
		}
		recorded, err := s.recordHostJob(reqCtx(c), u.Actor(), u.Email, j)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"id": j.ID, "kind": j.Kind, "state": j.State, "lines": j.Lines, "error": j.Error, "recorded": recorded}
		if rc := j.Receipt; rc != nil {
			out["result"], out["receipt_id"], out["dkim_records"], out["selector"] = rc.Result, rc.ID, rc.DKIMRecords, rc.Params.Selector
		}
		return out, nil
	})
}

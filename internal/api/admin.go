package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jhillyerd/enmime/v2"
	"github.com/njiwa-tz/mail/internal/service"
	"github.com/njiwa-tz/mail/internal/tmpl"
)

func (s *Server) adminRoutes(app *fiber.App) {
	svc := s.Svc
	q := func(c *fiber.Ctx, k string) string { return c.Query(k) }
	h := func(pattern string, fn adminFn) {
		method, path, _ := strings.Cut(pattern, " ")
		path = strings.ReplaceAll(strings.ReplaceAll(path, "{", ":"), "}", "")
		app.Add(method, "/admin/v1"+path, s.adm(fn))
	}

	// ---- auth
	app.Post("/admin/v1/auth/login", s.adminLogin)
	h("POST /auth/logout", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		tok, _ := cookieVal(c, adminCookie)
		if err := svc.Logout(reqCtx(c), tok); err != nil {
			return nil, &service.Error{Status: 503, Code: "service_unavailable"}
		}
		s.audit(c, u, "auth.logout", "user", u.ID, nil)
		return rawResponse{header: map[string]string{"Content-Type": "application/json",
			"Set-Cookie": (&http.Cookie{Name: adminCookie, Path: "/admin", HttpOnly: true, Secure: !s.InsecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: -1}).String()},
			body: []byte(`{"ok":true}`)}, nil
	})
	h("GET /auth/me", func(c *fiber.Ctx, u *service.AdminUser) (any, error) { return u, nil })

	// ---- overview
	h("GET /overview", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		st, err := svc.Stats(reqCtx(c))
		if err != nil {
			return nil, err
		}
		return struct {
			*service.Stats
			SMTPConfigured bool   `json:"smtp_configured"`
			InboundDomain  string `json:"inbound_domain"`
			BounceAddress  string `json:"bounce_address"`
		}{st, s.SMTPConfigured, svc.Opts.InboundDomain, svc.Opts.BounceAddress}, nil
	})
	h("GET /alerts", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListAlerts(reqCtx(c), q(c, "open") == "true", pageOf(c))
		return list(c, items, more, err)
	})
	h("POST /alerts/{id}/ack", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		id, _ := strconv.ParseInt(c.Params("id"), 10, 64)
		if err := svc.AckAlert(reqCtx(c), id, u.Email); err != nil {
			return nil, err
		}
		s.audit(c, u, "alert.ack", "alert", c.Params("id"), nil)
		return nil, nil
	})

	// ---- messages (metadata only: never variables or body)
	h("GET /messages", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		from, err := timeParam(c, "from")
		if err != nil {
			return nil, err
		}
		to, err := timeParam(c, "to")
		if err != nil {
			return nil, err
		}
		items, more, err := svc.AdminMessages(reqCtx(c), service.MessageFilter{Project: q(c, "project"), Mode: q(c, "mode"), State: q(c, "state"),
			Template: q(c, "template"), Recipient: q(c, "recipient"), Bounce: q(c, "bounce_state"), From: from, To: to}, pageOf(c))
		return list(c, items, more, err)
	})
	h("GET /messages/{id}", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		return svc.AdminMessage(reqCtx(c), c.Params("id"))
	})
	h("POST /messages/{id}/resolve", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			As   string `json:"as"`
			Note string `json:"note"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.ResolveUnknown(reqCtx(c), c.Params("id"), in.As, in.Note); err != nil {
			return nil, service.OpErr(err)
		}
		s.audit(c, u, "message.resolve_unknown", "message", c.Params("id"), map[string]any{"as": in.As})
		return nil, nil
	})
	h("GET /bounces", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListBounces(reqCtx(c), q(c, "type"), pageOf(c))
		return list(c, items, more, err)
	})

	// ---- inbound (all admins can read bodies; HTML is still served sandboxed)
	h("GET /inbound", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListInbound(reqCtx(c), "", q(c, "project"), q(c, "mode"), q(c, "q"), pageOf(c))
		return list(c, items, more, err)
	})
	h("GET /inbound/{id}", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		return svc.GetInbound(reqCtx(c), "", c.Params("id"), true)
	})
	h("GET /inbound/{id}/html", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		m, err := svc.GetInbound(reqCtx(c), "", c.Params("id"), true)
		if err != nil {
			return nil, err
		}
		html, _ := m["html"].(string)
		// Untrusted HTML: the CSP sandbox disables scripts, forms and
		// same-origin access, and nothing remote may load. The UI additionally
		// shows this inside a sandboxed iframe.
		return rawResponse{body: []byte(html), header: map[string]string{
			"Content-Type":            "text/html; charset=utf-8",
			"Content-Security-Policy": "sandbox; default-src 'none'; style-src 'unsafe-inline'; img-src data:",
			"X-Frame-Options":         "SAMEORIGIN",
			"Referrer-Policy":         "no-referrer",
		}}, nil
	})
	h("GET /inbound/{id}/attachments/{aid}", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		f, err := svc.OpenAttachment(reqCtx(c), "", c.Params("id"), c.Params("aid"))
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "inbound.attachment_download", "inbound_attachment", c.Params("aid"), nil)
		return download(f), nil
	})
	h("GET /inbound-addresses", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListInboundAddresses(reqCtx(c), pageOf(c))
		return list(c, items, more, err)
	})
	h("POST /inbound-addresses", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Project    string  `json:"project"`
			Mode       string  `json:"mode"`
			LocalPart  string  `json:"local_part"`
			EndpointID *string `json:"webhook_endpoint_id"`
			Route      string  `json:"route"`
			ForwardTo  string  `json:"forward_to"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, err := svc.CreateInboundAddress(reqCtx(c), in.Project, in.Mode, in.LocalPart, in.EndpointID, in.Route, in.ForwardTo)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "inbound_address.create", "inbound_address", id, map[string]any{"project": in.Project, "mode": in.Mode, "local_part": in.LocalPart})
		return map[string]string{"id": id}, nil
	})
	h("PATCH /inbound-addresses/{id}", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Enabled    *bool   `json:"enabled"`
			EndpointID *string `json:"webhook_endpoint_id"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.UpdateInboundAddress(reqCtx(c), c.Params("id"), in.Enabled, in.EndpointID); err != nil {
			return nil, err
		}
		s.audit(c, u, "inbound_address.update", "inbound_address", c.Params("id"), map[string]any{"enabled": in.Enabled})
		return nil, nil
	})
	h("DELETE /inbound-addresses/{id}", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		if err := svc.DeleteInboundAddress(reqCtx(c), c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "inbound_address.delete", "inbound_address", c.Params("id"), nil)
		return nil, nil
	})

	// ---- projects and environments
	h("GET /projects", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, more, err := svc.AdminProjects(reqCtx(c), pageOf(c))
		return list(c, items, more, err)
	})
	h("POST /projects", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Name string `json:"name"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.CreateProjectWithDefaults(reqCtx(c), in.Name); err != nil {
			return nil, err
		}
		s.audit(c, u, "project.create", "project", in.Name, nil)
		return nil, nil
	})
	h("PATCH /projects/{name}", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Enabled            *bool `json:"enabled"`
			SendPerMinute      int   `json:"send_per_minute"`
			SendPerDay         int   `json:"send_per_day"`
			RecipientPerMinute int   `json:"recipient_per_minute"`
			RecipientPerHour   int   `json:"recipient_per_hour"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		name := c.Params("name")
		if in.SendPerMinute < 0 || in.SendPerDay < 0 || in.RecipientPerMinute < 0 || in.RecipientPerHour < 0 {
			return nil, &service.Error{Status: 400, Code: "invalid_request", Message: "limits must be positive"}
		}
		if err := svc.SetQuotas(reqCtx(c), name, in.SendPerMinute, in.SendPerDay, in.RecipientPerMinute, in.RecipientPerHour); err != nil {
			return nil, service.OpErr(err)
		}
		if in.Enabled != nil {
			if err := svc.SetProjectEnabled(reqCtx(c), name, *in.Enabled); err != nil {
				return nil, service.OpErr(err)
			}
		}
		s.audit(c, u, "project.update", "project", name, map[string]any{"enabled": in.Enabled, "send_per_minute": in.SendPerMinute,
			"send_per_day": in.SendPerDay, "recipient_per_minute": in.RecipientPerMinute, "recipient_per_hour": in.RecipientPerHour})
		return nil, nil
	})
	h("PATCH /projects/{name}/environments/{mode}", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Enabled            *bool     `json:"enabled"`
			ApprovedOrigins    *[]string `json:"approved_origins"`
			RestrictRecipients *bool     `json:"restrict_recipients"`
			RecipientAllowlist *[]string `json:"recipient_allowlist"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		name, mode, ctx := c.Params("name"), c.Params("mode"), reqCtx(c)
		if mode != "sandbox" && mode != "live" {
			return nil, &service.Error{Status: 404, Code: "not_found"}
		}
		if in.ApprovedOrigins != nil {
			if err := svc.SetOrigins(ctx, name, mode, *in.ApprovedOrigins); err != nil {
				return nil, service.OpErr(err)
			}
		}
		if in.RecipientAllowlist != nil {
			if err := svc.SetAllowlist(ctx, name, mode, *in.RecipientAllowlist); err != nil {
				return nil, service.OpErr(err)
			}
		}
		if in.RestrictRecipients != nil {
			if err := svc.SetRestrictRecipients(ctx, name, mode, *in.RestrictRecipients); err != nil {
				return nil, service.OpErr(err)
			}
		}
		if in.Enabled != nil {
			if err := svc.SetEnvEnabled(ctx, name, mode, *in.Enabled); err != nil {
				return nil, service.OpErr(err)
			}
		}
		s.audit(c, u, "environment.update", "environment", name+"/"+mode, map[string]any{"enabled": in.Enabled,
			"restrict_recipients": in.RestrictRecipients, "origins_changed": in.ApprovedOrigins != nil, "allowlist_changed": in.RecipientAllowlist != nil})
		return nil, nil
	})

	// ---- API keys (the secret is returned exactly once)
	h("GET /keys", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, more, err := svc.AdminKeys(reqCtx(c), q(c, "project"), pageOf(c))
		return list(c, items, more, err)
	})
	h("POST /keys", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Project  string   `json:"project"`
			Mode     string   `json:"mode"`
			Scopes   []string `json:"scopes"`
			Label    string   `json:"label"`
			TTLHours int      `json:"ttl_hours"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if in.TTLHours < 0 || len(in.Label) > 100 {
			return nil, &service.Error{Status: 400, Code: "invalid_request", Message: "invalid ttl_hours or label"}
		}
		tok, prefix, err := svc.CreateKey(reqCtx(c), in.Project, in.Mode, in.Scopes, in.Label, time.Duration(in.TTLHours)*time.Hour)
		if err != nil {
			return nil, service.OpErr(err)
		}
		s.audit(c, u, "api_key.create", "api_key", prefix, map[string]any{"project": in.Project, "mode": in.Mode, "scopes": in.Scopes})
		return map[string]string{"prefix": prefix, "key": tok}, nil
	})
	h("POST /keys/{prefix}/rotate", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			GraceHours *int `json:"grace_hours"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		grace := 24 * time.Hour
		if in.GraceHours != nil {
			if *in.GraceHours < 0 || *in.GraceHours > 720 {
				return nil, &service.Error{Status: 400, Code: "invalid_request", Message: "grace_hours must be between 0 and 720"}
			}
			grace = time.Duration(*in.GraceHours) * time.Hour
		}
		tok, prefix, err := svc.RotateKey(reqCtx(c), c.Params("prefix"), grace)
		if err != nil {
			return nil, service.OpErr(err)
		}
		s.audit(c, u, "api_key.rotate", "api_key", c.Params("prefix"), map[string]any{"new_prefix": prefix, "grace_hours": grace.Hours()})
		return map[string]string{"prefix": prefix, "key": tok}, nil
	})
	h("POST /keys/{prefix}/revoke", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		if err := svc.RevokeKey(reqCtx(c), c.Params("prefix")); err != nil {
			return nil, service.OpErr(err)
		}
		s.audit(c, u, "api_key.revoke", "api_key", c.Params("prefix"), nil)
		return nil, nil
	})

	// ---- senders and DNS
	h("GET /senders", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, more, err := svc.AdminSenders(reqCtx(c), pageOf(c))
		return list(c, items, more, err)
	})
	h("POST /senders", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Project     string `json:"project"`
			Mode        string `json:"mode"`
			Email       string `json:"email"`
			DisplayName string `json:"display_name"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.AddSender(reqCtx(c), in.Project, in.Mode, in.Email, in.DisplayName); err != nil {
			return nil, service.OpErr(err)
		}
		s.audit(c, u, "sender.upsert", "sender", strings.ToLower(in.Email), map[string]any{"project": in.Project, "mode": in.Mode})
		return nil, nil
	})
	h("DELETE /senders/{id}", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		if err := svc.DeleteSender(reqCtx(c), c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "sender.delete", "sender", c.Params("id"), nil)
		return nil, nil
	})
	h("GET /dns", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, err := svc.LatestDNSChecks(reqCtx(c))
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items}, nil
	})
	h("POST /dns/check", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		res, err := svc.RunDNSChecks(reqCtx(c), s.Resolver)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "dns.check", "dns", "", nil)
		return map[string]any{"items": res}, nil
	})

	// ---- templates
	h("GET /templates", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListTemplates(reqCtx(c), pageOf(c))
		return list(c, items, more, err)
	})
	h("POST /templates", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in service.TemplateInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, err := svc.CreateTemplate(reqCtx(c), in)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "template.create", "template", id, map[string]any{"slug": in.Slug, "project": in.Project})
		return map[string]string{"id": id}, nil
	})
	h("GET /templates/{id}", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		return svc.GetTemplate(reqCtx(c), c.Params("id"))
	})
	h("PATCH /templates/{id}", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Description       *string `json:"description"`
			Archived          *bool   `json:"archived"`
			DefaultTTLSeconds *int    `json:"default_ttl_seconds"`
			MaxTTLSeconds     *int    `json:"max_ttl_seconds"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.UpdateTemplate(reqCtx(c), c.Params("id"), in.Description, in.Archived, in.DefaultTTLSeconds, in.MaxTTLSeconds); err != nil {
			return nil, err
		}
		s.audit(c, u, "template.update", "template", c.Params("id"), map[string]any{"archived": in.Archived})
		return nil, nil
	})
	h("PUT /templates/{id}/draft", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in service.VersionInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		v, err := svc.SaveDraft(reqCtx(c), c.Params("id"), in)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "template.save_draft", "template", c.Params("id"), map[string]any{"version": v})
		return map[string]int{"version": v}, nil
	})
	ver := func(c *fiber.Ctx) (int, error) {
		v, err := strconv.Atoi(c.Params("version"))
		if err != nil || v < 1 {
			return 0, &service.Error{Status: 404, Code: "not_found"}
		}
		return v, nil
	}
	h("POST /templates/{id}/versions/{version}/publish", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		v, err := ver(c)
		if err != nil {
			return nil, err
		}
		if err := svc.PublishVersion(reqCtx(c), c.Params("id"), v); err != nil {
			return nil, err
		}
		s.audit(c, u, "template.publish", "template", c.Params("id"), map[string]any{"version": v})
		return nil, nil
	})
	// Preview renders only; it never queues or sends. POST because it carries
	// sample variables, so it needs the CSRF token. Any signed-in admin may preview.
	h("POST /templates/{id}/versions/{version}/preview", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		v, err := ver(c)
		if err != nil {
			return nil, err
		}
		var in struct {
			Locale    string         `json:"locale"`
			Variables map[string]any `json:"variables"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		return svc.PreviewVersion(reqCtx(c), c.Params("id"), v, in.Locale, in.Variables)
	})
	h("POST /templates/{id}/versions/{version}/test-send", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		v, err := ver(c)
		if err != nil {
			return nil, err
		}
		var in service.TestSend
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		acc, err := svc.TestSendVersion(reqCtx(c), c.Params("id"), v, in)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "template.test_send", "message", acc.ID, map[string]any{"template_id": c.Params("id"), "version": v, "project": in.Project, "mode": in.Mode})
		return acc, nil
	})
	h("GET /template-meta", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		return map[string]any{"locales": tmpl.Locales, "variable_types": []string{tmpl.TypeString, tmpl.TypeInt, tmpl.TypeURL},
			"webhook_events": service.WebhookEvents}, nil
	})

	// ---- webhooks
	h("GET /webhooks", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListWebhooks(reqCtx(c), pageOf(c))
		return list(c, items, more, err)
	})
	h("POST /webhooks", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Project string   `json:"project"`
			Mode    string   `json:"mode"`
			URL     string   `json:"url"`
			Events  []string `json:"events"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, secret, err := svc.CreateWebhook(reqCtx(c), in.Project, in.Mode, in.URL, in.Events)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "webhook.create", "webhook_endpoint", id, map[string]any{"project": in.Project, "mode": in.Mode, "events": in.Events})
		return map[string]string{"id": id, "signing_secret": secret}, nil
	})
	h("PATCH /webhooks/{id}", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			URL     *string  `json:"url"`
			Events  []string `json:"events"`
			Enabled *bool    `json:"enabled"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.UpdateWebhook(reqCtx(c), c.Params("id"), in.URL, in.Events, in.Enabled); err != nil {
			return nil, err
		}
		s.audit(c, u, "webhook.update", "webhook_endpoint", c.Params("id"), map[string]any{"enabled": in.Enabled, "events": in.Events, "url_changed": in.URL != nil})
		return nil, nil
	})
	h("DELETE /webhooks/{id}", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		if err := svc.DeleteWebhook(reqCtx(c), c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "webhook.delete", "webhook_endpoint", c.Params("id"), nil)
		return nil, nil
	})
	h("POST /webhooks/{id}/rotate-secret", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		secret, err := svc.RotateWebhookSecret(reqCtx(c), c.Params("id"))
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "webhook.rotate_secret", "webhook_endpoint", c.Params("id"), nil)
		return map[string]string{"signing_secret": secret}, nil
	})
	h("GET /webhook-deliveries", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListDeliveries(reqCtx(c), q(c, "endpoint_id"), q(c, "state"), pageOf(c))
		return list(c, items, more, err)
	})
	h("POST /webhook-deliveries/{id}/replay", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		if err := svc.ReplayDelivery(reqCtx(c), c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "webhook.replay", "webhook_delivery", c.Params("id"), nil)
		return nil, nil
	})

	// ---- suppressions
	h("GET /suppressions", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, more, err := svc.AdminSuppressions(reqCtx(c), q(c, "project"), q(c, "mode"), q(c, "source"), pageOf(c))
		return list(c, items, more, err)
	})
	h("POST /suppressions", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Project string `json:"project"`
			Mode    string `json:"mode"`
			Email   string `json:"email"`
			Reason  string `json:"reason"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.Suppress(reqCtx(c), in.Project, in.Mode, in.Email, in.Reason); err != nil {
			return nil, service.OpErr(err)
		}
		s.audit(c, u, "suppression.add", "suppression", "", map[string]any{"project": in.Project, "mode": in.Mode, "reason": in.Reason})
		return nil, nil
	})
	h("DELETE /suppressions/{id}", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Reason string `json:"reason"`
		}
		if err := decode(c, &in); err != nil || strings.TrimSpace(in.Reason) == "" {
			return nil, &service.Error{Status: 400, Code: "invalid_request", Message: "a reason is required to remove a suppression"}
		}
		if err := svc.DeleteSuppression(reqCtx(c), "", c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "suppression.remove", "suppression", c.Params("id"), map[string]any{"reason": in.Reason})
		return nil, nil
	})

	// ---- sandbox captures: content, so authenticated admins only
	h("GET /sandbox", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, more, err := svc.AdminCaptures(reqCtx(c), pageOf(c))
		return list(c, items, more, err)
	})
	h("GET /sandbox/{id}", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		raw, err := svc.AdminCapture(reqCtx(c), c.Params("id"))
		if err != nil {
			return nil, err
		}
		out := map[string]any{"id": c.Params("id"), "raw": string(raw)}
		if env, err := enmime.ReadEnvelope(bytes.NewReader(raw)); err == nil {
			out["subject"], out["from"], out["to"] = env.GetHeader("Subject"), env.GetHeader("From"), env.GetHeader("To")
			out["text"], out["html"] = env.Text, env.HTML
		}
		s.audit(c, u, "sandbox.view_capture", "message", c.Params("id"), nil)
		return out, nil
	})

	// ---- users and audit log
	h("GET /users", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListUsers(reqCtx(c), pageOf(c))
		return list(c, items, more, err)
	})
	h("POST /users", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, err := svc.CreateUser(reqCtx(c), in.Email, in.Password, in.Role)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "user.create", "user", id, map[string]any{"email": strings.ToLower(in.Email), "role": service.RoleAdmin})
		return map[string]string{"id": id}, nil
	})
	h("PATCH /users/{id}", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Role     *string `json:"role"`
			Disabled *bool   `json:"disabled"`
			Password *string `json:"password"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.UpdateUser(reqCtx(c), c.Params("id"), in.Role, in.Disabled, in.Password); err != nil {
			return nil, err
		}
		if in.Role != nil {
			role := service.RoleAdmin
			in.Role = &role
		}
		s.audit(c, u, "user.update", "user", c.Params("id"), map[string]any{"role": in.Role, "disabled": in.Disabled, "password_changed": in.Password != nil})
		return nil, nil
	})
	h("GET /audit", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListAudit(reqCtx(c), q(c, "actor"), q(c, "action"), pageOf(c))
		return list(c, items, more, err)
	})

	// ---- settings
	h("GET /settings", func(c *fiber.Ctx, u *service.AdminUser) (any, error) { return svc.Settings(reqCtx(c)) })
	h("PUT /settings", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
		in := map[string]int{}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.UpdateSettings(reqCtx(c), in); err != nil {
			return nil, err
		}
		details := map[string]any{}
		for k, v := range in {
			details[k] = v
		}
		s.audit(c, u, "settings.update", "settings", "", details)
		return svc.Settings(reqCtx(c))
	})
}

// adminLogin: generic error for every failure; the cookie is HttpOnly, Secure
// and SameSite=Lax, and scoped to /admin.
func (s *Server) adminLogin(c *fiber.Ctx) error {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if len(c.Body()) > 4096 || json.Unmarshal(c.Body(), &in) != nil || in.Email == "" || in.Password == "" {
		return writeErr(c, &service.Error{Status: 401, Code: "invalid_credentials"})
	}
	tok, u, err := s.Svc.Login(reqCtx(c), in.Email, in.Password, s.clientIP(c), c.Get("User-Agent"))
	if err != nil {
		return writeErr(c, err)
	}
	setCookie(c, &http.Cookie{Name: adminCookie, Value: tok, Path: "/admin", HttpOnly: true, Secure: !s.InsecureCookies,
		SameSite: http.SameSiteLaxMode, Expires: u.ExpiresAt})
	return writeJSON(c, 200, u)
}

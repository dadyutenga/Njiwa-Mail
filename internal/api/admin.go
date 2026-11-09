package api

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ditronics-tz/mail/internal/service"
	"github.com/ditronics-tz/mail/internal/tmpl"
	"github.com/jhillyerd/enmime/v2"
)

// Admin API (/admin/v1): session cookie + CSRF token, role-checked on the
// server for every request. The UI may hide actions, but nothing here relies
// on that.

const (
	adminCookie  = "dm_admin"
	csrfHeader   = "X-CSRF-Token"
	adminMaxBody = 2 << 20
)

type adminFn func(r *http.Request, u *service.AdminUser) (any, error)

func (s *Server) clientIP(r *http.Request) string {
	if s.TrustProxy {
		if xf := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); net.ParseIP(xf) != nil {
			return xf
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func forbidden() error { return &service.Error{Status: 403, Code: "forbidden"} }

// adm wraps an admin handler with session, CSRF and role enforcement.
func (s *Server) adm(minRole string, h adminFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(adminCookie)
		if err != nil {
			writeErr(w, &service.Error{Status: 401, Code: "unauthenticated"})
			return
		}
		u, err := s.Svc.Session(r.Context(), c.Value)
		if err != nil {
			writeErr(w, err)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get(csrfHeader)), []byte(u.CSRF)) != 1 {
				writeErr(w, &service.Error{Status: 403, Code: "csrf_token_invalid"})
				return
			}
		}
		if !u.Can(minRole) {
			writeErr(w, forbidden())
			return
		}
		if s.Svc.Opts.RequireTOTP && (u.Role == service.RoleOwner || u.Role == service.RoleAdmin) && !u.TOTPEnabled &&
			r.Method != http.MethodGet && r.Method != http.MethodHead && !strings.Contains(r.URL.Path, "/auth/totp") {
			writeErr(w, &service.Error{Status: 403, Code: "totp_required"})
			return
		}
		out, err := h(r, u)
		if err != nil {
			writeErr(w, err)
			return
		}
		switch v := out.(type) {
		case nil:
			writeJSON(w, 200, map[string]bool{"ok": true})
		case rawResponse:
			v.write(w)
		default:
			writeJSON(w, 200, out)
		}
	}
}

// rawResponse is a non-JSON admin response (attachment download, HTML view).
type rawResponse struct {
	header map[string]string
	body   []byte
	file   string
}

func (rr rawResponse) write(w http.ResponseWriter) {
	for k, v := range rr.header {
		w.Header().Set(k, v)
	}
	if rr.file != "" {
		f, err := os.Open(rr.file)
		if err != nil {
			writeErr(w, &service.Error{Status: 404, Code: "not_found"})
			return
		}
		defer f.Close()
		w.WriteHeader(200)
		io.Copy(w, f)
		return
	}
	w.WriteHeader(200)
	w.Write(rr.body)
}

func download(f *service.AttachmentFile) rawResponse {
	// Always served as an opaque download: the sender's content type and
	// filename are data, never instructions to the browser.
	return rawResponse{file: f.Path, header: map[string]string{
		"Content-Type":            "application/octet-stream",
		"Content-Disposition":     mime.FormatMediaType("attachment", map[string]string{"filename": safeFilename(f.Filename)}),
		"Content-Length":          strconv.Itoa(f.Size),
		"Content-Security-Policy": "sandbox; default-src 'none'",
	}}
}

func safeFilename(n string) string {
	n = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, n)
	if n = strings.Trim(n, ". "); n == "" {
		n = "attachment"
	}
	return n
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, adminMaxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &service.Error{Status: 400, Code: "invalid_request", Message: "request body is not valid JSON for this endpoint"}
	}
	return nil
}

func pageOf(r *http.Request) service.Page {
	l, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	o, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return service.Page{Limit: l, Offset: o}
}

func list(r *http.Request, items []map[string]any, more bool, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	pg := pageOf(r)
	if pg.Limit <= 0 || pg.Limit > 100 {
		pg.Limit = 50
		if l, _ := strconv.Atoi(r.URL.Query().Get("limit")); l > 100 {
			pg.Limit = 100
		}
	}
	if pg.Offset < 0 {
		pg.Offset = 0
	}
	return map[string]any{"items": items, "has_more": more, "limit": pg.Limit, "offset": pg.Offset}, nil
}

func timeParam(r *http.Request, name string) (*time.Time, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, &service.Error{Status: 400, Code: "invalid_request", Message: name + " must be an RFC 3339 timestamp"}
	}
	return &t, nil
}

// audit records a completed admin write. A failure to record is logged loudly
// but does not turn an already-committed change into an error response.
func (s *Server) audit(r *http.Request, u *service.AdminUser, action, targetType, targetID string, details map[string]any) {
	if err := s.Svc.Audit(r.Context(), u.Actor(), action, targetType, targetID, details); err != nil {
		slog.Error("AUDIT WRITE FAILED", "action", action, "target_type", targetType, "target_id", targetID, "actor", u.Email)
	}
}

func (s *Server) adminRoutes(mux *http.ServeMux) {
	const V, A, O = service.RoleViewer, service.RoleAdmin, service.RoleOwner
	svc := s.Svc
	q := func(r *http.Request, k string) string { return r.URL.Query().Get(k) }
	h := func(pattern, role string, fn adminFn) {
		mux.HandleFunc(strings.Replace(pattern, " /", " /admin/v1/", 1), s.adm(role, fn))
	}

	// ---- auth
	mux.HandleFunc("POST /admin/v1/auth/login", s.adminLogin)
	h("POST /auth/logout", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		c, _ := r.Cookie(adminCookie)
		if err := svc.Logout(r.Context(), c.Value); err != nil {
			return nil, &service.Error{Status: 503, Code: "service_unavailable"}
		}
		s.audit(r, u, "auth.logout", "user", u.ID, nil)
		return rawResponse{header: map[string]string{"Content-Type": "application/json",
			"Set-Cookie": (&http.Cookie{Name: adminCookie, Path: "/admin", HttpOnly: true, Secure: !s.InsecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: -1}).String()},
			body: []byte(`{"ok":true}`)}, nil
	})
	h("GET /auth/me", V, func(r *http.Request, u *service.AdminUser) (any, error) { return u, nil })

	// ---- overview
	h("GET /overview", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		st, err := svc.Stats(r.Context())
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
	h("GET /alerts", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListAlerts(r.Context(), q(r, "open") == "true", pageOf(r))
		return list(r, items, more, err)
	})
	h("POST /alerts/{id}/ack", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := svc.AckAlert(r.Context(), id, u.Email); err != nil {
			return nil, err
		}
		s.audit(r, u, "alert.ack", "alert", r.PathValue("id"), nil)
		return nil, nil
	})

	// ---- messages (metadata only: never variables or body)
	h("GET /messages", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		from, err := timeParam(r, "from")
		if err != nil {
			return nil, err
		}
		to, err := timeParam(r, "to")
		if err != nil {
			return nil, err
		}
		items, more, err := svc.AdminMessages(r.Context(), service.MessageFilter{Project: q(r, "project"), Mode: q(r, "mode"), State: q(r, "state"),
			Template: q(r, "template"), Recipient: q(r, "recipient"), Bounce: q(r, "bounce_state"), From: from, To: to}, pageOf(r))
		return list(r, items, more, err)
	})
	h("GET /messages/{id}", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		return svc.AdminMessage(r.Context(), r.PathValue("id"))
	})
	h("POST /messages/{id}/resolve", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct {
			As   string `json:"as"`
			Note string `json:"note"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		if err := svc.ResolveUnknown(r.Context(), r.PathValue("id"), in.As, in.Note); err != nil {
			return nil, service.OpErr(err)
		}
		s.audit(r, u, "message.resolve_unknown", "message", r.PathValue("id"), map[string]any{"as": in.As})
		return nil, nil
	})
	h("GET /bounces", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListBounces(r.Context(), q(r, "type"), pageOf(r))
		return list(r, items, more, err)
	})

	// ---- inbound (viewers see metadata only, never bodies or attachments)
	h("GET /inbound", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListInbound(r.Context(), "", q(r, "project"), q(r, "mode"), q(r, "q"), pageOf(r))
		return list(r, items, more, err)
	})
	h("GET /inbound/{id}", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		return svc.GetInbound(r.Context(), "", r.PathValue("id"), u.Can(A))
	})
	h("GET /inbound/{id}/html", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		m, err := svc.GetInbound(r.Context(), "", r.PathValue("id"), true)
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
	h("GET /inbound/{id}/attachments/{aid}", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		f, err := svc.OpenAttachment(r.Context(), "", r.PathValue("id"), r.PathValue("aid"))
		if err != nil {
			return nil, err
		}
		s.audit(r, u, "inbound.attachment_download", "inbound_attachment", r.PathValue("aid"), nil)
		return download(f), nil
	})
	h("GET /inbound-addresses", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListInboundAddresses(r.Context(), pageOf(r))
		return list(r, items, more, err)
	})
	h("POST /inbound-addresses", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct {
			Project    string  `json:"project"`
			Mode       string  `json:"mode"`
			LocalPart  string  `json:"local_part"`
			EndpointID *string `json:"webhook_endpoint_id"`
			Route      string  `json:"route"`
			ForwardTo  string  `json:"forward_to"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		id, err := svc.CreateInboundAddress(r.Context(), in.Project, in.Mode, in.LocalPart, in.EndpointID, in.Route, in.ForwardTo)
		if err != nil {
			return nil, err
		}
		s.audit(r, u, "inbound_address.create", "inbound_address", id, map[string]any{"project": in.Project, "mode": in.Mode, "local_part": in.LocalPart})
		return map[string]string{"id": id}, nil
	})
	h("PATCH /inbound-addresses/{id}", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct {
			Enabled    *bool   `json:"enabled"`
			EndpointID *string `json:"webhook_endpoint_id"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		if err := svc.UpdateInboundAddress(r.Context(), r.PathValue("id"), in.Enabled, in.EndpointID); err != nil {
			return nil, err
		}
		s.audit(r, u, "inbound_address.update", "inbound_address", r.PathValue("id"), map[string]any{"enabled": in.Enabled})
		return nil, nil
	})
	h("DELETE /inbound-addresses/{id}", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		if err := svc.DeleteInboundAddress(r.Context(), r.PathValue("id")); err != nil {
			return nil, err
		}
		s.audit(r, u, "inbound_address.delete", "inbound_address", r.PathValue("id"), nil)
		return nil, nil
	})

	// ---- projects and environments
	h("GET /projects", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		items, more, err := svc.AdminProjects(r.Context(), pageOf(r))
		return list(r, items, more, err)
	})
	h("POST /projects", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct {
			Name string `json:"name"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		if err := svc.CreateProjectWithDefaults(r.Context(), in.Name); err != nil {
			return nil, err
		}
		s.audit(r, u, "project.create", "project", in.Name, nil)
		return nil, nil
	})
	h("PATCH /projects/{name}", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct {
			Enabled            *bool `json:"enabled"`
			SendPerMinute      int   `json:"send_per_minute"`
			SendPerDay         int   `json:"send_per_day"`
			RecipientPerMinute int   `json:"recipient_per_minute"`
			RecipientPerHour   int   `json:"recipient_per_hour"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		name := r.PathValue("name")
		if in.SendPerMinute < 0 || in.SendPerDay < 0 || in.RecipientPerMinute < 0 || in.RecipientPerHour < 0 {
			return nil, &service.Error{Status: 400, Code: "invalid_request", Message: "limits must be positive"}
		}
		if err := svc.SetQuotas(r.Context(), name, in.SendPerMinute, in.SendPerDay, in.RecipientPerMinute, in.RecipientPerHour); err != nil {
			return nil, service.OpErr(err)
		}
		if in.Enabled != nil {
			if err := svc.SetProjectEnabled(r.Context(), name, *in.Enabled); err != nil {
				return nil, service.OpErr(err)
			}
		}
		s.audit(r, u, "project.update", "project", name, map[string]any{"enabled": in.Enabled, "send_per_minute": in.SendPerMinute,
			"send_per_day": in.SendPerDay, "recipient_per_minute": in.RecipientPerMinute, "recipient_per_hour": in.RecipientPerHour})
		return nil, nil
	})
	h("PATCH /projects/{name}/environments/{mode}", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct {
			Enabled            *bool     `json:"enabled"`
			ApprovedOrigins    *[]string `json:"approved_origins"`
			RestrictRecipients *bool     `json:"restrict_recipients"`
			RecipientAllowlist *[]string `json:"recipient_allowlist"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		name, mode, ctx := r.PathValue("name"), r.PathValue("mode"), r.Context()
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
		s.audit(r, u, "environment.update", "environment", name+"/"+mode, map[string]any{"enabled": in.Enabled,
			"restrict_recipients": in.RestrictRecipients, "origins_changed": in.ApprovedOrigins != nil, "allowlist_changed": in.RecipientAllowlist != nil})
		return nil, nil
	})

	// ---- API keys (the secret is returned exactly once)
	h("GET /keys", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		items, more, err := svc.AdminKeys(r.Context(), q(r, "project"), pageOf(r))
		return list(r, items, more, err)
	})
	h("POST /keys", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct {
			Project  string   `json:"project"`
			Mode     string   `json:"mode"`
			Scopes   []string `json:"scopes"`
			Label    string   `json:"label"`
			TTLHours int      `json:"ttl_hours"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		if in.TTLHours < 0 || len(in.Label) > 100 {
			return nil, &service.Error{Status: 400, Code: "invalid_request", Message: "invalid ttl_hours or label"}
		}
		tok, prefix, err := svc.CreateKey(r.Context(), in.Project, in.Mode, in.Scopes, in.Label, time.Duration(in.TTLHours)*time.Hour)
		if err != nil {
			return nil, service.OpErr(err)
		}
		s.audit(r, u, "api_key.create", "api_key", prefix, map[string]any{"project": in.Project, "mode": in.Mode, "scopes": in.Scopes})
		return map[string]string{"prefix": prefix, "key": tok}, nil
	})
	h("POST /keys/{prefix}/rotate", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct {
			GraceHours *int `json:"grace_hours"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		grace := 24 * time.Hour
		if in.GraceHours != nil {
			if *in.GraceHours < 0 || *in.GraceHours > 720 {
				return nil, &service.Error{Status: 400, Code: "invalid_request", Message: "grace_hours must be between 0 and 720"}
			}
			grace = time.Duration(*in.GraceHours) * time.Hour
		}
		tok, prefix, err := svc.RotateKey(r.Context(), r.PathValue("prefix"), grace)
		if err != nil {
			return nil, service.OpErr(err)
		}
		s.audit(r, u, "api_key.rotate", "api_key", r.PathValue("prefix"), map[string]any{"new_prefix": prefix, "grace_hours": grace.Hours()})
		return map[string]string{"prefix": prefix, "key": tok}, nil
	})
	h("POST /keys/{prefix}/revoke", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		if err := svc.RevokeKey(r.Context(), r.PathValue("prefix")); err != nil {
			return nil, service.OpErr(err)
		}
		s.audit(r, u, "api_key.revoke", "api_key", r.PathValue("prefix"), nil)
		return nil, nil
	})

	// ---- senders and DNS
	h("GET /senders", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		items, more, err := svc.AdminSenders(r.Context(), pageOf(r))
		return list(r, items, more, err)
	})
	h("POST /senders", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct {
			Project     string `json:"project"`
			Mode        string `json:"mode"`
			Email       string `json:"email"`
			DisplayName string `json:"display_name"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		if err := svc.AddSender(r.Context(), in.Project, in.Mode, in.Email, in.DisplayName); err != nil {
			return nil, service.OpErr(err)
		}
		s.audit(r, u, "sender.upsert", "sender", strings.ToLower(in.Email), map[string]any{"project": in.Project, "mode": in.Mode})
		return nil, nil
	})
	h("DELETE /senders/{id}", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		if err := svc.DeleteSender(r.Context(), r.PathValue("id")); err != nil {
			return nil, err
		}
		s.audit(r, u, "sender.delete", "sender", r.PathValue("id"), nil)
		return nil, nil
	})
	h("GET /dns", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		items, err := svc.LatestDNSChecks(r.Context())
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items}, nil
	})
	h("POST /dns/check", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		res, err := svc.RunDNSChecks(r.Context(), s.Resolver)
		if err != nil {
			return nil, err
		}
		s.audit(r, u, "dns.check", "dns", "", nil)
		return map[string]any{"items": res}, nil
	})

	// ---- templates
	h("GET /templates", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListTemplates(r.Context(), pageOf(r))
		return list(r, items, more, err)
	})
	h("POST /templates", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in service.TemplateInput
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		id, err := svc.CreateTemplate(r.Context(), in)
		if err != nil {
			return nil, err
		}
		s.audit(r, u, "template.create", "template", id, map[string]any{"slug": in.Slug, "project": in.Project})
		return map[string]string{"id": id}, nil
	})
	h("GET /templates/{id}", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		return svc.GetTemplate(r.Context(), r.PathValue("id"))
	})
	h("PATCH /templates/{id}", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct {
			Description       *string `json:"description"`
			Archived          *bool   `json:"archived"`
			DefaultTTLSeconds *int    `json:"default_ttl_seconds"`
			MaxTTLSeconds     *int    `json:"max_ttl_seconds"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		if err := svc.UpdateTemplate(r.Context(), r.PathValue("id"), in.Description, in.Archived, in.DefaultTTLSeconds, in.MaxTTLSeconds); err != nil {
			return nil, err
		}
		s.audit(r, u, "template.update", "template", r.PathValue("id"), map[string]any{"archived": in.Archived})
		return nil, nil
	})
	h("PUT /templates/{id}/draft", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in service.VersionInput
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		v, err := svc.SaveDraft(r.Context(), r.PathValue("id"), in)
		if err != nil {
			return nil, err
		}
		s.audit(r, u, "template.save_draft", "template", r.PathValue("id"), map[string]any{"version": v})
		return map[string]int{"version": v}, nil
	})
	ver := func(r *http.Request) (int, error) {
		v, err := strconv.Atoi(r.PathValue("version"))
		if err != nil || v < 1 {
			return 0, &service.Error{Status: 404, Code: "not_found"}
		}
		return v, nil
	}
	h("POST /templates/{id}/versions/{version}/publish", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		v, err := ver(r)
		if err != nil {
			return nil, err
		}
		if err := svc.PublishVersion(r.Context(), r.PathValue("id"), v); err != nil {
			return nil, err
		}
		s.audit(r, u, "template.publish", "template", r.PathValue("id"), map[string]any{"version": v})
		return nil, nil
	})
	// Preview renders only; it never queues or sends. POST because it carries
	// sample variables, so it needs the CSRF token but only the viewer role.
	h("POST /templates/{id}/versions/{version}/preview", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		v, err := ver(r)
		if err != nil {
			return nil, err
		}
		var in struct {
			Locale    string         `json:"locale"`
			Variables map[string]any `json:"variables"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return svc.PreviewVersion(r.Context(), r.PathValue("id"), v, in.Locale, in.Variables)
	})
	h("POST /templates/{id}/versions/{version}/test-send", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		v, err := ver(r)
		if err != nil {
			return nil, err
		}
		var in service.TestSend
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		acc, err := svc.TestSendVersion(r.Context(), r.PathValue("id"), v, in)
		if err != nil {
			return nil, err
		}
		s.audit(r, u, "template.test_send", "message", acc.ID, map[string]any{"template_id": r.PathValue("id"), "version": v, "project": in.Project, "mode": in.Mode})
		return acc, nil
	})
	h("GET /template-meta", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		return map[string]any{"locales": tmpl.Locales, "variable_types": []string{tmpl.TypeString, tmpl.TypeInt, tmpl.TypeURL},
			"webhook_events": service.WebhookEvents}, nil
	})

	// ---- webhooks
	h("GET /webhooks", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListWebhooks(r.Context(), pageOf(r))
		return list(r, items, more, err)
	})
	h("POST /webhooks", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct {
			Project string   `json:"project"`
			Mode    string   `json:"mode"`
			URL     string   `json:"url"`
			Events  []string `json:"events"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		id, secret, err := svc.CreateWebhook(r.Context(), in.Project, in.Mode, in.URL, in.Events)
		if err != nil {
			return nil, err
		}
		s.audit(r, u, "webhook.create", "webhook_endpoint", id, map[string]any{"project": in.Project, "mode": in.Mode, "events": in.Events})
		return map[string]string{"id": id, "signing_secret": secret}, nil
	})
	h("PATCH /webhooks/{id}", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct {
			URL     *string  `json:"url"`
			Events  []string `json:"events"`
			Enabled *bool    `json:"enabled"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		if err := svc.UpdateWebhook(r.Context(), r.PathValue("id"), in.URL, in.Events, in.Enabled); err != nil {
			return nil, err
		}
		s.audit(r, u, "webhook.update", "webhook_endpoint", r.PathValue("id"), map[string]any{"enabled": in.Enabled, "events": in.Events, "url_changed": in.URL != nil})
		return nil, nil
	})
	h("DELETE /webhooks/{id}", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		if err := svc.DeleteWebhook(r.Context(), r.PathValue("id")); err != nil {
			return nil, err
		}
		s.audit(r, u, "webhook.delete", "webhook_endpoint", r.PathValue("id"), nil)
		return nil, nil
	})
	h("POST /webhooks/{id}/rotate-secret", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		secret, err := svc.RotateWebhookSecret(r.Context(), r.PathValue("id"))
		if err != nil {
			return nil, err
		}
		s.audit(r, u, "webhook.rotate_secret", "webhook_endpoint", r.PathValue("id"), nil)
		return map[string]string{"signing_secret": secret}, nil
	})
	h("GET /webhook-deliveries", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListDeliveries(r.Context(), q(r, "endpoint_id"), q(r, "state"), pageOf(r))
		return list(r, items, more, err)
	})
	h("POST /webhook-deliveries/{id}/replay", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		if err := svc.ReplayDelivery(r.Context(), r.PathValue("id")); err != nil {
			return nil, err
		}
		s.audit(r, u, "webhook.replay", "webhook_delivery", r.PathValue("id"), nil)
		return nil, nil
	})

	// ---- suppressions
	h("GET /suppressions", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		items, more, err := svc.AdminSuppressions(r.Context(), q(r, "project"), q(r, "mode"), q(r, "source"), pageOf(r))
		return list(r, items, more, err)
	})
	h("POST /suppressions", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct {
			Project string `json:"project"`
			Mode    string `json:"mode"`
			Email   string `json:"email"`
			Reason  string `json:"reason"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		if err := svc.Suppress(r.Context(), in.Project, in.Mode, in.Email, in.Reason); err != nil {
			return nil, service.OpErr(err)
		}
		s.audit(r, u, "suppression.add", "suppression", "", map[string]any{"project": in.Project, "mode": in.Mode, "reason": in.Reason})
		return nil, nil
	})
	h("DELETE /suppressions/{id}", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct {
			Reason string `json:"reason"`
		}
		if err := decode(r, &in); err != nil || strings.TrimSpace(in.Reason) == "" {
			return nil, &service.Error{Status: 400, Code: "invalid_request", Message: "a reason is required to remove a suppression"}
		}
		if err := svc.DeleteSuppression(r.Context(), "", r.PathValue("id")); err != nil {
			return nil, err
		}
		s.audit(r, u, "suppression.remove", "suppression", r.PathValue("id"), map[string]any{"reason": in.Reason})
		return nil, nil
	})

	// ---- sandbox captures: content, so admin and owner only
	h("GET /sandbox", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		items, more, err := svc.AdminCaptures(r.Context(), pageOf(r))
		return list(r, items, more, err)
	})
	h("GET /sandbox/{id}", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		raw, err := svc.AdminCapture(r.Context(), r.PathValue("id"))
		if err != nil {
			return nil, err
		}
		out := map[string]any{"id": r.PathValue("id"), "raw": string(raw)}
		if env, err := enmime.ReadEnvelope(bytes.NewReader(raw)); err == nil {
			out["subject"], out["from"], out["to"] = env.GetHeader("Subject"), env.GetHeader("From"), env.GetHeader("To")
			out["text"], out["html"] = env.Text, env.HTML
		}
		s.audit(r, u, "sandbox.view_capture", "message", r.PathValue("id"), nil)
		return out, nil
	})

	// ---- users (owner only) and audit log
	h("GET /users", O, func(r *http.Request, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListUsers(r.Context(), pageOf(r))
		return list(r, items, more, err)
	})
	h("POST /users", O, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		id, err := svc.CreateUser(r.Context(), in.Email, in.Password, in.Role)
		if err != nil {
			return nil, err
		}
		s.audit(r, u, "user.create", "user", id, map[string]any{"email": strings.ToLower(in.Email), "role": in.Role})
		return map[string]string{"id": id}, nil
	})
	h("PATCH /users/{id}", O, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct {
			Role     *string `json:"role"`
			Disabled *bool   `json:"disabled"`
			Password *string `json:"password"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		if err := svc.UpdateUser(r.Context(), r.PathValue("id"), in.Role, in.Disabled, in.Password); err != nil {
			return nil, err
		}
		s.audit(r, u, "user.update", "user", r.PathValue("id"), map[string]any{"role": in.Role, "disabled": in.Disabled, "password_changed": in.Password != nil})
		return nil, nil
	})
	h("GET /audit", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListAudit(r.Context(), q(r, "actor"), q(r, "action"), pageOf(r))
		return list(r, items, more, err)
	})

	// ---- settings
	h("GET /settings", V, func(r *http.Request, u *service.AdminUser) (any, error) { return svc.Settings(r.Context()) })
	h("PUT /settings", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		in := map[string]int{}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		if err := svc.UpdateSettings(r.Context(), in); err != nil {
			return nil, err
		}
		details := map[string]any{}
		for k, v := range in {
			details[k] = v
		}
		s.audit(r, u, "settings.update", "settings", "", details)
		return svc.Settings(r.Context())
	})
}

// adminLogin: generic error for every failure; the cookie is HttpOnly, Secure
// and SameSite=Lax, and scoped to /admin.
func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		TOTP     string `json:"totp"`
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil || json.Unmarshal(body, &in) != nil || in.Email == "" || in.Password == "" {
		writeErr(w, &service.Error{Status: 401, Code: "invalid_credentials"})
		return
	}
	tok, u, err := s.Svc.Login(r.Context(), in.Email, in.Password, s.clientIP(r), r.UserAgent(), in.TOTP)
	if err != nil {
		writeErr(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: adminCookie, Value: tok, Path: "/admin", HttpOnly: true, Secure: !s.InsecureCookies,
		SameSite: http.SameSiteLaxMode, Expires: u.ExpiresAt})
	writeJSON(w, 200, u)
}

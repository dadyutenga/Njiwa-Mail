package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/ditronics-tz/mail/internal/service"
)

func (s *Server) productRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/emails/batch", s.postBatch)
	mux.HandleFunc("POST /v1/emails/{id}/cancel", s.cancelEmail)
	svc := s.Svc
	h := func(pattern, role string, fn adminFn) {
		mux.HandleFunc(pattern, s.adm(role, fn))
	}
	const V, A = service.RoleViewer, service.RoleAdmin
	h("GET /admin/v1/analytics", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		q := r.URL.Query()
		items, err := svc.Analytics(r.Context(), q.Get("project"), q.Get("mode"), q.Get("template"), q.Get("domain"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items}, nil
	})
	h("GET /admin/v1/domains", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		items, more, err := svc.ListDomains(r.Context(), pageOf(r))
		return list(r, items, more, err)
	})
	h("POST /admin/v1/domains", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct {
			Project, Name, Purpose string
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		id, token, err := svc.CreateDomain(r.Context(), in.Project, in.Name, in.Purpose)
		if err != nil {
			return nil, err
		}
		s.audit(r, u, "domain.create", "domain", id, map[string]any{"name": in.Name, "purpose": in.Purpose})
		return map[string]string{"id": id, "txt": "ditronics-mail-verify=" + token}, nil
	})
	h("POST /admin/v1/domains/{id}/verify", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		if err := svc.VerifyDomain(r.Context(), r.PathValue("id"), s.Resolver); err != nil {
			return nil, err
		}
		s.audit(r, u, "domain.verify", "domain", r.PathValue("id"), nil)
		return nil, nil
	})
	h("POST /admin/v1/auth/totp", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		url, err := svc.BeginTOTP(r.Context(), u.ID, u.Email)
		if err != nil {
			return nil, err
		}
		return map[string]string{"otpauth_url": url}, nil
	})
	h("POST /admin/v1/auth/totp/confirm", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct {
			Code string `json:"code"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		codes, err := svc.ConfirmTOTP(r.Context(), u.ID, in.Code)
		if err != nil {
			return nil, err
		}
		s.audit(r, u, "auth.totp_enable", "user", u.ID, nil)
		return map[string]any{"recovery_codes": codes}, nil
	})
	h("GET /admin/v1/auth/sessions", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		items, err := svc.ListSessions(r.Context(), u.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items}, nil
	})
	h("DELETE /admin/v1/auth/sessions/{id}", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		if err := svc.RevokeSession(r.Context(), u.ID, r.PathValue("id")); err != nil {
			return nil, err
		}
		s.audit(r, u, "auth.session_revoke", "session", r.PathValue("id"), nil)
		return nil, nil
	})
	mux.HandleFunc("POST /admin/v1/auth/forgot", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Email string `json:"email"`
		}
		if err := decode(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		_ = svc.RequestPasswordReset(r.Context(), in.Email)
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /admin/v1/auth/reset", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Token, Password string
		}
		if err := decode(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if err := svc.ResetPassword(r.Context(), in.Token, in.Password); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	h("GET /admin/v1/alert-channels", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		items, err := svc.ListAlertChannels(r.Context())
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items}, nil
	})
	h("POST /admin/v1/alert-channels", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct{ Kind, Target string }
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		if err := svc.SaveAlertChannel(r.Context(), in.Kind, in.Target); err != nil {
			return nil, err
		}
		s.audit(r, u, "alert_channel.create", "alert_channel", in.Kind, nil)
		return nil, nil
	})
	h("POST /admin/v1/privacy/export", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct{ Project, Mode, Email string }
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return svc.ExportRecipient(r.Context(), in.Project, in.Mode, in.Email)
	})
	h("POST /admin/v1/privacy/delete", A, func(r *http.Request, u *service.AdminUser) (any, error) {
		var in struct{ Project, Mode, Email string }
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		if err := svc.DeleteRecipient(r.Context(), in.Project, in.Mode, in.Email); err != nil {
			return nil, err
		}
		s.audit(r, u, "privacy.delete", "recipient", "", map[string]any{"project": in.Project, "mode": in.Mode})
		return nil, nil
	})
	h("GET /admin/v1/docs", V, func(r *http.Request, u *service.AdminUser) (any, error) {
		return map[string]any{"operations": v1Operations}, nil
	})
}

func (s *Server) postBatch(w http.ResponseWriter, r *http.Request) {
	p, err := s.Svc.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := p.AllowIP(s.clientIP(r)); err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Messages []json.RawMessage `json:"messages"`
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 6<<20))
	if err != nil || json.Unmarshal(body, &in) != nil {
		writeErr(w, &service.Error{Status: 422, Code: "invalid_request"})
		return
	}
	out, err := s.Svc.AcceptBatch(r.Context(), p, in.Messages)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"results": out})
}

func (s *Server) cancelEmail(w http.ResponseWriter, r *http.Request) {
	p, err := s.Svc.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := p.AllowIP(s.clientIP(r)); err != nil {
		writeErr(w, err)
		return
	}
	if !p.HasScope(service.ScopeSend) && !p.HasScope(service.ScopeSendRaw) {
		writeErr(w, &service.Error{Status: 403, Code: "missing_scope"})
		return
	}
	if err := s.Svc.Cancel(r.Context(), p, r.PathValue("id")); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "cancelled"})
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

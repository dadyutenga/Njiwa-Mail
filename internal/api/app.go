package api

import (
	"crypto/subtle"
	"net/http"

	"github.com/ditronics-tz/mail/internal/service"
)

// Application API additions in v2: inbound mail and suppressions. Every
// request authenticates an API key and every query is scoped to that key's
// project AND environment.

type keyFn func(r *http.Request, p *service.Principal) (any, error)

func (s *Server) key(scope string, h keyFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := s.Svc.Authenticate(r.Context(), r.Header.Get("Authorization"))
		if err != nil {
			writeErr(w, err)
			return
		}
		if !p.HasScope(scope) {
			writeErr(w, &service.Error{Status: 403, Code: "missing_scope"})
			return
		}
		out, err := h(r, p)
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

func (s *Server) appRoutes(mux *http.ServeMux) {
	svc := s.Svc
	mux.HandleFunc("GET /v1/inbound", s.key(service.ScopeInboundRead, func(r *http.Request, p *service.Principal) (any, error) {
		items, more, err := svc.ListInbound(r.Context(), p.EnvironmentID, "", "", pageOf(r))
		return list(r, items, more, err)
	}))
	mux.HandleFunc("GET /v1/inbound/{id}", s.key(service.ScopeInboundRead, func(r *http.Request, p *service.Principal) (any, error) {
		return svc.GetInbound(r.Context(), p.EnvironmentID, r.PathValue("id"), true)
	}))
	mux.HandleFunc("GET /v1/inbound/{id}/attachments/{aid}", s.key(service.ScopeInboundRead, func(r *http.Request, p *service.Principal) (any, error) {
		f, err := svc.OpenAttachment(r.Context(), p.EnvironmentID, r.PathValue("id"), r.PathValue("aid"))
		if err != nil {
			return nil, err
		}
		return download(f), nil
	}))

	actor := func(p *service.Principal) service.Actor {
		return service.Actor{KeyID: p.KeyID, Name: "key:" + p.KeyPrefix}
	}
	mux.HandleFunc("GET /v1/suppressions", s.key(service.ScopeSuppressions, func(r *http.Request, p *service.Principal) (any, error) {
		items, more, err := svc.KeySuppressions(r.Context(), p, pageOf(r))
		return list(r, items, more, err)
	}))
	mux.HandleFunc("POST /v1/suppressions", s.key(service.ScopeSuppressions, func(r *http.Request, p *service.Principal) (any, error) {
		var in struct {
			Email  string `json:"email"`
			Reason string `json:"reason"`
		}
		if err := decode(r, &in); err != nil {
			return nil, &service.Error{Status: 422, Code: "invalid_request"}
		}
		id, err := svc.KeySuppress(r.Context(), p, in.Email, in.Reason)
		if err != nil {
			return nil, err
		}
		svc.Audit(r.Context(), actor(p), "suppression.add", "suppression", id, nil)
		return map[string]string{"id": id}, nil
	}))
	// {id} is a suppression id or the email address itself.
	mux.HandleFunc("DELETE /v1/suppressions/{id}", s.key(service.ScopeSuppressions, func(r *http.Request, p *service.Principal) (any, error) {
		if err := svc.KeyUnsuppress(r.Context(), p, r.PathValue("id")); err != nil {
			return nil, err
		}
		svc.Audit(r.Context(), actor(p), "suppression.remove", "suppression", "", nil)
		return nil, nil
	}))

	// Prometheus metrics: disabled (404) unless a token is configured.
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		want := "Bearer " + s.MetricsToken
		if s.MetricsToken == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(want)) != 1 {
			http.NotFound(w, r)
			return
		}
		text, err := svc.Metrics(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Write([]byte(text))
	})
}

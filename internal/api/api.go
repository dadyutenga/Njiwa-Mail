// Package api exposes the HTTP contract.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ditronics-tz/mail/internal/service"
)

type Server struct {
	Svc             *service.Service
	MaxBodyBytes    int64
	EmailMaxBytes   int64 // larger cap for POST /v1/emails so attachments fit; 0 uses MaxBodyBytes
	ConsolePassword string
	SessionTTL      time.Duration
	SMTPConfigured  bool

	// Admin API (/admin/v1).
	TrustProxy      bool             // take the client IP from X-Forwarded-For (set by our own reverse proxy)
	InsecureCookies bool             // local HTTP development only: drop the Secure cookie attribute
	MetricsToken    string           // bearer token for /metrics; empty disables the endpoint
	Resolver        service.Resolver // DNS resolver override for tests; nil = system resolver
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/emails", s.postEmail)
	mux.HandleFunc("GET /v1/emails/{id}", s.getEmail)
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /health/ready", s.ready)
	mux.HandleFunc("POST /console/login", s.consoleLogin)
	mux.HandleFunc("POST /console/logout", s.consoleLogout)
	mux.HandleFunc("GET /console/me", s.consoleMe)
	mux.HandleFunc("GET /console/overview", s.consoleOverview)
	mux.HandleFunc("GET /console/messages", s.consoleMessages)
	mux.HandleFunc("GET /console/messages/{id}", s.consoleMessage)
	mux.HandleFunc("GET /console/projects", s.consoleProjects)
	mux.HandleFunc("GET /console/keys", s.consoleKeys)
	s.appRoutes(mux)
	s.adminRoutes(mux)
	s.productRoutes(mux)
	return s.wrap(mux)
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(c int) { r.code = c; r.ResponseWriter.WriteHeader(c) }

// wrap adds recovery, no-store caching and metadata-only request logging
// (no bodies, no Authorization header, no query strings).
func (s *Server) wrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, code: 200}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		defer func() {
			if p := recover(); p != nil {
				slog.Error("handler panic", "panic", p)
				if rec.code == 200 {
					writeErr(rec, &service.Error{Status: 500, Code: "internal_error"})
				}
			}
			slog.Info("request", "method", r.Method, "route", r.Pattern, "status", rec.code, "ms", time.Since(start).Milliseconds())
		}()
		h.ServeHTTP(rec, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	var e *service.Error
	if !errors.As(err, &e) {
		e = &service.Error{Status: 500, Code: "internal_error"}
	}
	if e.RetryAfter > 0 {
		secs := int(e.RetryAfter.Round(time.Second) / time.Second)
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
	}
	if e.Status == 401 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="ditronics-mail"`)
	}
	body := map[string]string{"code": e.Code}
	if e.Message != "" {
		body["message"] = e.Message
	}
	writeJSON(w, e.Status, map[string]any{"error": body})
}

func (s *Server) postEmail(w http.ResponseWriter, r *http.Request) {
	p, err := s.Svc.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := p.AllowIP(s.clientIP(r)); err != nil {
		writeErr(w, err)
		return
	}
	limit := s.MaxBodyBytes
	if s.EmailMaxBytes > limit {
		limit = s.EmailMaxBytes
	}
	if r.ContentLength > limit {
		writeErr(w, &service.Error{Status: 413, Code: "body_too_large"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeErr(w, &service.Error{Status: 413, Code: "body_too_large"})
		} else {
			writeErr(w, &service.Error{Status: 422, Code: "invalid_request"})
		}
		return
	}
	res, err := s.Svc.Accept(r.Context(), p, r.Header.Get("Idempotency-Key"), body)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 202, res)
}

func (s *Server) getEmail(w http.ResponseWriter, r *http.Request) {
	p, err := s.Svc.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := p.AllowIP(s.clientIP(r)); err != nil {
		writeErr(w, err)
		return
	}
	st, err := s.Svc.GetStatus(r.Context(), p, r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, st)
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Svc.Ready(ctx); err != nil {
		writeJSON(w, 503, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ready"})
}

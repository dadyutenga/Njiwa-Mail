package api

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ditronics-tz/mail/internal/service"
)

const consoleCookie = "dm_session"

func (s *Server) consoleOn() bool {
	return strings.TrimSpace(s.ConsolePassword) != "" && s.Svc != nil && s.Svc.Box != nil
}

func (s *Server) sessionTTL() time.Duration {
	if s.SessionTTL <= 0 {
		return 12 * time.Hour
	}
	return s.SessionTTL
}

func (s *Server) passwordOK(given string) bool {
	if !s.consoleOn() {
		return false
	}
	want := s.Svc.Box.MAC("console-password", []byte(s.ConsolePassword))
	got := s.Svc.Box.MAC("console-password", []byte(given))
	return hmac.Equal(want, got)
}

func (s *Server) issueSession() (string, time.Time, error) {
	exp := time.Now().Add(s.sessionTTL())
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", time.Time{}, err
	}
	expStr := strconv.FormatInt(exp.Unix(), 10)
	mac := s.Svc.Box.MAC("console-session", []byte(expStr), nonce)
	token := expStr + "." + hex.EncodeToString(nonce) + "." + hex.EncodeToString(mac)
	return token, exp, nil
}

func (s *Server) validSession(tok string) bool {
	if !s.consoleOn() || len(tok) == 0 || len(tok) > 256 {
		return false
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return false
	}
	expUnix, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().After(time.Unix(expUnix, 0)) {
		return false
	}
	nonce, err := hex.DecodeString(parts[1])
	if err != nil || len(nonce) != 16 {
		return false
	}
	mac, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expect := s.Svc.Box.MAC("console-session", []byte(parts[0]), nonce)
	return hmac.Equal(expect, mac)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, exp time.Time) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     consoleCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		Expires:  exp,
		MaxAge:   int(s.sessionTTL().Seconds()),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     consoleCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (s *Server) requireConsole(w http.ResponseWriter, r *http.Request) bool {
	if !s.consoleOn() {
		http.NotFound(w, r)
		return false
	}
	c, err := r.Cookie(consoleCookie)
	if err != nil || !s.validSession(c.Value) {
		writeErr(w, &service.Error{Status: 401, Code: "unauthenticated"})
		return false
	}
	return true
}

func (s *Server) consoleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.consoleOn() {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if err != nil {
		writeErr(w, &service.Error{Status: 422, Code: "invalid_request"})
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.Unmarshal(body, &req); err != nil || strings.TrimSpace(req.Password) == "" {
		writeErr(w, &service.Error{Status: 401, Code: "invalid_credentials"})
		return
	}
	if !s.passwordOK(req.Password) {
		writeErr(w, &service.Error{Status: 401, Code: "invalid_credentials"})
		return
	}
	token, exp, err := s.issueSession()
	if err != nil {
		writeErr(w, &service.Error{Status: 500, Code: "internal_error"})
		return
	}
	s.setSessionCookie(w, r, token, exp)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) consoleLogout(w http.ResponseWriter, r *http.Request) {
	if !s.consoleOn() {
		http.NotFound(w, r)
		return
	}
	s.clearSessionCookie(w)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) consoleMe(w http.ResponseWriter, r *http.Request) {
	if !s.requireConsole(w, r) {
		return
	}
	writeJSON(w, 200, map[string]string{
		"id":   "operator",
		"name": "Operator",
		"role": "admin",
	})
}

func (s *Server) consoleOverview(w http.ResponseWriter, r *http.Request) {
	if !s.requireConsole(w, r) {
		return
	}
	ov, err := s.Svc.ConsoleOverview(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, struct {
		*service.ConsoleOverview
		SMTPConfigured bool `json:"smtp_configured"`
	}{ConsoleOverview: ov, SMTPConfigured: s.SMTPConfigured})
}

func (s *Server) consoleMessages(w http.ResponseWriter, r *http.Request) {
	if !s.requireConsole(w, r) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := s.Svc.ConsoleMessages(r.Context(), r.URL.Query().Get("state"), limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"messages": rows})
}

func (s *Server) consoleMessage(w http.ResponseWriter, r *http.Request) {
	if !s.requireConsole(w, r) {
		return
	}
	row, err := s.Svc.ConsoleMessage(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, row)
}

func (s *Server) consoleProjects(w http.ResponseWriter, r *http.Request) {
	if !s.requireConsole(w, r) {
		return
	}
	rows, err := s.Svc.ConsoleProjects(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"projects": rows})
}

func (s *Server) consoleKeys(w http.ResponseWriter, r *http.Request) {
	if !s.requireConsole(w, r) {
		return
	}
	keys, err := s.Svc.ListKeys(r.Context(), "")
	if err != nil {
		writeErr(w, err)
		return
	}
	type keyView struct {
		Prefix    string     `json:"prefix"`
		Project   string     `json:"project"`
		Mode      string     `json:"mode"`
		Label     string     `json:"label"`
		Scopes    []string   `json:"scopes"`
		Status    string     `json:"status"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}
	out := make([]keyView, 0, len(keys))
	now := time.Now()
	for _, k := range keys {
		scopes := k.Scopes
		if scopes == nil {
			scopes = []string{}
		}
		st := "active"
		if k.Revoked != nil {
			st = "revoked"
		} else if k.Expires != nil && !k.Expires.After(now) {
			st = "expired"
		}
		out = append(out, keyView{
			Prefix: k.Prefix, Project: k.Project, Mode: k.Mode, Label: k.Label,
			Scopes: scopes, Status: st, ExpiresAt: k.Expires,
		})
	}
	writeJSON(w, 200, map[string]any{"keys": out})
}

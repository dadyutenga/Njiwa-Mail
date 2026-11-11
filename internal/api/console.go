package api

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ditronics-tz/mail/internal/service"
	"github.com/gofiber/fiber/v2"
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

func (s *Server) setSessionCookie(c *fiber.Ctx, token string, exp time.Time) {
	secure := c.Protocol() == "https" || strings.EqualFold(c.Get("X-Forwarded-Proto"), "https")
	setCookie(c, &http.Cookie{
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

func (s *Server) clearSessionCookie(c *fiber.Ctx) {
	setCookie(c, &http.Cookie{
		Name:     consoleCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (s *Server) requireConsole(c *fiber.Ctx) bool {
	if !s.consoleOn() {
		_ = notFound(c)
		return false
	}
	tok, err := cookieVal(c, consoleCookie)
	if err != nil || !s.validSession(tok) {
		_ = writeErr(c, &service.Error{Status: 401, Code: "unauthenticated"})
		return false
	}
	return true
}

func (s *Server) consoleLogin(c *fiber.Ctx) error {
	if !s.consoleOn() {
		return notFound(c)
	}
	if len(c.Body()) > 1024 {
		return writeErr(c, &service.Error{Status: 422, Code: "invalid_request"})
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.Unmarshal(c.Body(), &req); err != nil || strings.TrimSpace(req.Password) == "" {
		return writeErr(c, &service.Error{Status: 401, Code: "invalid_credentials"})
	}
	if !s.passwordOK(req.Password) {
		return writeErr(c, &service.Error{Status: 401, Code: "invalid_credentials"})
	}
	token, exp, err := s.issueSession()
	if err != nil {
		return writeErr(c, &service.Error{Status: 500, Code: "internal_error"})
	}
	s.setSessionCookie(c, token, exp)
	return writeJSON(c, 200, map[string]bool{"ok": true})
}

func (s *Server) consoleLogout(c *fiber.Ctx) error {
	if !s.consoleOn() {
		return notFound(c)
	}
	s.clearSessionCookie(c)
	return writeJSON(c, 200, map[string]bool{"ok": true})
}

func (s *Server) consoleMe(c *fiber.Ctx) error {
	if !s.requireConsole(c) {
		return nil
	}
	return writeJSON(c, 200, map[string]string{
		"id":   "operator",
		"name": "Operator",
		"role": "admin",
	})
}

func (s *Server) consoleOverview(c *fiber.Ctx) error {
	if !s.requireConsole(c) {
		return nil
	}
	ov, err := s.Svc.ConsoleOverview(reqCtx(c))
	if err != nil {
		return writeErr(c, err)
	}
	return writeJSON(c, 200, struct {
		*service.ConsoleOverview
		SMTPConfigured bool `json:"smtp_configured"`
	}{ConsoleOverview: ov, SMTPConfigured: s.SMTPConfigured})
}

func (s *Server) consoleMessages(c *fiber.Ctx) error {
	if !s.requireConsole(c) {
		return nil
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	rows, err := s.Svc.ConsoleMessages(reqCtx(c), c.Query("state"), limit)
	if err != nil {
		return writeErr(c, err)
	}
	return writeJSON(c, 200, map[string]any{"messages": rows})
}

func (s *Server) consoleMessage(c *fiber.Ctx) error {
	if !s.requireConsole(c) {
		return nil
	}
	row, err := s.Svc.ConsoleMessage(reqCtx(c), c.Params("id"))
	if err != nil {
		return writeErr(c, err)
	}
	return writeJSON(c, 200, row)
}

func (s *Server) consoleProjects(c *fiber.Ctx) error {
	if !s.requireConsole(c) {
		return nil
	}
	rows, err := s.Svc.ConsoleProjects(reqCtx(c))
	if err != nil {
		return writeErr(c, err)
	}
	return writeJSON(c, 200, map[string]any{"projects": rows})
}

func (s *Server) consoleKeys(c *fiber.Ctx) error {
	if !s.requireConsole(c) {
		return nil
	}
	keys, err := s.Svc.ListKeys(reqCtx(c), "")
	if err != nil {
		return writeErr(c, err)
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
	return writeJSON(c, 200, map[string]any{"keys": out})
}

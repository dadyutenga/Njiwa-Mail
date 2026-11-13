package test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/njiwa-tz/mail/internal/api"
)

func consoleHandler(e *env, password string) *fiber.App {
	return (&api.Server{
		Svc:             e.svc,
		MaxBodyBytes:    32 * 1024,
		ConsolePassword: password,
		SMTPConfigured:  true,
	}).App()
}

func consoleDo(h *fiber.App, method, path, cookie string, body any) *httptest.ResponseRecorder {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	res, err := h.Test(req, fiber.TestConfig{Timeout: testTimeout, FailOnTimeout: true})
	w := httptest.NewRecorder()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return w
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	for k, vs := range res.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	w.Write(b)
	return w
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	res := w.Result()
	defer res.Body.Close()
	var parts []string
	for _, c := range res.Cookies() {
		if c.Name == "dm_session" && c.Value != "" {
			if !c.HttpOnly {
				t.Fatal("session cookie must be HttpOnly")
			}
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	if len(parts) == 0 {
		t.Fatalf("no session cookie: %s", w.Body.String())
	}
	return strings.Join(parts, "; ")
}

func TestConsoleDisabledWithoutPassword(t *testing.T) {
	e := setup(t)
	h := consoleHandler(e, "")
	w := consoleDo(h, http.MethodPost, "/console/login", "", map[string]string{"password": "correct-horse-1"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("disabled console status %d", w.Code)
	}
}

func TestConsoleIsMetadataOnly(t *testing.T) {
	e := setup(t)
	k := e.provision("billing")
	id := str(e.post(k.sbx, idem("console"), otpBody(testRcpt)).JSON, "id")
	if id == "" {
		t.Fatal("send failed")
	}
	e.runWorker()

	h := consoleHandler(e, "correct-horse-1")
	if w := consoleDo(h, http.MethodGet, "/console/overview", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("overview without session: %d", w.Code)
	}
	bad := consoleDo(h, http.MethodPost, "/console/login", "", map[string]string{"password": "wrong-password"})
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("bad login: %d %s", bad.Code, bad.Body.String())
	}
	ok := consoleDo(h, http.MethodPost, "/console/login", "", map[string]string{"password": "correct-horse-1"})
	if ok.Code != http.StatusOK {
		t.Fatalf("login: %d %s", ok.Code, ok.Body.String())
	}
	cookie := sessionCookie(t, ok)

	detail := consoleDo(h, http.MethodGet, "/console/messages/"+id, cookie, nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail: %d %s", detail.Code, detail.Body.String())
	}
	raw := detail.Body.String()
	for _, secret := range []string{otpCode, testRcpt, k.sbx, k.live, "reset_url", "variables"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("console detail leaked %q: %s", secret, raw)
		}
	}
	if strings.Contains(strings.ToLower(raw), "delivered") {
		t.Fatalf("status overclaims delivery: %s", raw)
	}

	keys := consoleDo(h, http.MethodGet, "/console/keys", cookie, nil)
	if keys.Code != http.StatusOK {
		t.Fatalf("keys: %d %s", keys.Code, keys.Body.String())
	}
	if strings.Contains(keys.Body.String(), k.sbx) || strings.Contains(keys.Body.String(), k.live) {
		t.Fatalf("key secret returned: %s", keys.Body.String())
	}

	projects := consoleDo(h, http.MethodGet, "/console/projects", cookie, nil)
	if projects.Code != http.StatusOK || !strings.Contains(projects.Body.String(), "billing") {
		t.Fatalf("projects: %d %s", projects.Code, projects.Body.String())
	}
	overview := consoleDo(h, http.MethodGet, "/console/overview", cookie, nil)
	if overview.Code != http.StatusOK || !strings.Contains(overview.Body.String(), `"smtp_configured":true`) {
		t.Fatalf("overview: %d %s", overview.Code, overview.Body.String())
	}
}

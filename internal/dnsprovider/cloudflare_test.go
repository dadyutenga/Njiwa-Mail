package dnsprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const goodToken = "tok_0123456789abcdefghijklmnopqrstuvwxyz"

func cfServer(t *testing.T, h http.HandlerFunc) (*Cloudflare, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewCloudflare(goodToken)
	if err != nil {
		t.Fatal(err)
	}
	c.BaseURL = srv.URL
	return c, srv
}

func reply(w http.ResponseWriter, status int, result any, errs ...cfError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	b, _ := json.Marshal(result)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": status < 300 && len(errs) == 0, "errors": errs, "result": json.RawMessage(b),
		"result_info": map[string]int{"total_pages": 1}})
}

func TestRejectsGlobalKeysAndBadTokens(t *testing.T) {
	for _, tok := range []string{"", "short", "0123456789abcdef0123456789abcdef01234", "has space in the token value 1234567"} {
		if _, err := NewCloudflare(tok); !errors.Is(err, ErrInvalid) {
			t.Errorf("token %q: want ErrInvalid, got %v", tok, err)
		}
	}
}

func TestVerifyStates(t *testing.T) {
	for status, wantErr := range map[string]bool{"active": false, "expired": true, "disabled": true} {
		c, _ := cfServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+goodToken || r.URL.Path != "/user/tokens/verify" {
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			}
			reply(w, 200, map[string]string{"status": status, "expires_on": "2030-01-02T03:04:05Z"})
		})
		exp, err := c.Verify(context.Background())
		if (err != nil) != wantErr {
			t.Errorf("%s: err=%v", status, err)
		}
		if exp.Year() != 2030 {
			t.Errorf("%s: expiry not parsed: %v", status, exp)
		}
	}
}

func TestStatusMapping(t *testing.T) {
	cases := map[int]func(error) bool{
		401: func(e error) bool { return errors.Is(e, ErrUnauthorized) },
		403: func(e error) bool { return errors.Is(e, ErrForbidden) },
		404: func(e error) bool { return errors.Is(e, ErrNotFound) },
		409: func(e error) bool { return errors.Is(e, ErrConflict) },
		400: func(e error) bool { return errors.Is(e, ErrInvalid) },
		503: func(e error) bool { var t *TransientError; return errors.As(e, &t) },
	}
	for code, ok := range cases {
		c, _ := cfServer(t, func(w http.ResponseWriter, r *http.Request) {
			reply(w, code, nil, cfError{Code: 1, Message: "SECRET-BODY-TEXT " + goodToken})
		})
		_, err := c.Zones(context.Background())
		if err == nil || !ok(err) {
			t.Errorf("status %d: got %v", code, err)
		}
		if err != nil && (strings.Contains(err.Error(), "SECRET-BODY-TEXT") || strings.Contains(err.Error(), goodToken)) {
			t.Errorf("status %d: error leaks provider body or token: %v", code, err)
		}
	}
}

func TestRateLimitCarriesRetryAfter(t *testing.T) {
	c, _ := cfServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		reply(w, 429, nil, cfError{Code: 971})
	})
	_, err := c.Zones(context.Background())
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != 7*time.Second {
		t.Fatalf("got %v", err)
	}
}

func TestTimeoutIsTransient(t *testing.T) {
	c, _ := cfServer(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) })
	c.HTTP = &http.Client{Timeout: 50 * time.Millisecond}
	_, err := c.Zones(context.Background())
	var tr *TransientError
	if !errors.As(err, &tr) {
		t.Fatalf("want transient, got %v", err)
	}
	if strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("error leaks the URL: %v", err)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	var hits int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.Header.Get("Authorization") != "" {
			t.Error("token was sent to the redirect target")
		}
	}))
	defer other.Close()
	c, _ := cfServer(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) })
	if _, err := c.Zones(context.Background()); err == nil {
		t.Fatal("a redirect must not succeed")
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatal("redirect was followed")
	}
}

func TestPlainHTTPToARemoteHostIsRefused(t *testing.T) {
	c, _ := NewCloudflare(goodToken)
	c.BaseURL = "http://api.example.com/client/v4"
	if _, err := c.Zones(context.Background()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestRecordsNormaliseAndPaginate(t *testing.T) {
	var page int32
	c, _ := cfServer(t, func(w http.ResponseWriter, r *http.Request) {
		p := atomic.AddInt32(&page, 1)
		w.Header().Set("Content-Type", "application/json")
		recs := []map[string]any{{"id": "r1", "type": "TXT", "name": "Example.com.", "content": `"v=spf1 " "include:a.example -all"`, "ttl": 300}}
		if p == 2 {
			recs = []map[string]any{{"id": "r2", "type": "MX", "name": "example.com", "content": "Mail.Example.com.", "priority": 10, "ttl": 1}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": recs, "result_info": map[string]int{"total_pages": 2}})
	})
	rs, err := c.Records(context.Background(), "zone1")
	if err != nil || len(rs) != 2 {
		t.Fatalf("%v %v", rs, err)
	}
	if rs[0].Content != "v=spf1 include:a.example -all" || rs[0].Name != "example.com" || rs[0].TTL != 300 {
		t.Errorf("TXT not normalised: %+v", rs[0])
	}
	if rs[1].Content != "mail.example.com" || rs[1].Priority != 10 {
		t.Errorf("MX not normalised: %+v", rs[1])
	}
}

func TestCreateSendsUnproxiedAutoTTLAndNoSecretsInPath(t *testing.T) {
	var got map[string]any
	c, _ := cfServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/zones/zone1/dns_records" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		reply(w, 200, map[string]any{"id": "new1", "type": "MX", "name": "example.com", "content": "mail.example.com", "priority": 10, "ttl": 1})
	})
	out, err := c.Create(context.Background(), "zone1", Record{Type: "MX", Name: "example.com", Content: "mail.example.com", Priority: 10})
	if err != nil || out.ID != "new1" {
		t.Fatalf("%v %v", out, err)
	}
	if got["proxied"] != false || got["ttl"] != float64(1) || got["priority"] != float64(10) {
		t.Errorf("body: %v", got)
	}
}

func TestIdentifiersAreValidated(t *testing.T) {
	c, _ := NewCloudflare(goodToken)
	for _, id := range []string{"../etc", "a/b", "", "x y"} {
		if _, err := c.Records(context.Background(), id); !errors.Is(err, ErrInvalid) {
			t.Errorf("zone id %q accepted: %v", id, err)
		}
		if err := c.Delete(context.Background(), "zone1", id); !errors.Is(err, ErrInvalid) {
			t.Errorf("record id %q accepted: %v", id, err)
		}
	}
}

func TestRetryIsBoundedAndSkipsPermanentErrors(t *testing.T) {
	var slept []time.Duration
	sleep := func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }
	n := 0
	err := Retry(context.Background(), 4, time.Second, 10*time.Second, sleep, func() error {
		n++
		return &TransientError{Err: errors.New("x")}
	})
	var tr *TransientError
	if !errors.As(err, &tr) || n != 4 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if len(slept) != 3 || slept[0] != time.Second || slept[1] != 2*time.Second || slept[2] != 4*time.Second {
		t.Errorf("backoff: %v", slept)
	}
	n, slept = 0, nil
	_ = Retry(context.Background(), 5, time.Second, 3*time.Second, sleep, func() error {
		n++
		if n < 3 {
			return &RateLimitError{RetryAfter: time.Hour}
		}
		return nil
	})
	if n != 3 || slept[0] != 3*time.Second {
		t.Errorf("Retry-After must be honoured but bounded: n=%d slept=%v", n, slept)
	}
	n = 0
	err = Retry(context.Background(), 5, time.Second, time.Second, sleep, func() error { n++; return ErrForbidden })
	if n != 1 || !errors.Is(err, ErrForbidden) {
		t.Errorf("a permanent error must not be retried: n=%d", n)
	}
}

func TestNormTXT(t *testing.T) {
	for in, want := range map[string]string{
		`"v=DKIM1; k=rsa; p=AAA" "BBB"`: "v=DKIM1; k=rsa; p=AAABBB",
		`plain value`:                   "plain value",
		`"with \"quote\""`:              `with "quote"`,
		`"unterminated`:                 `"unterminated`,
	} {
		if got := normTXT(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

// Account-owned tokens fail the user verify endpoint but work everywhere else.
func TestVerifyFallsBackToAZoneProbeForAccountTokens(t *testing.T) {
	c, _ := cfServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user/tokens/verify":
			reply(w, 400, nil, cfError{Code: 1000, Message: "Invalid API Token"})
		case "/zones":
			reply(w, 200, []map[string]any{{"id": "z1", "name": "example.com", "status": "active"}})
		}
	})
	if _, err := c.Verify(context.Background()); err != nil {
		t.Fatalf("an account token that can list zones must verify: %v", err)
	}
	bad, _ := cfServer(t, func(w http.ResponseWriter, r *http.Request) { reply(w, 403, nil, cfError{Code: 9109}) })
	if _, err := bad.Verify(context.Background()); err == nil {
		t.Fatal("a token refused everywhere must not verify")
	}
	// A timeout is not evidence either way.
	slow, _ := cfServer(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(200 * time.Millisecond) })
	slow.HTTP = &http.Client{Timeout: 30 * time.Millisecond}
	var tr *TransientError
	if _, err := slow.Verify(context.Background()); !errors.As(err, &tr) {
		t.Fatalf("want transient, got %v", err)
	}
}

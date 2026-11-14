package test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeIdP is an OpenID Connect provider that signs its own ID tokens.
type fakeIdP struct {
	srv       *httptest.Server
	key       *rsa.PrivateKey
	kid       string
	mu        sync.Mutex
	challenge string
	claims    map[string]any // claims for the next token
	secret    string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeIdP{key: k, kid: "k1", secret: "idp-client-secret"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"issuer": p.srv.URL, "authorization_endpoint": p.srv.URL + "/authorize",
			"token_endpoint": p.srv.URL + "/token", "jwks_uri": p.srv.URL + "/jwks"})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{"kty": "RSA", "kid": p.kid, "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(k.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes())}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		p.mu.Lock()
		defer p.mu.Unlock()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if r.Form.Get("client_secret") != p.secret || r.Form.Get("code") != "good-code" ||
			base64.RawURLEncoding.EncodeToString(sum[:]) != p.challenge {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"id_token": p.sign(p.key, p.kid, p.claims)})
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakeIdP) sign(k *rsa.PrivateKey, kid string, claims map[string]any) string {
	enc := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
	head := enc(map[string]string{"alg": "RS256", "typ": "JWT", "kid": kid})
	body := enc(claims)
	sum := sha256.Sum256([]byte(head + "." + body))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, sum[:])
	return head + "." + body + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// signIn runs one sign-in through the app: start, the provider's answer for
// the given claims (a function of the request's nonce), then the callback.
func (e *env) signIn(p *fakeIdP, providerID string, claims func(nonce string) map[string]any) resp {
	e.t.Helper()
	start := e.raw("GET", "/admin/v1/auth/sso/"+providerID+"/start", nil, nil)
	if start.Code != 302 {
		e.t.Fatalf("start: %d %s", start.Code, start.Raw)
	}
	loc, err := url.Parse(start.Hdr.Get("Location"))
	if err != nil || !strings.HasPrefix(loc.String(), p.srv.URL+"/authorize") {
		e.t.Fatalf("start redirected to %q", start.Hdr.Get("Location"))
	}
	q := loc.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" || q.Get("state") == "" || q.Get("nonce") == "" {
		e.t.Fatalf("authorization request: %v", q)
	}
	p.mu.Lock()
	p.challenge = q.Get("code_challenge")
	p.claims = claims(q.Get("nonce"))
	p.mu.Unlock()
	return e.raw("GET", "/admin/v1/auth/sso/callback?state="+url.QueryEscape(q.Get("state"))+"&code=good-code", nil, nil)
}

func idClaims(p *fakeIdP, clientID, nonce, sub, email string) map[string]any {
	now := time.Now().Unix()
	return map[string]any{"iss": p.srv.URL, "aud": clientID, "sub": sub, "email": email, "email_verified": true, "nonce": nonce,
		"iat": now, "exp": now + 300}
}

func TestSSOSignIn(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.svc.Opts.PublicURL = "http://localhost:3000"
	op := e.user("op@njiwa.co.tz", "admin")
	_, err := e.svc.CreateUserWith(ctx, "dev@njiwa.co.tz", adminPass, "admin", false)
	e.must(err)
	dev := e.login("dev@njiwa.co.tz")
	idp := newFakeIdP(t)

	body := map[string]any{"name": "Company", "issuer": idp.srv.URL, "client_id": "njiwa", "client_secret": idp.secret, "allowed_domains": []string{"njiwa.co.tz"}}
	if r := e.adm(dev, "POST", "/admin/v1/sso-providers", body); r.Code != 403 {
		t.Fatalf("non-operator: %d", r.Code)
	}
	bad := map[string]any{"name": "X", "issuer": "http://idp.example.com", "client_id": "a", "client_secret": "b"}
	if r := e.adm(op, "POST", "/admin/v1/sso-providers", bad); r.Code != 400 {
		t.Fatalf("plain http issuer: %d %s", r.Code, r.Raw)
	}
	if r := e.adm(op, "POST", "/admin/v1/sso-providers", map[string]any{"name": "X", "issuer": idp.srv.URL, "client_id": "a", "client_secret": "b", "auto_create": true}); r.Code != 400 {
		t.Fatalf("auto create without domains: %d %s", r.Code, r.Raw)
	}
	pid := str(e.admOK(op, "POST", "/admin/v1/sso-providers", body).JSON, "id")
	if g := e.admOK(op, "GET", "/admin/v1/sso-providers", nil); strings.Contains(g.Raw, idp.secret) || !strings.Contains(g.Raw, `"name":"Company"`) {
		t.Fatalf("list leaks or misses: %s", g.Raw)
	}
	pub := e.raw("GET", "/admin/v1/auth/sso/providers", nil, nil)
	if pub.Code != 200 || !strings.Contains(pub.Raw, `"name":"Company"`) || strings.Contains(pub.Raw, "issuer") {
		t.Fatalf("public list: %d %s", pub.Code, pub.Raw)
	}
	good := func(sub, email string) func(string) map[string]any {
		return func(n string) map[string]any { return idClaims(idp, "njiwa", n, sub, email) }
	}
	failed := func(r resp) bool { return r.Code == 302 && r.Hdr.Get("Location") == "/login?sso_error=sso_failed" }

	// An existing account signs in by its verified email; the subject is linked.
	r := e.signIn(idp, pid, good("sub-dev", "dev@njiwa.co.tz"))
	if r.Code != 302 || r.Hdr.Get("Location") != "/" || !strings.Contains(r.Hdr.Get("Set-Cookie"), "HttpOnly") {
		t.Fatalf("sign-in: %d %v", r.Code, r.Hdr)
	}
	cookie := strings.SplitN(r.Hdr.Get("Set-Cookie"), ";", 2)[0]
	if me := e.raw("GET", "/admin/v1/auth/me", map[string]string{"Cookie": cookie}, nil); me.Code != 200 || str(me.JSON, "email") != "dev@njiwa.co.tz" || me.JSON["installation_operator"] != false {
		t.Fatalf("session: %d %s", me.Code, me.Raw)
	}
	// The link is by subject: an email change at the provider keeps the account,
	// and the same subject cannot become another account by claiming its email.
	if r := e.signIn(idp, pid, good("sub-dev", "dev@njiwa.co.tz")); r.Hdr.Get("Location") != "/" {
		t.Fatalf("second sign-in: %v", r.Hdr)
	}
	if r := e.signIn(idp, pid, good("sub-dev", "op@njiwa.co.tz")); r.Hdr.Get("Location") != "/" ||
		str(e.raw("GET", "/admin/v1/auth/me", map[string]string{"Cookie": strings.SplitN(r.Hdr.Get("Set-Cookie"), ";", 2)[0]}, nil).JSON, "email") != "dev@njiwa.co.tz" {
		t.Fatal("a linked subject must keep its own account")
	}
	// Not allowed: an unknown email while creation is off, another domain, an unverified email.
	if r := e.signIn(idp, pid, good("sub-new", "new@njiwa.co.tz")); !failed(r) {
		t.Fatalf("unknown email with creation off: %d %v", r.Code, r.Hdr)
	}
	if n := e.count(`SELECT count(*) FROM users WHERE email='new@njiwa.co.tz'`); n != 0 {
		t.Fatal("a user was created while creation is off")
	}
	if r := e.signIn(idp, pid, good("sub-x", "dev@elsewhere.example")); !failed(r) {
		t.Fatalf("other domain: %v", r.Hdr)
	}
	if r := e.signIn(idp, pid, func(n string) map[string]any {
		c := idClaims(idp, "njiwa", n, "sub-dev", "dev@njiwa.co.tz")
		c["email_verified"] = false
		return c
	}); !failed(r) {
		t.Fatalf("unverified email: %v", r.Hdr)
	}
	// Token problems: wrong audience, wrong nonce, expired, signed by another key.
	for name, mod := range map[string]func(map[string]any){
		"audience": func(c map[string]any) { c["aud"] = "someone-else" },
		"nonce":    func(c map[string]any) { c["nonce"] = "replayed" },
		"expired":  func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() },
		"issuer":   func(c map[string]any) { c["iss"] = "https://evil.example" },
	} {
		if r := e.signIn(idp, pid, func(n string) map[string]any {
			c := idClaims(idp, "njiwa", n, "sub-dev", "dev@njiwa.co.tz")
			mod(c)
			return c
		}); !failed(r) {
			t.Fatalf("%s: %v", name, r.Hdr)
		}
	}
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	idp.mu.Lock()
	idp.key, idp.kid = other, "unknown"
	idp.mu.Unlock()
	if r := e.signIn(idp, pid, good("sub-dev", "dev@njiwa.co.tz")); !failed(r) {
		t.Fatalf("token from an unknown key: %v", r.Hdr)
	}
	// A made-up state and a provider-side error are refused.
	if r := e.raw("GET", "/admin/v1/auth/sso/callback?state=made-up&code=good-code", nil, nil); !failed(r) {
		t.Fatalf("made-up state: %v", r.Hdr)
	}
	if r := e.raw("GET", "/admin/v1/auth/sso/callback?error=access_denied", nil, nil); !failed(r) {
		t.Fatalf("provider error: %v", r.Hdr)
	}
}

// Creation on first sign-in (never an operator), an account with an
// authenticator still needs its code, a state works once, and disabled
// accounts and a wrong client secret are refused.
func TestSSOCreationMFAAndRefusals(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.svc.Opts.PublicURL = "http://localhost:3000"
	op := e.user("op@njiwa.co.tz", "admin")
	idp := newFakeIdP(t)
	pid := str(e.admOK(op, "POST", "/admin/v1/sso-providers", map[string]any{"name": "Company", "issuer": idp.srv.URL, "client_id": "njiwa",
		"client_secret": idp.secret, "allowed_domains": []string{"njiwa.co.tz"}, "auto_create": true}).JSON, "id")
	good := func(sub, email string) func(string) map[string]any {
		return func(n string) map[string]any { return idClaims(idp, "njiwa", n, sub, email) }
	}
	// First sign-in creates a non-operator account that cannot sign in with a password.
	r := e.signIn(idp, pid, good("sub-new", "New.Person@njiwa.co.tz"))
	if r.Code != 302 || r.Hdr.Get("Location") != "/" {
		t.Fatalf("create: %d %v", r.Code, r.Hdr)
	}
	var operator bool
	e.must(e.pool.QueryRow(ctx, `SELECT installation_operator FROM users WHERE email='new.person@njiwa.co.tz'`).Scan(&operator))
	if operator {
		t.Fatal("an SSO-created user must not be an operator")
	}
	if e.loginWith("new.person@njiwa.co.tz", "x").Code == 200 || e.loginWith("new.person@njiwa.co.tz", adminPass).Code == 200 {
		t.Fatal("an SSO-created account must not have a known password")
	}
	if n := e.count(`SELECT count(*) FROM audit_log WHERE action='auth.login' AND details->>'method'='sso'`); n != 1 {
		t.Fatalf("login audit: %d", n)
	}
	// A state is used once.
	start := e.raw("GET", "/admin/v1/auth/sso/"+pid+"/start", nil, nil)
	loc, _ := url.Parse(start.Hdr.Get("Location"))
	idp.mu.Lock()
	idp.challenge = loc.Query().Get("code_challenge")
	idp.claims = idClaims(idp, "njiwa", loc.Query().Get("nonce"), "sub-new", "new.person@njiwa.co.tz")
	idp.mu.Unlock()
	cb := "/admin/v1/auth/sso/callback?state=" + url.QueryEscape(loc.Query().Get("state")) + "&code=good-code"
	if r := e.raw("GET", cb, nil, nil); r.Hdr.Get("Location") != "/" {
		t.Fatalf("first use: %v", r.Hdr)
	}
	if r := e.raw("GET", cb, nil, nil); r.Hdr.Get("Location") != "/login?sso_error=sso_failed" {
		t.Fatalf("second use of a state: %v", r.Hdr)
	}
	// An account with an authenticator: the provider is not enough.
	mfa := e.user("mfa@njiwa.co.tz", "admin")
	secret, _ := e.enrol(mfa)
	r = e.signIn(idp, pid, good("sub-mfa", "mfa@njiwa.co.tz"))
	loc2, err := url.Parse(r.Hdr.Get("Location"))
	if err != nil || loc2.Path != "/login" || loc2.Query().Get("sso_pending") == "" || r.Hdr.Get("Set-Cookie") != "" {
		t.Fatalf("pending: %d %v", r.Code, r.Hdr)
	}
	pending := loc2.Query().Get("sso_pending")
	if c := e.raw("POST", "/admin/v1/auth/sso/complete", nil, map[string]any{"pending": pending, "code": "000000"}); c.Code != 401 {
		t.Fatalf("wrong code: %d %s", c.Code, c.Raw)
	}
	c := e.raw("POST", "/admin/v1/auth/sso/complete", nil, map[string]any{"pending": pending, "code": totpCode(t, secret, 1)})
	if c.Code != 200 || str(c.JSON, "email") != "mfa@njiwa.co.tz" || !strings.Contains(c.Hdr.Get("Set-Cookie"), "HttpOnly") {
		t.Fatalf("right code: %d %s", c.Code, c.Raw)
	}
	if c := e.raw("POST", "/admin/v1/auth/sso/complete", nil, map[string]any{"pending": pending, "code": totpCode(t, secret, 1)}); c.Code != 401 {
		t.Fatalf("pending used twice: %d", c.Code)
	}
	// Five wrong codes end a pending sign-in, even if the sixth is right.
	r = e.signIn(idp, pid, good("sub-mfa", "mfa@njiwa.co.tz"))
	l3, _ := url.Parse(r.Hdr.Get("Location"))
	p3 := l3.Query().Get("sso_pending")
	for i := 0; i < 5; i++ {
		e.raw("POST", "/admin/v1/auth/sso/complete", nil, map[string]any{"pending": p3, "code": "000000"})
	}
	if c := e.raw("POST", "/admin/v1/auth/sso/complete", nil, map[string]any{"pending": p3, "code": totpCode(t, secret, 1)}); c.Code != 401 {
		t.Fatalf("pending after five wrong codes: %d", c.Code)
	}
	// A disabled account is refused; a wrong client secret at the provider fails.
	_, err = e.pool.Exec(ctx, `UPDATE users SET disabled=true WHERE email='new.person@njiwa.co.tz'`)
	e.must(err)
	if r := e.signIn(idp, pid, good("sub-new", "new.person@njiwa.co.tz")); r.Hdr.Get("Location") != "/login?sso_error=sso_failed" {
		t.Fatalf("disabled: %v", r.Hdr)
	}
	e.admOK(op, "PUT", "/admin/v1/sso-providers/"+pid, map[string]any{"name": "Company", "issuer": idp.srv.URL, "client_id": "njiwa",
		"client_secret": "rotated-wrong-secret", "allowed_domains": []string{"njiwa.co.tz"}, "auto_create": true})
	if r := e.signIn(idp, pid, good("sub-mfa", "mfa@njiwa.co.tz")); r.Hdr.Get("Location") != "/login?sso_error=sso_failed" {
		t.Fatalf("wrong client secret: %v", r.Hdr)
	}
	// Disabled provider: no start. Deleted: gone.
	e.admOK(op, "PUT", "/admin/v1/sso-providers/"+pid, map[string]any{"name": "Company", "issuer": idp.srv.URL, "client_id": "njiwa", "enabled": false})
	if r := e.raw("GET", "/admin/v1/auth/sso/"+pid+"/start", nil, nil); r.Hdr.Get("Location") != "/login?sso_error=sso_unavailable" {
		t.Fatalf("disabled provider: %v", r.Hdr)
	}
	e.admOK(op, "DELETE", "/admin/v1/sso-providers/"+pid, nil)
	if n := e.count(`SELECT count(*) FROM sso_identities`); n != 0 {
		t.Fatalf("identities left after delete: %d", n)
	}
}

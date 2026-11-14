// Package oidc is the small part of OpenID Connect sign-in Njiwa-Mail needs:
// discovery, the authorization code exchange with PKCE, and ID token
// verification (RS256 and ES256, with the provider's published keys). It
// uses only the standard library.
package oidc

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Provider is the discovered configuration of an issuer.
type Provider struct {
	Issuer   string `json:"issuer"`
	AuthURL  string `json:"authorization_endpoint"`
	TokenURL string `json:"token_endpoint"`
	JWKSURL  string `json:"jwks_uri"`
}

type cached[T any] struct {
	v  T
	at time.Time
}

// Client fetches discovery documents and keys, caching them for an hour.
type Client struct {
	HTTP  *http.Client
	Clock func() time.Time

	mu   sync.Mutex
	disc map[string]cached[*Provider]
	keys map[string]cached[map[string]crypto.PublicKey]
}

func (c *Client) now() time.Time {
	if c.Clock != nil {
		return c.Clock()
	}
	return time.Now()
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (c *Client) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	res, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("%s answered %d", u, res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(v)
}

// SecureURL accepts https, and http only for a loopback host (tests, or a
// local identity provider during development).
func SecureURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	h := u.Hostname()
	return u.Scheme == "http" && (h == "localhost" || h == "127.0.0.1" || h == "::1")
}

// Discover reads issuer/.well-known/openid-configuration. The document must
// name the same issuer and every endpoint must be https.
func (c *Client) Discover(ctx context.Context, issuer string) (*Provider, error) {
	issuer = strings.TrimRight(issuer, "/")
	c.mu.Lock()
	if c.disc == nil {
		c.disc = map[string]cached[*Provider]{}
	}
	if d, ok := c.disc[issuer]; ok && c.now().Sub(d.at) < time.Hour {
		c.mu.Unlock()
		return d.v, nil
	}
	c.mu.Unlock()
	var p Provider
	if err := c.getJSON(ctx, issuer+"/.well-known/openid-configuration", &p); err != nil {
		return nil, fmt.Errorf("discovery: %w", err)
	}
	if strings.TrimRight(p.Issuer, "/") != issuer {
		return nil, fmt.Errorf("discovery names issuer %q, not %q", p.Issuer, issuer)
	}
	for _, u := range []string{p.Issuer, p.AuthURL, p.TokenURL, p.JWKSURL} {
		if !SecureURL(u) {
			return nil, fmt.Errorf("endpoint %q is not https", u)
		}
	}
	c.mu.Lock()
	c.disc[issuer] = cached[*Provider]{&p, c.now()}
	c.mu.Unlock()
	return &p, nil
}

// Challenge is the PKCE S256 challenge for a verifier.
func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// AuthCodeURL builds the authorization request with PKCE (S256).
func AuthCodeURL(p *Provider, clientID, redirectURI, state, nonce, verifier string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {"openid email profile"},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {Challenge(verifier)},
		"code_challenge_method": {"S256"},
	}
	sep := "?"
	if strings.Contains(p.AuthURL, "?") {
		sep = "&"
	}
	return p.AuthURL + sep + q.Encode()
}

// Exchange trades the code for tokens and returns the raw ID token.
func (c *Client) Exchange(ctx context.Context, p *Provider, clientID, clientSecret, redirectURI, code, verifier string) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := c.client().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var out struct {
		IDToken string `json:"id_token"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out); err != nil {
		return "", fmt.Errorf("token response: %w", err)
	}
	if res.StatusCode != 200 || out.IDToken == "" {
		return "", fmt.Errorf("token endpoint answered %d %s", res.StatusCode, out.Error)
	}
	return out.IDToken, nil
}

// Claims are the ID token claims Njiwa-Mail uses.
type Claims struct {
	Subject string
	Email   string
	Name    string
}

func (c *Client) keySet(ctx context.Context, jwksURL string, refresh bool) (map[string]crypto.PublicKey, error) {
	c.mu.Lock()
	if c.keys == nil {
		c.keys = map[string]cached[map[string]crypto.PublicKey]{}
	}
	if k, ok := c.keys[jwksURL]; ok && !refresh && c.now().Sub(k.at) < time.Hour {
		c.mu.Unlock()
		return k.v, nil
	}
	c.mu.Unlock()
	var set struct {
		Keys []struct {
			Kty, Kid, Use, Crv, N, E, X, Y string
		} `json:"keys"`
	}
	if err := c.getJSON(ctx, jwksURL, &set); err != nil {
		return nil, fmt.Errorf("keys: %w", err)
	}
	out := map[string]crypto.PublicKey{}
	for _, k := range set.Keys {
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		switch k.Kty {
		case "RSA":
			n, err1 := base64.RawURLEncoding.DecodeString(k.N)
			e, err2 := base64.RawURLEncoding.DecodeString(k.E)
			if err1 != nil || err2 != nil || len(e) > 4 {
				continue
			}
			pub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
			if pub.N.BitLen() < 2048 {
				continue // too weak to trust
			}
			out[k.Kid] = pub
		case "EC":
			if k.Crv != "P-256" {
				continue
			}
			x, err1 := base64.RawURLEncoding.DecodeString(k.X)
			y, err2 := base64.RawURLEncoding.DecodeString(k.Y)
			if err1 != nil || err2 != nil {
				continue
			}
			pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
			if !pub.Curve.IsOnCurve(pub.X, pub.Y) {
				continue
			}
			out[k.Kid] = pub
		}
	}
	c.mu.Lock()
	c.keys[jwksURL] = cached[map[string]crypto.PublicKey]{out, c.now()}
	c.mu.Unlock()
	return out, nil
}

// InvalidTokenError is every ID token failure. Detail says which check
// failed (for the log, never for the browser).
type InvalidTokenError struct{ Detail string }

func (e *InvalidTokenError) Error() string { return "invalid ID token: " + e.Detail }

func bad(detail string) error { return &InvalidTokenError{Detail: detail} }

// Verify checks an ID token's signature (RS256 or ES256, by key id; the key
// set is fetched again once for an unknown key id), issuer, audience,
// authorized party, expiry, issue time, nonce, and a verified email address.
func (c *Client) Verify(ctx context.Context, p *Provider, raw, clientID, nonce string) (*Claims, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, bad("not a JWT")
	}
	var hdr struct{ Alg, Kid string }
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(hb, &hdr) != nil {
		return nil, bad("header")
	}
	if hdr.Alg != "RS256" && hdr.Alg != "ES256" {
		return nil, bad("algorithm " + hdr.Alg + " is not allowed")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, bad("signature encoding")
	}
	keys, err := c.keySet(ctx, p.JWKSURL, false)
	if err != nil {
		return nil, err
	}
	key, ok := keys[hdr.Kid]
	if !ok {
		if keys, err = c.keySet(ctx, p.JWKSURL, true); err != nil {
			return nil, err
		}
		if key, ok = keys[hdr.Kid]; !ok {
			return nil, bad("unknown key id")
		}
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	switch k := key.(type) {
	case *rsa.PublicKey:
		if hdr.Alg != "RS256" || rsa.VerifyPKCS1v15(k, crypto.SHA256, digest[:], sig) != nil {
			return nil, bad("signature")
		}
	case *ecdsa.PublicKey:
		if hdr.Alg != "ES256" || len(sig) != 64 ||
			!ecdsa.Verify(k, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
			return nil, bad("signature")
		}
	default:
		return nil, bad("key type")
	}
	pb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, bad("payload encoding")
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(pb, &m); err != nil {
		return nil, bad("payload")
	}
	str := func(k string) string {
		var v string
		_ = json.Unmarshal(m[k], &v)
		return v
	}
	num := func(k string) (int64, bool) {
		var v float64
		if json.Unmarshal(m[k], &v) != nil {
			return 0, false
		}
		return int64(v), true
	}
	if strings.TrimRight(str("iss"), "/") != strings.TrimRight(p.Issuer, "/") {
		return nil, bad("issuer")
	}
	var auds []string
	if json.Unmarshal(m["aud"], &auds) != nil {
		auds = []string{str("aud")}
	}
	found := false
	for _, a := range auds {
		found = found || a == clientID
	}
	if !found {
		return nil, bad("audience")
	}
	if azp := str("azp"); (len(auds) > 1 || azp != "") && azp != clientID {
		return nil, bad("authorized party")
	}
	now := c.now()
	exp, ok := num("exp")
	if !ok || now.After(time.Unix(exp, 0).Add(time.Minute)) {
		return nil, bad("expired")
	}
	if iat, ok := num("iat"); ok && time.Unix(iat, 0).After(now.Add(5*time.Minute)) {
		return nil, bad("issued in the future")
	}
	if nonce == "" || str("nonce") != nonce {
		return nil, bad("nonce")
	}
	var ev any
	_ = json.Unmarshal(m["email_verified"], &ev)
	cl := &Claims{Subject: str("sub"), Email: strings.ToLower(strings.TrimSpace(str("email"))), Name: str("name")}
	if cl.Email == "" || !(ev == true || ev == "true") {
		return nil, bad("no verified email address")
	}
	if cl.Subject == "" {
		return nil, bad("subject")
	}
	return cl, nil
}

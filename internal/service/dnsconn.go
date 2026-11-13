package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/njiwa-tz/mail/internal/dnsprovider"
	"github.com/njiwa-tz/mail/internal/secure"
)

// DNS provider connections. A connection belongs to exactly one project; every
// query below filters by project id, so another project's connection is
// indistinguishable from one that does not exist. Credentials are sealed with
// the application key, bound to the row, and never returned.

// DNSOAuth is the operator-supplied OAuth 2.0 client for a DNS provider.
// Endpoints come from configuration only, never from a user.
type DNSOAuth struct {
	ClientID, ClientSecret string
	AuthURL, TokenURL      string
	RevokeURL              string // optional
	Scope                  string
}

// Configured reports whether the OAuth flow can be offered.
func (o DNSOAuth) Configured() bool {
	return o.ClientID != "" && o.ClientSecret != "" && o.AuthURL != "" && o.TokenURL != ""
}

// ValidateDNSOAuth checks the operator configuration at start-up: https only.
func ValidateDNSOAuth(o DNSOAuth) error {
	if !o.Configured() {
		return nil
	}
	for _, raw := range []string{o.AuthURL, o.TokenURL, o.RevokeURL} {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return errors.New("Cloudflare OAuth URLs must be https URLs")
		}
	}
	return nil
}

const (
	maxDNSConnections = 10
	oauthStateTTL     = 10 * time.Minute
	oauthCallbackPath = "/admin/v1/dns/oauth/callback"
)

func connAAD(id, field string) []byte { return []byte("dns-connection:" + id + ":" + field) }

// mapDNSErr converts a provider failure to a safe, client-visible error. The
// provider's own message is never passed on. None of these is a 401, which the
// dashboard reads as "your Njiwa-Mail session ended".
func mapDNSErr(err error) error {
	var rl *dnsprovider.RateLimitError
	var tr *dnsprovider.TransientError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, dnsprovider.ErrUnauthorized):
		return &Error{Status: 409, Code: "dns_credential_rejected", Message: "The DNS provider rejected the saved credential: it has expired or been revoked. Reconnect to continue."}
	case errors.Is(err, dnsprovider.ErrForbidden):
		return &Error{Status: 403, Code: "dns_zone_forbidden", Message: "The saved credential is not allowed to use this zone."}
	case errors.Is(err, dnsprovider.ErrNotFound):
		return &Error{Status: 404, Code: "dns_not_found", Message: "The DNS provider could not find that zone or record."}
	case errors.Is(err, dnsprovider.ErrConflict):
		return &Error{Status: 409, Code: "dns_record_conflict", Message: "The DNS provider refused the record because it conflicts with an existing one."}
	case errors.Is(err, dnsprovider.ErrInvalid):
		return &Error{Status: 400, Code: "dns_request_rejected", Message: "The DNS provider rejected the request."}
	case errors.As(err, &rl):
		return &Error{Status: 429, Code: "dns_rate_limited", RetryAfter: rl.RetryAfter, Message: "The DNS provider is rate limiting requests. Try again shortly."}
	case errors.As(err, &tr), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return &Error{Status: 503, Code: "dns_provider_unavailable", RetryAfter: 10 * time.Second, Message: "The DNS provider could not be reached. Nothing was changed that is not listed in the change history."}
	}
	if e, ok := IsError(err); ok {
		return e
	}
	return unavailable("dns_provider", err)
}

// dnsLimit applies a fixed-window limit shared across instances.
func (s *Service) dnsLimit(ctx context.Context, key string, window time.Duration, n int) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("dns_limit", err)
	}
	defer tx.Rollback(ctx)
	wait, ok, err := hit(ctx, tx, key, window, n)
	if err != nil {
		return unavailable("dns_limit", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable("dns_limit", err)
	}
	if !ok {
		return &Error{Status: 429, Code: "rate_limited", RetryAfter: wait, Message: "Too many DNS operations. Try again later."}
	}
	return nil
}

func (s *Service) dnsFactory() dnsprovider.Factory {
	if s.Opts.NewDNSProvider != nil {
		return s.Opts.NewDNSProvider
	}
	return dnsprovider.New
}

type dnsConn struct {
	ID, ProjectID, Provider, Method, Status string
	Token, Refresh                          []byte
	Expires                                 *time.Time
}

func (s *Service) loadConn(ctx context.Context, projectID, id string) (*dnsConn, error) {
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	c := &dnsConn{}
	err := s.Pool.QueryRow(ctx, `SELECT id::text, project_id::text, provider, auth_method, status, token_enc, refresh_enc, expires_at
FROM dns_connections WHERE id=$1 AND project_id=$2`, id, projectID).Scan(&c.ID, &c.ProjectID, &c.Provider, &c.Method, &c.Status, &c.Token, &c.Refresh, &c.Expires)
	if err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

func (s *Service) markConn(ctx context.Context, id, status, detail string) {
	_, _ = s.Pool.Exec(ctx, `UPDATE dns_connections SET status=$2, status_detail=$3 WHERE id=$1 AND status='active'`, id, status, detail)
}

// dnsProviderFor opens a usable provider for a connection of this project.
func (s *Service) dnsProviderFor(ctx context.Context, projectID, connID string) (dnsprovider.Provider, *dnsConn, error) {
	c, err := s.loadConn(ctx, projectID, connID)
	if err != nil {
		return nil, nil, err
	}
	if c.Status != "active" || len(c.Token) == 0 {
		return nil, nil, &Error{Status: 409, Code: "dns_connection_inactive", Message: "This Cloudflare connection is " + c.Status + ". Connect again to continue."}
	}
	if c.Method == "oauth" && c.Expires != nil && time.Until(*c.Expires) < 2*time.Minute {
		if err := s.refreshOAuth(ctx, c); err != nil {
			return nil, nil, err
		}
	}
	tok, err := s.Box.Open(c.Token, connAAD(c.ID, "token"))
	if err != nil {
		return nil, nil, unavailable("dns_open_token", err)
	}
	p, err := s.dnsFactory()(c.Provider, string(tok))
	if err != nil {
		return nil, nil, mapDNSErr(err)
	}
	return p, c, nil
}

// dnsCall maps a provider error and records a rejected credential on the connection.
func (s *Service) dnsCall(ctx context.Context, c *dnsConn, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, dnsprovider.ErrUnauthorized) && c != nil {
		s.markConn(ctx, c.ID, "error", "credential rejected by the provider")
	}
	return mapDNSErr(err)
}

func (s *Service) connView(ctx context.Context, projectID, id string) (map[string]any, error) {
	return s.one(ctx, `SELECT id::text AS id, provider, auth_method, label, status, status_detail, zone_count, created_by, created_at,
  last_verified_at, rotated_at, disconnected_at, expires_at FROM dns_connections WHERE id=$1 AND project_id=$2`, id, projectID)
}

// DNSConnections lists this project's connections (never credentials).
func (s *Service) DNSConnections(ctx context.Context, sc *Scope) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT id::text AS id, provider, auth_method, label, status, status_detail, zone_count, created_by, created_at,
  last_verified_at, rotated_at, disconnected_at, expires_at FROM dns_connections WHERE project_id=$1 AND status <> 'disconnected'
ORDER BY created_at DESC`, sc.ProjectID)
}

// DNSCapabilities says which connection methods this installation offers.
func (s *Service) DNSCapabilities() map[string]any {
	return map[string]any{
		"providers": []string{"cloudflare"},
		"cloudflare": map[string]any{
			"token":          true,
			"oauth":          s.Opts.DNSOAuth.Configured() && s.Opts.PublicURL != "",
			"domain_connect": false, // needs a registered template; see docs/DNS_SETUP.md
		},
		"token_help": "Create a Cloudflare API token with Zone → DNS → Edit and Zone → Zone → Read, restricted to the one zone you will use. Never paste a Global API Key or your password.",
	}
}

// ConnectDNSToken saves a scoped API token after proving it works.
func (s *Service) ConnectDNSToken(ctx context.Context, sc *Scope, a Actor, provider, token, label string) (map[string]any, error) {
	label = strings.TrimSpace(label)
	if len(label) > 80 {
		return nil, badRequest("label is too long")
	}
	if strings.TrimSpace(token) == "" || len(token) > 2048 {
		return nil, badRequest("an API token is required")
	}
	if err := s.dnsLimit(ctx, "dns:connect:"+a.UserID, time.Hour, 20); err != nil {
		return nil, err
	}
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM dns_connections WHERE project_id=$1 AND status='active'`, sc.ProjectID).Scan(&n); err != nil {
		return nil, unavailable("dns_connections", err)
	}
	if n >= maxDNSConnections {
		return nil, &Error{Status: 409, Code: "too_many_connections", Message: "This project has too many active DNS connections. Disconnect one first."}
	}
	p, err := s.dnsFactory()(provider, token)
	if err != nil {
		return nil, mapDNSErr(err)
	}
	return s.saveConnection(ctx, sc, a, p, provider, "token", label, token, "", nil)
}

func (s *Service) saveConnection(ctx context.Context, sc *Scope, a Actor, p dnsprovider.Provider, provider, method, label, access, refresh string, exp *time.Time) (map[string]any, error) {
	expires, err := p.Verify(ctx)
	if err != nil {
		return nil, mapDNSErr(err)
	}
	if exp == nil && !expires.IsZero() {
		exp = &expires
	}
	zones, err := p.Zones(ctx)
	if err != nil {
		return nil, mapDNSErr(err)
	}
	id := secure.NewUUID()
	tokEnc, err := s.Box.Seal([]byte(access), connAAD(id, "token"))
	if err != nil {
		return nil, unavailable("dns_seal", err)
	}
	var refEnc []byte
	if refresh != "" {
		if refEnc, err = s.Box.Seal([]byte(refresh), connAAD(id, "refresh")); err != nil {
			return nil, unavailable("dns_seal", err)
		}
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, unavailable("dns_connect", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO dns_connections(id, project_id, provider, auth_method, label, token_enc, refresh_enc, expires_at, zone_count, created_by, last_verified_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10, now())`, id, sc.ProjectID, provider, method, label, tokEnc, refEnc, exp, len(zones), a.Name); err != nil {
		return nil, unavailable("dns_connect", err)
	}
	if err := audit(ctx, tx, a, "dns.connect", "dns_connection", id, map[string]any{"project": sc.Project, "provider": provider, "method": method, "zones": len(zones)}); err != nil {
		return nil, unavailable("dns_connect", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, unavailable("dns_connect", err)
	}
	v, err := s.connView(ctx, sc.ProjectID, id)
	if err != nil {
		return nil, err
	}
	if len(zones) > 1 {
		v["warning"] = "This credential can reach several zones. For least privilege, create a token restricted to the one zone you want Njiwa-Mail to manage."
	}
	return v, nil
}

// RotateDNSToken replaces the stored token after proving the new one works.
func (s *Service) RotateDNSToken(ctx context.Context, sc *Scope, a Actor, id, token string) (map[string]any, error) {
	c, err := s.loadConn(ctx, sc.ProjectID, id)
	if err != nil {
		return nil, err
	}
	if c.Method != "token" {
		return nil, badRequest("only API-token connections can be rotated by pasting a token; reconnect with OAuth instead")
	}
	if c.Status == "disconnected" {
		return nil, newErr(404, "not_found")
	}
	if err := s.dnsLimit(ctx, "dns:connect:"+a.UserID, time.Hour, 20); err != nil {
		return nil, err
	}
	p, err := s.dnsFactory()(c.Provider, token)
	if err != nil {
		return nil, mapDNSErr(err)
	}
	exp, err := p.Verify(ctx)
	if err != nil {
		return nil, mapDNSErr(err)
	}
	zones, err := p.Zones(ctx)
	if err != nil {
		return nil, mapDNSErr(err)
	}
	enc, err := s.Box.Seal([]byte(token), connAAD(c.ID, "token"))
	if err != nil {
		return nil, unavailable("dns_seal", err)
	}
	var e *time.Time
	if !exp.IsZero() {
		e = &exp
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, unavailable("dns_rotate", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE dns_connections SET token_enc=$3, expires_at=$4, status='active', status_detail='', zone_count=$5, rotated_at=now(), last_verified_at=now()
WHERE id=$1 AND project_id=$2`, c.ID, sc.ProjectID, enc, e, len(zones)); err != nil {
		return nil, unavailable("dns_rotate", err)
	}
	if err := audit(ctx, tx, a, "dns.rotate", "dns_connection", c.ID, map[string]any{"project": sc.Project}); err != nil {
		return nil, unavailable("dns_rotate", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, unavailable("dns_rotate", err)
	}
	return s.connView(ctx, sc.ProjectID, c.ID)
}

// DisconnectDNS wipes the credential. Records already published stay, and the
// change history stays so they can be reviewed or removed by hand. Nothing is
// deleted from DNS.
func (s *Service) DisconnectDNS(ctx context.Context, sc *Scope, a Actor, id string) error {
	c, err := s.loadConn(ctx, sc.ProjectID, id)
	if err != nil {
		return err
	}
	revoked := false
	if c.Method == "oauth" && s.Opts.DNSOAuth.RevokeURL != "" && len(c.Token) > 0 {
		if tok, err := s.Box.Open(c.Token, connAAD(c.ID, "token")); err == nil {
			revoked = s.revokeOAuth(ctx, string(tok)) == nil
		}
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("dns_disconnect", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE dns_connections SET token_enc=NULL, refresh_enc=NULL, status='disconnected', status_detail='', disconnected_at=now()
WHERE id=$1 AND project_id=$2`, c.ID, sc.ProjectID); err != nil {
		return unavailable("dns_disconnect", err)
	}
	if err := audit(ctx, tx, a, "dns.disconnect", "dns_connection", c.ID, map[string]any{"project": sc.Project, "revoked_at_provider": revoked}); err != nil {
		return unavailable("dns_disconnect", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable("dns_disconnect", err)
	}
	return nil
}

// DNSZones lists the zones the connection can see.
func (s *Service) DNSZones(ctx context.Context, sc *Scope, connID string) ([]map[string]any, error) {
	p, c, err := s.dnsProviderFor(ctx, sc.ProjectID, connID)
	if err != nil {
		return nil, err
	}
	zs, err := p.Zones(ctx)
	if err != nil {
		return nil, s.dnsCall(ctx, c, err)
	}
	out := make([]map[string]any, 0, len(zs))
	for _, z := range zs {
		out = append(out, map[string]any{"id": z.ID, "name": z.Name, "status": z.Status})
	}
	return out, nil
}

// ---- OAuth 2.0 authorization code flow with PKCE

func randToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Service) oauthRedirectURI() string {
	return strings.TrimRight(s.Opts.PublicURL, "/") + oauthCallbackPath
}

// StartDNSOAuth creates single-use state and a PKCE challenge and returns the
// provider's authorization URL.
func (s *Service) StartDNSOAuth(ctx context.Context, sc *Scope, a Actor, domainID string) (string, error) {
	o := s.Opts.DNSOAuth
	if !o.Configured() || s.Opts.PublicURL == "" {
		return "", &Error{Status: 409, Code: "oauth_not_configured", Message: "Cloudflare sign-in is not set up on this installation. Use an API token instead."}
	}
	if err := s.dnsLimit(ctx, "dns:oauth:"+a.UserID, time.Hour, 20); err != nil {
		return "", err
	}
	var dom any
	if domainID != "" {
		var ok bool
		if !uuidRe.MatchString(domainID) {
			return "", newErr(404, "not_found")
		}
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM domains WHERE id=$1 AND project_id=$2)`, domainID, sc.ProjectID).Scan(&ok); err != nil || !ok {
			return "", newErr(404, "not_found")
		}
		dom = domainID
	}
	state, err := randToken(32)
	if err != nil {
		return "", unavailable("dns_oauth", err)
	}
	verifier, err := randToken(48)
	if err != nil {
		return "", unavailable("dns_oauth", err)
	}
	sum := sha256.Sum256([]byte(state))
	venc, err := s.Box.Seal([]byte(verifier), []byte("dns-oauth:"+hex.EncodeToString(sum[:])))
	if err != nil {
		return "", unavailable("dns_oauth", err)
	}
	redirect := s.oauthRedirectURI()
	if _, err := s.Pool.Exec(ctx, `INSERT INTO dns_oauth_states(state_hash, project_id, user_id, verifier_enc, redirect_uri, expires_at, domain_id) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		sum[:], sc.ProjectID, a.UserID, venc, redirect, time.Now().Add(oauthStateTTL), dom); err != nil {
		return "", unavailable("dns_oauth", err)
	}
	ch := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {o.ClientID},
		"redirect_uri":          {redirect},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(ch[:])},
		"code_challenge_method": {"S256"},
	}
	if o.Scope != "" {
		q.Set("scope", o.Scope)
	}
	sep := "?"
	if strings.Contains(o.AuthURL, "?") {
		sep = "&"
	}
	return o.AuthURL + sep + q.Encode(), nil
}

// oauthClient never follows redirects and, unless a test allows it, refuses to
// connect to loopback, private or link-local addresses.
func (s *Service) oauthClient() *http.Client {
	d := &net.Dialer{Timeout: 10 * time.Second, Control: func(network, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if !s.Opts.DNSAllowPrivate && !PublicIP(net.ParseIP(host)) {
			return errors.New("destination address not allowed")
		}
		return nil
	}}
	return &http.Client{
		Timeout:       20 * time.Second,
		Transport:     &http.Transport{DialContext: d.DialContext, Proxy: nil, ResponseHeaderTimeout: 20 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type oauthToken struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	Type    string `json:"token_type"`
	Expires int    `json:"expires_in"`
}

func (s *Service) oauthPost(ctx context.Context, endpoint string, form url.Values) (*oauthToken, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := s.oauthClient().Do(req)
	if err != nil {
		return nil, &dnsprovider.TransientError{Err: errors.New("token endpoint unreachable")}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		if resp.StatusCode >= 500 {
			return nil, &dnsprovider.TransientError{Err: errors.New("token endpoint error")}
		}
		return nil, dnsprovider.ErrUnauthorized
	}
	var t oauthToken
	if json.Unmarshal(body, &t) != nil || t.Access == "" || (t.Type != "" && !strings.EqualFold(t.Type, "bearer")) {
		return nil, dnsprovider.ErrUnauthorized
	}
	return &t, nil
}

func (s *Service) oauthForm(extra url.Values) url.Values {
	o := s.Opts.DNSOAuth
	f := url.Values{"client_id": {o.ClientID}, "client_secret": {o.ClientSecret}}
	for k, v := range extra {
		f[k] = v
	}
	return f
}

// CompleteDNSOAuth finishes the flow. The state is consumed atomically (so a
// replay fails), must belong to this user, and must be unexpired. It returns
// the project name so the caller can send the browser back to the right page.
func (s *Service) CompleteDNSOAuth(ctx context.Context, a Actor, code, state string) (project, domainID string, conn map[string]any, err error) {
	if code == "" || state == "" || len(state) > 256 || len(code) > 2048 {
		return "", "", nil, badRequest("the sign-in response is incomplete")
	}
	sum := sha256.Sum256([]byte(state))
	var projectID, userID, redirect string
	var dom *string
	var venc []byte
	err = s.Pool.QueryRow(ctx, `UPDATE dns_oauth_states SET used_at=now() WHERE state_hash=$1 AND used_at IS NULL AND expires_at > now()
RETURNING project_id::text, user_id::text, verifier_enc, redirect_uri, domain_id::text`, sum[:]).Scan(&projectID, &userID, &venc, &redirect, &dom)
	if err != nil {
		return "", "", nil, &Error{Status: 400, Code: "oauth_state_invalid", Message: "This sign-in link is invalid, expired or was already used. Start again."}
	}
	if userID != a.UserID {
		return "", "", nil, &Error{Status: 403, Code: "oauth_state_invalid", Message: "This sign-in was started by someone else."}
	}
	verifier, err := s.Box.Open(venc, []byte("dns-oauth:"+hex.EncodeToString(sum[:])))
	if err != nil {
		return "", "", nil, unavailable("dns_oauth", err)
	}
	sc := &Scope{ProjectID: projectID}
	if err := s.Pool.QueryRow(ctx, `SELECT name FROM projects WHERE id=$1`, projectID).Scan(&sc.Project); err != nil {
		return "", "", nil, notFound(err)
	}
	tok, err := s.oauthPost(ctx, s.Opts.DNSOAuth.TokenURL, s.oauthForm(url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}, "code_verifier": {string(verifier)}}))
	if err != nil {
		return "", "", nil, mapDNSErr(err)
	}
	p, err := s.dnsFactory()("cloudflare", tok.Access)
	if err != nil {
		return "", "", nil, mapDNSErr(err)
	}
	var exp *time.Time
	if tok.Expires > 0 {
		t := time.Now().Add(time.Duration(tok.Expires) * time.Second)
		exp = &t
	}
	v, err := s.saveConnection(ctx, sc, a, p, "cloudflare", "oauth", "", tok.Access, tok.Refresh, exp)
	d := ""
	if dom != nil {
		d = *dom
	}
	return sc.Project, d, v, err
}

func (s *Service) refreshOAuth(ctx context.Context, c *dnsConn) error {
	if len(c.Refresh) == 0 {
		s.markConn(ctx, c.ID, "expired", "the sign-in expired")
		return &Error{Status: 409, Code: "dns_credential_rejected", Message: "The Cloudflare sign-in expired. Connect again to continue."}
	}
	rt, err := s.Box.Open(c.Refresh, connAAD(c.ID, "refresh"))
	if err != nil {
		return unavailable("dns_open_token", err)
	}
	tok, err := s.oauthPost(ctx, s.Opts.DNSOAuth.TokenURL, s.oauthForm(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {string(rt)}}))
	if err != nil {
		if errors.Is(err, dnsprovider.ErrUnauthorized) {
			s.markConn(ctx, c.ID, "expired", "the sign-in could not be renewed")
		}
		return mapDNSErr(err)
	}
	access, err := s.Box.Seal([]byte(tok.Access), connAAD(c.ID, "token"))
	if err != nil {
		return unavailable("dns_seal", err)
	}
	refresh := c.Refresh
	if tok.Refresh != "" {
		if refresh, err = s.Box.Seal([]byte(tok.Refresh), connAAD(c.ID, "refresh")); err != nil {
			return unavailable("dns_seal", err)
		}
	}
	var exp *time.Time
	if tok.Expires > 0 {
		t := time.Now().Add(time.Duration(tok.Expires) * time.Second)
		exp = &t
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE dns_connections SET token_enc=$2, refresh_enc=$3, expires_at=$4, rotated_at=now() WHERE id=$1 AND status='active'`, c.ID, access, refresh, exp); err != nil {
		return unavailable("dns_refresh", err)
	}
	c.Token, c.Refresh, c.Expires = access, refresh, exp
	return nil
}

func (s *Service) revokeOAuth(ctx context.Context, token string) error {
	_, err := s.oauthPost(ctx, s.Opts.DNSOAuth.RevokeURL, s.oauthForm(url.Values{"token": {token}}))
	if errors.Is(err, dnsprovider.ErrUnauthorized) {
		return nil // an unreadable reply to a revoke still means we stop using it
	}
	return err
}

// PurgeDNSOAuthStates removes expired or used OAuth state rows.
func (s *Service) PurgeDNSOAuthStates(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM dns_oauth_states WHERE expires_at < now() - interval '1 hour'`)
	return err
}

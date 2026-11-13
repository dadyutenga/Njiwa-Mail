package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CloudflareAPI is the only host this adapter talks to in production.
const CloudflareAPI = "https://api.cloudflare.com/client/v4"

const (
	maxResponse = 4 << 20
	pageSize    = 100
	maxPages    = 50 // 5,000 records per zone is the most we read
)

// Cloudflare is the adapter for the Cloudflare v4 API, bound to one API token
// (a scoped token, or an OAuth access token: both are bearer credentials).
type Cloudflare struct {
	Token   string
	BaseURL string       // empty = CloudflareAPI. Tests point it at httptest.
	HTTP    *http.Client // nil = a client with a 20s timeout that never follows redirects
}

var _ Provider = (*Cloudflare)(nil)

var globalKeyRe = regexp.MustCompile(`^[0-9a-f]{37}$`)

// NewCloudflare validates the credential's shape. A Global API Key (37 hex
// characters) is refused: only scoped tokens are accepted.
func NewCloudflare(token string) (*Cloudflare, error) {
	token = strings.TrimSpace(token)
	switch {
	case token == "":
		return nil, fmt.Errorf("%w: empty token", ErrInvalid)
	case globalKeyRe.MatchString(token):
		return nil, fmt.Errorf("%w: this looks like a Global API Key; create a scoped API token instead", ErrInvalid)
	case len(token) < 20 || len(token) > 2048 || strings.ContainsAny(token, " \t\r\n\"'"):
		return nil, fmt.Errorf("%w: not a valid API token", ErrInvalid)
	}
	return &Cloudflare{Token: token}, nil
}

// New builds the provider named by name for a stored credential.
func New(name, token string) (Provider, error) {
	switch name {
	case "cloudflare":
		return NewCloudflare(token)
	}
	return nil, fmt.Errorf("%w: unknown DNS provider", ErrInvalid)
}

func (c *Cloudflare) Name() string { return "cloudflare" }

func (c *Cloudflare) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{
		Timeout: 20 * time.Second,
		// A redirect could carry the bearer token to another host.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type cfEnvelope struct {
	Success    bool            `json:"success"`
	Errors     []cfError       `json:"errors"`
	Result     json.RawMessage `json:"result"`
	ResultInfo struct {
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cfRecord struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Priority *int   `json:"priority,omitempty"`
	Proxied  *bool  `json:"proxied,omitempty"`
	Comment  string `json:"comment,omitempty"`
	Modified string `json:"modified_on,omitempty"`
}

func (c *Cloudflare) base() (string, error) {
	b := c.BaseURL
	if b == "" {
		b = CloudflareAPI
	}
	u, err := url.Parse(b)
	if err != nil || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname()))) {
		return "", fmt.Errorf("%w: provider URL must be https", ErrInvalid)
	}
	return strings.TrimRight(b, "/"), nil
}

// isLoopbackHost lets tests use http://127.0.0.1; production never sets BaseURL.
func isLoopbackHost(h string) bool {
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

var zoneIDRe = regexp.MustCompile(`^[0-9a-zA-Z]{1,64}$`)

func checkID(s string) error {
	if !zoneIDRe.MatchString(s) {
		return fmt.Errorf("%w: bad identifier", ErrInvalid)
	}
	return nil
}

// do performs one request and maps every failure to the typed errors. It never
// returns the response body or the token inside an error.
func (c *Cloudflare) do(ctx context.Context, method, path string, q url.Values, body, out any) (cfEnvelope, error) {
	var env cfEnvelope
	base, err := c.base()
	if err != nil {
		return env, err
	}
	u := base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return env, fmt.Errorf("%w: encode", ErrInvalid)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return env, fmt.Errorf("%w: request", ErrInvalid)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return env, ctx.Err()
		}
		// Do not include err: a *url.Error carries the URL.
		return env, &TransientError{Err: errors.New("request failed")}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	_ = json.Unmarshal(raw, &env)
	switch {
	case resp.StatusCode == 401:
		return env, ErrUnauthorized
	case resp.StatusCode == 403:
		return env, ErrForbidden
	case resp.StatusCode == 404:
		return env, ErrNotFound
	case resp.StatusCode == 409:
		return env, ErrConflict
	case resp.StatusCode == 429:
		ra, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return env, &RateLimitError{RetryAfter: time.Duration(ra) * time.Second}
	case resp.StatusCode >= 500:
		return env, &TransientError{Err: fmt.Errorf("status %d", resp.StatusCode)}
	case resp.StatusCode >= 300: // includes 3xx (redirects are not followed) and 4xx
		for _, e := range env.Errors {
			if e.Code == 81057 || e.Code == 81058 || e.Code == 81053 { // record already exists / identical
				return env, ErrConflict
			}
		}
		return env, ErrInvalid
	}
	if !env.Success {
		for _, e := range env.Errors {
			if e.Code == 81057 || e.Code == 81058 {
				return env, ErrConflict
			}
		}
		return env, ErrInvalid
	}
	if out != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return env, fmt.Errorf("%w: unreadable response", ErrInvalid)
		}
	}
	return env, nil
}

func (c *Cloudflare) Verify(ctx context.Context) (time.Time, error) {
	var res struct {
		Status    string `json:"status"`
		ExpiresOn string `json:"expires_on"`
		NotBefore string `json:"not_before"`
	}
	if _, err := c.do(ctx, "GET", "/user/tokens/verify", nil, nil, &res); err != nil {
		// Account-owned tokens are not known to the user endpoint. Listing one
		// zone is the same proof for them: an invalid token is refused there too.
		var tr *TransientError
		var rl *RateLimitError
		if errors.As(err, &tr) || errors.As(err, &rl) || ctx.Err() != nil {
			return time.Time{}, err
		}
		var probe []cfZone
		if _, perr := c.do(ctx, "GET", "/zones", url.Values{"per_page": {"1"}}, nil, &probe); perr != nil {
			return time.Time{}, err
		}
		return time.Time{}, nil
	}
	var exp time.Time
	if res.ExpiresOn != "" {
		exp, _ = time.Parse(time.RFC3339, res.ExpiresOn)
	}
	switch res.Status {
	case "active":
		return exp, nil
	case "expired":
		return exp, ErrUnauthorized
	}
	return exp, ErrUnauthorized // disabled and anything unknown
}

type cfZone struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	NameServers []string `json:"name_servers"`
}

func (z cfZone) zone() Zone {
	ns := make([]string, 0, len(z.NameServers))
	for _, n := range z.NameServers {
		ns = append(ns, strings.ToLower(strings.TrimSuffix(n, ".")))
	}
	return Zone{ID: z.ID, Name: strings.ToLower(strings.TrimSuffix(z.Name, ".")), Status: z.Status, NameServers: ns}
}

func (c *Cloudflare) Zones(ctx context.Context) ([]Zone, error) {
	var out []Zone
	for page := 1; page <= maxPages; page++ {
		var zs []cfZone
		env, err := c.do(ctx, "GET", "/zones", url.Values{"per_page": {"50"}, "page": {strconv.Itoa(page)}}, nil, &zs)
		if err != nil {
			return nil, err
		}
		for _, z := range zs {
			out = append(out, z.zone())
		}
		if page >= env.ResultInfo.TotalPages || len(zs) == 0 {
			break
		}
	}
	return out, nil
}

func (c *Cloudflare) Zone(ctx context.Context, id string) (Zone, error) {
	if err := checkID(id); err != nil {
		return Zone{}, err
	}
	var z cfZone
	if _, err := c.do(ctx, "GET", "/zones/"+id, nil, nil, &z); err != nil {
		return Zone{}, err
	}
	return z.zone(), nil
}

// normTXT removes the quoting Cloudflare puts around TXT content and joins
// the 255-byte chunks of a long value: `"abc" "def"` becomes `abcdef`.
func normTXT(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, `"`) {
		return s
	}
	var b strings.Builder
	rest := s
	for strings.HasPrefix(rest, `"`) {
		end := -1
		for i := 1; i < len(rest); i++ {
			if rest[i] == '\\' {
				i++
				continue
			}
			if rest[i] == '"' {
				end = i
				break
			}
		}
		if end < 0 {
			return s
		}
		b.WriteString(strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(rest[1:end]))
		rest = strings.TrimLeft(rest[end+1:], " ")
	}
	if rest != "" {
		return s
	}
	return b.String()
}

func (r cfRecord) record() Record {
	out := Record{ID: r.ID, Type: strings.ToUpper(r.Type), Name: strings.ToLower(strings.TrimSuffix(r.Name, ".")), Content: r.Content,
		TTL: r.TTL, Comment: r.Comment, Modified: r.Modified}
	if out.Type == "TXT" {
		out.Content = normTXT(r.Content)
	}
	if out.Type == "MX" || out.Type == "CNAME" {
		out.Content = strings.ToLower(strings.TrimSuffix(out.Content, "."))
	}
	if r.Priority != nil {
		out.Priority = *r.Priority
	}
	if r.Proxied != nil {
		out.Proxied = *r.Proxied
	}
	return out
}

func toCF(r Record) cfRecord {
	c := cfRecord{Type: r.Type, Name: r.Name, Content: r.Content, TTL: r.TTL, Comment: r.Comment}
	if c.TTL == 0 {
		c.TTL = 1
	}
	if r.Type == "MX" {
		p := r.Priority
		c.Priority = &p
	}
	// Mail names are never proxied (a proxy cannot carry SMTP); TXT and MX
	// cannot be, so say so explicitly.
	if r.Type == "MX" || r.Type == "TXT" {
		f := false
		c.Proxied = &f
	}
	return c
}

func (c *Cloudflare) Records(ctx context.Context, zoneID string) ([]Record, error) {
	if err := checkID(zoneID); err != nil {
		return nil, err
	}
	var out []Record
	for page := 1; page <= maxPages; page++ {
		var rs []cfRecord
		env, err := c.do(ctx, "GET", "/zones/"+zoneID+"/dns_records", url.Values{"per_page": {strconv.Itoa(pageSize)}, "page": {strconv.Itoa(page)}}, nil, &rs)
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			out = append(out, r.record())
		}
		if page >= env.ResultInfo.TotalPages || len(rs) == 0 {
			return out, nil
		}
	}
	return nil, fmt.Errorf("%w: zone has too many records to analyse safely", ErrInvalid)
}

func (c *Cloudflare) DNSSEC(ctx context.Context, zoneID string) (string, error) {
	if err := checkID(zoneID); err != nil {
		return "", err
	}
	var res struct {
		Status string `json:"status"`
	}
	if _, err := c.do(ctx, "GET", "/zones/"+zoneID+"/dnssec", nil, nil, &res); err != nil {
		return "", err
	}
	return res.Status, nil
}

func (c *Cloudflare) Create(ctx context.Context, zoneID string, r Record) (Record, error) {
	if err := checkID(zoneID); err != nil {
		return Record{}, err
	}
	var out cfRecord
	if _, err := c.do(ctx, "POST", "/zones/"+zoneID+"/dns_records", nil, toCF(r), &out); err != nil {
		return Record{}, err
	}
	return out.record(), nil
}

func (c *Cloudflare) Update(ctx context.Context, zoneID, recordID string, r Record) (Record, error) {
	if err := checkID(zoneID); err != nil {
		return Record{}, err
	}
	if err := checkID(recordID); err != nil {
		return Record{}, err
	}
	var out cfRecord
	if _, err := c.do(ctx, "PATCH", "/zones/"+zoneID+"/dns_records/"+recordID, nil, toCF(r), &out); err != nil {
		return Record{}, err
	}
	return out.record(), nil
}

func (c *Cloudflare) Delete(ctx context.Context, zoneID, recordID string) error {
	if err := checkID(zoneID); err != nil {
		return err
	}
	if err := checkID(recordID); err != nil {
		return err
	}
	_, err := c.do(ctx, "DELETE", "/zones/"+zoneID+"/dns_records/"+recordID, nil, nil, nil)
	return err
}

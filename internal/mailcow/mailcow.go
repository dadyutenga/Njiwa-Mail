// Package mailcow is a small client for the Mailcow API (/api/v1). Njiwa-Mail
// uses it only for per-domain actions: add a domain, create and read its DKIM
// key, and add the relay transport for inbound mail. It never changes global
// Mailcow settings and never touches Mailcow's database.
//
// The endpoints follow the API documentation served by a Mailcow instance at
// https://<mailcow-host>/api. Check them against the installed version before
// enabling the integration (docs/MAILCOW_SETUP.md, "Mailcow API").
package mailcow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	Key     string
	HTTP    *http.Client
}

func New(baseURL, key string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Key: key, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// result is one entry of Mailcow's write response: [{"type":"success","msg":[...]}].
type result struct {
	Type string `json:"type"`
	Msg  any    `json:"msg"`
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.Key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach the Mailcow API")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return errors.New("the Mailcow API refused the key (check the key and its allowed IPs)")
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("the Mailcow API answered HTTP %d", resp.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	var res []result
	if json.Unmarshal(raw, &res) == nil {
		for _, r := range res {
			if r.Type != "success" {
				return fmt.Errorf("Mailcow: %v", r.Msg)
			}
		}
	}
	return nil
}

// Ping checks the URL and key with a read-only call.
func (c *Client) Ping(ctx context.Context) error {
	var v any
	return c.do(ctx, http.MethodGet, "/api/v1/get/status/version", nil, &v)
}

// AddDomain adds a domain. relayAll makes Mailcow relay every recipient of
// the domain (used for an inbound subdomain whose mail goes to Njiwa-Mail).
func (c *Client) AddDomain(ctx context.Context, domain string, relayAll bool) error {
	relay := "0"
	if relayAll {
		relay = "1"
	}
	return c.do(ctx, http.MethodPost, "/api/v1/add/domain", map[string]any{
		"domain": domain, "description": "Njiwa-Mail", "active": "1", "aliases": "100", "mailboxes": "10",
		"defquota": "1024", "maxquota": "2048", "quota": "10240", "relay_all_recipients": relay, "relay_unknown_only": "0",
		"restart_sogo": "0",
	}, nil)
}

// AddDKIM generates a 2048-bit DKIM key for the domain with the selector.
func (c *Client) AddDKIM(ctx context.Context, domain, selector string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/add/dkim", map[string]any{
		"domains": domain, "dkim_selector": selector, "key_size": "2048",
	}, nil)
}

// GetDKIM returns the DNS TXT value of the domain's DKIM key, or "" when none exists.
func (c *Client) GetDKIM(ctx context.Context, domain string) (string, error) {
	var out map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/get/dkim/"+url.PathEscape(domain), nil, &out); err != nil {
		var arr []any // Mailcow answers [] when the domain has no key
		if c.do(ctx, http.MethodGet, "/api/v1/get/dkim/"+url.PathEscape(domain), nil, &arr) == nil {
			return "", nil
		}
		return "", err
	}
	txt, _ := out["dkim_txt"].(string)
	return txt, nil
}

// AddTransport sends mail for destination to nexthop (e.g. "[njiwa-inbound]:2525").
func (c *Client) AddTransport(ctx context.Context, destination, nexthop string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/add/transport", map[string]any{
		"destination": destination, "nexthop": nexthop, "active": "1",
	}, nil)
}

// GetDomain reads one domain. ok is false when Mailcow does not know it
// (it answers an empty object or list).
func (c *Client) GetDomain(ctx context.Context, domain string) (map[string]any, bool, error) {
	var out map[string]any
	path := "/api/v1/get/domain/" + url.PathEscape(domain)
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		var arr []any // an unknown domain is answered with []
		if c.do(ctx, http.MethodGet, path, nil, &arr) == nil {
			return nil, false, nil
		}
		return nil, false, err
	}
	if len(out) == 0 {
		return nil, false, nil
	}
	return out, true, nil
}

// GetTransport finds the transport map entry for destination.
func (c *Client) GetTransport(ctx context.Context, destination string) (map[string]any, bool, error) {
	var all []map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/get/transport/all", nil, &all); err != nil {
		return nil, false, err
	}
	for _, t := range all {
		if d, _ := t["destination"].(string); strings.EqualFold(d, destination) {
			return t, true, nil
		}
	}
	return nil, false, nil
}

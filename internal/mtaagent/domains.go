package mtaagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/njiwa-tz/mail/internal/installer/managed"
)

type DomainManager interface {
	Plan([]string) (*managed.Plan, error)
	Apply([]string, string) (*managed.DomainResult, error)
}

type domainRequest struct {
	Domains []string `json:"domains"`
	Digest  string   `json:"digest"`
}

func (a *Agent) domains(w http.ResponseWriter, r *http.Request) {
	if a.Domains == nil {
		writeJSON(w, 503, map[string]string{"error": "managed_domains_unavailable"})
		return
	}
	var in domainRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil || len(in.Domains) == 0 || len(in.Domains) > 1000 {
		writeJSON(w, 400, map[string]string{"error": "invalid_domains"})
		return
	}
	if dec.Decode(new(any)) != io.EOF {
		writeJSON(w, 400, map[string]string{"error": "invalid_domains"})
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var result any
	var err error
	if r.URL.Path == "/domains/plan" {
		result, err = a.Domains.Plan(in.Domains)
	} else {
		result, err = a.Domains.Apply(in.Domains, in.Digest)
	}
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": "managed_domain_plan_changed_or_blocked"})
		return
	}
	writeJSON(w, 200, result)
}

func (c *Client) domainCall(ctx context.Context, path string, in domainRequest, out any) error {
	b, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, "POST", "http://agent/domains/"+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var e struct{ Error string }
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&e)
		if e.Error == "" {
			e.Error = "managed_domains_unavailable_or_changed"
		}
		return &AgentError{Status: resp.StatusCode, Code: e.Error}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}
func (c *Client) PlanDomains(ctx context.Context, domains []string) (*managed.Plan, error) {
	var out managed.Plan
	err := c.domainCall(ctx, "plan", domainRequest{Domains: domains}, &out)
	return &out, err
}
func (c *Client) ApplyDomains(ctx context.Context, domains []string, digest string) (*managed.DomainResult, error) {
	var out managed.DomainResult
	err := c.domainCall(ctx, "apply", domainRequest{Domains: domains, Digest: digest}, &out)
	return &out, err
}

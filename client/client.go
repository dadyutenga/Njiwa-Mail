// Package client is a small, dependency-free Go client for the Njiwa Mail
// API. Any backend can use it (or copy it): it only needs net/http.
//
// Retry rules follow the service contract: network errors, timeouts, 429 and
// 5xx are retried with the SAME idempotency key (so no duplicate message can be
// created); other 4xx responses are returned immediately.
package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string        // e.g. https://mail.njiwa.co.tz
	APIKey     string        // project+environment key; keep server-side only
	HTTP       *http.Client  // optional; defaults to a 10s-timeout client
	MaxRetries int           // retries after the first attempt (default 2)
	Backoff    time.Duration // base backoff (default 300ms, doubled each retry)
}

type Email struct {
	From            string
	To              string
	Template        string
	TemplateVersion int    // 0 = newest published version (pinned by the service at accept time)
	Locale          string // "en" (default) or "sw"
	Variables       map[string]any
	TTLSeconds      int    // 0 = template default
	RequestID       string // optional correlation id (metadata.request_id)
}

type Accepted struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type Event struct {
	Type   string    `json:"type"`
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at"`
}

type Status struct {
	ID        string            `json:"id"`
	Status    string            `json:"status"`
	Attempts  int               `json:"attempts"`
	SMTPReply string            `json:"smtp_reply,omitempty"`
	Metadata  map[string]string `json:"metadata"`
	Events    []Event           `json:"events"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// APIError is a non-success response. Code is a stable reason code.
type APIError struct {
	Status     int
	Code       string
	RetryAfter time.Duration
}

func (e *APIError) Error() string { return fmt.Sprintf("njiwa-mail: %d %s", e.Status, e.Code) }

// Retryable reports whether the same request may be safely re-sent with the
// same idempotency key.
func (e *APIError) Retryable() bool { return e.Status == 429 || e.Status >= 500 }

// NewIdempotencyKey returns a random key. Generate ONE per logical event (e.g.
// per OTP issued) and reuse it for every retry of that event.
func NewIdempotencyKey(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + "-" + hex.EncodeToString(b)
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (c *Client) do(ctx context.Context, method, path, idem string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return b, resp.StatusCode, nil
	}
	ae := &APIError{Status: resp.StatusCode}
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil {
		ae.Code = e.Error.Code
	}
	if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
		ae.RetryAfter = time.Duration(s) * time.Second
	}
	return nil, resp.StatusCode, ae
}

// Send queues one email. The same idempotencyKey is used on every attempt.
// A returned error that is not an *APIError (or a Retryable one) means the
// outcome is uncertain: do NOT fall back to another provider; retry later with
// the same key.
func (c *Client) Send(ctx context.Context, idempotencyKey string, e Email) (*Accepted, error) {
	payload := map[string]any{
		"from": e.From, "to": e.To, "template": e.Template, "variables": e.Variables,
	}
	if e.TemplateVersion > 0 {
		payload["template_version"] = e.TemplateVersion
	}
	if e.Locale != "" {
		payload["locale"] = e.Locale
	}
	if e.TTLSeconds > 0 {
		payload["ttl_seconds"] = e.TTLSeconds
	}
	if e.RequestID != "" {
		payload["metadata"] = map[string]string{"request_id": e.RequestID}
	}
	body, _ := json.Marshal(payload)

	retries, backoff := c.MaxRetries, c.Backoff
	if retries == 0 {
		retries = 2
	}
	if backoff == 0 {
		backoff = 300 * time.Millisecond
	}
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			wait := backoff << (attempt - 1)
			var ae *APIError
			if errors.As(lastErr, &ae) && ae.RetryAfter > 0 {
				wait = min(ae.RetryAfter, 5*time.Second)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}
		b, _, err := c.do(ctx, "POST", "/v1/emails", idempotencyKey, body)
		if err == nil {
			var a Accepted
			if err := json.Unmarshal(b, &a); err != nil {
				lastErr = err
				continue
			}
			return &a, nil
		}
		lastErr = err
		var ae *APIError
		if errors.As(err, &ae) && !ae.Retryable() {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

// Status fetches metadata-only status for a message.
func (c *Client) Status(ctx context.Context, id string) (*Status, error) {
	b, _, err := c.do(ctx, "GET", "/v1/emails/"+id, "", nil)
	if err != nil {
		return nil, err
	}
	var s Status
	return &s, json.Unmarshal(b, &s)
}

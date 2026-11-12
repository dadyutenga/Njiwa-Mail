package mtaagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// ErrUnreachable means the agent could not be contacted at all.
var ErrUnreachable = errors.New("mta agent unreachable")

// Client talks to an Agent over its Unix socket.
type Client struct {
	Socket string
	Token  string
	http   *http.Client
}

// NewClient builds a client for the agent socket.
func NewClient(socket, token string) *Client {
	return &Client{Socket: socket, Token: token, http: &http.Client{
		Timeout: 40 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
			DisableKeepAlives: true,
		},
	}}
}

func (c *Client) do(ctx context.Context, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, "http://agent"+path, bytes.NewReader(nil))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != 200 {
		var e struct{ Error string }
		_ = json.Unmarshal(body, &e)
		return &AgentError{Status: resp.StatusCode, Code: e.Error}
	}
	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

// AgentError is an error answer from the agent.
type AgentError struct {
	Status int
	Code   string
}

func (e *AgentError) Error() string { return fmt.Sprintf("mta agent: %d %s", e.Status, e.Code) }

func (c *Client) Queue(ctx context.Context) ([]QueueEntry, error) {
	var r struct{ Items []QueueEntry }
	if err := c.do(ctx, "GET", "/queue", &r); err != nil {
		return nil, err
	}
	return r.Items, nil
}

func (c *Client) Status(ctx context.Context) (*Status, error) {
	var st Status
	if err := c.do(ctx, "GET", "/status", &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (c *Client) Act(ctx context.Context, id string, a Action) error {
	if !ValidQueueID(id) {
		return &AgentError{Status: 400, Code: "invalid_queue_id"}
	}
	return c.do(ctx, "POST", "/queue/"+id+"/"+string(a), nil)
}

func (c *Client) Flush(ctx context.Context) error { return c.do(ctx, "POST", "/flush", nil) }

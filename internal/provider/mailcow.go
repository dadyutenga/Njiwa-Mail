package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/njiwa-tz/mail/internal/mailcow"
)

// Mailcow adapts the Mailcow API client to Provider.
type Mailcow struct{ c *mailcow.Client }

// NewMailcow is the default Factory.
func NewMailcow(baseURL, key string) Provider { return &Mailcow{c: mailcow.New(baseURL, key)} }

func (m *Mailcow) Name() string                   { return "mailcow" }
func (m *Mailcow) Ping(ctx context.Context) error { return m.c.Ping(ctx) }

func (m *Mailcow) DomainState(ctx context.Context, domain string) (State, error) {
	d, ok, err := m.c.GetDomain(ctx, domain)
	if err != nil {
		return nil, err
	}
	if !ok {
		return State{"exists": false}, nil
	}
	return State{"exists": true, "active": truthy(d["active"]), "relay_all_recipients": truthy(d["relay_all_recipients"])}, nil
}

func (m *Mailcow) AddDomain(ctx context.Context, domain string, relayAll bool) error {
	return m.c.AddDomain(ctx, domain, relayAll)
}

func (m *Mailcow) DKIMRecord(ctx context.Context, domain string) (string, error) {
	return m.c.GetDKIM(ctx, domain)
}

// DKIMState reports presence and a short fingerprint of the public record, so a
// change of key is visible in an audit trail without storing the key.
func (m *Mailcow) DKIMState(ctx context.Context, domain string) (State, error) {
	txt, err := m.c.GetDKIM(ctx, domain)
	if err != nil {
		return nil, err
	}
	if txt == "" {
		return State{"present": false}, nil
	}
	sum := sha256.Sum256([]byte(txt))
	return State{"present": true, "fingerprint": hex.EncodeToString(sum[:8])}, nil
}

func (m *Mailcow) AddDKIM(ctx context.Context, domain, selector string) error {
	return m.c.AddDKIM(ctx, domain, selector)
}

func (m *Mailcow) TransportState(ctx context.Context, destination string) (State, error) {
	t, ok, err := m.c.GetTransport(ctx, destination)
	if err != nil {
		return nil, err
	}
	if !ok {
		return State{"exists": false}, nil
	}
	nh, _ := t["nexthop"].(string)
	return State{"exists": true, "nexthop": nh, "active": truthy(t["active"])}, nil
}

func (m *Mailcow) AddTransport(ctx context.Context, destination, nexthop string) error {
	return m.c.AddTransport(ctx, destination, nexthop)
}

// truthy reads Mailcow's mixed booleans (1, "1", true).
func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		return t == "1" || t == "true"
	}
	return false
}

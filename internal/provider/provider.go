// Package provider is the boundary between Njiwa-Mail and an existing mail
// server's management API (today only Mailcow). The service layer talks to this
// interface, never to a vendor client, so the Mailcow specifics stay in one
// adapter and every write can be audited the same way.
//
// Only per-domain actions exist here: add a domain, create its DKIM key, add the
// relay for inbound mail. No provider call changes global server settings, and
// none is needed for SMTP sending, which works with a plain mail server.
package provider

import "context"

// State is a small, non-secret description of one object on the provider
// (whether a domain exists, whether it is active, a fingerprint of a key).
// It is what audit records show as "before" and "after". It never holds a key,
// a password or any other secret.
type State map[string]any

// Provider is the management API of the operator's mail server.
type Provider interface {
	// Name identifies the provider in audit records ("mailcow").
	Name() string
	// Ping checks the URL and credentials with a read-only call.
	Ping(ctx context.Context) error

	DomainState(ctx context.Context, domain string) (State, error)
	AddDomain(ctx context.Context, domain string, relayAll bool) error

	DKIMState(ctx context.Context, domain string) (State, error)
	// DKIMRecord is the DNS TXT value of the domain's DKIM public key, or "" when none exists.
	DKIMRecord(ctx context.Context, domain string) (string, error)
	AddDKIM(ctx context.Context, domain, selector string) error

	TransportState(ctx context.Context, destination string) (State, error)
	AddTransport(ctx context.Context, destination, nexthop string) error
}

// Factory builds a Provider from the stored API URL and key. Tests replace it.
type Factory func(baseURL, key string) Provider

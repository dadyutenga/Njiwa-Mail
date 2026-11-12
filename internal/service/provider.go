package service

import (
	"context"
	"reflect"
	"strings"

	"github.com/njiwa-tz/mail/internal/provider"
)

// C05: every call to the mail server's management API goes through
// provider.Provider, and every write through this recorder, which writes an
// audit row with the object's state before and after (never a secret) and
// whether it actually changed anything.

// recorded wraps a Provider so each write is audited with before/after state.
type recorded struct {
	p      provider.Provider
	s      *Service
	actor  Actor
	domain string // the Njiwa-Mail domain the action serves
	secret string // the API key: stripped from any error text before it is stored
}

func (r recorded) Name() string                   { return r.p.Name() }
func (r recorded) Ping(ctx context.Context) error { return r.p.Ping(ctx) }
func (r recorded) DomainState(ctx context.Context, d string) (provider.State, error) {
	return r.p.DomainState(ctx, d)
}
func (r recorded) DKIMState(ctx context.Context, d string) (provider.State, error) {
	return r.p.DKIMState(ctx, d)
}
func (r recorded) DKIMRecord(ctx context.Context, d string) (string, error) {
	return r.p.DKIMRecord(ctx, d)
}
func (r recorded) TransportState(ctx context.Context, d string) (provider.State, error) {
	return r.p.TransportState(ctx, d)
}

func (r recorded) write(ctx context.Context, op, target string, read func() (provider.State, error), do func() error, extra map[string]any) error {
	before, berr := read()
	err := do()
	after, aerr := read()
	details := map[string]any{"provider": r.p.Name(), "domain": r.domain, "ok": err == nil}
	for k, v := range extra {
		details[k] = v
	}
	state := func(st provider.State, e error) any {
		if e != nil {
			return map[string]any{"unknown": true}
		}
		return st
	}
	details["before"], details["after"] = state(before, berr), state(after, aerr)
	details["changed"] = berr == nil && aerr == nil && !reflect.DeepEqual(before, after)
	if err != nil {
		msg := err.Error()
		if r.secret != "" {
			msg = strings.ReplaceAll(msg, r.secret, "[redacted]")
		}
		details["error"] = clean(msg, 200)
	}
	// An audit failure must not hide a write that already happened: the caller still gets its result.
	_ = r.s.Audit(ctx, r.actor, "provider."+r.p.Name()+"."+op, "domain", target, details)
	return err
}

func (r recorded) AddDomain(ctx context.Context, domain string, relayAll bool) error {
	return r.write(ctx, "add_domain", domain,
		func() (provider.State, error) { return r.p.DomainState(ctx, domain) },
		func() error { return r.p.AddDomain(ctx, domain, relayAll) },
		map[string]any{"relay_all_recipients": relayAll})
}

func (r recorded) AddDKIM(ctx context.Context, domain, selector string) error {
	return r.write(ctx, "add_dkim", domain,
		func() (provider.State, error) { return r.p.DKIMState(ctx, domain) },
		func() error { return r.p.AddDKIM(ctx, domain, selector) },
		map[string]any{"selector": clean(selector, 63)})
}

func (r recorded) AddTransport(ctx context.Context, destination, nexthop string) error {
	return r.write(ctx, "add_transport", destination,
		func() (provider.State, error) { return r.p.TransportState(ctx, destination) },
		func() error { return r.p.AddTransport(ctx, destination, nexthop) },
		map[string]any{"nexthop": clean(nexthop, 200)})
}

// audited returns the provider for the stored settings, with audit recording of every write.
func (s *Service) audited(ctx context.Context, actor Actor, domain string) (provider.Provider, error) {
	p, key, err := s.providerWithKey(ctx)
	if err != nil {
		return nil, err
	}
	return recorded{p: p, s: s, actor: actor, domain: domain, secret: key}, nil
}

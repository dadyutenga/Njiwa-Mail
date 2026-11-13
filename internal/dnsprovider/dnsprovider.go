// Package dnsprovider is the boundary between Njiwa-Mail and a DNS host's API.
// The service layer plans and applies DNS changes through this interface and
// never imports a vendor client, so Cloudflare specifics stay in one adapter
// and a second provider is one more adapter.
//
// The interface has no way to delete or replace a record other than by record
// id, and the callers only ever delete records they created themselves.
package dnsprovider

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Record is one DNS record as the provider holds it.
type Record struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Name     string `json:"name"` // fully qualified, lowercase, no trailing dot
	Content  string `json:"content"`
	TTL      int    `json:"ttl"` // 1 = provider "automatic"
	Priority int    `json:"priority,omitempty"`
	Proxied  bool   `json:"proxied,omitempty"`
	Comment  string `json:"comment,omitempty"`
	// Modified is the provider's last-modified stamp, used to detect a change
	// made by someone else between planning and applying.
	Modified string `json:"modified,omitempty"`
}

// Zone is a DNS zone the credential can see.
type Zone struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	NameServers []string `json:"name_servers"`
	Status      string   `json:"status"` // active, pending, ...
}

// Provider is the API of one DNS host, bound to one credential.
type Provider interface {
	Name() string
	// Verify checks that the credential is valid and active. It returns the
	// credential's expiry when the provider reports one.
	Verify(ctx context.Context) (expires time.Time, err error)
	Zones(ctx context.Context) ([]Zone, error)
	Zone(ctx context.Context, zoneID string) (Zone, error)
	Records(ctx context.Context, zoneID string) ([]Record, error)
	// DNSSEC returns the provider's DNSSEC status ("active", "pending",
	// "disabled", ...). It is read only: Njiwa-Mail never changes DNSSEC.
	DNSSEC(ctx context.Context, zoneID string) (string, error)
	Create(ctx context.Context, zoneID string, r Record) (Record, error)
	// Update replaces the content of one record by id. It must not be retried
	// blindly: callers re-read the record first.
	Update(ctx context.Context, zoneID, recordID string, r Record) (Record, error)
	Delete(ctx context.Context, zoneID, recordID string) error
}

// Error classes. Callers branch on these with errors.Is / errors.As and never
// show the provider's raw message to a client.
var (
	ErrUnauthorized = errors.New("dns provider: credential rejected")
	ErrForbidden    = errors.New("dns provider: credential lacks permission for this zone")
	ErrNotFound     = errors.New("dns provider: not found")
	ErrConflict     = errors.New("dns provider: conflicting record")
	ErrInvalid      = errors.New("dns provider: request rejected as invalid")
)

// RateLimitError is returned for HTTP 429.
type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("dns provider: rate limited (retry after %s)", e.RetryAfter)
}

// TransientError is a timeout, network failure or 5xx: the outcome of a write
// is unknown.
type TransientError struct{ Err error }

func (e *TransientError) Error() string { return "dns provider: transient failure: " + e.Err.Error() }
func (e *TransientError) Unwrap() error { return e.Err }

// Factory builds a Provider from a stored credential. Tests replace it.
type Factory func(provider, token string) (Provider, error)

// Retry runs fn up to attempts times with exponential backoff and jitter-free
// doubling, for transient failures and rate limits only. It honours a
// provider Retry-After (bounded by maxWait). Use it for idempotent calls
// (reads, and creates that the caller can verify). sleep is replaceable in tests.
func Retry(ctx context.Context, attempts int, base, maxWait time.Duration, sleep func(context.Context, time.Duration) error, fn func() error) error {
	if attempts < 1 {
		attempts = 1
	}
	if sleep == nil {
		sleep = Sleep
	}
	wait := base
	var err error
	for i := 0; i < attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		var rl *RateLimitError
		var tr *TransientError
		d := wait
		switch {
		case errors.As(err, &rl):
			if rl.RetryAfter > 0 {
				d = rl.RetryAfter
			}
		case errors.As(err, &tr):
		default:
			return err // permanent: never retried
		}
		if i == attempts-1 {
			break
		}
		if d > maxWait {
			d = maxWait
		}
		if serr := sleep(ctx, d); serr != nil {
			return serr
		}
		wait *= 2
	}
	return err
}

// Sleep waits for d or until ctx ends.
func Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

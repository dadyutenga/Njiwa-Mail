// Package transport defines the small contract between the worker and
// whatever hands a message to the outside world (managed Postfix or an
// existing SMTP server). It carries no provider-specific types: Mailcow and
// Postfix details live behind implementations and control-plane modules.
package transport

import (
	"context"
	"time"

	"github.com/njiwa-tz/mail/internal/mailer"
)

type Mode string

const (
	ManagedMTA   Mode = "managed_mta"
	ExistingSMTP Mode = "existing_smtp"
)

func (m Mode) Valid() bool { return m == ManagedMTA || m == ExistingSMTP }

// Outcome is the honest classification of one submission attempt.
type Outcome string

const (
	Accepted  Outcome = "accepted"
	Temporary Outcome = "temporary_failure" // proven not accepted; retry cannot duplicate
	Permanent Outcome = "permanent_failure" // proven not accepted; will not succeed
	Unknown   Outcome = "unknown"           // body sent, no definitive reply; never auto-retry
)

// Capability describes facts the domain/DNS engine needs about egress.
type Capability struct {
	EgressIPs           []string
	DKIMSelector        string
	SigningDomain       string
	EnvelopeDomain      string
	SupportsInboundDSN  bool
	SupportsQueueLookup bool
}

type Submission struct {
	MessageID    string
	EnvelopeFrom string
	Recipients   []string
	RFC5322      []byte
}

type Receipt struct {
	Outcome      Outcome
	SafeReason   string // stable code, never server text containing secrets
	TransportID  string // server reply (queue id etc.) when accepted
	MTAQueueID   string
	Acknowledged *time.Time
}

type Transport interface {
	Mode() Mode
	// Check performs a read-only readiness probe and reports capabilities.
	Check(ctx context.Context) (Capability, error)
	Submit(ctx context.Context, s Submission) Receipt
}

// FromMailer maps the existing, well-tested mailer result onto a Receipt.
func FromMailer(r mailer.Result, now time.Time) Receipt {
	rc := Receipt{SafeReason: r.Reason, TransportID: r.Reply}
	switch r.Outcome {
	case mailer.Accepted:
		rc.Outcome = Accepted
		rc.Acknowledged = &now
	case mailer.TempFailure:
		rc.Outcome = Temporary
	case mailer.PermFailure:
		rc.Outcome = Permanent
	default:
		rc.Outcome = Unknown
	}
	return rc
}

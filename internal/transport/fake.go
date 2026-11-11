package transport

import (
	"context"
	"sync"
	"time"
)

// Fake is an in-memory Transport for contract and service tests.
type Fake struct {
	M    Mode
	Cap  Capability
	Err  error                             // returned by Check
	Fn   func(n int, s Submission) Receipt // nil: always Accepted
	mu   sync.Mutex
	Subs []Submission
}

func (f *Fake) Mode() Mode {
	if f.M == "" {
		return ExistingSMTP
	}
	return f.M
}
func (f *Fake) Check(context.Context) (Capability, error) { return f.Cap, f.Err }
func (f *Fake) Submit(_ context.Context, s Submission) Receipt {
	f.mu.Lock()
	f.Subs = append(f.Subs, s)
	n := len(f.Subs)
	f.mu.Unlock()
	if f.Fn != nil {
		return f.Fn(n, s)
	}
	now := time.Now().UTC()
	return Receipt{Outcome: Accepted, SafeReason: "smtp_250", Acknowledged: &now}
}

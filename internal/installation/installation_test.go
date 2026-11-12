package installation

import (
	"testing"
	"time"
)

var now = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

func pass(rev int64, age time.Duration) Check {
	exp := now.Add(-age).Add(DefaultCheckTTL)
	return Check{Result: "pass", Revision: rev, CheckedAt: now.Add(-age), ExpiresAt: &exp}
}

func TestNoProfileIsNotStarted(t *testing.T) {
	r := Compute(Facts{Now: now})
	if r.Status != NotStarted || r.Progress.Required != 1 || r.Progress.Complete != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestExistingSMTPReadyOnlyWithFreshChecksForThisRevision(t *testing.T) {
	f := Facts{Now: now, Mode: ModeExisting, ProfileRevision: 3, ConfigComplete: true,
		Checks: map[string]Check{"connection": pass(3, time.Hour), "test_send": pass(3, time.Hour)}}
	if r := Compute(f); r.Status != Ready || r.Progress.Complete != 4 || r.Progress.Required != 4 {
		t.Fatalf("%+v", r)
	}
	// Config changed (revision bumped): old green must go stale.
	f.ProfileRevision = 4
	if r := Compute(f); r.Status != Stale {
		t.Fatalf("want stale after revision change, got %s", r.Status)
	}
	// Expired check goes stale.
	f.ProfileRevision = 3
	f.Checks["test_send"] = pass(3, 25*time.Hour)
	if r := Compute(f); r.Status != Stale {
		t.Fatalf("want stale after expiry, got %s", r.Status)
	}
}

func TestFailedAndIncomplete(t *testing.T) {
	f := Facts{Now: now, Mode: ModeExisting, ProfileRevision: 1, ConfigComplete: false}
	r := Compute(f)
	if r.Status == Ready || r.Steps[1].ProblemCode != CodeSMTPIncomplete {
		t.Fatalf("%+v", r)
	}
	f.ConfigComplete = true
	f.Checks = map[string]Check{"connection": {Result: "fail", Revision: 1, CheckedAt: now, ProblemCode: "SMTP_AUTH_REJECTED"}}
	r = Compute(f)
	if r.Status != NeedsAction || r.Steps[2].ProblemCode != "SMTP_AUTH_REJECTED" {
		t.Fatalf("%+v", r)
	}
	f.Checks["connection"] = Check{Result: "fail", Revision: 1, CheckedAt: now, ProblemCode: CodeEgressBlocked, External: true}
	if r := Compute(f); r.Status != BlockedExternalAction {
		t.Fatalf("%+v", r)
	}
}

func TestManagedNeverReadyWithoutInstaller(t *testing.T) {
	f := Facts{Now: now, Mode: ModeManaged, ProfileRevision: 1,
		Checks: map[string]Check{"preflight": pass(1, time.Minute), "connection": pass(1, time.Minute)}}
	if r := Compute(f); r.Status == Ready {
		t.Fatalf("managed mode must not be ready without an installer receipt: %+v", r)
	}
	f.ManagedInstalled = true
	if r := Compute(f); r.Status != Ready {
		t.Fatalf("%+v", r)
	}
}

func TestDeliveryFailuresDegradeReadiness(t *testing.T) {
	f := Facts{Now: now, Mode: ModeExisting, ProfileRevision: 1, ConfigComplete: true,
		Checks: map[string]Check{"connection": pass(1, time.Hour), "test_send": pass(1, time.Hour)}}
	if r := Compute(f); r.Status != Ready || len(r.Steps) != 4 {
		t.Fatalf("baseline: %+v", r)
	}
	fail := now.Add(-time.Minute)
	f.Delivery = &DeliveryHealth{TransportFailures: 2, LastFailure: &fail, LastCode: "smtp_connect_failed"}
	if r := Compute(f); r.Status != Ready {
		t.Fatalf("below the threshold must not degrade: %+v", r)
	}
	f.Delivery.TransportFailures = 3
	r := Compute(f)
	if r.Status != Error || r.Steps[len(r.Steps)-1].ProblemCode != CodeTransportFailing {
		t.Fatalf("failing delivery must degrade: %+v", r)
	}
	later := now
	f.Delivery.LastSubmitted = &later
	if r := Compute(f); r.Status != Ready {
		t.Fatalf("a submission after the failures recovers: %+v", r)
	}
}

func TestBounceStepIsOptionalAndHonest(t *testing.T) {
	f := Facts{Now: now, Mode: ModeExisting, ProfileRevision: 1, ConfigComplete: true,
		Checks: map[string]Check{"connection": pass(1, time.Hour), "test_send": pass(1, time.Hour)}}
	f.Bounces = &BounceFacts{}
	r := Compute(f)
	last := r.Steps[len(r.Steps)-1]
	if r.Status != Ready || r.Progress.Required != 4 || last.ID != "bounce_reports" || last.Required || last.ProblemCode != CodeBouncesNotSetUp {
		t.Fatalf("no bounce path must be reported but must not block: %+v", r)
	}
	f.Bounces = &BounceFacts{Configured: true}
	if s := Compute(f).Steps[4]; s.ProblemCode != CodeBounceListener {
		t.Fatalf("listener down: %+v", s)
	}
	f.Bounces = &BounceFacts{Configured: true, ListenerUp: true}
	if s := Compute(f).Steps[4]; s.Status != Ready || s.CheckedAt != nil {
		t.Fatalf("ready without reports: %+v", s)
	}
}

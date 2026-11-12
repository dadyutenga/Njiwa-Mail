package service

import "testing"

func TestSuggestOutcome(t *testing.T) {
	for _, c := range []struct {
		name     string
		queue    string
		evidence []evidenceRow
		outcome  string
		strength string
	}{
		{"nothing at all", "", nil, "", "none"},
		{"queue id only", "4C1D", nil, "submitted", "weak"},
		{"deferred", "4C1D", []evidenceRow{{"deferred", "mta_log"}}, "", "none"},
		{"remote accepted", "4C1D", []evidenceRow{{"deferred", "mta_log"}, {"remote_accepted", "mta_log"}}, "submitted", "strong"},
		{"bounced by log", "", []evidenceRow{{"bounced", "mta_log"}}, "failed", "strong"},
		{"bounced by dsn", "", []evidenceRow{{"bounced", "dsn"}}, "failed", "strong"},
		{"gave up", "4C1D", []evidenceRow{{"expired_in_queue", "mta_log"}}, "failed", "strong"},
		{"conflict is not guessed", "4C1D", []evidenceRow{{"remote_accepted", "mta_log"}, {"bounced", "dsn"}}, "", "none"},
	} {
		got := SuggestOutcome(c.queue, c.evidence)
		if got.Outcome != c.outcome || got.Strength != c.strength || got.Basis == "" {
			t.Errorf("%s: %+v", c.name, got)
		}
		if got.Outcome != "" && (got.Note == "" || len(got.Note) > 120) {
			t.Errorf("%s: note %q", c.name, got.Note)
		}
	}
	if s := SuggestOutcome("4C1D", []evidenceRow{{"remote_accepted", "mta_log"}}); !contains(s.Basis, "not proof of inbox") {
		t.Errorf("suggestion must not claim inbox delivery: %q", s.Basis)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

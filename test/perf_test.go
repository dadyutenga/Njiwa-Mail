package test

import "testing"

func TestPerfIndexesExist(t *testing.T) {
	e := setup(t)
	for _, name := range []string{"messages_submitted_at", "messages_claim_order"} {
		if e.count(`SELECT count(*) FROM pg_indexes WHERE indexname=$1`, name) != 1 {
			t.Fatalf("missing index %s", name)
		}
	}
}

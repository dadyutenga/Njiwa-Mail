package service

import (
	"strings"
	"testing"
)

// T7.3: in managed mode the inbound listener must accept the gateway of
// Njiwa's network, where host Postfix relays bounces from.
func TestInboundSourcesProblem(t *testing.T) {
	for _, c := range []struct {
		sources, subnet string
		ok              bool
		want            string
	}{
		{"172.30.0.1", "", true, ""},
		{"10.0.0.5, 172.30.0.0/24", "", true, ""},
		{"172.31.250.1", "172.31.250.0/24", true, ""},
		{"", "", false, "is empty"},
		{"172.22.1.253", "", false, "does not include 172.30.0.1"},
		{"172.30.0.1", "172.31.250.0/24", false, "INBOUND_ALLOWED_SOURCES=172.31.250.1"},
		// An entry the listener rejects (it would not start) is reported.
		{"172.30.0.1, mailcow-host", "", false, "is invalid"},
	} {
		got := inboundSourcesProblem(c.sources, c.subnet)
		if (got == "") != c.ok || !strings.Contains(got, c.want) {
			t.Errorf("%q in %q: %q", c.sources, c.subnet, got)
		}
	}
}

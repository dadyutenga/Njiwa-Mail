package mailer

import (
	"context"
	"testing"
	"time"
)

// F05: a send (not only a connection test) must never connect to link-local
// or multicast addresses such as the cloud metadata service.
func TestSendRefusesForbiddenTargets(t *testing.T) {
	for _, host := range []string{"169.254.169.254", "169.254.1.1", "224.0.0.1", "0.0.0.0"} {
		s := NewSMTP(SMTPConfig{Host: host, Port: 25, Timeout: 2 * time.Second})
		r := s.Send(context.Background(), Envelope{From: "a@example.com", To: "b@example.com", Raw: []byte("x")})
		if r.Outcome != PermFailure || r.Reason != "smtp_host_forbidden" {
			t.Errorf("%s: %+v", host, r)
		}
	}
	// Loopback stays allowed (a relay on the same host is normal): it fails
	// to connect, but not because of the guard.
	s := NewSMTP(SMTPConfig{Host: "127.0.0.1", Port: 1, Timeout: 2 * time.Second})
	if r := s.Send(context.Background(), Envelope{From: "a@example.com", To: "b@example.com", Raw: []byte("x")}); r.Reason == "smtp_host_forbidden" {
		t.Fatalf("loopback refused: %+v", r)
	}
}

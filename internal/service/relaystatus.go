package service

import (
	"context"
	"net"
	"time"
)

// RelayStatus tells the dashboard how an application connects to the SMTP
// relay and whether a relay process has checked in. The relay is a separate,
// optional process; "running" means it sent a heartbeat in the last minute,
// which is the honest evidence available here. It does not prove that the
// port is reachable from your application's network.
func (s *Service) RelayStatus(ctx context.Context) (map[string]any, error) {
	var last *time.Time
	if err := s.Pool.QueryRow(ctx, `SELECT max(last_seen) FROM worker_heartbeats WHERE role='relay'`).Scan(&last); err != nil {
		return nil, unavailable("relay_status", err)
	}
	port := ""
	if _, p, err := net.SplitHostPort(s.Opts.RelayAddr); err == nil {
		port = p
	}
	tls := "starttls"
	if s.Opts.RelayImplicitTLS {
		tls = "implicit"
	}
	return map[string]any{
		"running":   last != nil && time.Since(*last) <= time.Minute,
		"last_seen": last,
		"host":      s.Opts.MailHost,
		"port":      port,
		"tls":       tls,
		"username":  "any value (the API key is the password)",
		"scope":     "emails:send_raw",
	}, nil
}

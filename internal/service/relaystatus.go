package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
)

// Relay states shown to the dashboard.
const (
	RelayNotEnabled = "not_enabled" // not configured and never seen
	RelayStarting   = "starting"
	RelayRunning    = "running"
	RelayStale      = "stale" // configured or seen before, no recent heartbeat
	RelayError      = "error"
)

// ReportRole is a heartbeat that also says how the role is doing.
func (s *Service) ReportRole(ctx context.Context, role, instanceID, state, detail string, info map[string]any) error {
	if info == nil {
		info = map[string]any{}
	}
	j, err := json.Marshal(info)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO worker_heartbeats(worker_id, role, last_seen, state, detail, info) VALUES ($1,$2,now(),$3,$4,$5::jsonb)
ON CONFLICT (worker_id) DO UPDATE SET last_seen=now(), role=EXCLUDED.role, state=EXCLUDED.state, detail=EXCLUDED.detail, info=EXCLUDED.info`,
		role+":"+instanceID, role, state, clean(detail, 300), j)
	return err
}

// relayEndpoint is where applications are told to connect.
func (s *Service) relayEndpoint(requestHost string) (host, source, port string) {
	host, source = s.Opts.RelayPublicHost, "setting"
	if host == "" {
		host, source = requestHost, "request"
		if h, _, err := net.SplitHostPort(requestHost); err == nil {
			host = h
		}
	}
	port = s.Opts.RelayPublicPort
	if port == "" {
		_, port, _ = net.SplitHostPort(s.Opts.RelayAddr)
	}
	return host, source, port
}

// RelayStatus tells the dashboard how an application connects to the SMTP
// relay and how the relay process says it is doing. "running" means it
// reported in the last minute that it is listening with a certificate. It does
// not prove that the port is reachable from an application's network.
func (s *Service) RelayStatus(ctx context.Context, requestHost string) (map[string]any, error) {
	var last *time.Time
	var reported, detail string
	var raw []byte
	err := s.Pool.QueryRow(ctx, `SELECT last_seen, state, detail, info FROM worker_heartbeats WHERE role='relay'
ORDER BY (last_seen > now() - interval '1 minute' AND state IN ('','running')) DESC, last_seen DESC LIMIT 1`).Scan(&last, &reported, &detail, &raw)
	if err != nil && !errors.Is(err, db.ErrNoRows) {
		return nil, unavailable("relay_status", err)
	}
	var info struct {
		Port         string `json:"port"`
		ImplicitTLS  *bool  `json:"implicit_tls"`
		CertNotAfter string `json:"cert_not_after"`
	}
	_ = json.Unmarshal(raw, &info)

	state := RelayNotEnabled
	switch {
	case last == nil:
		if s.Opts.RelayEnabled {
			state = RelayStale
		}
	case time.Since(*last) > time.Minute:
		state = RelayStale
	case reported == RelayStarting || reported == RelayError:
		state = reported
	default:
		state = RelayRunning
	}
	if state == RelayStale || state == RelayNotEnabled {
		detail = "" // it described a process that is no longer reporting
	}
	host, source, port := s.relayEndpoint(requestHost)
	if s.Opts.RelayPublicPort == "" && info.Port != "" {
		port = info.Port
	}
	implicit := s.Opts.RelayImplicitTLS
	if info.ImplicitTLS != nil {
		implicit = *info.ImplicitTLS
	}
	tls := "starttls"
	if implicit {
		tls = "implicit"
	}
	return map[string]any{
		"state":          state,
		"running":        state == RelayRunning,
		"enabled":        s.Opts.RelayEnabled,
		"last_seen":      last,
		"detail":         detail,
		"cert_not_after": info.CertNotAfter,
		"host":           host,
		"host_source":    source,
		"port":           port,
		"tls":            tls,
		"username":       "any value (the API key is the password)",
		"scope":          ScopeSendRaw,
	}, nil
}

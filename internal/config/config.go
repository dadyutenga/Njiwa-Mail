// Package config loads service configuration from the environment.
// Secrets may be supplied directly (VAR) or from a file (VAR_FILE).
package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type SMTP struct {
	Host     string
	Port     int
	Username string
	Password string
	Implicit bool // implicit TLS (port 465) instead of STARTTLS
	HeloName string
	Timeout  time.Duration
}

func (s SMTP) Configured() bool { return s.Host != "" }

type Config struct {
	DatabaseURL   string
	ListenAddr    string
	EncryptionKey []byte
	HMACKey       []byte
	MaxBodyBytes  int64
	SMTP          SMTP

	MetadataRetention time.Duration
	CaptureRetention  time.Duration
	UnknownPurge      time.Duration // payload purge for unknown/terminal rows (<= 24h)

	WorkerPoll        time.Duration
	WorkerBatch       int
	WorkerConcurrency int
	LeaseDuration     time.Duration
	MaxAttempts       int
	RetentionInterval time.Duration
	LogLevel          string

	// ConsolePassword gates the read-only operator UI. Empty disables /console.
	// It is not a project API key and must never be embedded in frontend code.
	ConsolePassword   string
	ConsoleSessionTTL time.Duration

	// Admin API.
	AdminSessionTTL time.Duration
	TrustProxy      bool
	InsecureCookies bool
	MetricsToken    string

	// Inbound mail and bounces.
	InboundListenAddr     string
	InboundDomain         string // e.g. in.example.com
	BounceAddress         string // e.g. bounces@notify.example.com; also the SMTP envelope sender
	InboundAllowedSources string // Mailcow host IPs / CIDRs allowed to connect to the listener
	InboundMaxBytes       int
	DataDir               string

	// DNS health checker expectations.
	MailHostname string
	OutboundIP   string
	DKIMSelector string
}

func boolVar(name string) (bool, error) {
	switch strings.ToLower(get(name, "false")) {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	}
	return false, fmt.Errorf("%s must be true or false", name)
}

func get(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

// secret reads NAME, or the file named by NAME_FILE.
func secret(name string) (string, error) {
	if p := strings.TrimSpace(os.Getenv(name + "_FILE")); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			return "", fmt.Errorf("%s_FILE: %w", name, err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return strings.TrimSpace(os.Getenv(name)), nil
}

func intVar(name string, def int) (int, error) {
	v := get(name, "")
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return n, nil
}

func durVar(name string, def time.Duration) (time.Duration, error) {
	v := get(name, "")
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return d, nil
}

func key(name string, minLen int, exact bool) ([]byte, error) {
	s, err := secret(name)
	if err != nil {
		return nil, err
	}
	if s == "" {
		return nil, fmt.Errorf("%s is required", name)
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%s must be standard base64: %w", name, err)
	}
	if exact && len(b) != minLen || !exact && len(b) < minLen {
		return nil, fmt.Errorf("%s must decode to %d bytes", name, minLen)
	}
	return b, nil
}

// Load reads and validates configuration. Errors never include secret values.
func Load() (*Config, error) {
	var c Config
	var err error
	if c.DatabaseURL, err = secret("DATABASE_URL"); err != nil {
		return nil, err
	}
	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	c.ListenAddr = get("LISTEN_ADDR", "127.0.0.1:8080")
	if c.EncryptionKey, err = key("ENCRYPTION_KEY", 32, true); err != nil {
		return nil, err
	}
	if c.HMACKey, err = key("HMAC_KEY", 32, false); err != nil {
		return nil, err
	}
	mb, err := intVar("MAX_BODY_BYTES", 32*1024)
	if err != nil {
		return nil, err
	}
	c.MaxBodyBytes = int64(mb)

	c.SMTP.Host = get("SMTP_HOST", "")
	if c.SMTP.Port, err = intVar("SMTP_PORT", 587); err != nil {
		return nil, err
	}
	c.SMTP.Username = get("SMTP_USERNAME", "")
	if c.SMTP.Password, err = secret("SMTP_PASSWORD"); err != nil {
		return nil, err
	}
	switch strings.ToLower(get("SMTP_TLS_MODE", "starttls")) {
	case "starttls":
	case "implicit":
		c.SMTP.Implicit = true
	default:
		return nil, fmt.Errorf("SMTP_TLS_MODE must be starttls or implicit (plaintext SMTP is not supported)")
	}
	c.SMTP.HeloName = get("SMTP_HELO_NAME", "")
	if c.SMTP.Timeout, err = durVar("SMTP_TIMEOUT", 40*time.Second); err != nil {
		return nil, err
	}
	if c.SMTP.Configured() && (c.SMTP.Username == "" || c.SMTP.Password == "") {
		return nil, fmt.Errorf("SMTP_USERNAME and SMTP_PASSWORD are required when SMTP_HOST is set")
	}

	if c.MetadataRetention, err = durVar("METADATA_RETENTION", 30*24*time.Hour); err != nil {
		return nil, err
	}
	if c.CaptureRetention, err = durVar("CAPTURE_RETENTION", 24*time.Hour); err != nil {
		return nil, err
	}
	if c.UnknownPurge, err = durVar("UNKNOWN_PAYLOAD_PURGE", 24*time.Hour); err != nil {
		return nil, err
	}
	if c.UnknownPurge > 24*time.Hour {
		return nil, fmt.Errorf("UNKNOWN_PAYLOAD_PURGE must not exceed 24h")
	}
	if c.WorkerPoll, err = durVar("WORKER_POLL_INTERVAL", 500*time.Millisecond); err != nil {
		return nil, err
	}
	if c.WorkerBatch, err = intVar("WORKER_BATCH", 10); err != nil {
		return nil, err
	}
	if c.WorkerConcurrency, err = intVar("WORKER_CONCURRENCY", 4); err != nil {
		return nil, err
	}
	if c.LeaseDuration, err = durVar("LEASE_DURATION", 90*time.Second); err != nil {
		return nil, err
	}
	if c.LeaseDuration <= c.SMTP.Timeout {
		return nil, fmt.Errorf("LEASE_DURATION must exceed SMTP_TIMEOUT")
	}
	if c.MaxAttempts, err = intVar("MAX_ATTEMPTS", 4); err != nil {
		return nil, err
	}
	if c.RetentionInterval, err = durVar("RETENTION_INTERVAL", 10*time.Minute); err != nil {
		return nil, err
	}
	c.LogLevel = get("LOG_LEVEL", "info")
	if c.ConsolePassword, err = secret("CONSOLE_PASSWORD"); err != nil {
		return nil, err
	}
	if c.ConsolePassword != "" && len(c.ConsolePassword) < 12 {
		return nil, fmt.Errorf("CONSOLE_PASSWORD must be at least 12 characters")
	}
	if c.ConsoleSessionTTL, err = durVar("CONSOLE_SESSION_TTL", 12*time.Hour); err != nil {
		return nil, err
	}
	if c.ConsoleSessionTTL < time.Minute || c.ConsoleSessionTTL > 24*time.Hour {
		return nil, fmt.Errorf("CONSOLE_SESSION_TTL must be between 1m and 24h")
	}
	if c.AdminSessionTTL, err = durVar("ADMIN_SESSION_TTL", 12*time.Hour); err != nil {
		return nil, err
	}
	if c.AdminSessionTTL < 5*time.Minute || c.AdminSessionTTL > 7*24*time.Hour {
		return nil, fmt.Errorf("ADMIN_SESSION_TTL must be between 5m and 168h")
	}
	if c.TrustProxy, err = boolVar("TRUST_PROXY"); err != nil {
		return nil, err
	}
	if c.InsecureCookies, err = boolVar("ADMIN_INSECURE_COOKIES"); err != nil {
		return nil, err
	}
	if c.MetricsToken, err = secret("METRICS_TOKEN"); err != nil {
		return nil, err
	}
	if c.MetricsToken != "" && len(c.MetricsToken) < 16 {
		return nil, fmt.Errorf("METRICS_TOKEN must be at least 16 characters")
	}
	c.InboundListenAddr = get("INBOUND_LISTEN_ADDR", "0.0.0.0:2525")
	c.InboundDomain = strings.ToLower(get("INBOUND_DOMAIN", ""))
	c.BounceAddress = strings.ToLower(get("BOUNCE_ADDRESS", ""))
	if c.BounceAddress != "" && (strings.Count(c.BounceAddress, "@") != 1 || strings.ContainsAny(c.BounceAddress, " <>\r\n")) {
		return nil, fmt.Errorf("BOUNCE_ADDRESS must be a bare email address")
	}
	c.InboundAllowedSources = get("INBOUND_ALLOWED_SOURCES", "")
	if c.InboundMaxBytes, err = intVar("INBOUND_MAX_BYTES", 10<<20); err != nil {
		return nil, err
	}
	c.DataDir = get("DATA_DIR", "/var/lib/ditronics-mail")
	c.MailHostname = strings.ToLower(get("MAIL_HOSTNAME", ""))
	c.OutboundIP = get("OUTBOUND_IP", "")
	c.DKIMSelector = get("DKIM_SELECTOR", "dkim")
	return &c, nil
}

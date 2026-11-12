// Package config loads service configuration from the environment.
// Secrets may be supplied directly (VAR) or from a file (VAR_FILE).
package config

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type SMTP struct {
	Host         string
	Port         int
	Username     string
	Password     string
	Implicit     bool // implicit TLS (port 465) instead of STARTTLS
	HeloName     string
	Timeout      time.Duration
	FailoverHost string
}

func (s SMTP) Configured() bool { return s.Host != "" }

type Config struct {
	DatabaseURL   string
	ListenAddr    string
	EncryptionKey []byte
	// EncryptionKeyPrevious are keys that can still open sealed data during a
	// rotation (ENCRYPTION_KEY_PREVIOUS, comma-separated base64); they never seal.
	EncryptionKeyPrevious [][]byte
	HMACKey               []byte
	MaxBodyBytes          int64
	SMTP                  SMTP

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
	PublicURL       string
	IngressAddr     string

	// IMAP is the inbound fallback when Mailcow cannot relay to the SMTP listener.
	IMAPHost     string
	IMAPPort     int
	IMAPUsername string
	IMAPPassword string
	IMAPMailbox  string
	MetricsToken string

	// Inbound mail and bounces.
	InboundListenAddr string
	InboundDomain     string // e.g. in.example.com
	BounceAddress     string // e.g. bounces@notify.example.com; also the SMTP envelope sender
	// BounceVERP: "auto" (signed return path bounces+<id>.<tag>@... only with the managed Postfix),
	// "on" (always) or "off" (always the plain bounce address). F06.
	BounceVERP            string
	InboundAllowedSources string // Mailcow host IPs / CIDRs allowed to connect to the listener
	InboundMaxBytes       int
	DataDir               string

	// DNS health checker expectations.
	MailHostname string
	OutboundIP   string
	DKIMSelector string

	// AllowUnregisteredLiveDomains is a temporary upgrade escape hatch: Live
	// mail and Live senders may use a domain no project has registered or
	// claimed. Off by default. It never allows a domain another project claims.
	AllowUnregisteredLiveDomains bool

	// Installation-wide Live sending limits (0 disables one).
	LiveGlobalPerMinute, LiveGlobalPerHour int
	LiveDomainPerMinute, LiveDomainPerHour int
	LiveQueueMax                           int
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

// previousKeys reads ENCRYPTION_KEY_PREVIOUS: zero or more comma-separated
// base64 keys of exactly 32 bytes that may still open older sealed data.
func previousKeys() ([][]byte, error) {
	s, err := secret("ENCRYPTION_KEY_PREVIOUS")
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		b, err := base64.StdEncoding.DecodeString(part)
		if err != nil || len(b) != 32 {
			return nil, fmt.Errorf("ENCRYPTION_KEY_PREVIOUS must be comma-separated standard base64 keys of exactly 32 bytes")
		}
		out = append(out, b)
	}
	return out, nil
}

// loadDotEnv copies KEY=VALUE lines from .env into the process environment.
// Variables already set are left alone, so the shell and the container win.
func loadDotEnv() error {
	f, err := os.Open(".env")
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf(".env: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		val = strings.TrimSpace(val)
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		if err := os.Setenv(key, val); err != nil {
			return fmt.Errorf(".env %s: %w", key, err)
		}
	}
	return sc.Err()
}

// Load reads and validates configuration. Errors never include secret values.
func Load() (*Config, error) {
	if err := loadDotEnv(); err != nil {
		return nil, err
	}
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
	if c.EncryptionKeyPrevious, err = previousKeys(); err != nil {
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
	c.SMTP.FailoverHost = get("SMTP_FAILOVER_HOST", "")
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
	c.PublicURL = get("PUBLIC_URL", "")
	c.IngressAddr = get("INGRESS_ADDR", "")
	if c.MetricsToken, err = secret("METRICS_TOKEN"); err != nil {
		return nil, err
	}
	if c.MetricsToken != "" && len(c.MetricsToken) < 16 {
		return nil, fmt.Errorf("METRICS_TOKEN must be at least 16 characters")
	}
	c.InboundListenAddr = get("INBOUND_LISTEN_ADDR", "0.0.0.0:2525")
	c.IMAPHost = get("IMAP_HOST", "")
	if c.IMAPPort, err = intVar("IMAP_PORT", 993); err != nil {
		return nil, err
	}
	c.IMAPUsername = get("IMAP_USERNAME", "")
	if c.IMAPPassword, err = secret("IMAP_PASSWORD"); err != nil {
		return nil, err
	}
	c.IMAPMailbox = get("IMAP_MAILBOX", "INBOX")
	if c.IMAPHost != "" && (c.IMAPUsername == "" || c.IMAPPassword == "") {
		return nil, fmt.Errorf("IMAP_USERNAME and IMAP_PASSWORD are required when IMAP_HOST is set")
	}
	c.InboundDomain = strings.ToLower(get("INBOUND_DOMAIN", ""))
	c.BounceAddress = strings.ToLower(get("BOUNCE_ADDRESS", ""))
	if c.BounceAddress != "" && (strings.Count(c.BounceAddress, "@") != 1 || strings.ContainsAny(c.BounceAddress, " <>\r\n")) {
		return nil, fmt.Errorf("BOUNCE_ADDRESS must be a bare email address")
	}
	switch c.BounceVERP = strings.ToLower(get("BOUNCE_VERP", "auto")); c.BounceVERP {
	case "auto", "on", "off":
	default:
		return nil, fmt.Errorf("BOUNCE_VERP must be auto, on or off")
	}
	c.InboundAllowedSources = get("INBOUND_ALLOWED_SOURCES", "")
	if c.InboundMaxBytes, err = intVar("INBOUND_MAX_BYTES", 10<<20); err != nil {
		return nil, err
	}
	c.DataDir = get("DATA_DIR", "/var/lib/njiwa-mail")
	c.MailHostname = strings.ToLower(get("MAIL_HOSTNAME", ""))
	c.OutboundIP = get("OUTBOUND_IP", "")
	c.DKIMSelector = get("DKIM_SELECTOR", "dkim")
	if c.AllowUnregisteredLiveDomains, err = boolVar("ALLOW_UNREGISTERED_LIVE_DOMAINS"); err != nil {
		return nil, err
	}
	for _, l := range []struct {
		name string
		def  int
		dst  *int
	}{
		{"LIVE_GLOBAL_PER_MINUTE", 600, &c.LiveGlobalPerMinute}, {"LIVE_GLOBAL_PER_HOUR", 20000, &c.LiveGlobalPerHour},
		{"LIVE_DOMAIN_PER_MINUTE", 300, &c.LiveDomainPerMinute}, {"LIVE_DOMAIN_PER_HOUR", 10000, &c.LiveDomainPerHour},
		{"LIVE_QUEUE_MAX", 50000, &c.LiveQueueMax},
	} {
		if *l.dst, err = intVar(l.name, l.def); err != nil {
			return nil, err
		}
		if *l.dst < 0 {
			return nil, fmt.Errorf("%s must be 0 (off) or more", l.name)
		}
	}
	return &c, nil
}

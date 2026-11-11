package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/mailcow"
	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/secure"
)

// Mail server settings live in one row. The SMTP password and the Mailcow API
// key are sealed with the application key and are never returned by the API.
// SMTP settings from the environment (SMTP_HOST, ...) take precedence; the
// stored settings are used when the environment does not set SMTP_HOST.

type MailServerInput struct {
	SMTPHost        string  `json:"smtp_host"`
	SMTPPort        int     `json:"smtp_port"`
	TLSMode         string  `json:"smtp_tls_mode"`
	Username        string  `json:"smtp_username"`
	Password        *string `json:"smtp_password"` // nil or "" keeps the stored one
	TestSender      string  `json:"test_sender"`
	InboundListener string  `json:"inbound_listener"`
	MailcowAPIURL   string  `json:"mailcow_api_url"`
	MailcowAPIKey   *string `json:"mailcow_api_key"`
}

func (s *Service) GetMailServer(ctx context.Context) (map[string]any, error) {
	out, err := s.maps(ctx, `SELECT smtp_host, smtp_port, smtp_tls_mode, smtp_username, smtp_password_enc IS NOT NULL AS smtp_password_set,
  test_sender, inbound_listener, mailcow_api_url, mailcow_api_key_enc IS NOT NULL AS mailcow_api_key_set, last_test, last_test_at, updated_at
FROM mail_server WHERE id=1`)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return map[string]any{"smtp_host": "", "smtp_port": 587, "smtp_tls_mode": "starttls", "smtp_username": "", "smtp_password_set": false,
			"test_sender": "", "inbound_listener": "", "mailcow_api_url": "", "mailcow_api_key_set": false}, nil
	}
	return out[0], nil
}

func (s *Service) SaveMailServer(ctx context.Context, in MailServerInput) error {
	in.SMTPHost = strings.TrimSpace(in.SMTPHost)
	if in.SMTPHost != "" && (strings.ContainsAny(in.SMTPHost, " /:@") || len(in.SMTPHost) > 253) {
		return badRequest("SMTP host must be a host name, without a port or scheme")
	}
	if in.SMTPPort == 0 {
		in.SMTPPort = 587
	}
	if in.SMTPPort < 1 || in.SMTPPort > 65535 {
		return badRequest("port must be between 1 and 65535")
	}
	if in.TLSMode == "" {
		in.TLSMode = "starttls"
	}
	if in.TLSMode != "starttls" && in.TLSMode != "implicit" {
		return badRequest("encryption must be starttls or implicit (plaintext SMTP is not supported)")
	}
	if in.TestSender != "" {
		e, ok := NormalizeEmail(strings.ToLower(strings.TrimSpace(in.TestSender)))
		if !ok {
			return badRequest("test sender must be an email address")
		}
		in.TestSender = e
	}
	if in.InboundListener != "" {
		if _, _, err := net.SplitHostPort(in.InboundListener); err != nil {
			return badRequest("inbound listener must be host:port, for example njiwa-inbound:2525")
		}
	}
	in.MailcowAPIURL = strings.TrimRight(strings.TrimSpace(in.MailcowAPIURL), "/")
	if in.MailcowAPIURL != "" && !strings.HasPrefix(in.MailcowAPIURL, "https://") {
		return badRequest("Mailcow API URL must start with https://")
	}
	var pw, key []byte
	var err error
	if in.Password != nil && *in.Password != "" {
		if pw, err = s.Box.Seal([]byte(*in.Password), []byte("mail_server:smtp_password")); err != nil {
			return unavailable("seal", err)
		}
	}
	if in.MailcowAPIKey != nil && *in.MailcowAPIKey != "" {
		if key, err = s.Box.Seal([]byte(*in.MailcowAPIKey), []byte("mail_server:mailcow_api_key")); err != nil {
			return unavailable("seal", err)
		}
	}
	clearKey := in.MailcowAPIURL == ""
	_, err = s.Pool.Exec(ctx, `INSERT INTO mail_server(id, smtp_host, smtp_port, smtp_tls_mode, smtp_username, smtp_password_enc, test_sender, inbound_listener, mailcow_api_url, mailcow_api_key_enc)
VALUES (1,$1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (id) DO UPDATE SET smtp_host=EXCLUDED.smtp_host, smtp_port=EXCLUDED.smtp_port, smtp_tls_mode=EXCLUDED.smtp_tls_mode,
  smtp_username=EXCLUDED.smtp_username, smtp_password_enc=COALESCE(EXCLUDED.smtp_password_enc, mail_server.smtp_password_enc),
  test_sender=EXCLUDED.test_sender, inbound_listener=EXCLUDED.inbound_listener, mailcow_api_url=EXCLUDED.mailcow_api_url,
  mailcow_api_key_enc=CASE WHEN $10 THEN NULL ELSE COALESCE(EXCLUDED.mailcow_api_key_enc, mail_server.mailcow_api_key_enc) END,
  last_test=NULL, last_test_at=NULL, updated_at=now()`,
		in.SMTPHost, in.SMTPPort, in.TLSMode, strings.TrimSpace(in.Username), pw, in.TestSender, in.InboundListener, in.MailcowAPIURL, key, clearKey)
	if err != nil {
		return unavailable("mail_server_save", err)
	}
	return nil
}

// StoredSMTP returns the stored SMTP settings with the password decrypted.
// ok is false when no host is stored.
func (s *Service) StoredSMTP(ctx context.Context) (cfg mailer.SMTPConfig, sender string, updated time.Time, ok bool, err error) {
	var tlsMode string
	var pwEnc []byte
	err = s.Pool.QueryRow(ctx, `SELECT smtp_host, smtp_port, smtp_tls_mode, smtp_username, smtp_password_enc, test_sender, updated_at FROM mail_server WHERE id=1`).
		Scan(&cfg.Host, &cfg.Port, &tlsMode, &cfg.Username, &pwEnc, &sender, &updated)
	if errors.Is(err, db.ErrNoRows) {
		return cfg, "", updated, false, nil
	}
	if err != nil {
		return cfg, "", updated, false, err
	}
	if cfg.Host == "" {
		return cfg, sender, updated, false, nil
	}
	cfg.Implicit = tlsMode == "implicit"
	if pwEnc != nil {
		pw, err := s.Box.Open(pwEnc, []byte("mail_server:smtp_password"))
		if err != nil {
			return cfg, "", updated, false, err
		}
		cfg.Password = string(pw)
	}
	return cfg, sender, updated, true, nil
}

// RecordMailTest stores the outcome of a connection test or test send.
func (s *Service) RecordMailTest(ctx context.Context, kind string, ok bool, detail any) error {
	b, _ := json.Marshal(map[string]any{"kind": kind, "ok": ok, "detail": detail})
	_, err := s.Pool.Exec(ctx, `INSERT INTO mail_server(id, last_test, last_test_at) VALUES (1,$1,now())
ON CONFLICT (id) DO UPDATE SET last_test=EXCLUDED.last_test, last_test_at=now()`, b)
	return err
}

// TestSendDirect sends one message straight to the mail server (not through
// the queue) and reports the honest outcome.
func (s *Service) TestSendDirect(ctx context.Context, cfg mailer.SMTPConfig, from, to string) (map[string]any, error) {
	f, ok := NormalizeEmail(strings.ToLower(strings.TrimSpace(from)))
	if !ok {
		return nil, badRequest("set a test sender address first")
	}
	t, ok := NormalizeEmail(strings.ToLower(strings.TrimSpace(to)))
	if !ok {
		return nil, badRequest("enter the address to send the test to")
	}
	id := secure.NewUUID()
	raw, err := mailer.Build(mailer.Message{FromName: "Njiwa-Mail", FromEmail: f, To: t, Subject: "Njiwa-Mail test message",
		Text:      "This is a test message from the Njiwa-Mail Mail server page.\n\nIf you can read it, outbound mail works. Check the headers for SPF, DKIM and DMARC pass.",
		MessageID: id + "@" + f[strings.LastIndexByte(f, '@')+1:], DMMessageID: id, Date: time.Now()})
	if err != nil {
		return nil, badRequest("could not build the message")
	}
	envFrom := f
	if s.Opts.BounceAddress != "" {
		envFrom = s.Opts.BounceAddress
	}
	sctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	res := mailer.NewSMTP(cfg).Send(sctx, mailer.Envelope{From: envFrom, To: t, Raw: raw})
	state := map[mailer.Outcome]string{mailer.Accepted: "submitted", mailer.TempFailure: "failed", mailer.PermFailure: "failed", mailer.Unknown: "unknown"}[res.Outcome]
	out := map[string]any{"state": state, "reason": res.Reason, "reply": res.Reply, "to": maskEmail(t)}
	_ = s.RecordMailTest(ctx, "test_send", res.Outcome == mailer.Accepted, out)
	return out, nil
}

// MailServerStatus summarises outbound, inbound and queue state.
func (s *Service) MailServerStatus(ctx context.Context) (map[string]any, error) {
	m, err := s.one(ctx, `SELECT
 (SELECT max(received_at) FROM inbound_spool WHERE kind='inbound') AS last_inbound_at,
 (SELECT max(received_at) FROM inbound_spool WHERE kind='bounce') AS last_bounce_at,
 (SELECT count(*) FROM messages WHERE state IN ('queued','submitting')) AS queue_depth,
 (SELECT count(*) FROM messages WHERE state='retry_wait') AS retry_wait,
 (SELECT count(*) FROM messages WHERE state='unknown') AS unknown,
 (SELECT max(submitted_at) FROM messages) AS last_submitted_at,
 (SELECT last_test FROM mail_server WHERE id=1) AS last_test,
 (SELECT last_test_at FROM mail_server WHERE id=1) AS last_test_at,
 (SELECT inbound_listener FROM mail_server WHERE id=1) AS inbound_listener`)
	if err != nil {
		return nil, err
	}
	m["inbound_reachable"] = nil
	if l, _ := m["inbound_listener"].(string); l != "" {
		c, err := net.DialTimeout("tcp", l, 3*time.Second)
		m["inbound_reachable"] = err == nil
		if err == nil {
			c.Close()
		}
	}
	m["inbound_domain"], m["bounce_address"] = s.Opts.InboundDomain, s.Opts.BounceAddress
	return m, nil
}

// ---------------------------------------------------------------- Mailcow API

func (s *Service) mailcowClient(ctx context.Context) (*mailcow.Client, error) {
	var u string
	var enc []byte
	err := s.Pool.QueryRow(ctx, `SELECT mailcow_api_url, mailcow_api_key_enc FROM mail_server WHERE id=1`).Scan(&u, &enc)
	if err != nil || u == "" || enc == nil {
		return nil, newErr(409, "mailcow_api_not_configured")
	}
	key, err := s.Box.Open(enc, []byte("mail_server:mailcow_api_key"))
	if err != nil {
		return nil, unavailable("mailcow_key", err)
	}
	return mailcow.New(u, string(key)), nil
}

func (s *Service) MailcowConfigured(ctx context.Context) bool {
	_, err := s.mailcowClient(ctx)
	return err == nil
}

func (s *Service) MailcowPing(ctx context.Context) error {
	c, err := s.mailcowClient(ctx)
	if err != nil {
		return err
	}
	return c.Ping(ctx)
}

func (s *Service) mailcowDKIM(ctx context.Context, domain string) (string, error) {
	c, err := s.mailcowClient(ctx)
	if err != nil {
		return "", err
	}
	return c.GetDKIM(ctx, domain)
}

// ProvisionDomain uses the Mailcow API to add the domain, create its DKIM
// key and, for receiving domains, add the relay transport to the inbound
// listener. Each step is reported separately; a step that fails does not
// undo earlier ones (each is idempotent on Mailcow's side or reports "exists").
func (s *Service) ProvisionDomain(ctx context.Context, sc *Scope, id string) ([]map[string]any, error) {
	c, err := s.mailcowClient(ctx)
	if err != nil {
		return nil, &Error{Status: 409, Code: "mailcow_api_not_configured", Message: "set the Mailcow API URL and key on the Mail server page, or follow the manual steps"}
	}
	var name, purpose, listener string
	if err := s.Pool.QueryRow(ctx, `SELECT name, purpose FROM domains WHERE id=$1 AND project_id=$2`, id, sc.ProjectID).Scan(&name, &purpose); err != nil {
		return nil, notFound(err)
	}
	_ = s.Pool.QueryRow(ctx, `SELECT inbound_listener FROM mail_server WHERE id=1`).Scan(&listener)
	var steps []map[string]any
	step := func(key, title string, err error) bool {
		st := map[string]any{"key": key, "title": title, "ok": err == nil}
		if err != nil {
			st["detail"] = err.Error()
		}
		steps = append(steps, st)
		return err == nil
	}
	relay := receivingPurpose(purpose) && !sendingPurpose(purpose)
	if !step("domain", "Add domain to Mailcow", ignoreExists(c.AddDomain(ctx, name, relay))) {
		return steps, nil
	}
	if sendingPurpose(purpose) {
		if step("dkim", "Create DKIM key", ignoreExists(c.AddDKIM(ctx, name, s.Opts.DKIMSelector))) {
			txt, err := c.GetDKIM(ctx, name)
			if err == nil && txt == "" {
				err = errors.New("Mailcow returned no DKIM key")
			}
			step("dkim_read", "Read DKIM key", err)
		}
	}
	if receivingPurpose(purpose) {
		if listener == "" {
			step("transport", "Add relay transport to Njiwa-Mail", errors.New("set the inbound listener address on the Mail server page first"))
		} else {
			step("transport", "Add relay transport to Njiwa-Mail", ignoreExists(c.AddTransport(ctx, name, "["+strings.Split(listener, ":")[0]+"]:"+listenerPort(listener))))
		}
	}
	allOK := true
	for _, st := range steps {
		allOK = allOK && st["ok"].(bool)
	}
	if allOK {
		_, _ = s.Pool.Exec(ctx, `UPDATE domains SET mailcow_provisioned_at=now() WHERE id=$1`, id)
	}
	return steps, nil
}

func listenerPort(l string) string {
	_, p, err := net.SplitHostPort(l)
	if err != nil {
		return "2525"
	}
	return p
}

func ignoreExists(err error) error {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "exist") {
		return nil
	}
	return err
}

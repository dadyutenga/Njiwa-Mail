package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/mailer"
)

// SMTPConnectInput contains only outbound settings. Advanced and inbound
// settings already saved on the installation are retained.
type SMTPConnectInput struct {
	Host         string  `json:"smtp_host"`
	Username     string  `json:"smtp_username"`
	Password     *string `json:"smtp_password"`
	ProviderType *string `json:"provider_type"`
	Port         *int    `json:"smtp_port"`
	TLSMode      *string `json:"smtp_tls_mode"`
	Sender       *string `json:"test_sender"`
}

// PrepareSMTPConnection creates a candidate without changing a working
// installation. The API tests it before saving or selecting its profile.
func (s *Service) PrepareSMTPConnection(ctx context.Context, in SMTPConnectInput) (MailServerInput, mailer.SMTPConfig, error) {
	var cfg mailer.SMTPConfig
	var saved MailServerInput
	active, err := s.LoadActiveSMTP(ctx)
	if err != nil {
		return saved, cfg, err
	}
	if active.Mode == "managed_mta" && active.OK {
		return saved, cfg, &Error{Status: 409, Code: "mode_switch_requires_plan", Message: "An active managed mail server must be changed through the transport switch planner in Advanced Settings."}
	}
	current, err := s.GetMailServer(ctx)
	if err != nil {
		return saved, cfg, err
	}
	raw, _ := json.Marshal(current)
	if err := json.Unmarshal(raw, &saved); err != nil {
		return saved, cfg, unavailable("smtp_settings", err)
	}
	saved.SMTPHost, saved.Username = strings.TrimSpace(in.Host), strings.TrimSpace(in.Username)
	if saved.SMTPHost == "" || saved.Username == "" {
		return saved, cfg, badRequest("Enter the SMTP hostname and mailbox username.")
	}
	saved.SMTPPort, saved.TLSMode = 587, "starttls"
	if in.Port != nil {
		saved.SMTPPort = *in.Port
	}
	if in.TLSMode != nil {
		saved.TLSMode = *in.TLSMode
	}
	if in.ProviderType != nil {
		saved.ProviderType = in.ProviderType
	}
	if saved.ProviderType == nil {
		provider := "generic"
		saved.ProviderType = &provider
	}
	// The mailbox itself is the default authorized identity. Account-name
	// logins can supply a different sender in the advanced overrides.
	saved.TestSender = saved.Username
	if in.Sender != nil && strings.TrimSpace(*in.Sender) != "" {
		saved.TestSender = strings.TrimSpace(*in.Sender)
	}
	if _, ok := NormalizeEmail(saved.TestSender); !ok {
		return saved, cfg, badRequest("Use your full mailbox address as the username, or set an authorized sender address in Advanced Settings.")
	}
	password := ""
	if in.Password != nil {
		password = *in.Password
	}
	if password == "" && active.Cfg.Host == saved.SMTPHost && active.Cfg.Username == saved.Username {
		password = active.Cfg.Password
	}
	if password == "" {
		return saved, cfg, badRequest("Enter the mailbox password. A saved password can only be reused for the same host and username.")
	}
	saved.Password = &password
	if err := validateSMTPInput(&saved); err != nil {
		return saved, cfg, err
	}
	cfg = mailer.SMTPConfig{Host: saved.SMTPHost, Port: saved.SMTPPort, Username: saved.Username,
		Password: password, Implicit: saved.TLSMode == "implicit", Timeout: 20 * time.Second}
	return saved, cfg, nil
}

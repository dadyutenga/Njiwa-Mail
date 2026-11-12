// Package cli runs the transactional email service and operator commands.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/njiwa-tz/mail/internal/api"
	"github.com/njiwa-tz/mail/internal/config"
	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/dnsauth"
	"github.com/njiwa-tz/mail/internal/inbound"
	"github.com/njiwa-tz/mail/internal/installer/managed"
	"github.com/njiwa-tz/mail/internal/installer/preflight"
	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/secure"
	"github.com/njiwa-tz/mail/internal/service"
	"github.com/njiwa-tz/mail/internal/transport"
	"github.com/njiwa-tz/mail/internal/webhook"
	"github.com/njiwa-tz/mail/internal/worker"
)

const usage = `njiwa-mail <command>

Runtime:
  serve                       run the HTTP API (application API + admin API)
  worker                      run the dispatch worker and the webhook delivery worker
  inbound                     run the private SMTP listener and the inbound/bounce parser
  scheduler                   run periodic jobs: retention, lease recovery, DNS checks, bounce-rate alerts
  migrate                     apply database migrations and seed the built-in templates
  worker-health [-max 60s]    exit non-zero if no worker heartbeat (for container healthchecks)
  role-health -role R [-max 60s]   same, for worker | inbound | scheduler
  dns-check                   run the read-only DNS health check now and print the results
  dkim-verify [-file message.eml|-]
                              verify every DKIM signature on a received message (needs DNS, not the database)
  mta-log [-file /var/log/mail.log|-] [-follow]
                              record delivery evidence (sent, deferred, bounced) from Postfix log lines,
                              correlated to messages by queue ID; "-" reads stdin (journalctl -fu postfix | ...)
  smtp-check                  verify TLS + auth against the SMTP server (sends nothing)
  preflight [-hostname H -ip IP -spool DIR -probe DOMAIN -json -record]
                              read-only host checks for "Send from my VPS" (managed mode); changes nothing.
                              -record stores the result for the Delivery infrastructure page (needs the database)
  installer plan|apply|rollback|status   (root, on the mail host; managed mode only)
       [-hostname H -domains a.example,b.example -bounce-domain D -selector S
        -submission 127.0.0.1:10587 -inbound 127.0.0.1:2525 -compose-subnet CIDR -no-firewall
        -adopt -approve DIGEST -record]
                              plan prints the exact changes and a digest; apply needs -approve with that digest,
                              backs up, validates (postfix check, opendkim -n) and rolls back on failure
  installer dkim-rotate prepare|activate|cancel|retire|status -selector S   (staged DKIM key rotation)
  retention                   run the retention job once
  attachments encrypt         seal inbound attachment files written before encryption at rest (safe to repeat)

Provisioning (operator CLI only):
  project create -name N
  project quotas -name N [-per-minute n -per-day n -rcpt-per-minute n -rcpt-per-hour n]
  project enable|disable -name N
  env origins   -project N -mode sandbox|live -origin https://app.example.com [-origin ...]
  env allow     -project N -mode live -pattern user@x.com|@domain.com [-remove]
  env restrict  -project N -mode live -on=true|false
  sender add    -project N -mode M -email addr [-display "Name"]
  key create    -project N -mode M [-scopes emails:send,emails:read] [-label L] [-ttl 2160h]
  key rotate    -prefix P [-grace 24h]
  key revoke    -prefix P
  key list      [-project N]
  suppress add|remove -project N -mode M -email addr [-reason R]
  user create   -email E [-role admin] [-operator=false]   (password read from stdin; the first user is created here;
                -operator=false makes a restricted admin without installation-level access)
  user password -email E                            (new password read from stdin)
  user disable|enable -email E
  user operator -email E [-on=true|false]   (grant or remove the installation-operator permission)
  user mfa-reset -email E                   (lost authenticator and recovery codes; ends the user's sessions)

Operations:
  message list    [-state unknown] [-limit 50]
  message resolve -id ID -as submitted|failed -note "mailcow queue id / log ref"
  capture show    -id ID       (sandbox capture, operator only)
`

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func die(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

// Run executes a runtime or operator command.
func Run(args []string) {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	// The preflight runs on a fresh host, before a database or .env exists;
	// only -record needs the full configuration.
	if args[0] == "preflight" && !hasFlag(args[1:], "record") {
		runPreflight(context.Background(), nil, args[1:])
		return
	}
	if args[0] == "dkim-verify" {
		runDKIMVerify(args[1:])
		return
	}
	if args[0] == "installer" && !hasFlag(args[1:], "record") {
		runInstaller(context.Background(), nil, args[1:])
		return
	}
	cfg, err := config.Load()
	if err != nil {
		die(err)
	}
	lvl := slog.LevelInfo
	if cfg.LogLevel == "debug" {
		lvl = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		die(err)
	}
	defer pool.Close()
	box, err := secure.New(cfg.EncryptionKey, cfg.HMACKey)
	if err != nil {
		die(err)
	}
	svc := service.New(pool, box, service.Options{MetadataRetention: cfg.MetadataRetention, CaptureRetention: cfg.CaptureRetention,
		UnknownPurge: cfg.UnknownPurge, MaxAttempts: cfg.MaxAttempts,
		InboundDomain: cfg.InboundDomain, BounceAddress: cfg.BounceAddress, DataDir: cfg.DataDir,
		MailHost: cfg.MailHostname, OutboundIP: cfg.OutboundIP, DKIMSelector: cfg.DKIMSelector, SessionTTL: cfg.AdminSessionTTL,
		PublicURL: cfg.PublicURL, AllowUnregisteredLiveDomains: cfg.AllowUnregisteredLiveDomains,
		LiveGlobalPerMinute: cfg.LiveGlobalPerMinute, LiveGlobalPerHour: cfg.LiveGlobalPerHour,
		LiveDomainPerMinute: cfg.LiveDomainPerMinute, LiveDomainPerHour: cfg.LiveDomainPerHour, LiveQueueMax: cfg.LiveQueueMax})
	if cfg.AllowUnregisteredLiveDomains {
		slog.Warn("ALLOW_UNREGISTERED_LIVE_DOMAINS is on: Live mail may use sending domains that no project has registered. Register and verify every sending domain, then turn this off.")
	}

	cmd, args := args[0], args[1:]
	switch cmd {
	case "migrate":
		if err := db.Migrate(ctx, pool); err != nil {
			die(err)
		}
		if err := svc.SeedTemplates(ctx); err != nil {
			die(err)
		}
		if e := envSMTP(cfg); e != nil {
			// Make an environment-provided SMTP setting visible in the profile
			// registry. It never overwrites a profile and never changes precedence.
			if err := svc.AdoptEnvSMTP(ctx, *e, ""); err != nil {
				die(err)
			}
		}
		fmt.Println("migrations applied")
	case "serve":
		serve(ctx, cfg, svc)
	case "worker":
		runWorker(ctx, cfg, svc)
	case "inbound":
		runInbound(ctx, cfg, svc)
	case "scheduler":
		runScheduler(ctx, cfg, svc)
	case "role-health":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		role := fs.String("role", "worker", "worker, inbound or scheduler")
		max := fs.Duration("max", 60*time.Second, "maximum heartbeat age")
		fs.Parse(args)
		ok, err := svc.RoleHealthy(ctx, *role, *max)
		if err != nil || !ok {
			fmt.Fprintln(os.Stderr, *role, "unhealthy")
			os.Exit(1)
		}
		fmt.Println(*role, "healthy")
	case "installer":
		runInstaller(ctx, &installerDeps{svc: svc, cfg: cfg}, args)
	case "preflight":
		runPreflight(ctx, &preflightDeps{svc: svc, hostname: cfg.MailHostname, ip: cfg.OutboundIP, spool: cfg.DataDir}, args)
	case "mta-log":
		runMTALog(ctx, svc, args)
	case "dns-check":
		res, err := svc.RunDNSChecks(ctx, nil)
		if err != nil {
			die(err)
		}
		failed := 0
		for _, r := range res {
			st := "PASS"
			if !r.Pass {
				st, failed = "FAIL", failed+1
			}
			fmt.Printf("%s  %-5s %s\n      expected: %s\n      observed: %s\n", st, r.Type, r.Domain, r.Expected, r.Observed)
		}
		if failed > 0 {
			os.Exit(1)
		}
	case "worker-health":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		max := fs.Duration("max", 60*time.Second, "maximum heartbeat age")
		fs.Parse(args)
		ok, err := svc.WorkerHealthy(ctx, *max)
		if err != nil || !ok {
			fmt.Fprintln(os.Stderr, "worker unhealthy")
			os.Exit(1)
		}
		fmt.Println("worker healthy")
	case "smtp-check":
		if !cfg.SMTP.Configured() {
			die(errors.New("SMTP_HOST is not configured"))
		}
		if err := smtpTransport(cfg).Check(ctx); err != nil {
			die(err)
		}
		fmt.Println("smtp ok: verified TLS and authenticated (no message sent)")
	case "retention":
		r, err := svc.RunRetention(ctx)
		if err != nil {
			die(err)
		}
		fmt.Printf("captures=%d messages=%d payloads=%d buckets=%d delivery_events=%d installer_receipts=%d attachments_sealed=%d\n",
			r.Captures, r.Messages, r.Payloads, r.Buckets, r.DeliveryEvents, r.Receipts, r.AttachmentsSealed)
	case "attachments":
		if len(args) == 0 || args[0] != "encrypt" {
			die(errors.New("usage: attachments encrypt"))
		}
		total := 0
		for {
			n, err := svc.EncryptLegacyAttachments(ctx, 500)
			if err != nil {
				die(err)
			}
			total += n
			if n == 0 {
				break
			}
		}
		fmt.Printf("sealed %d attachment files\n", total)
	case "project", "env", "sender", "key", "suppress", "message", "capture", "user":
		if err := admin(ctx, svc, cmd, args); err != nil {
			die(err)
		}
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

// envSMTP is the SMTP configuration from the environment, for the Mail server
// page's connection test; nil when SMTP_HOST is not set.
func envSMTP(cfg *config.Config) *mailer.SMTPConfig {
	if !cfg.SMTP.Configured() {
		return nil
	}
	return &mailer.SMTPConfig{Host: cfg.SMTP.Host, Port: cfg.SMTP.Port, Username: cfg.SMTP.Username, Password: cfg.SMTP.Password,
		Implicit: cfg.SMTP.Implicit, HeloName: cfg.SMTP.HeloName, Timeout: cfg.SMTP.Timeout}
}

func smtpTransport(cfg *config.Config) *mailer.SMTP {
	primary := mailer.NewSMTP(mailer.SMTPConfig{Host: cfg.SMTP.Host, Port: cfg.SMTP.Port, Username: cfg.SMTP.Username,
		Password: cfg.SMTP.Password, Implicit: cfg.SMTP.Implicit, HeloName: cfg.SMTP.HeloName, Timeout: cfg.SMTP.Timeout})
	if cfg.SMTP.FailoverHost != "" {
		primary.SetFailover(mailer.NewSMTP(mailer.SMTPConfig{Host: cfg.SMTP.FailoverHost, Port: cfg.SMTP.Port, Username: cfg.SMTP.Username,
			Password: cfg.SMTP.Password, Implicit: cfg.SMTP.Implicit, HeloName: cfg.SMTP.HeloName, Timeout: cfg.SMTP.Timeout}))
	}
	return primary
}

func serve(ctx context.Context, cfg *config.Config, svc *service.Service) {
	app := (&api.Server{
		Svc: svc, MaxBodyBytes: cfg.MaxBodyBytes, EmailMaxBytes: 6 << 20,
		ConsolePassword: cfg.ConsolePassword, SessionTTL: cfg.ConsoleSessionTTL,
		SMTPConfigured: cfg.SMTP.Configured(),
		EnvSMTP:        envSMTP(cfg),
		TrustProxy:     cfg.TrustProxy, InsecureCookies: cfg.InsecureCookies, MetricsToken: cfg.MetricsToken,
	}).App()
	errCh := make(chan error, 1)
	go func() { errCh <- app.Listen(cfg.ListenAddr) }()
	select {
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = app.ShutdownWithContext(sctx)
		<-errCh
	case err := <-errCh:
		if err != nil {
			die(err)
		}
	}
}

func runWorker(ctx context.Context, cfg *config.Config, svc *service.Service) {
	w := &worker.Worker{Svc: svc, ID: hostID(), Poll: cfg.WorkerPoll, Batch: cfg.WorkerBatch, Concurrency: cfg.WorkerConcurrency,
		Lease: cfg.LeaseDuration, SendTimeout: cfg.SMTP.Timeout + 5*time.Second, RetentionInterval: cfg.RetentionInterval,
		BounceAddress: cfg.BounceAddress}
	// Webhook deliveries share the worker role: one process drains both outboxes.
	go (&webhook.Deliverer{Svc: svc}).Run(ctx)
	if cfg.SMTP.Configured() {
		w.Transport = &transport.ExistingSMTPTransport{SMTP: smtpTransport(cfg)}
	} else {
		w.Transport = &worker.StoredTransport{Svc: svc, Timeout: cfg.SMTP.Timeout}
		slog.Warn("SMTP_HOST not set: live mail uses the settings saved on the Mail server page; until they are saved, live messages stay queued")
	}
	w.Run(ctx)
}

// runInbound serves the private SMTP listener and parses what it spools.
func runInbound(ctx context.Context, cfg *config.Config, svc *service.Service) {
	if cfg.InboundDomain == "" && cfg.BounceAddress == "" && cfg.IMAPHost == "" {
		die(errors.New("set INBOUND_DOMAIN, BOUNCE_ADDRESS, or IMAP_HOST to run the inbound role"))
	}
	sources, err := inbound.ParseSources(cfg.InboundAllowedSources)
	if err != nil {
		die(err)
	}
	id := hostID()
	go beat(ctx, svc, "inbound", id)
	go (&inbound.Processor{Svc: svc}).Run(ctx)
	if cfg.IMAPHost != "" {
		go inbound.PollLoop(ctx, inbound.IMAPConfig{Host: cfg.IMAPHost, Port: cfg.IMAPPort, Username: cfg.IMAPUsername,
			Password: cfg.IMAPPassword, Mailbox: cfg.IMAPMailbox}, svc)
	}
	if len(sources) == 0 {
		if cfg.IMAPHost == "" {
			die(errors.New("INBOUND_ALLOWED_SOURCES is required when the SMTP listener is used"))
		}
		<-ctx.Done()
		return
	}
	srv := &inbound.Server{Svc: svc, Addr: cfg.InboundListenAddr, AllowedSources: sources, Hostname: cfg.MailHostname, MaxBytes: int64(cfg.InboundMaxBytes)}
	if err := srv.ListenAndServe(ctx); err != nil {
		die(err)
	}
}

func beat(ctx context.Context, svc *service.Service, role, id string) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		if err := svc.HeartbeatRole(ctx, role, id); err != nil && ctx.Err() == nil {
			slog.Error("heartbeat failed", "role", role, "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// runScheduler runs the periodic jobs. Every job is idempotent, so more than
// one scheduler (or a worker doing the same lease recovery) is harmless.
func runScheduler(ctx context.Context, cfg *config.Config, svc *service.Service) {
	go beat(ctx, svc, "scheduler", hostID())
	slog.Info("scheduler started")
	every := func(d time.Duration, name string, fn func() error) {
		go func() {
			t := time.NewTicker(d)
			defer t.Stop()
			for {
				if err := fn(); err != nil && ctx.Err() == nil {
					slog.Error("scheduled job failed", "job", name, "err", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
			}
		}()
	}
	every(30*time.Second, "scheduled_send", func() error {
		_, err := svc.PromoteScheduled(ctx)
		return err
	})
	every(30*time.Second, "lease_recovery", func() error {
		if n, err := svc.ReapLeases(ctx); err != nil {
			return err
		} else if n > 0 {
			slog.Warn("leases expired; outcomes unknown", "count", n)
		}
		_, err := svc.ExpireDue(ctx)
		return err
	})
	every(cfg.RetentionInterval, "retention", func() error {
		r, err := svc.RunRetention(ctx)
		if err == nil {
			slog.Info("retention run", "captures", r.Captures, "messages", r.Messages, "payloads", r.Payloads, "inbound", r.Inbound, "deliveries", r.Deliveries,
				"delivery_events", r.DeliveryEvents, "installer_receipts", r.Receipts, "attachments_sealed", r.AttachmentsSealed)
		}
		return err
	})
	every(15*time.Second, "bulk_campaigns", func() error {
		_, err := svc.PumpCampaigns(ctx)
		return err
	})
	every(5*time.Minute, "temporary_inboxes", func() error { return svc.ExpireTempInboxes(ctx) })
	every(10*time.Minute, "domain_recheck", func() error {
		_, err := svc.RecheckPendingDomains(ctx, nil)
		return err
	})
	every(5*time.Minute, "bounce_rate_alerts", func() error { return svc.CheckBounceRates(ctx) })
	every(24*time.Hour, "daily_summary", func() error {
		body, recipients, err := svc.DailySummary(ctx)
		if err != nil {
			return err
		}
		slog.Info("daily summary", "recipients", len(recipients), "bytes", len(body))
		return nil
	})
	every(time.Hour, "dns_checks", func() error { // runs the check when the last one is a day old
		due, err := svc.DNSCheckDue(ctx, 24*time.Hour)
		if err != nil || !due {
			return err
		}
		res, err := svc.RunDNSChecks(ctx, nil)
		for _, r := range res {
			if !r.Pass {
				slog.Warn("dns check failed", "domain", r.Domain, "type", r.Type)
			}
		}
		return err
	})
	<-ctx.Done()
	slog.Info("scheduler stopped")
}

// readPassword reads one line from stdin (pipe it in, or type it and press Enter).
func readPassword() (string, error) {
	fmt.Fprint(os.Stderr, "password (input is read from stdin): ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("no password supplied on stdin")
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func hostID() string {
	h, _ := os.Hostname()
	return fmt.Sprintf("%s-%d", h, os.Getpid())
}

// admin runs an operator command and records every write in the audit log.
func admin(ctx context.Context, svc *service.Service, group string, args []string) error {
	if err := adminCmd(ctx, svc, group, args); err != nil {
		return err
	}
	if len(args) > 0 && args[0] != "list" && args[0] != "show" {
		target := ""
		for i, a := range args { // identify the target without recording secrets or full recipient addresses
			if (a == "-name" || a == "-project" || a == "-prefix" || a == "-id") && i+1 < len(args) {
				target = args[i+1]
				break
			}
		}
		return svc.Audit(ctx, service.CLIActor, "cli."+group+"."+args[0], group, target, nil)
	}
	return nil
}

func adminCmd(ctx context.Context, svc *service.Service, group string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing subcommand for %s\n\n%s", group, usage)
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet(group+" "+sub, flag.ContinueOnError)
	var (
		name     = fs.String("name", "", "")
		project  = fs.String("project", "", "")
		mode     = fs.String("mode", "", "")
		email    = fs.String("email", "", "")
		display  = fs.String("display", "", "")
		reason   = fs.String("reason", "", "")
		pattern  = fs.String("pattern", "", "")
		remove   = fs.Bool("remove", false, "")
		on       = fs.Bool("on", true, "")
		scopes   = fs.String("scopes", "emails:send,emails:read", "")
		label    = fs.String("label", "", "")
		ttl      = fs.Duration("ttl", 0, "")
		grace    = fs.Duration("grace", 24*time.Hour, "")
		prefix   = fs.String("prefix", "", "")
		id       = fs.String("id", "", "")
		as       = fs.String("as", "", "")
		note     = fs.String("note", "", "")
		state    = fs.String("state", "", "")
		limit    = fs.Int("limit", 50, "")
		perMin   = fs.Int("per-minute", 0, "")
		perDay   = fs.Int("per-day", 0, "")
		rcptMin  = fs.Int("rcpt-per-minute", 0, "")
		rcptHour = fs.Int("rcpt-per-hour", 0, "")
		role     = fs.String("role", service.RoleAdmin, "admin")
		operator = fs.Bool("operator", true, "installation-operator permission for a new user")
		origins  multi
	)
	fs.Var(&origins, "origin", "")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	key := group + " " + sub
	switch key {
	case "project create":
		if err := svc.CreateProject(ctx, *name); err != nil {
			return err
		}
		fmt.Printf("project %q created with sandbox (unrestricted, capture-only) and live (recipient-restricted) environments\n", *name)
	case "project quotas":
		return svc.SetQuotas(ctx, *name, *perMin, *perDay, *rcptMin, *rcptHour)
	case "project enable":
		return svc.SetProjectEnabled(ctx, *name, true)
	case "project disable":
		return svc.SetProjectEnabled(ctx, *name, false)
	case "env origins":
		return svc.SetOrigins(ctx, *project, *mode, origins)
	case "env allow":
		return svc.AllowRecipient(ctx, *project, *mode, *pattern, *remove)
	case "env restrict":
		return svc.SetRestrictRecipients(ctx, *project, *mode, *on)
	case "sender add":
		return svc.AddSender(ctx, *project, *mode, *email, *display)
	case "key create":
		tok, pre, err := svc.CreateKey(ctx, *project, *mode, strings.Split(*scopes, ","), *label, *ttl)
		if err != nil {
			return err
		}
		fmt.Printf("prefix: %s\nkey (shown once, store it in a secret manager):\n%s\n", pre, tok)
	case "key rotate":
		tok, pre, err := svc.RotateKey(ctx, *prefix, *grace)
		if err != nil {
			return err
		}
		fmt.Printf("old key %s now expires in %s\nnew prefix: %s\nnew key (shown once):\n%s\n", *prefix, *grace, pre, tok)
	case "key revoke":
		return svc.RevokeKey(ctx, *prefix)
	case "key list":
		ks, err := svc.ListKeys(ctx, *project)
		if err != nil {
			return err
		}
		for _, k := range ks {
			st := "active"
			if k.Revoked != nil {
				st = "revoked"
			} else if k.Expires != nil {
				st = "expires " + k.Expires.Format(time.RFC3339)
			}
			fmt.Printf("%s  %s/%s  %v  %s  %s\n", k.Prefix, k.Project, k.Mode, k.Scopes, st, k.Label)
		}
	case "suppress add":
		return svc.Suppress(ctx, *project, *mode, *email, *reason)
	case "suppress remove":
		return svc.Unsuppress(ctx, *project, *mode, *email)
	case "message list":
		rows, err := svc.ListMessages(ctx, *state, *limit)
		if err != nil {
			return err
		}
		for _, r := range rows {
			fmt.Printf("%s  %-10s %-15s %s/%s attempts=%d %s\n", r.ID, r.State, r.Template, r.Project, r.Mode, r.Attempts, r.Created.Format(time.RFC3339))
		}
	case "message resolve":
		return svc.ResolveUnknown(ctx, *id, *as, *note)
	case "user create":
		pw, err := readPassword()
		if err != nil {
			return err
		}
		id, err := svc.CreateUserWith(ctx, *email, pw, *role, *operator)
		if err != nil {
			return cliErr(err)
		}
		fmt.Printf("user %s created with role %s (id %s)\n", strings.ToLower(*email), service.RoleAdmin, id)
	case "user operator", "user mfa-reset":
		id, err := svc.UserIDByEmail(ctx, *email)
		if err != nil {
			return cliErr(err)
		}
		if sub == "mfa-reset" {
			return cliErr(svc.ResetMFA(ctx, service.CLIActor, id))
		}
		return cliErr(svc.SetOperator(ctx, id, *on))
	case "user password", "user disable", "user enable":
		id, err := svc.UserIDByEmail(ctx, *email)
		if err != nil {
			return cliErr(err)
		}
		switch sub {
		case "password":
			pw, err := readPassword()
			if err != nil {
				return err
			}
			return cliErr(svc.UpdateUser(ctx, id, nil, nil, &pw))
		case "disable":
			t := true
			return cliErr(svc.UpdateUser(ctx, id, nil, &t, nil))
		default:
			f := false
			return cliErr(svc.UpdateUser(ctx, id, nil, &f, nil))
		}
	case "capture show":
		raw, err := svc.ShowCapture(ctx, *id)
		if err != nil {
			return err
		}
		os.Stdout.Write(raw)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", key, usage)
	}
	return nil
}

// cliErr shows the operator-facing message of a service error.
func cliErr(err error) error {
	if e, ok := service.IsError(err); ok && e.Message != "" {
		return errors.New(e.Message)
	}
	return err
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		a = strings.TrimLeft(a, "-")
		if a == name || strings.HasPrefix(a, name+"=") {
			return true
		}
	}
	return false
}

type preflightDeps struct {
	svc                 *service.Service
	hostname, ip, spool string
}

// runPreflight prints the managed-mode host checks and exits non-zero unless
// every required one is ready. With -record (and deps) it stores the result.
func runPreflight(ctx context.Context, deps *preflightDeps, args []string) {
	fs := flag.NewFlagSet("preflight", flag.ExitOnError)
	var defHost, defIP, defSpool string
	if deps != nil {
		defHost, defIP, defSpool = deps.hostname, deps.ip, deps.spool
	}
	hostname := fs.String("hostname", firstNonEmpty(defHost, os.Getenv("MAIL_HOSTNAME")), "public mail hostname (PTR and HELO name)")
	ip := fs.String("ip", firstNonEmpty(defIP, os.Getenv("OUTBOUND_IP")), "public IP mail is sent from")
	spool := fs.String("spool", firstNonEmpty(defSpool, "/var"), "directory whose disk holds the mail queue")
	probe := fs.String("probe", "gmail.com", "domain whose MX is used to test outbound port 25 (no mail is sent)")
	asJSON := fs.Bool("json", false, "print JSON")
	record := fs.Bool("record", false, "store the result for the Delivery infrastructure page")
	fs.Parse(args)

	checks := preflight.Run(ctx, preflight.HostEnv(), preflight.Params{Hostname: *hostname, IP: *ip, SpoolDir: *spool, ProbeMX: *probe})
	ok, first := preflight.Summary(checks)
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"ready": ok, "checks": checks})
	} else {
		for _, c := range checks {
			fmt.Printf("%-46s %-24s %s\n", c.Title, strings.ToUpper(string(c.Status)), c.Detail)
			if c.Status != preflight.Ready && c.Fix != "" {
				fmt.Printf("%-46s %-24s fix: %s\n", "", "", c.Fix)
			}
		}
		if ok {
			fmt.Println("\nThis host is ready for managed delivery.")
		} else {
			fmt.Println("\nNot ready: fix the items above and run the preflight again.")
		}
	}
	if *record && deps != nil {
		summary := map[string]any{}
		for _, c := range checks {
			summary[c.Key] = string(c.Status)
		}
		code, external := "", false
		if first != nil {
			code, external = first.Code, first.Status == preflight.BlockedExternalAction
		}
		if err := deps.svc.RecordManagedPreflight(ctx, ok, code, external, summary); err != nil {
			die(err)
		}
		fmt.Println("Result recorded.")
	}
	if !ok {
		os.Exit(1)
	}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

type installerDeps struct {
	svc *service.Service
	cfg *config.Config
}

type execRunner struct{}

func (execRunner) Run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// runInstaller is the privileged, operator-run managed-mode installer. It is
// never reachable from the HTTP API.
func runInstaller(ctx context.Context, deps *installerDeps, args []string) {
	if len(args) == 0 {
		die(errors.New("usage: njiwa-mail installer plan|apply|rollback|status [flags]"))
	}
	sub, args := args[0], args[1:]
	action := "" // dkim-rotate's own sub-command, taken before the flags
	if sub == "dkim-rotate" && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("installer "+sub, flag.ExitOnError)
	def := func(env, fallback string) string { return firstNonEmpty(os.Getenv(env), fallback) }
	bounceDomain := ""
	if _, d, ok := strings.Cut(os.Getenv("BOUNCE_ADDRESS"), "@"); ok {
		bounceDomain = d
	}
	hostname := fs.String("hostname", def("MAIL_HOSTNAME", ""), "public mail hostname (HELO, PTR, certificate)")
	domains := fs.String("domains", "", "comma-separated sending domains to sign (default with -record: the installation's claimed domains)")
	bounce := fs.String("bounce-domain", bounceDomain, "domain of BOUNCE_ADDRESS; DSNs to it are relayed to Njiwa")
	selector := fs.String("selector", def("DKIM_SELECTOR", "dkim"), "DKIM selector")
	submission := fs.String("submission", "127.0.0.1:10587", "private listener for the Njiwa worker (ip:port, never 0.0.0.0)")
	inbound := fs.String("inbound", "127.0.0.1:2525", "Njiwa inbound listener that receives DSNs (host:port)")
	adopt := fs.Bool("adopt", false, "replace an existing Postfix configuration not written by Njiwa-Mail (backed up first)")
	noFirewall := fs.Bool("no-firewall", false, "do not install the nftables table that drops Njiwa's private ports from outside")
	renewCerts := fs.Bool("renew-certs", false, "reissue the submission server and worker certificates now (otherwise done automatically within 60 days of expiry)")
	subnet := fs.String("compose-subnet", firstNonEmpty(os.Getenv("NJIWA_SUBNET"), "172.30.0.0/24"), "Docker network the worker connects from")
	approve := fs.String("approve", "", "apply only if the plan digest equals this value")
	receipt := fs.String("receipt", "", "rollback: receipt id (default: the latest)")
	record := fs.Bool("record", false, "store the result in the database for the Delivery infrastructure page")
	asJSON := fs.Bool("json", false, "print JSON")
	fs.Parse(args)

	var ds []string
	for _, d := range strings.Split(*domains, ",") {
		if d = strings.TrimSpace(d); d != "" {
			ds = append(ds, d)
		}
	}
	if len(ds) == 0 && deps != nil {
		var err error
		if ds, err = deps.svc.ClaimedDomains(ctx); err != nil {
			die(err)
		}
	}
	p := managed.Params{Hostname: *hostname, BounceDomain: *bounce, Domains: ds, Selector: *selector,
		SubmissionBind: *submission, InboundRelay: *inbound, Adopt: *adopt, NoFirewall: *noFirewall, ComposeSubnet: *subnet, RenewCerts: *renewCerts}
	show := func(v any, text string) {
		if *asJSON {
			_ = json.NewEncoder(os.Stdout).Encode(v)
			return
		}
		fmt.Print(text)
	}
	switch sub {
	case "plan":
		pl, err := managed.BuildPlan(p)
		if err != nil {
			die(err)
		}
		show(pl, pl.Summary()+"\nReview the changes, then run:\n  njiwa-mail installer apply -approve "+pl.Digest+" (same flags)\n")
	case "apply":
		applyInstaller(ctx, deps, p, *approve, *record, show)
	case "dkim-rotate":
		runRotation(ctx, deps, p, action, *approve, *record, show)
	case "rollback":
		if os.Geteuid() != 0 {
			die(errors.New("the installer must run as root on the mail host"))
		}
		rc, err := managed.Rollback(p, *receipt, execRunner{})
		if err != nil {
			die(err)
		}
		if *record && deps != nil {
			if err := deps.svc.RecordManagedRollback(ctx, service.CLIActor, rc.PlanDigest); err != nil {
				die(err)
			}
		}
		show(rc, "Rolled back receipt "+rc.ID+".\n")
	case "status":
		rc, err := managed.LoadReceipt(p, *receipt)
		if err != nil {
			die(err)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Latest receipt %s: %s, rollback %s, digest %s, Postfix %s\n", rc.ID, rc.Result, rc.RollbackState, rc.PlanDigest, rc.PostfixVersion)
		for _, c := range managed.Certificates(p) {
			fmt.Fprintf(&b, "%-30s expires %s%s\n", c.Name, c.NotAfter.Format("2006-01-02"), c.Note)
		}
		show(rc, b.String())
	default:
		die(errors.New("usage: njiwa-mail installer plan|apply|rollback|status [flags]"))
	}
}

// runMTALog feeds Postfix log lines to the delivery-evidence collector. With
// -follow it keeps reading new lines and reopens the file after rotation.
func runMTALog(ctx context.Context, svc *service.Service, args []string) {
	fs := flag.NewFlagSet("mta-log", flag.ExitOnError)
	file := fs.String("file", "/var/log/mail.log", `Postfix log file, or "-" for stdin`)
	follow := fs.Bool("follow", false, "keep reading new lines (like tail -F)")
	fs.Parse(args)
	if *file == "-" {
		n, err := svc.IngestMTALog(ctx, os.Stdin)
		if err != nil {
			die(err)
		}
		fmt.Printf("%d delivery events recorded\n", n)
		return
	}
	f, err := os.Open(*file)
	if err != nil {
		die(err)
	}
	if !*follow {
		defer f.Close()
		n, err := svc.IngestMTALog(ctx, f)
		if err != nil {
			die(err)
		}
		fmt.Printf("%d delivery events recorded\n", n)
		return
	}
	_, _ = f.Seek(0, io.SeekEnd) // only new lines; run once without -follow to backfill
	r := bufio.NewReader(f)
	var pending strings.Builder
	for ctx.Err() == nil {
		line, err := r.ReadString('\n')
		pending.WriteString(line)
		if err == nil {
			if _, err := svc.IngestMTALog(ctx, strings.NewReader(pending.String())); err != nil {
				slog.Error("mta-log ingest failed", "err", err)
			}
			pending.Reset()
			continue
		}
		// End of file: wait, and reopen if the file was rotated or truncated.
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
		if st, err := os.Stat(*file); err == nil {
			if cur, _ := f.Stat(); cur != nil && (!os.SameFile(st, cur) || st.Size() < offset(f)) {
				f.Close()
				if f, err = os.Open(*file); err != nil {
					die(err)
				}
				r.Reset(f)
			}
		}
	}
	f.Close()
}

func offset(f *os.File) int64 {
	o, _ := f.Seek(0, io.SeekCurrent)
	return o
}

// runDKIMVerify prints the result of every DKIM signature on a message, for
// example the "original" of a test message downloaded from a mailbox.
func runDKIMVerify(args []string) {
	fs := flag.NewFlagSet("dkim-verify", flag.ExitOnError)
	file := fs.String("file", "-", `message file, or "-" for stdin`)
	fs.Parse(args)
	var raw []byte
	var err error
	if *file == "-" {
		raw, err = io.ReadAll(io.LimitReader(os.Stdin, service.MaxVerifyBytes+1))
	} else {
		raw, err = os.ReadFile(*file)
	}
	if err != nil {
		die(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res := dnsauth.VerifyDKIM(ctx, net.DefaultResolver, raw, time.Now())
	if len(res) == 0 {
		fmt.Println("no DKIM-Signature header: the message is not signed")
		os.Exit(1)
	}
	ok := false
	for _, r := range res {
		fmt.Printf("%-9s d=%s s=%s %s\n", strings.ToUpper(r.Result), r.Domain, r.Selector, r.Reason)
		ok = ok || r.Result == "pass"
	}
	if !ok {
		os.Exit(1)
	}
}

// applyInstaller runs an approved apply, prints the receipt and, with
// record, stores it in the database.
func applyInstaller(ctx context.Context, deps *installerDeps, p managed.Params, approve string, record bool, show func(any, string)) *managed.Receipt {
	if os.Geteuid() != 0 {
		die(errors.New("the installer must run as root on the mail host"))
	}
	rc, err := managed.Apply(p, approve, execRunner{}, time.Now())
	if rc != nil {
		var b strings.Builder
		for _, s := range rc.Steps {
			st := "ok  "
			if !s.OK {
				st = "FAIL"
			}
			fmt.Fprintf(&b, "%s %s %s\n", st, s.Name, s.Detail)
		}
		fmt.Fprintf(&b, "\nResult: %s (receipt %s)\n", rc.Result, rc.ID)
		if rc.Result == "ok" {
			b.WriteString("\nPublish these DKIM records, then check each domain on the Domains page:\n")
			for d, txt := range rc.DKIMRecords {
				fmt.Fprintf(&b, "  %s._domainkey.%s TXT \"%s\"\n", rc.Params.Selector, d, txt)
			}
		}
		show(rc, b.String())
	}
	if err != nil {
		die(err)
	}
	if record {
		if deps == nil {
			die(errors.New("-record needs the database configuration"))
		}
		read := func(path string) []byte {
			b, err := os.ReadFile(path)
			if err != nil {
				die(err)
			}
			return b
		}
		host, _, _ := strings.Cut(rc.Params.SubmissionBind, ":")
		portN := managed.SubmissionPort(rc.Params.SubmissionBind)
		if err := deps.svc.RecordManagedInstall(ctx, service.CLIActor, service.ManagedInstall{ReceiptID: rc.ID, PlanDigest: rc.PlanDigest,
			ApprovedBy: "cli", BackupRef: rc.BackupDir, PostfixVersion: rc.PostfixVersion, SubmissionHost: host, SubmissionPort: portN,
			ServerName: rc.Params.Hostname, Selector: rc.Params.Selector, DKIMRecords: rc.DKIMRecords,
			ClientCertPEM: read("/etc/njiwa-mail/submission/client.crt"), ClientKeyPEM: read("/etc/njiwa-mail/submission/client.key"),
			CAPEM: read("/etc/njiwa-mail/submission/ca.crt")}); err != nil {
			die(err)
		}
		fmt.Println("Recorded. Run the connection test on the Delivery infrastructure page next.")
	}
	return rc
}

// runRotation is the staged DKIM key rotation (see internal/installer/managed/rotate.go).
func runRotation(ctx context.Context, deps *installerDeps, p managed.Params, action, approve string, record bool, show func(any, string)) {
	if action != "status" && os.Geteuid() != 0 {
		die(errors.New("the installer must run as root on the mail host"))
	}
	switch action {
	case "prepare":
		rot, err := managed.PrepareRotation(p, p.Selector, time.Now())
		if err != nil {
			die(err)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "New keys for selector %s are ready; mail is still signed with %s.\n\nPublish these records (keep the %s records too):\n", rot.To, rot.From, rot.From)
		for _, d := range rot.Domains {
			fmt.Fprintf(&b, "  %s._domainkey.%s TXT \"%s\"\n", rot.To, d, rot.Records[d])
		}
		fmt.Fprintf(&b, "\nWhen DNS shows them, review and activate:\n  njiwa-mail installer plan -selector %s (same flags)\n  njiwa-mail installer dkim-rotate activate -selector %s -approve <digest> (same flags)\n", rot.To, rot.To)
		show(rot, b.String())
	case "activate":
		rot, err := managed.LoadRotation(p)
		if err != nil {
			die(err)
		}
		if rot.To != p.Selector {
			die(fmt.Errorf("the prepared rotation is to %s; pass -selector %s", rot.To, rot.To))
		}
		dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		probs := managed.RotationDNSProblems(dctx, net.DefaultResolver, rot)
		cancel()
		if len(probs) > 0 {
			die(errors.New("not activating, mail would fail DKIM: " + strings.Join(probs, "; ")))
		}
		if rc := applyInstaller(ctx, deps, p, approve, record, show); rc != nil && rc.Result == "ok" {
			if err := managed.FinishRotation(p); err != nil {
				die(err)
			}
			fmt.Printf("\nSigning now uses %s. Keep the %s DNS records for at least 7 days, then: njiwa-mail installer dkim-rotate retire -selector %s\n", rot.To, rot.From, rot.From)
		}
	case "retire":
		removed, err := managed.RetireSelector(p, p.Selector)
		if err != nil {
			die(err)
		}
		show(removed, fmt.Sprintf("Removed %d private key(s) for selector %s. You can now delete its DNS records.\n", len(removed), p.Selector))
	case "cancel":
		rot, err := managed.CancelRotation(p)
		if err != nil {
			die(err)
		}
		show(rot, "Cancelled the rotation to "+rot.To+"; its keys were deleted. Signing is unchanged.\n")
	case "status":
		cur, err := managed.CurrentSelector(p)
		if err != nil {
			die(err)
		}
		msg := "Signing selector: " + cur + "\n"
		if rot, err := managed.LoadRotation(p); err == nil {
			msg += "Prepared rotation: " + rot.From + " -> " + rot.To + " (" + rot.PreparedAt.Format(time.RFC3339) + ")\n"
		}
		show(map[string]string{"selector": cur}, msg)
	default:
		die(errors.New("usage: njiwa-mail installer dkim-rotate prepare|activate|cancel|retire|status -selector S"))
	}
}

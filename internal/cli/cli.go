// Package cli runs the transactional email service and operator commands.
package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/njiwa-tz/mail/internal/api"
	"github.com/njiwa-tz/mail/internal/config"
	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/inbound"
	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/secure"
	"github.com/njiwa-tz/mail/internal/service"
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
  smtp-check                  verify TLS + auth against the SMTP server (sends nothing)
  retention                   run the retention job once

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
  user create   -email E -role owner|admin   (password read from stdin; first owner is created here)
  user password -email E                            (new password read from stdin)
  user disable|enable -email E

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
		PublicURL: cfg.PublicURL})

	cmd, args := args[0], args[1:]
	switch cmd {
	case "migrate":
		if err := db.Migrate(ctx, pool); err != nil {
			die(err)
		}
		if err := svc.SeedTemplates(ctx); err != nil {
			die(err)
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
		fmt.Printf("captures=%d messages=%d payloads=%d buckets=%d\n", r.Captures, r.Messages, r.Payloads, r.Buckets)
	case "project", "env", "sender", "key", "suppress", "message", "capture", "user":
		if err := admin(ctx, svc, cmd, args); err != nil {
			die(err)
		}
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
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
		w.SMTP = smtpTransport(cfg)
	} else {
		slog.Warn("SMTP not configured: worker handles sandbox messages only; live messages stay queued until they expire")
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
			slog.Info("retention run", "captures", r.Captures, "messages", r.Messages, "payloads", r.Payloads, "inbound", r.Inbound, "deliveries", r.Deliveries)
		}
		return err
	})
	every(5*time.Minute, "bounce_rate_alerts", func() error { return svc.CheckBounceRates(ctx) })
	every(24*time.Hour, "daily_summary", func() error {
		body, owners, err := svc.DailySummary(ctx)
		if err != nil {
			return err
		}
		slog.Info("daily summary", "owners", len(owners), "bytes", len(body))
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
		role     = fs.String("role", "", "")
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
		id, err := svc.CreateUser(ctx, *email, pw, *role)
		if err != nil {
			return cliErr(err)
		}
		fmt.Printf("user %s created with role %s (id %s)\n", strings.ToLower(*email), *role, id)
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

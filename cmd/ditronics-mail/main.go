// ditronics-mail: transactional email service (API, worker and operator CLI).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ditronics-tz/mail/internal/api"
	"github.com/ditronics-tz/mail/internal/config"
	"github.com/ditronics-tz/mail/internal/db"
	"github.com/ditronics-tz/mail/internal/mailer"
	"github.com/ditronics-tz/mail/internal/secure"
	"github.com/ditronics-tz/mail/internal/service"
	"github.com/ditronics-tz/mail/internal/worker"
)

const usage = `ditronics-mail <command>

Runtime:
  serve                       run the HTTP API
  worker                      run the dispatch worker (+ retention loop)
  migrate                     apply database migrations
  worker-health [-max 60s]    exit non-zero if no worker heartbeat (for container healthchecks)
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

func main() {
	if len(os.Args) < 2 {
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
		UnknownPurge: cfg.UnknownPurge, MaxAttempts: cfg.MaxAttempts})

	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "migrate":
		if err := db.Migrate(ctx, pool); err != nil {
			die(err)
		}
		fmt.Println("migrations applied")
	case "serve":
		serve(ctx, cfg, svc)
	case "worker":
		runWorker(ctx, cfg, svc)
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
	case "project", "env", "sender", "key", "suppress", "message", "capture":
		if err := admin(ctx, svc, cmd, args); err != nil {
			die(err)
		}
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

func smtpTransport(cfg *config.Config) *mailer.SMTP {
	return mailer.NewSMTP(mailer.SMTPConfig{Host: cfg.SMTP.Host, Port: cfg.SMTP.Port, Username: cfg.SMTP.Username,
		Password: cfg.SMTP.Password, Implicit: cfg.SMTP.Implicit, HeloName: cfg.SMTP.HeloName, Timeout: cfg.SMTP.Timeout})
}

func serve(ctx context.Context, cfg *config.Config, svc *service.Service) {
	srv := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: (&api.Server{
			Svc: svc, MaxBodyBytes: cfg.MaxBodyBytes,
			ConsolePassword: cfg.ConsolePassword, SessionTTL: cfg.ConsoleSessionTTL,
			SMTPConfigured: cfg.SMTP.Configured(),
		}).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	go func() {
		<-ctx.Done()
		sctx, c := context.WithTimeout(context.Background(), 15*time.Second)
		defer c()
		srv.Shutdown(sctx)
	}()
	slog.Info("api listening", "addr", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		die(err)
	}
}

func runWorker(ctx context.Context, cfg *config.Config, svc *service.Service) {
	w := &worker.Worker{Svc: svc, ID: hostID(), Poll: cfg.WorkerPoll, Batch: cfg.WorkerBatch, Concurrency: cfg.WorkerConcurrency,
		Lease: cfg.LeaseDuration, SendTimeout: cfg.SMTP.Timeout + 5*time.Second, RetentionInterval: cfg.RetentionInterval}
	if cfg.SMTP.Configured() {
		w.SMTP = smtpTransport(cfg)
	} else {
		slog.Warn("SMTP not configured: worker handles sandbox messages only; live messages stay queued until they expire")
	}
	w.Run(ctx)
}

func hostID() string {
	h, _ := os.Hostname()
	return fmt.Sprintf("%s-%d", h, os.Getpid())
}

func admin(ctx context.Context, svc *service.Service, group string, args []string) error {
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

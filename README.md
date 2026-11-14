# Njiwa Mail

**Self-hosted transactional email you can see into: one API, one dashboard, your own server.**

[![CI](https://github.com/dadyutenga/Njiwa-Mail/actions/workflows/ci.yml/badge.svg?branch=dev)](https://github.com/dadyutenga/Njiwa-Mail/actions/workflows/ci.yml)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8.svg)](go.mod)
[![PRs welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

*Njiwa* is Kiswahili for "pigeon", the bird that carries the message. Njiwa Mail sends the mail your applications need (one-time
codes, password resets, account notices, receipts) from infrastructure you own, and tells you honestly what happened to each
message. It is free and open source under the [GNU AGPL-3.0](LICENSE), built in Tanzania, and in English and Kiswahili.

## Why Njiwa Mail

- **Two ways to send, one API.** **Send from my VPS**: Njiwa installs and manages Postfix and OpenDKIM on your host. **Connect an
  existing mail server**: any authenticated SMTP server (Postfix, Exim, Mailcow or a provider). Mailcow is supported, never required.
- **Installs safely next to what you already run.** A read-only scan finds the mail services already on the server and recommends
  a mode. The installer shows a plan before it changes anything, needs that exact plan approved, keeps a receipt and can roll
  back. It never replaces a mail server that is in use.
- **Runs on Debian, Ubuntu, AlmaLinux, Rocky Linux, RHEL and Fedora**, with Docker Compose for the application.
- **Honest delivery status.** `submitted` means your mail server accepted the message. Later evidence (deferred, accepted by the
  recipient's server, bounced) is shown with its source. Njiwa never claims "delivered".
- **Domains done properly.** SPF, DKIM and DMARC are evaluated, not just looked up. DKIM keys rotate in stages. Live mail needs a
  domain the project has verified.
- **Built for operators.** A dashboard in English and Kiswahili, with audit log, optional TOTP sign-in, metrics, rate limits and
  backup/restore.

## What it does

Njiwa Mail is **independent of any client application**: it has its own repository layout, database, Compose stack,
configuration and release cycle. Every app is an API client with its own key. The contract is [docs/INTEGRATION.md](docs/INTEGRATION.md).

- **Outbound:** authenticated `POST /v1/emails` → durable PostgreSQL queue → worker → verified-TLS SMTP submission to the active delivery profile (managed Postfix over mutual TLS, or your SMTP server).
- **Setup:** the Delivery infrastructure page guides the choice of mode and shows each check's real state, with the fix for every problem. Live mail needs a sending domain the project has verified and holds the installation-wide claim for; SPF, DKIM and DMARC are evaluated, not just looked up.
- **Templates** in the database, versioned and immutable once published, in English and Kiswahili (`locale`: `en` or `sw`). Seeded: `otp`, `password-reset`, `account-notice`.
- **SMTP relay:** point an application that only speaks SMTP at Njiwa (AUTH with an API key over TLS); its mail goes through the same pipeline as the API, so you can move to the HTTP API at your own pace.
- **Address verification** before sending (syntax, MX, disposable and role addresses, suppressions), and an optional signed **"view in browser"** link.
- **Inbound:** your mail server (or managed Postfix, for bounces) relays to a private SMTP listener, or a server of yours posts raw messages over signed HTTP; mail is parsed to JSON (text, HTML, headers, attachments), and the dashboard's inbox updates live.
- **Bounces:** DSNs are parsed; hard bounces suppress the recipient automatically; policy bounces raise an alert and never suppress.
- **Webhooks:** signed (HMAC-SHA256), retried for about 24 hours, replayable, with SSRF controls.
- **Admin API** (`/admin/v1`): per-user accounts (argon2id), sessions, CSRF, one role (`admin`) with an installation-operator permission, optional TOTP two-step sign-in, audit log, DNS health check, metrics.
- The operator workspace opens in Live at `/p/{project}/…`. Live/Sandbox is selected locally for sending, template tests, and API keys; Sandbox captures have their own page. Only the last project is remembered.
- Per-project, per-environment (sandbox/live) API keys, approved senders, approved link origins, recipient allowlist, rate limits.
- Single static Go binary with four roles (`serve`, `worker`, `inbound`, `scheduler`) and the operator CLI. The HTTP API is Fiber v3. Persistence is GORM on PostgreSQL. The dashboard is React.

> **Project status: pre-1.0.** See [STATUS.md](STATUS.md) and [docs/RELEASE_REPORT.md](docs/RELEASE_REPORT.md). The managed
> installer is checked on every change against real Postfix and OpenDKIM (no open relay, mutual-TLS submission, DKIM signing) on
> Ubuntu, AlmaLinux, Rocky Linux and Fedora. Real delivery to external providers and an HTTPS deployment have **not** been
> verified yet. Help with that is very welcome.

## Installing on a server

Njiwa-Mail installs on a new **or an existing** server; a fresh VPS is not required. Start with a read-only scan
(`sh scripts/discover.sh`): it finds any mail service already there (Mailcow, Postfix, Exim), recommends a delivery mode, and never
changes anything. Existing mail servers are reused over SMTP, never replaced. Full guide: [docs/INSTALL.md](docs/INSTALL.md).

## Quick start (local, sandbox)

```bash
cp .env.example .env     # fill POSTGRES_PASSWORD, ENCRYPTION_KEY, HMAC_KEY (see file); leave SMTP_* empty for sandbox
docker compose up -d --build          # postgres, migrate, api (127.0.0.1:8080), worker, scheduler

CLI="docker compose run --rm api"     # operator CLI runs inside the same image
$CLI project create -name myapp
$CLI env origins -project myapp -mode sandbox -origin https://app.example.com
$CLI sender add  -project myapp -mode sandbox -email noreply@notify.example.com -display "My App"
$CLI key create  -project myapp -mode sandbox -label dev      # secret shown ONCE
docker compose run --rm -T api user create -email you@example.com -role admin <<< 'a-long-password'   # first admin user
```

Without Docker: `go build -o njiwa-mail ./cmd/njiwa-mail`, set the env vars from `.env.example`,
run `njiwa-mail migrate`, then `serve`, `worker` and `scheduler` (systemd units in `deploy/`).
The `inbound` role needs a relay from your mail server first. For managed mode see [docs/OPERATIONS.md](docs/OPERATIONS.md)
(`njiwa-mail preflight`, `njiwa-mail installer`); for Mailcow see [docs/MAILCOW_SETUP.md](docs/MAILCOW_SETUP.md).

To start the HTTP API directly from the project root, set the same environment variables and run `go run .`.
You can also run CLI commands from the root, for example `go run . migrate`.

## Admin API

`/admin/v1` is the backend for the operator UI: projects, keys, senders, templates (draft, preview, publish, test-send), messages,
the `unknown` queue, inbound mail, bounces, suppressions, webhooks, DNS checks, users, audit log and settings.
Sign in with `POST /admin/v1/auth/login`; the response carries a `csrf_token` that must be sent as `X-CSRF-Token` on every
state-changing request. There is no signup endpoint: the first admin is created with `njiwa-mail user create`.
The contract is in [docs/openapi.yaml](docs/openapi.yaml).

## Operator UI

`frontend` is the operator UI for `/admin/v1`. The browser holds a session cookie, never a project API key.

1. Create the first admin: `njiwa-mail user create -email you@example.com -role admin` (password on stdin).
2. For local HTTP, set `ADMIN_INSECURE_COOKIES=true` in the API environment. Leave it false behind HTTPS.
3. Start the API on `127.0.0.1:8080`. From `frontend`: `cp .env.example .env && npm install && npm run dev`.
4. Open `http://localhost:3000` and sign in with that email and password.

Vite proxies `/admin` to the API. The UI can create projects, keys, senders, templates and the rest of the admin surface. Message pages stay metadata-only. Inbound HTML is shown in a sandboxed frame. The older `/console` routes remain for the previous read-only session.

## Sending

```bash
curl -X POST https://mail-api.example.com/v1/emails \
  -H "Authorization: Bearer $KEY" -H "Idempotency-Key: evt-2026-0001" -H 'Content-Type: application/json' \
  -d '{"from":"noreply@notify.example.com","to":"user@example.org","template":"otp","locale":"sw",
       "variables":{"name":"Ada","code":"482913","expires_in_minutes":5}}'
# 202 {"id":"…","status":"queued"}      then:   GET /v1/emails/{id}
```

Sandbox keys (`dmk_sbx_…`) never contact SMTP; messages end as `captured` (inspect with `njiwa-mail capture show -id ID`).
Live keys (`dmk_liv_…`) submit through the active delivery profile (managed Postfix or your existing SMTP server; Mailcow is optional), initially only to recipients on the environment allowlist.

**Public developer documentation** (landing page, quickstart, API reference, webhooks, setup guides, `llms.txt`, per-page Markdown, an agent skill): built from [documentation/](documentation/), deployed with [docs/public-portal/DEPLOY.md](docs/public-portal/DEPLOY.md), contributed to with [docs/public-portal/CONTRIBUTING.md](docs/public-portal/CONTRIBUTING.md). It is a static site you publish yourself; there is no central hosted instance.

Full contract: [docs/openapi.yaml](docs/openapi.yaml) (application API) and [docs/openapi-admin.yaml](docs/openapi-admin.yaml) (admin API); every route and table: [docs/ENDPOINTS.md](docs/ENDPOINTS.md). Client libraries/integration guide: [docs/INTEGRATION.md](docs/INTEGRATION.md), code in curl, Go, Node and Python: [docs/EXAMPLES.md](docs/EXAMPLES.md). Problem and error codes: [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md).
Running it: [docs/OPERATIONS.md](docs/OPERATIONS.md).

## Tests

Needs PostgreSQL (the suite resets the `public` schema of the database it connects to; **use a throwaway database**).

```bash
createdb -T template0 njiwa_mail_test
export TEST_DATABASE_URL='postgres://USER@/njiwa_mail_test?host=/var/run/postgresql'
go vet ./... && go test ./... -race -count=1
```

## Layout

```
cmd/njiwa-mail   CLI entrypoint (serve | worker | inbound | scheduler | migrate | provisioning | ops)
internal/api         Fiber HTTP: application API, admin API, legacy console
internal/service     auth, accept, dispatch state machine, templates, webhooks outbox, inbound, bounces, users, DNS, stats
internal/worker      outbound claim/dispatch loop      internal/webhook   signed webhook delivery (SSRF-guarded)
internal/inbound     private SMTP listener + MIME/DSN parsing
internal/mailer      MIME + SMTP transport             internal/tmpl      template engine + seed content (en, sw)
internal/secure      AES-GCM + keyed HMAC              internal/db        GORM session, models, SQL migrations
client/              dependency-free Go client and webhook verifier
test/                acceptance tests                  deploy/            nginx + systemd; Dockerfile, compose.yaml at root
```

## What a message can carry

One request accepts a `to` address plus `cc` and `bcc` (10 addresses altogether), file attachments, raw HTML or plain text, and custom `X-` headers. Addresses may use international domains. Operator sign-in uses a password and session cookie; Two-step sign-in (TOTP) is optional per user and can be required for all admins.

Inbound mail arrives by Mailcow relay, or by IMAP polling of a catch-all mailbox (`IMAP_HOST`). Outbound submission uses `SMTP_HOST` and, when that host fails before acceptance, `SMTP_FAILOVER_HOST`. Extra `api` and `worker` replicas share the same PostgreSQL queue, so a second instance does not double-send.

This remains a transactional service. It does not send marketing campaigns.

## Contributing

Contributions of every size are welcome: bug reports, docs fixes, translations, tests on a distribution you run, features.
Start with [CONTRIBUTING.md](CONTRIBUTING.md). It covers the development setup, the branch flow (pull requests go to `dev`) and
the checks CI runs. Good places to start:

- **Try the installer on your distribution** and report what you see (`sh scripts/discover.sh`, then `njiwa-mail installer plan`).
  Real-host runs with SELinux enforcing on AlmaLinux or Rocky Linux are especially wanted.
- **Kiswahili and English wording** in the dashboard and the email templates.
- **Client libraries and examples** beyond the [Go client](client/) and [docs/EXAMPLES.md](docs/EXAMPLES.md).
- **Open items** in [docs/REMAINING_WORK.md](docs/REMAINING_WORK.md) and [task.md](task.md).

Please report security problems privately, not in issues: see [SECURITY.md](SECURITY.md). Everyone taking part follows the
[Code of Conduct](CODE_OF_CONDUCT.md).

## License

Njiwa Mail is free software, licensed under the [GNU Affero General Public License v3.0](LICENSE). You can use it, change it and
run it for any purpose, including commercially. If you run a modified version as a service that other people use over a network,
you must offer them the source of your version (AGPL section 13). Contributions are accepted under the same license.

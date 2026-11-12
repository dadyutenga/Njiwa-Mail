# Njiwa Mail

Self-hosted transactional email service: one API, one dashboard and one domain experience, with two delivery modes —
**Send from my VPS** (Njiwa installs and manages Postfix + OpenDKIM on your host) or **Connect an existing mail server**
(any authenticated SMTP server: Postfix, Exim, Mailcow or a provider). Mailcow is supported, never required.
It is **independent of any client application**: it has its own repository layout, database, Compose stack,
configuration and release cycle. Every app is an API client with its own key. The contract is [docs/INTEGRATION.md](docs/INTEGRATION.md).

- **Outbound:** authenticated `POST /v1/emails` → durable PostgreSQL queue → worker → verified-TLS SMTP submission to the active delivery profile (managed Postfix over mutual TLS, or your SMTP server).
- **Setup:** the Delivery infrastructure page guides the choice of mode and shows each check's real state, with the fix for every problem. Live mail needs a sending domain the project has verified and holds the installation-wide claim for; SPF, DKIM and DMARC are evaluated, not just looked up.
- **Templates** in the database, versioned and immutable once published, in English and Kiswahili (`locale`: `en` or `sw`). Seeded: `otp`, `password-reset`, `account-notice`.
- **Inbound:** your mail server (or managed Postfix, for bounces) relays to a private SMTP listener; mail is parsed to JSON (text, HTML, headers, attachments).
- **Bounces:** DSNs are parsed; hard bounces suppress the recipient automatically; policy bounces raise an alert and never suppress.
- **Webhooks:** signed (HMAC-SHA256), retried for about 24 hours, replayable, with SSRF controls.
- **Admin API** (`/admin/v1`): per-user accounts (argon2id), sessions, CSRF, one role (`admin`) with an installation-operator permission, optional TOTP two-step sign-in, audit log, DNS health check, metrics.
- The operator workspace opens in Live at `/p/{project}/…`. Live/Sandbox is selected locally for sending, template tests, and API keys; Sandbox captures have their own page. Only the last project is remembered.
- Per-project, per-environment (sandbox/live) API keys, approved senders, approved link origins, recipient allowlist, rate limits.
- Honest status: `submitted` = your mail server acknowledged acceptance. Later evidence (deferred, accepted by the recipient's server, bounced) is shown with its source. Never "delivered".
- Single static Go binary with four roles (`serve`, `worker`, `inbound`, `scheduler`) and the operator CLI. The HTTP API is Fiber. Persistence is GORM on PostgreSQL.

> Release status: see [STATUS.md](STATUS.md) and [docs/RELEASE_REPORT.md](docs/RELEASE_REPORT.md). The managed installer has been verified against real
> Postfix and OpenDKIM (no open relay, mutual-TLS submission, DKIM signing), but real delivery to external providers and an HTTPS
> deployment have **not** been verified yet.

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
Live keys (`dmk_liv_…`) submit to Mailcow, initially only to recipients on the environment allowlist.

Full contract: [docs/openapi.yaml](docs/openapi.yaml). Client libraries/integration guide: [docs/INTEGRATION.md](docs/INTEGRATION.md).
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

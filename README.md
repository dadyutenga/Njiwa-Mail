# Ditronics Mail

Standalone transactional email service for Ditronics software, built on the existing Mailcow instance.
It is **independent of any client application**: it has its own repository layout, database, Compose stack,
configuration and release cycle. Every app is an API client with its own key. The contract is [docs/INTEGRATION.md](docs/INTEGRATION.md).

- **Outbound:** authenticated `POST /v1/emails` → durable PostgreSQL queue → worker → verified-TLS SMTP submission to Mailcow.
- **Templates** in the database, versioned and immutable once published, in English and Kiswahili (`locale`: `en` or `sw`). Seeded: `otp`, `password-reset`, `account-notice`.
- **Inbound:** Mailcow relays `in.<domain>` to a private SMTP listener; mail is parsed to JSON (text, HTML, headers, attachments).
- **Bounces:** DSNs are parsed; hard bounces suppress the recipient automatically; policy bounces raise an alert and never suppress.
- **Webhooks:** signed (HMAC-SHA256), retried for about 24 hours, replayable, with SSRF controls.
- **Admin API** (`/admin/v1`): per-user accounts (argon2id), sessions, CSRF, roles (`owner`, `admin`, `viewer`), audit log, DNS health check, metrics.
- Per-project, per-environment (sandbox/live) API keys, approved senders, approved link origins, recipient allowlist, rate limits.
- Honest status: `submitted` = Mailcow acknowledged acceptance. Never "delivered".
- Single static Go binary with four roles (`serve`, `worker`, `inbound`, `scheduler`) and the operator CLI. Only dependency is PostgreSQL.

> Release status: see [docs/RELEASE_REPORT.md](docs/RELEASE_REPORT.md). Real Mailcow delivery and HTTPS deployment have **not** been verified yet.

## Quick start (local, sandbox)

```bash
cp .env.example .env     # fill POSTGRES_PASSWORD, ENCRYPTION_KEY, HMAC_KEY (see file); leave SMTP_* empty for sandbox
docker compose up -d --build          # postgres, migrate, api (127.0.0.1:8080), worker, scheduler

CLI="docker compose run --rm api"     # operator CLI runs inside the same image
$CLI project create -name myapp
$CLI env origins -project myapp -mode sandbox -origin https://app.example.com
$CLI sender add  -project myapp -mode sandbox -email noreply@notify.example.com -display "My App"
$CLI key create  -project myapp -mode sandbox -label dev      # secret shown ONCE
docker compose run --rm -T api user create -email you@example.com -role owner <<< 'a-long-password'   # first admin user
```

Without Docker: `go build -o ditronics-mail ./cmd/ditronics-mail`, set the env vars from `.env.example`,
run `ditronics-mail migrate`, then `serve`, `worker` and `scheduler` (systemd units in `deploy/`).
The `inbound` role needs Mailcow routing first: see [docs/MAILCOW_SETUP.md](docs/MAILCOW_SETUP.md).

## Admin API

`/admin/v1` is the backend for the operator UI: projects, keys, senders, templates (draft, preview, publish, test-send), messages,
the `unknown` queue, inbound mail, bounces, suppressions, webhooks, DNS checks, users, audit log and settings.
Sign in with `POST /admin/v1/auth/login`; the response carries a `csrf_token` that must be sent as `X-CSRF-Token` on every
state-changing request. There is no signup endpoint: the first owner is created with `ditronics-mail user create`.
The contract is in [docs/openapi.yaml](docs/openapi.yaml).

## Operator UI

`frontend` is the operator UI for `/admin/v1`. The browser holds a session cookie, never a project API key.

1. Create the first owner: `ditronics-mail user create -email you@example.com -role owner` (password on stdin).
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

Sandbox keys (`dmk_sbx_…`) never contact SMTP; messages end as `captured` (inspect with `ditronics-mail capture show -id ID`).
Live keys (`dmk_liv_…`) submit to Mailcow, initially only to recipients on the environment allowlist.

Full contract: [docs/openapi.yaml](docs/openapi.yaml). Client libraries/integration guide: [docs/INTEGRATION.md](docs/INTEGRATION.md).
Running it: [docs/OPERATIONS.md](docs/OPERATIONS.md).

## Tests

Needs PostgreSQL (the suite resets the `public` schema of the database it connects to; **use a throwaway database**).

```bash
createdb -T template0 ditronics_mail_test
export TEST_DATABASE_URL='postgres://USER@/ditronics_mail_test?host=/var/run/postgresql'
go vet ./... && go test ./... -race -count=1
```

## Layout

```
cmd/ditronics-mail   CLI entrypoint (serve | worker | inbound | scheduler | migrate | provisioning | ops)
internal/api         HTTP: application API, admin API, legacy console
internal/service     auth, accept, dispatch state machine, templates, webhooks outbox, inbound, bounces, users, DNS, stats
internal/worker      outbound claim/dispatch loop      internal/webhook   signed webhook delivery (SSRF-guarded)
internal/inbound     private SMTP listener + MIME/DSN parsing
internal/mailer      MIME + SMTP transport             internal/tmpl      template engine + seed content (en, sw)
internal/secure      AES-GCM + keyed HMAC              internal/db        pool + migrations
client/              dependency-free Go client and webhook verifier
test/                acceptance tests                  deploy/            nginx + systemd; Dockerfile, compose.yaml at root
```

## Current limitations

One recipient per message; no attachments/CC/BCC/custom headers/raw HTML on outbound; ASCII addresses only;
no TOTP second factor yet; IMAP-polling fallback for inbound is not implemented; single outbound server; no HA.
Transactional mail only: not a bulk or marketing service.

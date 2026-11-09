# Ditronics Mail

Standalone transactional email service for Ditronics software, built on the existing Mailcow instance.
It is **independent of any client application**: it has its own repository layout, database, Compose stack,
configuration and release cycle. Applications (SACAS, or any other) are just API clients.

- Authenticated `POST /v1/emails` → durable PostgreSQL queue → worker → verified-TLS SMTP submission to Mailcow.
- Three code-owned, versioned templates: `otp`, `password-reset`, `account-notice` (HTML + plain text).
- Per-project, per-environment (sandbox/live) API keys, approved senders, approved link origins, recipient allowlist, rate limits, manual suppressions.
- Honest status: `submitted` = Mailcow acknowledged acceptance. Never "delivered".
- Single static Go binary: API, worker and operator CLI (`ditronics-mail`). Only dependency is PostgreSQL.

> Release status: see [docs/RELEASE_REPORT.md](docs/RELEASE_REPORT.md). Real Mailcow delivery and HTTPS deployment have **not** been verified yet.

## Quick start (local, sandbox)

```bash
cp .env.example .env     # fill POSTGRES_PASSWORD, ENCRYPTION_KEY, HMAC_KEY (see file); leave SMTP_* empty for sandbox
docker compose up -d --build          # postgres, migrate, api (127.0.0.1:8080), worker

CLI="docker compose run --rm api"     # operator CLI runs inside the same image
$CLI project create -name myapp
$CLI env origins -project myapp -mode sandbox -origin https://app.example.com
$CLI sender add  -project myapp -mode sandbox -email noreply@notify.example.com -display "My App"
$CLI key create  -project myapp -mode sandbox -label dev      # secret shown ONCE
```

Without Docker: `go build -o ditronics-mail ./cmd/ditronics-mail`, set the env vars from `.env.example`,
run `ditronics-mail migrate`, then `serve` and `worker` (systemd units in `deploy/`).

## Operator console

`template-01` is a read-only console for this API. The browser never receives a project API key.

1. Set `CONSOLE_PASSWORD` (at least 12 characters) in `.env` and start the API on `127.0.0.1:8080`.
2. From `template-01`: `cp .env.example .env && npm install && npm run dev`.
3. Open `http://localhost:3000` and sign in with that password.

Vite proxies `/console` to the API. Projects, senders, and keys are still created with the CLI. The console shows queue state, project quotas, and key prefixes. It does not show recipients, template variables, sandbox captures, or secrets. Leave `CONSOLE_PASSWORD` empty to disable `/console`.

## Sending

```bash
curl -X POST https://mail-api.example.com/v1/emails \
  -H "Authorization: Bearer $KEY" -H "Idempotency-Key: evt-2026-0001" -H 'Content-Type: application/json' \
  -d '{"from":"noreply@notify.example.com","to":"user@example.org","template":"otp","template_version":1,
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
cmd/ditronics-mail   CLI entrypoint (serve | worker | migrate | provisioning | ops)
internal/api         HTTP handlers          internal/service   auth, accept, dispatch state machine, admin
internal/worker      claim/dispatch loop    internal/mailer    MIME + SMTP transport (outcome classification)
internal/tmpl        templates + schemas    internal/secure    AES-GCM + keyed HMAC
internal/db          pool + migrations      client/            dependency-free Go client
test/                acceptance tests       deploy/            nginx + systemd; Dockerfile, compose.yaml at root
```

## Current limitations (by design for this release)

One recipient per message; no attachments/CC/BCC/custom headers/raw HTML; ASCII addresses only; no dashboard,
webhooks, bounce ingestion or automatic suppression (operators review Mailcow failures and add suppressions manually);
single outbound server; no HA. Not a public bulk-mail service.

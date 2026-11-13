---
name: njiwa-mail-integration
description: Integrate an application with a self-hosted Njiwa-Mail instance to send transactional email (OTP, password reset, notices), read message status, verify webhooks and manage keys. Use when asked to send email through Njiwa-Mail or to write code that calls its /v1 API.
---

# Njiwa-Mail integration

Njiwa-Mail is a **self-hosted** transactional email service. There is no central hosted endpoint: the base URL is the user's own instance (`NJIWA_MAIL_URL`). In examples use `https://mail-api.example.com`.

## Ground rules (never break these)

1. **Only use documented endpoints, fields and states.** The contract is `/openapi.yaml`; every page is available as `.md`; `/llms.txt` is the index. If something is not there, say it is not documented. Do not invent endpoints, SDK package names or states.
2. **Server side only.** Keys (`dmk_sbx_…` sandbox, `dmk_liv_…` live) never go into a browser, mobile app, URL, log or repository. Read them from `NJIWA_MAIL_API_KEY`.
3. **One `Idempotency-Key` per logical event** (8 to 128 chars of `A-Za-z0-9._:-`), reused on every retry of that event, new for a new event.
4. **Never say "delivered".** `submitted` means the mail server accepted the message. There is no `delivered` state or event.
5. **Retry only** timeouts, network errors, `429` and `5xx`, with the same key, honouring `Retry-After`. Fix the request for every other `4xx`.
6. **No npm, PyPI or Go-proxy package exists.** Go: `github.com/njiwa-tz/mail/njiwamail` from the repository (send, status, webhook verification only). TypeScript: copy `clients/typescript/index.ts`. Everything else: plain HTTP.

## Fetch order

`/llms.txt` → the page you need (`/docs/<slug>.md`) → `/openapi.yaml`. Then read the playbook that matches the task:

| Task | Read |
|---|---|
| Send an OTP | `references/otp.md` |
| Password reset | `references/password-reset.md` |
| Verify webhooks | `references/webhooks.md` |
| Create, scope, rotate or revoke keys | `references/keys.md` |
| Diagnose DNS or domain readiness | `references/dns.md` |
| Managed Postfix vs existing SMTP | `references/transports.md` |

## The call

```bash
curl -sS -X POST "$NJIWA_MAIL_URL/v1/emails" \
  -H "Authorization: Bearer $NJIWA_MAIL_API_KEY" \
  -H "Idempotency-Key: $(uuidgen)" \
  -H "Content-Type: application/json" \
  -d '{"from":"noreply@notify.example.com","to":"user@example.org","template":"otp","variables":{"code":"482913","expires_in_minutes":5}}'
```

`202 {"id","status":"queued"}`. Read state with `GET /v1/emails/{id}` (scope `emails:read`). States: `queued`, `scheduled`, `submitting`, `retry_wait`, `submitted`, `failed`, `unknown`, `expired`, `captured` (sandbox only), `cancelled`.

## Both transports

The application API is identical for managed Postfix and for an existing SMTP server (Mailcow is optional). Never write code or instructions that require Mailcow.

## Before you finish

Check the generated code: no key in client code, an idempotency key per event reused on retries, `429`/`5xx` retried and other `4xx` not, no claim of delivery, base URL from configuration.

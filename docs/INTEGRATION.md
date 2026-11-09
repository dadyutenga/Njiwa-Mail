# Integrating an application with Ditronics Mail

Ditronics Mail is application-agnostic. Each application gets its own **project** with a **sandbox** and a **live**
environment, each with its own keys, approved sender(s), approved link origins and recipient policy.

## 1. Onboard an application (operator)

```bash
ditronics-mail project create -name billing
ditronics-mail project quotas -name billing -per-minute 30 -per-day 500 -rcpt-per-minute 3 -rcpt-per-hour 10
for M in sandbox live; do
  ditronics-mail env origins -project billing -mode $M -origin https://billing.example.com   # required for password-reset links
  ditronics-mail sender add  -project billing -mode $M -email noreply@notify.example.com -display "Billing"
done
ditronics-mail env allow -project billing -mode live -pattern tester@example.org   # live starts closed; open deliberately
ditronics-mail key create -project billing -mode sandbox -scopes emails:send,emails:read -label billing-sandbox
ditronics-mail key create -project billing -mode live    -scopes emails:send,emails:read -label billing-live -ttl 2160h
```
Hand the key to the application owner through your secret manager; it is shown once and cannot be recovered
(rotate with `key rotate`). The sender address must be authorised for the SMTP account on Mailcow.

## 2. Rules every client must follow

1. **Call from the backend only.** Never put a key in browser/mobile code.
2. **One idempotency key per logical event** (e.g. per OTP issued). Reuse it on every retry. A *new* OTP/reset gets a *new* key.
3. **Retry** timeouts, network errors, 429 (honor `Retry-After`) and 5xx with the **same** key. Do not retry other 4xx.
4. **Uncertain result ⇒ never fall back to another provider.** Retry later with the same key or report failure.
5. `202` means *durably queued*, not sent. `submitted` means Mailcow acknowledged; it is not delivery.
6. **Your app owns token/OTP validity.** TTL only stops *our worker* sending stale mail; it cannot recall mail already accepted.
   Always check real expiry/attempts/used-state when the user submits the code.
7. Keep account-recovery responses generic (do not reveal whether an account exists); apply per-user/IP limits in your app.

## 3. Go (dependency-free client)

```go
import mail "github.com/ditronics-tz/mail/client"   // or copy client/client.go

c := &mail.Client{BaseURL: os.Getenv("DITRONICS_MAIL_URL"), APIKey: os.Getenv("DITRONICS_MAIL_API_KEY")}
key := mail.NewIdempotencyKey("otp")               // once per OTP issued
_, err := c.Send(ctx, key, mail.Email{
    From: "noreply@notify.example.com", To: user.Email, Template: "otp", TemplateVersion: 1,
    Variables: map[string]any{"name": user.Name, "code": code, "expires_in_minutes": 5}, TTLSeconds: 300,
})
```

## 4. Any other language

Implement the OpenAPI contract in [openapi.yaml](openapi.yaml): one `POST`, Bearer auth, `Idempotency-Key` header,
the retry rules above. Generate a client with any OpenAPI generator if you prefer.

## 5. Templates

| Template | Variables | Default / max TTL |
|---|---|---|
| `otp` | `name?`, `code` (4–12 alnum), `expires_in_minutes` | 300s / 900s |
| `password-reset` | `name?`, `reset_url` (https, origin approved), `expires_in_minutes` | 300s / 900s |
| `account-notice` | `name?`, `title` (≤100), `message` (≤1000) | 1h / 24h |

Each is available in English and Kiswahili: send `"locale": "sw"` (default `en`; a version without Kiswahili content falls back to English).

Templates live in the database as immutable published versions. `template_version` is optional: leave it out and the newest
published version is pinned when the request is accepted; later edits never change mail already queued. Send a number to pin a
specific version. Variables are always escaped and never evaluated as template code. New copy is a new version published
from the admin UI, not a per-request override. A project can also have its own templates, which other projects cannot use.

Inbound mail, suppressions and webhooks for applications: see [openapi.yaml](openapi.yaml), [WEBHOOKS.md](WEBHOOKS.md)
and, for SACAS, [SACAS_INTEGRATION.md](SACAS_INTEGRATION.md).

## 6. Example: SACAS (one client among many)

SACAS (Go) already abstracts mail behind `NotificationService`; it is integrated like any other client, from its own repo:

- `SendEmailOTP` → `otp` v1 (`expires_in_minutes: 5`, `ttl_seconds: 300`, matches SACAS's "valid for 5 minutes").
- `SendTransactionalEmail(to, subject, body)` → `account-notice` v1 (`title=subject`, `message=body`).
- `SendPasswordReset` — **note**: SACAS currently emails an admin-generated *temporary password*, not a reset link,
  so `password-reset` (link-based) does not fit. To preserve current behaviour it can use `account-notice` with a short TTL
  (e.g. 900s); the better long-term design is a link-based reset using the `password-reset` template, or a dedicated
  `temporary-password` template here. That is a product decision for SACAS owners.
- SACAS's existing `EmailProvider.Send(to, subject, plain, html)` interface takes pre-rendered content, which this service
  deliberately does not accept; map at the `NotificationService` method level, keep the provider switch
  (`EMAIL_PROVIDER`) configurable for new requests, and keep SendGrid as the default until the release gate passes.

No SACAS code was changed by this repository.

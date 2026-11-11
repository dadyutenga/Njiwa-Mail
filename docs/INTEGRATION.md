# App integration

Every Njiwa app uses this contract. There is no per-app adapter and no app-specific mail package in this repository. An app is a project with its own API keys.

The application API is `/v1`. The operator API (`/admin/v1`) is not for apps.

## 1. What the operator gives the app

One project, two environments, two keys. The secret is shown once.

```bash
njiwa-mail project create -name billing
njiwa-mail env origins -project billing -mode sandbox -origin https://billing.example.com
njiwa-mail env origins -project billing -mode live    -origin https://billing.example.com
njiwa-mail sender add -project billing -mode sandbox -email noreply@notify.example.com -display "Billing"
njiwa-mail sender add -project billing -mode live    -email noreply@notify.example.com -display "Billing"
njiwa-mail key create -project billing -mode sandbox -scopes emails:send,emails:read -label billing-sandbox
njiwa-mail key create -project billing -mode live    -scopes emails:send,emails:read -label billing-live
```

Add `emails:send_raw` only for an app that sends its own subject and body. Add `inbound:read` or `suppressions:manage` only when that app needs them.

Put the key in the app's server configuration:

| Variable | Value |
|---|---|
| `NJIWA_MAIL_URL` | `https://mail-api.example.com` |
| `NJIWA_MAIL_API_KEY` | the key (`dmk_sbx_…` or `dmk_liv_…`) |
| `NJIWA_MAIL_FROM` | an approved sender for that key's environment |

Never put the key in a browser, a mobile app, or a frontend build.

## 2. Send

```http
POST /v1/emails
Authorization: Bearer <api key>
Idempotency-Key: <one id per logical event>
Content-Type: application/json

{
  "from": "noreply@notify.example.com",
  "to": "user@example.org",
  "template": "otp",
  "locale": "sw",
  "variables": { "name": "Ada", "code": "482913", "expires_in_minutes": 5 }
}
```

`202` body: `{"id":"…","status":"queued"}` (or `scheduled` when `send_at` is in the future). A repeat with the same key and the same body returns the same id. The same key with a different body returns `409`.

| Field | Rule |
|---|---|
| `template` | `otp`, `password-reset`, or `account-notice`, or a template published for that project |
| `locale` | `en` (default) or `sw`. Missing Kiswahili content falls back to English |
| `template_version` | Omit it to pin the newest published version. A number pins that version |
| `variables` | Names and types from the template schema. HTML is escaped |
| `ttl_seconds` | Optional. The worker will not start submission after this |
| `reply_to`, `cc`, `bcc` | Optional. Together with `to`, at most 10 addresses |
| `priority` | `high` or `normal`. OTP and password-reset default to `high` |
| `send_at` | Optional RFC3339 time, at most 7 days ahead |
| `metadata.request_id` | Optional correlation id. Not a secret |
| Raw send | Omit `template`. Send `subject` plus `html` and/or `text`. Requires `emails:send_raw` |

`otp` variables: `name` (optional), `code` (4–12 letters or digits), `expires_in_minutes`.
`password-reset` variables: `name` (optional), `reset_url` (https, origin approved for the environment), `expires_in_minutes`.
`account-notice` variables: `name` (optional), `title`, `message`.

## 3. Status, cancel, batch

```http
GET /v1/emails/{id}
Authorization: Bearer <api key>
```

The body is metadata: state, attempts, safe reason codes. It never includes variables, tokens, or the recipient.

```http
POST /v1/emails/{id}/cancel
Authorization: Bearer <api key>
```

Cancels a message that is still `queued` or `scheduled`. `submitted` cannot be cancelled.

```http
POST /v1/emails/batch
Authorization: Bearer <api key>

{ "messages": [ { "idempotency_key": "evt-1", "from": "…", "to": "…", "template": "otp", "variables": { } } ] }
```

Up to 100 messages. Each item has its own key and its own result. One rejection does not undo the others.

States an app may see: `queued`, `scheduled`, `submitting`, `submitted`, `retry_wait`, `failed`, `unknown`, `expired`, `captured`, `cancelled`. `submitted` means the SMTP server accepted the message. There is no `delivered` state.

## 4. Rules

1. Call from the app's backend only.
2. One idempotency key per logical event (one OTP, one reset). A new event gets a new key. Retries reuse the key.
3. Retry timeouts, network errors, `429` (honor `Retry-After`) and `5xx` with the same key. Do not retry other `4xx`.
4. If the result is uncertain, do not send the same event through another provider.
5. The app owns codes, links, expiry, and sessions. This service only carries the message.
6. Account-recovery responses stay generic. Do not tell the user whether the address exists.
7. Sandbox keys (`dmk_sbx_…`) never leave the server. Live keys (`dmk_liv_…`) submit to Mailcow.

## 5. Clients

Go:

```go
import mail "github.com/njiwa-tz/mail/njiwamail"

c := mail.New(os.Getenv("NJIWA_MAIL_URL"), os.Getenv("NJIWA_MAIL_API_KEY"))
key := mail.NewIdempotencyKey("otp")
_, err := c.Send(ctx, key, mail.Email{
    From: os.Getenv("NJIWA_MAIL_FROM"), To: user.Email,
    Template: "otp", Locale: "sw",
    Variables: map[string]any{"name": user.Name, "code": code, "expires_in_minutes": 5},
})
```

`github.com/njiwa-tz/mail/client` is the same client. It retries with the same idempotency key.

TypeScript: `clients/typescript/index.ts` (`NjiwaMail.send`).

Any other language implements this document. The machine-readable contract is [openapi.yaml](openapi.yaml). Webhook verification is [WEBHOOKS.md](WEBHOOKS.md).

## 6. Webhooks

Subscribe in the operator UI or `POST /admin/v1/webhooks`. Payloads contain ids and states, not bodies or tokens. Verify `X-DM-Signature` before trusting the body. The app stores the message id returned by send if it needs to match a later `email.bounced` event to a user.

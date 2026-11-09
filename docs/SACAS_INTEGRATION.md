# SACAS integration (Go/Gin)

**Status: not implemented.** The SACAS backend is a different repository and was not touched. This document is the design for section 8 of the v2 plan, written against the API that exists in this repository. The Go client it uses is `client/` here (copy the package or import it; it depends only on the standard library).

## 1. Provision (operator, once)

```
ditronics-mail project create -name sacas
ditronics-mail env origins -project sacas -mode live -origin https://<sacas host>
ditronics-mail sender add  -project sacas -mode live -email security@notify.<domain> -display "SACAS"
ditronics-mail key create  -project sacas -mode live -scopes emails:send,emails:read -label sacas-backend
```

Repeat for `-mode sandbox` for staging. The same can be done in the admin UI. The live environment restricts recipients to an allowlist by default: add staff addresses for the warm-up, and turn the restriction off only for the final rollout step.

## 2. The `mailer` package in SACAS

```go
package mailer

type Mailer interface {
    SendOTP(ctx context.Context, eventID, to, name, code string, expiresMin int, locale string) error
    SendPasswordReset(ctx context.Context, eventID, to, name, resetURL string, expiresMin int, locale string) error
    SendNotice(ctx context.Context, eventID, to, name, title, message, locale string) error
}
```

Two implementations: the current provider, and `ditronicsMailer` below. Choose by configuration (`MAIL_PROVIDER=current|ditronics`) at start-up so a switch or rollback is a config change and a restart.

```go
type ditronicsMailer struct {
    c    *client.Client // BaseURL + APIKey from server configuration only
    from string
}

func (m *ditronicsMailer) SendOTP(ctx context.Context, eventID, to, name, code string, expiresMin int, locale string) error {
    _, err := m.c.Send(ctx, "otp-"+eventID, client.Email{
        From: m.from, To: to, Template: "otp", Locale: locale, RequestID: eventID,
        Variables: map[string]any{"name": name, "code": code, "expires_in_minutes": expiresMin},
    })
    return err
}
```

`password-reset` takes `name`, `reset_url`, `expires_in_minutes`; `account-notice` takes `name`, `title`, `message`. `locale` is `en` or `sw`.

## 3. Rules

| Rule | Why |
|---|---|
| `eventID` is a stable id of the application event (the OTP row id, the reset-request id). A new OTP or reset request gets a new id. | It becomes the `Idempotency-Key`: a retry can never create a second email. |
| On timeout, network error, 429 or 5xx, retry with the **same** key. The client does this (2 retries by default). | The outcome is uncertain; only the same key is safe. |
| **Never fall back to the other provider** when the result is uncertain. | The message may already be queued; a fallback would send two codes. |
| A 4xx other than 429 is final. Log the `code`, do not retry. | `recipient_suppressed`, `sender_not_approved`, `url_origin_not_approved` and so on will not fix themselves. |
| Token generation, hashing, storage, expiry and sessions stay in SACAS. | Ditronics Mail only carries the message. |
| Account-recovery responses stay generic ("If the account exists, we sent an email"), whatever the mailer returned. | Do not reveal whether an account exists. |
| The API key lives in SACAS server configuration, never in the frontend. | It can send mail as SACAS. |
| A 202 means queued. `submitted` means Mailcow accepted it. Never show "delivered". | The service cannot prove delivery. |
| `reset_url` must start with an origin approved for the environment. | Otherwise the request is rejected with `url_origin_not_approved`. |

## 4. Optional: flag dead addresses

Create a webhook endpoint for the SACAS environment subscribed to `email.bounced`, and add a receiver:

```go
r.POST("/internal/mail-events", func(c *gin.Context) {
    body, _ := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
    ev, err := client.VerifyWebhook(cfg.MailWebhookSecret, c.GetHeader("X-DM-Signature"), body, time.Now())
    if err != nil { c.Status(http.StatusUnauthorized); return }
    // ev.ID: skip if already processed. ev.Data: {"message_id","type","status","suppressed"}
    c.Status(http.StatusNoContent)
})
```

The payload carries `message_id`, not the address. SACAS must keep `message_id → user` (store the id returned by `Send`) to know whose address bounced. For `type = hard` with `suppressed = true`, mark the user's email as undeliverable and ask for a new one at next login. See `docs/WEBHOOKS.md`.

## 5. Rollout

1. Staging on a **sandbox** key: nothing leaves the server; check the rendered mail under Sandbox in the admin UI, in both languages.
2. Staging on a **live** key with recipients restricted to internal test accounts: OTP and reset end to end; check SPF, DKIM and DMARC pass in the received headers.
3. Production, `MAIL_PROVIDER=ditronics`, allowlist of SACAS staff.
4. Remove the recipient restriction for all users. Watch the Overview page (queue age, `unknown`, bounce rate) for the first days.

Rollback at any step: set `MAIL_PROVIDER=current` and restart. Messages already queued in Ditronics Mail still go out.

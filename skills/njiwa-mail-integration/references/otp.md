# Playbook: send an OTP

You own the code, its expiry, attempt limits and the session. Njiwa-Mail only carries the message.

1. Generate the code on your server and store only a hash with an expiry and an attempt counter.
2. Generate **one** idempotency key for this OTP issue and store it with the record.
3. `POST /v1/emails` with `template: "otp"` and `variables: { name?, code (4-12 letters or digits), expires_in_minutes (1-60) }`. `ttl_seconds` defaults to 300 (max 900) for auth templates.
4. On timeout, `429` or `5xx`: retry with the **same** key. If it still fails, tell the user to try again; do not send through another provider while the result is uncertain.
5. A resend request is a **new** event: new code, new key.
6. Respond generically (do not reveal whether the address has an account).
7. Do not log the code. The status endpoint never returns it.

Errors you must handle: `recipient_suppressed`, `recipient_unsubscribed`, `rate_limited` (wait `Retry-After`), `domain_not_ready` (live DNS not ready: an operator problem, not the user's).

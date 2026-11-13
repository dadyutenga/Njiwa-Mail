# Playbook: password reset

1. Create a single-use reset token on your server, store a hash, expiry 15 to 60 minutes.
2. Build `reset_url` as an `https` URL on an **origin your operator approved** for that environment (`njiwa-mail env origins`). Any other origin is refused (`422 url_origin_not_approved`).
3. `POST /v1/emails` with `template: "password-reset"`, `variables: { name?, reset_url, expires_in_minutes (1-1440) }`, one idempotency key per reset request.
4. Return the same generic answer whether or not the address exists.
5. Retry rules are as for OTP. A second reset request is a new event with a new key.
6. After use or expiry the token is dead on your side; the email cannot be recalled (`submitted` cannot be cancelled), so keep the expiry short.

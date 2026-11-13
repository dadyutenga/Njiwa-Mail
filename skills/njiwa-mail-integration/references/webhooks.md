# Playbook: webhooks

Events: `email.submitted`, `email.failed`, `email.unknown`, `email.expired`, `email.bounced`, `email.delivery_deferred`, `email.delivery_remote_accepted`, `email.delivery_bounced`, `email.delivery_expired_in_queue`, `inbound.received`. There is no `email.delivered`.

Endpoints are created in the dashboard, which shows the signing secret (`whsec_…`) once.

Verify every request:

1. Read the raw body before parsing.
2. Header `X-DM-Signature: t=<unix>,v1=<hex>`.
3. `HMAC-SHA256(secret, t + "." + raw_body)` as hex; compare to `v1` in constant time.
4. Reject if `t` is more than 5 minutes from now.

Then: answer 2xx within 10 s, store `id` and ignore ids already handled (at least once delivery, retries and replays reuse `id`), do slow work after answering, do not rely on ordering (call `GET /v1/emails/{id}` for current state), ignore unknown fields (`schema_version` is 1). Go: `njiwamail.VerifyWebhook`. TypeScript: `verifyWebhook` in the client file.

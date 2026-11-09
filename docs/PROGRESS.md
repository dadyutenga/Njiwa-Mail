# Progress

The public API is `/v1` and the operator API is `/admin/v1`. There is no `/v2`.

| Feature | State | Tests |
|---|---|---|
| Template send, idempotency, queue, sandbox, honest states | Done | `test/api_test.go` |
| Locales, DB templates, inbound, bounces, webhooks, DNS, admin UI | Done | `test/v2_test.go`, frontend typecheck |
| Raw send (`emails:send_raw`), cc/bcc/reply-to, attachments, priority, schedule, cancel, batch | Done | `TestRawSendScheduleCancelAndBatch` |
| Domain TXT challenge and live-send gate | Done | `TestDomainGateAndSearch` |
| TOTP required for owner/admin when `TOTP_REQUIRED=true`, recovery codes, sessions, password reset token | Done | `TestTOTPAndPrivacy` |
| Recipient export and deletion | Done | `TestTOTPAndPrivacy` |
| Encryption key re-encrypt | Done | `TestReencrypt` |
| Inbound search, reply link, forward route | Done | covered by inbound store path |
| Analytics, domains, alerts, security, API docs, scheduled pages | Done | frontend typecheck |
| Shared API-key integration for every app | Done | `docs/INTEGRATION.md`, `ditronicsmail`, `clients/typescript` |
| SMTP submission listener | Config hook `INGRESS_ADDR` is reserved; apps send over HTTPS `/v1` | — |
| Daily owner summary | Computed and logged by the scheduler; not handed to Mailcow from this process | — |
| Backup and restore scripts | Written in `scripts/`; not executed against a production database | — |
| Real Mailcow delivery | Not run | no credentials in this session |

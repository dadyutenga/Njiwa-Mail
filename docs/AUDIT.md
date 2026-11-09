# Phase 0 audit — existing code against v2 section 3

Date: 2026-11-09. Audited commit: `941df02` (v1 Go service), before any v2 change.
Method: read every Go file and the migration, then ran `go test ./...` against PostgreSQL 18 (31 tests, all passing).

| Item | Status | Evidence | Gap closed in v2 |
|---|---|---|---|
| Projects and environments | **Done** | `0001_init.sql`: `projects`, `environments` (`sandbox`/`live`, composite FK). Every query filters project AND environment (`TestProjectEnvironmentIsolation`). | — |
| API keys | **Done** | Prefix + SHA-256 hash, scopes, expiry, revocation, rotation with grace; secret printed once by the CLI (`service/auth.go`, `admin.go`). | Added `last_used_at`; new scopes `inbound:read`, `suppressions:manage`. |
| `POST /v1/emails` | **Done** | 202 only after `tx.Commit` (`accept.go`); strict JSON, 24 validation cases (`TestValidationAndInjection`, `TestDatabaseFailureNoAcceptance`). | Added `locale`; `template_version` became optional. |
| Idempotency | **Done** | `UNIQUE (project_id, environment_id, idempotency_key)`; keyed HMAC of the canonical request; 409 on mismatch; 30-way concurrency test. | Fingerprint now records the *requested* version so a retry survives a publish. |
| Dispatch worker | **Done** | Single-statement `FOR UPDATE SKIP LOCKED` claim with lease token, committed before any SMTP I/O (`dispatch.go`). | — |
| State machine | **Done** | All eight states including `unknown`; expired lease goes to `unknown`, never back to `queued` (`ReapLeases`). | — |
| SMTP transport | **Done** | TLS always verified (no skip option), finite timeouts, pre-DATA vs post-DATA classification (`mailer/smtp.go`, `TestSMTP*`). | — |
| Sandbox transport | **Done** | Sandbox messages never reach the transport (`TestSandboxSeparationAndCapture`). | — |
| Rate limits | **Done** | Per project and per recipient, atomic upsert inside the accept transaction (`TestRateLimitUnderConcurrency`). | — |
| Suppressions | **Done** | Checked at accept time; manual only. | Automatic hard-bounce suppression, `source`, `bounce_id`. |
| Payload encryption | **Done** | AES-256-GCM bound to the message id; cleared on every terminal state; retention purge. | Inbound bodies and webhook secrets use the same box. |
| Tests | **Done** | 31 tests; `go vet` and `-race` clean. | 16 more tests for v2 (see RELEASE_REPORT). |

## Partial items found (fixed before new modules)

| Item from section 5.1 | Was | Now |
|---|---|---|
| Retry backoff 15/45/120 s with jitter, never past TTL | Done | unchanged |
| Headers: stable `Message-ID`, `Auto-Submitted` | Done | unchanged |
| Header `X-DM-Message-ID` | Missing | added (`mailer/mime.go`) |
| Envelope sender = bounce address | Missing (envelope = header From) | `BOUNCE_ADDRESS` is used as `MAIL FROM` |
| `locale`, version pinned at accept time | Missing (templates compiled into the binary) | templates in the database, version id pinned on the message |
| Webhook outbox in the state-change transaction | Missing | `dm_emit()` called inside every transition |

## Things that were not in the section 3 table but matter

- The repo already had a read-only operator console (`/console/*`, one shared password). It is **kept unchanged** because the existing frontend calls it. The v2 admin API is separate (`/admin/v1`, per-user accounts, roles, CSRF, audit).
- `messages.message_id_header` was renamed to `smtp_message_id` to match the v2 schema.
- `messages.api_key_id` is now nullable: an operator test-send has no API key.

## Blockers

None for the backend code. Operator inputs are still needed for anything real: Mailcow host and submission credentials, the real domains, DNS access. See `docs/RELEASE_REPORT.md`.

# Release report — Ditronics Mail v1 (2026-11-09)

## Decision: **Working staging build — NOT released for production traffic**

The application is implemented and its automated gates pass. Production/controlled release is blocked on
infrastructure inputs that were not available to the implementer (see Blockers). No claim is made that the
service is deployed, that real email delivery works, or that it is production-ready.

## Deviation from the plan (recorded, per plan §4 "record mismatch")

The plan names Laravel/PHP. The implementation machine has **no PHP/Composer and no sudo**, while Go and PostgreSQL were available.
The service was therefore built in **Go** (single static binary, stdlib + `pgx`). All other architectural decisions are unchanged
(PostgreSQL-backed durable queue with `FOR UPDATE SKIP LOCKED`, code-owned templates, SMTP to Mailcow, CLI-first admin).
If Laravel is a hard requirement for your team, the specification, schema, state machine and tests carry over; the code does not.

## Evidence (real results from this build)

Command: `go vet ./... && go test ./... -race -count=1` against a real PostgreSQL 18 — **all pass, no data races**.

| Mandatory test (plan §9) | Result | Where |
|---|---|---|
| Invalid / revoked / expired key; scope | PASS | `TestInvalidAndRevokedKey` |
| Project/environment isolation | PASS | `TestProjectEnvironmentIsolation` |
| Approved sender enforcement | PASS | `TestApprovedSenderEnforcement` |
| Idempotency sequential + 30-way concurrent → one id, one submission | PASS | `TestIdempotencySequentialAndConcurrent` |
| Payload conflict → 409 | PASS | `TestPayloadConflict409` |
| Database failure → no 202 | PASS | `TestDatabaseFailureNoAcceptance` |
| Worker interruption before claim | PASS | `TestWorkerInterruptionBeforeClaim` |
| Worker interruption during SMTP → `unknown`, no resubmission | PASS | `TestWorkerInterruptionDuringSMTPBecomesUnknown` |
| Temporary failure: bounded retry, honors TTL | PASS | `TestTemporaryFailureBoundedRetryHonorsTTL` |
| Expiry: expired message never starts SMTP | PASS | `TestExpiredMessageNeverSubmits` |
| Sandbox separation (never contacts SMTP) | PASS | `TestSandboxSeparationAndCapture` |
| Rate limit under concurrency (24 parallel → exactly 5 accepted) | PASS | `TestRateLimitUnderConcurrency`, `TestRecipientLimitAndReplayNotCharged` |
| Secrets absent from logs/status; payload encrypted at rest | PASS | `TestSecretsNeverLoggedOrReturned` |
| Template/URL validation & injection (24 cases) | PASS | `TestValidationAndInjection` |
| Manual suppression | PASS | `TestManualSuppressionAndRecipientPolicy` |
| Restart persistence + worker resume | PASS | `TestRestartPersistsAndWorkerResumes` |
| **Real controlled delivery to an inbox** | **NOT RUN** | no SMTP credentials/inbox available |
| **Application-level recovery test (token expired/used)** | **NOT RUN** | belongs to the client app; no client integrated |

Also tested: SMTP outcome classification against a protocol-level fake server with real STARTTLS (accepted; RCPT 4xx→retryable;
RCPT 5xx→failed; final 4xx/5xx; connection dropped after DATA→`unknown`; no STARTTLS; auth failure; connect failure; timeout),
**TLS verification enforced** (untrusted certificate → no credentials or mail sent), MIME header-injection rejection, HTML escaping,
Go client retry rules (same idempotency key on every retry; no retry on 4xx).
A real-binary smoke test over HTTP (sandbox key): `202` → `captured`, operator capture readable, no secret in logs.

Bug found and fixed during testing: a net/textproto pipeline slot was left open after the DATA terminator, which would have hung a worker
goroutine on `QUIT` after every accepted message. Caught by the first SMTP protocol test.

## Fake vs real

Fake/simulated: the SMTP server in tests, the `fakeTransport` used for deterministic worker-failure tests. Real: PostgreSQL, the HTTP stack,
the SMTP client code path (against the fake server), encryption, rate limiting, the built binary in the smoke test.
**Nothing was sent through Mailcow.**

## Blockers / not done

1. **Mailcow**: no SMTP host/credentials/approved sender/test inbox → real delivery, headers, SPF/DKIM/DMARC, reverse DNS and queue audit are unverified. Run `ditronics-mail smtp-check`, then the OPERATIONS preflight.
2. **Deployment**: `Dockerfile`, `compose.yaml`, `deploy/nginx.conf` and systemd units are written; **compose stack and HTTPS routing were not deployed or exercised** (see Image build below). Hostname/TLS certificate not provided.
3. **Backups/restore**: not configured; no restore test.
4. **Client integration**: this service is deliberately independent. No client application was modified. `client/` (Go) and `docs/INTEGRATION.md` are ready; SACAS observations are in the integration guide (notably: SACAS emails a generated password rather than a reset link).
5. **Operator access to Mailcow logs** for failure investigation: not verified.
6. Worker supervision/retention scheduling are implemented (worker heartbeat + built-in retention loop) but unverified on a real host.

## Image build

**Not verified.** A first `podman build` failed on a Dockerfile bug (Alpine already has a `mail` user), which was fixed (user renamed `dmail`).
The rebuild was stopped before completing because it was consuming the operator's mobile data. The Dockerfile and `compose.yaml` have therefore
never been built or run end to end. The Go binary itself builds and runs natively (see smoke test). Build the image on a machine with a normal connection before relying on it.

## Rollback

See `docs/OPERATIONS.md#rollback`. Nothing is deployed, so there is currently nothing to roll back.

## Remaining full-product backlog (not implemented)

Postfix log collector/bounce processor; automated suppression; signed webhooks; operational dashboard; admin auth/roles/MFA; domain onboarding;
template editor/publishing; analytics/complaint feedback; scale/availability (HA, restore drills, isolated reputation); external customers/billing.

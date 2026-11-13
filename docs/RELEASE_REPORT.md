# Release report — Njiwa-Mail (main, 2026-11-12)

## Decision: **Release candidate for a controlled staging install — NOT released for production traffic**

Everything that can be proven without a dedicated mail host is implemented, tested and green in CI. What is **not** proven is the part
that needs a real server with outbound port 25, DNS you control and a Gmail and an Outlook mailbox: managed mode sending real mail
(G05), real delivery and bounces (E06/E07), other distributions (E12), a real Mailcow or generic SMTP server (G04), a mode switch with
real queued mail (G06), and an install by someone who did not write the code (G09). Until those are done and their evidence is in
`docs/evidence/`, this must not carry production mail. No claim is made that mail reaches an inbox.

The development server (`rida-server`) could not be used for the managed-mode proof: it is a live multi-service host whose ports 25/465/587
belong to Mailcow, and the installer (correctly) refuses a host that already runs a mail server. Use a clean VPS.

## What is verified, and where

| Claim | Evidence |
|---|---|
| Go suite, race detector, real PostgreSQL 16 | CI job `test` (`go test ./... -race`); also run on a throwaway PostgreSQL 16 in Docker on the development server |
| Frontend: typecheck, 67 unit tests, lint (0 errors), build | CI job `frontend` |
| Browser tests (Chromium) against the Compose stack: wrong password, sign-in and reload, protected redirect, delivery-mode wizard, add domain | CI job `e2e`; also run on the development server against an isolated Compose project |
| The shipped stack starts from nothing, is ready, accepts a sign-in, refuses an unauthenticated admin call, runs `rekey -check` and `retention` | CI job `smoke` |
| Managed installer against **real Postfix and OpenDKIM**: no open relay, mutual-TLS-only submission, sender restrictions, DKIM-signed mail | CI job `managed-mta` (Ubuntu 26.04 runner: Postfix 3.10.6, OpenDKIM 2.11.0~beta2); earlier on Postfix 3.8.6 |
| No known vulnerabilities in dependencies and standard library | CI job `govulncheck` (blocking; it found Go 1.26.0 stdlib, brotli and Fiber advisories on its first run, all fixed) |
| Reproducible build; dependency inventory | CI job `supply-chain` (two builds from different directories are byte-identical; SBOM artifact) |
| Security matrix over every route (auth, CSRF, no secrets in any GET) | `TestEveryRouteRequiresAuthAndCSRF`, `TestNoSecretInAnyAdminResponse`; `docs/SECURITY_REVIEW.md` |

The detailed ticket-by-ticket record is `STATUS.md`; open items are `docs/REMAINING_WORK.md` and `HANDOFF.md`.

## What changed in this release (since the first build)

Operator permission and optional two-step sign-in (F09); sealed attachments, retention and data-subject export/delete (F10); webhook
`schema_version` (F07); reconciliation queue (F02); signed bounce return paths (F06) and per-domain return paths (D04); encryption-key
rotation (C02); the mail queue agent and dashboard page (E10); the Mailcow provider boundary with audited writes (C05); Mail host,
profile editing and Kiswahili for every string (B07/B09); generated endpoint/schema docs, admin OpenAPI, examples and troubleshooting
(A01/G08); CI hardening (A02/G07). Migrations `0016`–`0021` are additive.

## Compatibility

Only what has actually been run is listed as verified.

| Component | Verified | Not verified |
|---|---|---|
| Host OS for managed mode | Ubuntu 26.04 (CI runner, installer + real Postfix/OpenDKIM e2e); a distribution shipping Postfix 3.8.6 (earlier build container) | **Debian 12, Ubuntu 22.04, Ubuntu 24.04 on a real VPS** (E12) |
| Postfix | 3.10.6 (CI), 3.8.6 (earlier) | 3.7 (Debian 12), 3.6 (Ubuntu 22.04) |
| OpenDKIM | 2.11.0~beta2 (CI), 2.11.0 (earlier) | other packages |
| PostgreSQL | 16 (CI and test runs), 18 (earlier build) | 14, 15, 17 |
| Go | 1.26.11 (`go.mod` toolchain) | older than 1.26.11 (advisories) |
| Node (dashboard build) | 22 (CI), 24 (development server) | — |
| Docker Compose | v2 syntax; v5.7 and the GitHub runner's version | older than v2.24 for `!override` use in overrides (not needed by the shipped files) |
| Existing SMTP | a protocol-level fake with real STARTTLS and TLS verification | a real Mailcow, Postfix or Exim server (G04) |
| Browser (e2e) | Chromium via Playwright 1.49.1 | Firefox, Safari |

## Rollback runbook

**Before every upgrade:** `scripts/backup.sh` (with `BACKUP_QUEUE=1` on a managed host), copy it off the machine, and keep the current image tag.
Back up `ENCRYPTION_KEY`, `HMAC_KEY` and, during a rotation, `ENCRYPTION_KEY_PREVIOUS`, separately from the database backup.

1. **Application rollback:** deploy the previous image tag (`docker compose up -d` with the old tag). Migrations are additive, so an older binary
   starts against the newer schema and ignores what it does not know. The database does not need to be restored for a code-only rollback.
2. **What an older binary cannot do after this release** (data is not lost; rolling forward again restores it):
   - *Attachments written after F10 are sealed on disk.* A pre-F10 binary would serve the sealed file as if it were the attachment; downloads
     are broken until you roll forward again.
   - *Two-step sign-in and the operator flag are ignored* by a pre-F09 binary: a user with MFA could sign in with a password alone, and every
     admin is an operator. Do not leave a rolled-back install exposed; if MFA matters, restore access controls (IP allowlist) first.
   - *Signed return paths (`bounces+<id>.<tag>@…`)*: a pre-F06 binary does not recognise them, so a bounce for a message sent just before the
     rollback is refused with "user unknown". Set `BOUNCE_VERP=off` **before** rolling back and let in-flight mail drain.
   - *Webhook `schema_version`, per-domain return paths, DKIM rotation state* are simply unused.
   - *Rotated keys:* a binary started without `ENCRYPTION_KEY_PREVIOUS` cannot open data still sealed under the old key. Finish `rekey`, or keep both keys.
3. **Database restore** (data loss or a bad migration): stop the stack, `scripts/restore.sh <backup>`, start the previous image, run `njiwa-mail installer plan`
   (it must report no change), then send a sandbox test. On a managed host the restore also puts back the Postfix/OpenDKIM configuration, keys and certificates.
4. **Mode rollback without code rollback:** Installation → Delivery profiles → *Review switching back*; the previous profile is kept for this.
   Messages already sent keep the profile that actually sent them.
5. **Managed host change gone wrong:** `njiwa-mail installer rollback` restores the previous Postfix/OpenDKIM files from the backup the last `apply` took;
   every apply also rolls itself back automatically when `postfix check` or `opendkim -n` fails.

## What a person must still do before production

1. A clean VPS, then `HANDOFF.md` section 3 in order (G05), with the evidence written into `docs/evidence/`.
2. E06/E07 (real delivery, queue durability across reboot and upgrade, a real bounce — including `correlated_by = verp`).
3. E12 on Debian 12 and Ubuntu 22.04/24.04, then fill the compatibility table above.
4. G04: a real Mailcow and a generic Postfix/Exim server; G06: a mode switch with queued mail.
5. G09: someone who did not write the code installs both modes using only the documents; fix every place they get stuck.
6. A screen-reader and phone pass, and a native-speaker review of the Kiswahili.
7. An external penetration test (`docs/SECURITY_REVIEW.md`, "Not covered yet").


---

# History: the first build (v1, 2026-11-09)

Kept as the original record; the current status is above.

## Public documentation portal (2026-11-13)

**Decision:** the portal is implemented and verified as far as an automated environment can verify it; it is **not** yet published anywhere (there is no central hosted site, and the operator chooses the host). What a person must still do is listed under "Not done".

What exists: `documentation/` (Next.js 16 + Fumadocs 16 + MDX, static export, 29 pages), generated from one source: per-page `.md`, `llms.txt`, `llms-full.txt`, `openapi.yaml`/`openapi.json` and the skill `skills/njiwa-mail-integration` (`SKILL.md` + 6 playbooks). `docs/openapi.yaml` was rewritten from the handlers (it had described 3 of 14 public operations, with a wrong request schema, missing states and a wrong size limit). The audit, discrepancy list (12 items) and plan are in `docs/public-portal/AUDIT.md`.

### Evidence (commands and real results, run on `rida-server` in throwaway containers; nothing installed on the workstation)

| Check | Command | Result |
|---|---|---|
| OpenAPI drift (routes, scopes, states, error codes, request/response fields; documented examples sent to the real handlers) | `go test ./test -run TestPublicOpenAPI` | 5 tests pass. Mutation-checked: a wrong path, a wrong field and an invented error code each fail it |
| Docs vs code (every `curl-check` snippet replayed against the real handlers: 8; Go snippets compiled with `go vet`: 3; webhook events, scopes, states and error codes equal the source; only real endpoints named in the docs and the skill; no delivery claim, no Mailcow requirement) | `go test ./test -run 'TestDocs\|TestDocumented'` | 6 tests pass. Mutation-checked: a wrong field in a documented request, a fake `email.delivered` event and an invented endpoint in the skill each fail it |
| Full Go suite with the race detector | `go vet ./... && go test ./... -race -count=1` | `go vet` clean; every package passes, `test` in 238 s (includes the 11 portal tests above) |
| Typecheck, lint, content lint | `npm run typecheck && npm run lint` (in `documentation/`) | clean (eslint 0 errors; content lint ok on 29 pages) |
| Unit tests (markdown conversion, 22 content-lint rejection cases, AI-service URL rules, version switching) | `npm test` | 33 pass |
| Static export | `npm run build` | 33 static routes; search index as a file |
| Snippets | `npm run check:snippets` | curl 8 validated against the OpenAPI schemas, TypeScript 3 compiled, Python 2 compiled |
| Export parity | `npm run check:export` | every page's `.md` equals the source, `llms.txt` lists and resolves every page, `openapi.yaml` byte-identical to `docs/openapi.yaml`, `openapi.json` the same document, skill files identical, one version and one commit everywhere, no secret pattern in `out/` |
| Browser tests (Chromium via Playwright 1.65.0) | `npx playwright test` | 22 pass: landing, sidebar/breadcrumb/TOC, next/previous, code tabs, keyboard search, Copy Markdown (real clipboard, refused-clipboard fallback, failure toast), Open menu (prefill link with a short prompt, copy-and-open, Escape, local origin gets no link), 404, version selector, theme, machine-readable routes, axe WCAG 2 A/AA with no serious or critical violation on six pages, mobile navigation, no horizontal scroll at 375 px |
| Frontend (dashboard) | `npm run typecheck && npm run lint && npm test` (in `frontend/`) | 0 errors; 81 tests pass (documentation link: shown with `rel="noopener noreferrer"` when configured, an explanation instead of a dead link when not) |
| Independent deployment | `docker build -f documentation/Dockerfile …`; `documentation/scripts/smoke.sh` | image builds (the build runs the lint, export and parity checks); runs as uid 101, read-only filesystem, all capabilities dropped, no environment secrets; smoke: 41 checks (200 pages, types of `.md`/`llms.txt`/`openapi.*`/search, real 404 page, **404 for `/admin/v1/*`, `/v1/*`, `/console`, `/metrics`, `/health/*`, dotfiles** including a POST with a bearer header, CSP and nosniff headers, no server version, one commit everywhere) |
| Deploy, upgrade and rollback | image `aaaa111` deployed, `bbbb222` deployed over it, rolled back to `aaaa111`, smoke after each | all passed; asking the smoke for the wrong commit failed, as it must. The run also exposed a typo in `nginx.conf` that made the container exit, fixed |

Screenshots (desktop and mobile) of the landing page, a docs page, search, the Open menu, the 404 page and the mobile navigation were produced by the browser tests (`SHOT_DIR=… npx playwright test`) and reviewed.

### Fixes this work made to existing code and docs
`docs/openapi.yaml` (above); `docs/INTEGRATION.md` and README said live keys submit to Mailcow (they use the active delivery profile; Mailcow is optional); the TypeScript client typed `status()` as the accepted response (now `MessageStatus`); the dashboard welcome page defaulted to a hard-coded third-party GitHub URL (now the real repository, with the documentation link hidden until `VITE_DOCS_URL` is set); `go.mod` gained `gopkg.in/yaml.v3` for the drift tests.

### Not done, and why
- **Not published.** No public host, certificate or DNS exists for it; `DOCS_BASE_URL` defaults to the placeholder `https://docs.example.com`. The deployment demonstration used a loopback port on a development server, not a production deployment.
- **"Open in …" URL formats are not provider-verified.** ChatGPT, Scira AI and T3 Chat use a `q` prefill that appears in community and reference-implementation documentation only; Claude's prefill is reported broken, so it copies the prompt. A person must try each in a real browser (`docs/public-portal/RELEASE_CHECKLIST.md`).
- **Single version.** Exactly one documentation version (`1.0`) exists, so the version selector has one entry; the switching logic is unit-tested with two versions, and the policy and the steps to add one are in `docs/public-portal/VERSIONING.md`.
- **`'unsafe-inline'` for scripts** in the docs CSP (the Next.js static export needs it). It is safe only because content is linted so MDX cannot carry script; documented in `DEPLOY.md`.
- Manual screen-reader and phone checks, a cold read by someone new, a coding-agent run against the skill, and the real-host evidence for managed Postfix and Cloudflare one-click that the pages describe as outstanding.
- Optional documentation-only MCP tools: deliberately not built (page `planned/documentation-mcp`).
- The brief's package documents other than `09_` (including `07_IMPLEMENTATION_BACKLOG.md`) were not supplied, so their IDs and dependencies were not followed.

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

1. **Mailcow**: no SMTP host/credentials/approved sender/test inbox → real delivery, headers, SPF/DKIM/DMARC, reverse DNS and queue audit are unverified. Run `njiwa-mail smtp-check`, then the OPERATIONS preflight.
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

## v1 API additions (2026-11-10)

The HTTP API remains `/v1` and `/admin/v1`. New behaviour covered by `go test ./test/ -run 'TestRawSend|TestDomainGate|TestPrivacy|TestReencrypt'` against local PostgreSQL:

- Raw send with scope `emails:send_raw`, cc/bcc/reply-to, attachments, priority, schedule, cancel, and batch.
- A sending domain that has been added cannot be used for live mail until its TXT challenge, SPF, DKIM and DMARC pass.
- Admin accounts use password sessions with optional TOTP two-step sign-in (F09); the installation-operator permission separates delivery infrastructure, users and settings from project work.
- Recipient export and deletion, and re-encryption onto a new key.

Not verified here: real Mailcow delivery and a restore drill. Apps integrate through `docs/INTEGRATION.md` and an API key; there is no per-app adapter. The daily summary is computed by the scheduler and written to the log. `scripts/backup.sh` and `scripts/restore.sh` are the operator commands; they were not run against a production database.

## Remaining full-product backlog (not implemented)

Postfix log collector/bounce processor; automated suppression; signed webhooks; operational dashboard; admin auth/roles; domain onboarding;
template editor/publishing; analytics/complaint feedback; scale/availability (HA, restore drills, isolated reputation); external customers/billing.

## Mail workspace and Mailcow checks (2026-11-11)

Scope: `NJIWA_MAIL_WORKSPACE_AND_UX_TASK.md`. **All results below are local**: a development stack on one machine (PostgreSQL, API, worker, scheduler, inbound listener on 127.0.0.1, Vite). No Mailcow server was available, so nothing below proves delivery through Mailcow.

| Check (task §9) | Result | Evidence |
|---|---|---|
| Workspace scope isolation (unknown project → 404, no cross-project reads or key revokes) | PASS (local, automated) | `TestWorkspaceScopeIsolation` |
| Domain added → records → verified; sender only on verified domain; delete blocked while in use | PASS (local, fake DNS resolver) | `TestWorkspaceDomainsSendersAndChecklist` |
| Compose: double submit sends once, suppression named and refused, sent copy, drafts, schedule → send now | PASS (local, automated) | `TestComposeIdempotencySuppressionAndCopy` |
| Receive → read → reply threaded (`In-Reply-To`), auto-reply from template once per sender per day, none to automatic mail, block sender, temporary inbox expiry | PASS (local, real SMTP listener on 127.0.0.1) | `TestInboxReplyAutomationAndTempInbox` |
| Bulk: duplicates/invalid listed, restart never sends twice, `List-Unsubscribe` headers, one-click unsubscribe, unsubscribed address skipped next time, complaint pauses campaign and suppresses | PASS (local, automated) | `TestBulkCampaignUnsubscribeAndComplaintPause` |
| Mail server secrets encrypted, never returned, kept on re-save | PASS (local, automated) | `TestMailServerSettingsSecrets` |
| Inbound via the listener into the Inbox UI | PASS (local, manual: Python `smtplib` → 127.0.0.1:2525 → Inbox) | dev walk |
| UI walk: 32 workspace pages × 360/768/1024/1440 px × light and dark | PASS (local): no runtime error, no horizontal page scroll after two fixes (inbox grid, table sr-only overflow) | headless Chromium walk |
| **Mailcow connection test** | **NOT RUN** | needs the real Mailcow host and submission mailbox (`docs/MAILCOW_SETUP.md` A1–A5) |
| **Test email to Gmail/Outlook with SPF/DKIM/DMARC pass** | **NOT RUN** | C3 |
| **Inbound from outside through Mailcow relay** | **NOT RUN** | B1–B2, C4 |
| **Bounce from a real remote server** | **NOT RUN** | C6 (the parser path is covered by `TestBounceHardPolicyDuplicate`) |

Release state is unchanged: working build, not released for production traffic until the four Mailcow checks above pass and are recorded here.

## Shared admin access and local sending modes (2026-11-11)

All enabled users now have one `admin` role and the same pages and settings. Migrations `0008_one_role.sql` and `0009_default_admin_role.sql` convert existing accounts and default new accounts to admin. Existing sessions survive migration, and concurrent updates cannot disable the last enabled user.

The main workspace uses Live at `/p/{project}/…`; choosing Sandbox for a send, template test, API key or message filter does not change other pages. Only the last project is saved in browser storage. Legacy environment URLs redirect to the new routes, and captures remain accessible on the Sandbox page.

Validation: `go test ./... -race -count=1` and `go vet ./...` pass; all 34 frontend tests and the production build pass. Frontend lint reports warnings only. Authenticated local checks verify Live and Sandbox API scopes and the new workspace paths. Real Mailcow delivery was not exercised for these changes.

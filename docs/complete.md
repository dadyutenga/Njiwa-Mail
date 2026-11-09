# Ditronics Mail — Master Build Prompt (finish the whole product)

Give this file to the coding agent together with `DITRONICS_MAIL_V2_IMPLEMENTATION_PLAN.md`. Put both in the repository root.

---

## Prompt

You are completing **Ditronics Mail**, our internal email platform: a Go backend and an admin UI running on top of our existing Mailcow server. The backend is about half built and the frontend template is already in the repository. Your job is to finish the **entire product**, backend and UI, to a working, tested, deployable state. Partial work is not acceptable.

### Sources of truth

1. `DITRONICS_MAIL_V2_IMPLEMENTATION_PLAN.md` is the specification. Implement every section of it.
2. The "Additional scope" section below adds features the plan left for later. They are now required.
3. Where the existing code disagrees with the plan, keep the code if it is correct and safe, and record the difference in `docs/AUDIT.md`. Do not rewrite working code for style.

### Additional scope (required, on top of the plan)

**Sending**

- Raw sends: `POST /v1/emails` accepts either a template or a direct `subject` + `html` + `text`. Raw sends need the `emails:send_raw` scope. Sanitise headers; reject header injection.
- Outbound attachments: up to 5 files, 5 MiB total, allowlisted content types, stored encrypted until the message reaches a terminal state.
- `reply_to`, `cc` and `bcc` with a combined limit of 10 recipients per message, each checked against suppressions and rate limits.
- Scheduled sends with `send_at` (maximum 7 days ahead) and cancellation of queued or scheduled messages: `POST /v1/emails/{id}/cancel`.
- Batch endpoint `POST /v1/emails/batch` (up to 100 messages, each with its own idempotency key and its own result).
- Priority field: `high` for OTP and password reset, `normal` for notices. The worker serves `high` first.
- SMTP ingress: an authenticated SMTP submission listener (STARTTLS, API key as password) so apps that only speak SMTP can send. It writes into the same queue and follows the same rules as the HTTP API.

**Domains and senders**

- Multi-domain support: add a sending or inbound domain, show the exact DNS records required (SPF, DKIM, DMARC, MX, return path), verify ownership with a TXT challenge, and block live sending from a domain until SPF, DKIM and DMARC all pass.
- Up to 100 sender identities per verified domain.
- Scheduled re-verification; a domain that starts failing is flagged in the UI and raises an alert.

**Inbound**

- Routing rules per inbound address: store only, forward to a webhook, or forward to another email address.
- Reply threading: link an inbound message to the outbound message it answers using `In-Reply-To` and `References`.
- Full-text search across inbound subject, sender and body.

**Security**

- TOTP two-factor authentication for admin users, with recovery codes. Required for `owner` and `admin` roles.
- Password reset for admin users, delivered through the service itself.
- Session list with remote sign-out; forced sign-out on password change.
- API key options: IP allowlist, per-key rate limit, last-used timestamp.
- Security headers and a strict Content Security Policy on the UI.

**Analytics and notifications**

- Analytics page: sends, failures, hard, soft and policy bounces, inbound volume and webhook success rate over time, filterable by project, environment, template and domain. Every chart is built from real recorded events only.
- Operator alerts by email and by webhook: queue stuck, worker heartbeat lost, bounce rate above threshold, DNS check failing, messages in `unknown`, disk usage for attachments.
- Daily summary email to owners.

**Developer experience**

- Go client package `ditronicsmail` with typed requests, retries using the same idempotency key, and webhook signature verification.
- TypeScript client with the same features.
- API documentation page in the UI, generated from `docs/openapi.yaml`.
- CLI `dmctl` for every admin operation: create owner, projects, keys, domains, suppressions, replaying webhooks, reconciling `unknown` messages.

**Operations**

- Backup script for PostgreSQL and attachment storage, a restore script, and a documented restore test.
- Encryption key rotation procedure, implemented and tested.
- CI workflow: lint, vet, unit tests, integration tests, frontend build.
- Data export and deletion per recipient address, for privacy requests.
- Seed command that creates demo data in a development environment only.

**Still out of scope**: wallet and billing, public signup, SMS, AI auto-reply, marketing campaigns and mailing lists, open and click tracking.

### Definition of done

The product is finished only when all of these are true:

- Every backend module in the plan and in "Additional scope" is implemented, with migrations.
- Every UI page in section 7 of the plan exists, plus Domains, Analytics, Alerts, API docs, Security (2FA and sessions) and Scheduled messages.
- Every UI page reads and writes through the real API. No mock data, no hard-coded arrays, no placeholder charts, no "coming soon".
- No `TODO`, `FIXME`, stub handlers, empty functions, commented-out features or endpoints that return fake success anywhere in the code.
- Every acceptance test in section 10 of the plan passes, and each item in "Additional scope" has its own tests, including failure cases.
- `docker compose up` starts the full stack from a clean machine using only `.env.example` values replaced with real ones.
- The frontend builds with no type errors and no lint errors.
- All documents in section 12 of the plan exist and match the code.

### How to work

1. **Audit first.** Read the whole repository. Write `docs/AUDIT.md` marking every item in the plan and in "Additional scope" as Done, Partial or Missing.
2. **Keep a progress file.** Maintain `docs/PROGRESS.md` with a checklist of every feature, its state and the tests that cover it. Update it after each unit of work. At the start of every session, read it and continue from the first unfinished item.
3. **Build in this order:** harden outbound → admin auth and 2FA → core UI → templates → domains and DNS → the shared API-key integration in `docs/INTEGRATION.md` → inbound → bounces and suppression → webhooks → raw, batch, scheduled and SMTP ingress → analytics and alerts → client packages and docs → backup, CI and runbook.
4. **Finish each feature fully before the next:** migration, backend logic, API, UI page, tests, documentation. Do not leave a backend feature without its UI or a UI page without its backend.
5. **Write tests with the code,** not at the end. Run the full suite after each feature and fix regressions immediately.
6. **Do not stop to ask** about things you can decide from the plan or the existing code. Make the decision, write one line about it in `docs/PROGRESS.md`, and continue.
7. **Stop and report only for real blockers:** missing credentials, a destructive change to existing data, or a contradiction in the specification that changes behaviour.

### Things only the operator can do

You cannot change DNS or Mailcow. For those:

- Write exact step-by-step instructions in `docs/MAILCOW_SETUP.md` and `docs/DNS_SETUP.md`.
- Add a local mail server to the development Compose file so the full send, receive and bounce flow runs in tests without Mailcow.
- Label local results as local. Never write that real delivery works unless an email actually arrived in a real inbox and the headers were checked.

### Rules that never bend

- Never report `delivered`. The strongest positive state is `submitted`.
- Never auto-retry a message whose acceptance is uncertain; use `unknown`.
- Never log, return or display OTPs, reset tokens, message bodies, API keys, SMTP passwords or webhook secrets.
- Every query is scoped by project and environment.
- Sandbox never contacts a real mail server.
- Never read or write Mailcow's database.
- A policy bounce never suppresses a recipient.
- Inbound HTML is shown only in a sandboxed frame with scripts and remote content blocked.
- Never commit secrets. `.env.example` contains placeholders only.

### Final report

When everything in the definition of done is true, write `docs/RELEASE_REPORT.md` with:

- Every feature and its state.
- Exact commands to install, migrate, run, test and deploy.
- Test results, copied from a real run.
- What was verified locally and what still needs the operator: DNS, Mailcow routing, real delivery test, IP reputation.
- Rollback steps.

If anything is not finished, say so plainly in that report with the reason. Do not describe unfinished work as complete.

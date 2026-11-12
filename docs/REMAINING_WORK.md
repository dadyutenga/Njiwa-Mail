# Remaining work — checkpoint

**Checkpoint date:** 2026-11-12 (updated) · **Branch:** `claude/happy-brahmagupta-pbfu6r` (after PRs #1–#9)
**Reference:** `15_IMPLEMENTATION_BACKLOG` and the master specification. What *is* done, with test evidence, is in
[`STATUS.md`](../STATUS.md). Step-by-step handoff with every open item and the server runbook: [`HANDOFF.md`](HANDOFF.md). This file lists only what is **not done**, **partly done**, or **not yet verified**.

Legend: ❌ not started · 🟡 partly done (the missing part is listed) · 🔍 built but not verified in a real environment

---

## 1. Release blockers (P0) still open

These stand between the current code and a first real release. They are mostly **verification on real infrastructure**, which
cannot be done from the development container.

| ID | Status | What is missing |
|---|---|---|
| G05 | ❌ | Real managed-MTA end-to-end on a fresh supported VPS (Debian 12 / Ubuntu 22.04 or 24.04) with outbound port 25 open and a PTR: `preflight` → `installer apply -record` → connection test → activate → Live mail to Gmail and Outlook, with `Received`, SPF, DKIM and DMARC headers recorded. |
| G04 | ❌ | Real existing-SMTP end-to-end: once with Mailcow, once with a generic Postfix/Exim server; logs and received headers recorded. |
| E06 | 🔍 | Direct-to-MX delivery is configured but has only been exercised to a local relay. Not verified: delivery to external MX servers, and that the Postfix queue survives a host restart and an upgrade. |
| E07 | 🔍 | The bounce route (bounce domain → Postfix port 25 → Njiwa inbound listener) is configured and the DSN parser exists, but a real DSN has not been sent through managed Postfix and correlated end to end. |
| E05 | 🟡 | Hostname A, PTR/FCrDNS and port-25 checks exist (preflight). Missing: a publicly trusted TLS certificate for inbound port 25 (today it is signed by the installer's private CA, which is acceptable for opportunistic TLS but not ideal), and a HELO check against what remote servers see. |
| G06 | ❌ | Mode-switch regression and legacy-migration end to end (existing SMTP → managed with a non-empty queue, and back). Unit/integration tests cover the planner and migration, not a full switch with real transports. |
| C06 | ❌ | Generic Postfix/Exim staging integration test with the same API (the fake SMTP server and Mailcow are the only targets tested so far). |
| C01 | ✅ | Worker dispatch uses `transport.Transport`; DB-backed tests verified on PostgreSQL 16 (2026-11-12). |
| A02 | 🟡 | Go and frontend suites run in CI on throwaway PostgreSQL. Missing: a container smoke test that brings the Compose stack up and checks `/health/ready`. |
| G07 | 🟡 | `govulncheck` and the `managed-mta` job are **non-blocking** until their first green run on GitHub (the vulnerability database is blocked from the dev container, so the scan has never run). Missing: SBOM, reproducible tagged build, frontend end-to-end tests (browser). |
| G08 | 🟡 | README and the public OpenAPI describe both modes and the new error codes. Missing: OpenAPI for the admin API (installation, transport profiles, wizard, domain claims, transport test), SDK examples, a troubleshooting guide;  |
| A01 | ❌ | Baseline endpoint list and schema map document. |

## 2. Managed-mode (E-series) gaps and limitations

| ID | Status | What is missing |
|---|---|---|
| E10 | ❌ | MTA queue controls in the dashboard (view, hold, release, retry, purge with confirmation) — today only `postqueue`/`postsuper` on the host. |
| E12 | ❌ | Multi-distro clean-install matrix (Debian 12, Ubuntu 22.04, Ubuntu 24.04) with documented results. |
| — | 🔍 | The installer's **systemd** path (`systemctl enable/restart opendkim`, `reload-or-restart postfix`) has not run: the dev container has no systemd. The new `managed-mta` CI job will exercise it on a hosted runner. |
| — | 🟡 | The installer does not install packages; the operator runs `apt-get install postfix opendkim` (documented). |
| — | 🟡 | New sending domains need the installer to be run again (`plan`/`apply -record`) to get a DKIM key; there is no automatic hand-off from "domain verified" to "key generated". |
| — | 🟡 | `INBOUND_ALLOWED_SOURCES` must be set by hand (documented). The host firewall table is now installed by the installer. |

## 3. Existing-SMTP (C-series) gaps

| ID | Status | What is missing |
|---|---|---|
| C05 | 🟡 | Mailcow is optional and works without its API. Missing: moving the adapter under a provider package boundary, and before/after audit records for Mailcow API writes. |
| C02 | 🟡 | Secrets are sealed, never echoed and kept on blank saves. Missing: secrets from mounted files, and `ENCRYPTION_KEY` rotation with a dual-read window. |

## 4. Domains and DNS (D-series) gaps

| ID | Status | What is missing |
|---|---|---|
| D04 | 🟡 | Requirements follow the actual sender, including an existing server's stated sending facts. Missing: per-domain return-path (bounce) domains — today one installation-wide `BOUNCE_ADDRESS`. |

## 5. Setup and dashboard (B-series) gaps

| ID | Status | What is missing |
|---|---|---|
| B07 | 🟡 | Kiswahili exists for the new installation screens only; older strings untranslated. No full keyboard / screen-reader test pass; the DNS record table is not yet reworked for phones. |
| B09 | 🟡 | Health card on every dashboard (mode, readiness, queue, retrying, unknown, last hand-over). Missing: managed-specific details (Postfix deferred count, signer status). |
| — | 🟡 | Screens still missing for: installer receipts, delivery-evidence ingestion status, editing an existing transport profile's settings in place. |

## 6. Reliability, security, operations (F-series) gaps

| ID | Status | What is missing |
|---|---|---|
| F02 | 🟡 | Operators can resolve unknown outcomes with a required note (audited, recorded as evidence). Missing: suggesting the outcome from MTA log evidence / queue ID, and a dedicated reconciliation queue screen. |
| F07 | 🟡 | `email.delivery_*` events are documented (`docs/WEBHOOKS.md`). Missing: event schema versioning and retry/idempotency tests for the new events. |
| F09 | ✅ | Installation-operator permission (migration 0016, default true so nobody loses access) and optional TOTP with recovery codes, replay protection, lockout and a `require_mfa` policy. |
| F10 | ✅ | Retention for delivery evidence and installer receipts (settings), per-message and per-recipient export (JSON/CSV) and delete incl. attachment files, inbound attachments sealed at rest with a legacy converter (`attachments encrypt`). |
| F06 | ✅* | Suppression only for DSNs correlated to our message and its recipient. *Correlation relies on the unguessable message UUID in the returned headers; a signed correlation token (VERP or HMAC) would be stronger. |

## 7. Release (G-series) — not started

| ID | Status | What is missing |
|---|---|---|
| G09 | ❌ | First-time user acceptance: a non-author completes both modes from a clean install. |
| G10 | ❌ | Versioned release report (`docs/RELEASE_REPORT.md` predates this work), rollback runbook, compatibility table (distros, Postfix/OpenDKIM versions, PostgreSQL). |

## 8. Known limitations to keep in mind (by design, documented)

- **Rollback boundary:** an older binary ignores the new tables; Mail server edits made by an old binary after a rollback are not
  seen by the transport profile until re-saved.
- **Environment SMTP** (`SMTP_HOST`) still overrides saved settings at runtime; its mail is not pinned to a profile.
- **Mode switch:** jobs queued before a switch are sent by the new profile only after the operator acknowledges `migrate_pending`;
  there is no per-message hold at dispatch. A worker may finish sends through the previous profile for up to 30 s (they are pinned to
  the profile that really sent them).
- **Accepted messages** are not re-checked against domain claims at dispatch; a revoked or transferred domain stops *new* Live mail.
- **Strict domain gate:** Live mail from unregistered domains is refused; `ALLOW_UNREGISTERED_LIVE_DOMAINS=true` is a temporary
  bridge for unclaimed names only.
- **Fixed Compose subnet** `172.30.0.0/24` (`NJIWA_SUBNET`) and new Live rate-limit defaults (600/min, 20 000/h installation;
  300/min, 10 000/h per domain).

## 9. Suggested order for the next rounds

1. **Real-host verification:** G05 (managed VPS, Gmail/Outlook), G04 (existing SMTP), E06/E07 evidence; make the
   `govulncheck` and `managed-mta` CI jobs blocking after their first green run.
2. **Safety net:** repeat the G03 restore drill and the firewall check on the production host; ACME automation for the dashboard certificate.
3. **Operator UI:** E10 queue controls, a rotation screen (the CLI exists), B07 accessibility and translations.
4. **Hardening and release:** F07, C01 cut-over, G06, G08–G10.

## Done since the first checkpoint

- **G03** backup and restore of the managed host, with a restore drill (database, queued message, byte-identical keys, permissions,
  `managed_e2e` green after restore).
- **F03** bounded Live queue (`503 queue_full`), worker circuit breaker on mail-server outages, `TRANSPORT_FAILING` readiness.
- **D06** DKIM signature verification of received messages (tested against OpenDKIM 2.11.0), proof recorded per domain from a pasted
  message or the project's own inbox, `njiwa-mail dkim-verify`.
- Webhook docs for `email.delivery_*`; the signer-down case is diagnosed as `SMTP_SERVER_TEMP_REFUSED`.
- **G02** host firewall table from the installer (verified from a separate network namespace) and a working HTTPS proxy config
  (the dashboard was not proxied before; verified with nginx 1.24).
- **C07** sending facts for existing SMTP servers: real SPF/DKIM requirements and checks.
- **B08/B09** delivery-profile and mode-switch screen; delivery health card on dashboards.
- **D09** domain ownership page.
- **D10/E11** staged DKIM rotation (CLI, DNS-gated switch, retire; verified with real OpenDKIM). Certificate renewal still open.
- **C08** optional bounce-visibility readiness step (not configured / listener down / last report).
- **F05** security regression matrix over every route, secrets sweep of every admin `GET`, SSRF guard on real sends;
  [`SECURITY_REVIEW.md`](SECURITY_REVIEW.md).
- **E11** certificate renewal (automatic within 60 days, `-renew-certs`, 7-day grace for the replaced worker certificate, readiness
  warning; verified against real Postfix). The CA itself is replaced by hand (documented).
- **F08** delivery-health metrics and Prometheus alert rules.
- `mta-log` systemd unit (journal based); the Domains page flags requirements that changed since the last check.

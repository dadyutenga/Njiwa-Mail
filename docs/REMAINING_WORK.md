# Remaining work — checkpoint

**Checkpoint date:** 2026-11-12 (updated) · **Branch:** `claude/happy-brahmagupta-pbfu6r` (after PRs #1–#5)
**Reference:** `15_IMPLEMENTATION_BACKLOG` and the master specification. What *is* done, with test evidence, is in
[`STATUS.md`](../STATUS.md). This file lists only what is **not done**, **partly done**, or **not yet verified**.

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
| G02 | ❌ | HTTPS, certificate and host-firewall defaults: no automated firewall rules (allow 25 in, allow 10587 only from the Compose subnet, block 2525 and 5432 from outside), no ACME/Let's Encrypt automation for the dashboard, no external scan. `deploy/nginx.conf` predates this work. |
| G06 | ❌ | Mode-switch regression and legacy-migration end to end (existing SMTP → managed with a non-empty queue, and back). Unit/integration tests cover the planner and migration, not a full switch with real transports. |
| F05 | ❌ | Formal security review and regression matrix (open relay, SSRF, CSRF, RBAC, secrets in logs). Pieces exist (SMTP-test SSRF guard, webhook SSRF guard, no open relay verified), but no consolidated review. |
| C06 | ❌ | Generic Postfix/Exim staging integration test with the same API (the fake SMTP server and Mailcow are the only targets tested so far). |
| C01 | 🟡 | `internal/transport` defines the interface and an SMTP wrapper, but the worker still sends through `mailer.Transport`/`StoredTransport`. The cut-over to `transport.Transport` is not done. |
| A02 | 🟡 | Go and frontend suites run in CI on throwaway PostgreSQL. Missing: a container smoke test that brings the Compose stack up and checks `/health/ready`. |
| G07 | 🟡 | `govulncheck` and the `managed-mta` job are **non-blocking** until their first green run on GitHub (the vulnerability database is blocked from the dev container, so the scan has never run). Missing: SBOM, reproducible tagged build, frontend end-to-end tests (browser). |
| G08 | 🟡 | README and the public OpenAPI describe both modes and the new error codes. Missing: OpenAPI for the admin API (installation, transport profiles, wizard, domain claims, transport test), SDK examples, a troubleshooting guide;  |
| A01 | ❌ | Baseline endpoint list and schema map document. |

## 2. Managed-mode (E-series) gaps and limitations

| ID | Status | What is missing |
|---|---|---|
| E10 | ❌ | MTA queue controls in the dashboard (view, hold, release, retry, purge with confirmation) — today only `postqueue`/`postsuper` on the host. |
| E11 | ❌ | Key and certificate lifecycle: DKIM selector rotation (new key, publish, verify, switch, retire), and renewal of the installer's server/client certificates (issued for 5 years, CA for 10; no renewal command). |
| E12 | ❌ | Multi-distro clean-install matrix (Debian 12, Ubuntu 22.04, Ubuntu 24.04) with documented results. |
| — | 🔍 | The installer's **systemd** path (`systemctl enable/restart opendkim`, `reload-or-restart postfix`) has not run: the dev container has no systemd. The new `managed-mta` CI job will exercise it on a hosted runner. |
| — | 🟡 | The installer does not install packages; the operator runs `apt-get install postfix opendkim` (documented). |
| — | 🟡 | New sending domains need the installer to be run again (`plan`/`apply -record`) to get a DKIM key; there is no automatic hand-off from "domain verified" to "key generated". |
| — | 🟡 | `INBOUND_ALLOWED_SOURCES` and the host firewall rule for the submission listener must be set by hand (documented in `compose.yaml` and `docs/OPERATIONS.md`). |
| — | 🟡 | `njiwa-mail mta-log -follow` is a manual process; there is no systemd unit or Compose service for it yet. |

## 3. Existing-SMTP (C-series) gaps

| ID | Status | What is missing |
|---|---|---|
| C07 | ❌ | Provider metadata intake: a way to enter the downstream server's sending IPs / SPF include and its DKIM public key, so SPF/DKIM for a generic SMTP server are checked against real values instead of being shown as templates. |
| C08 | ❌ | Bounce visibility for existing SMTP: a clear "limited observability" state, and optional DSN intake configuration (bounce mailbox / IMAP / relay). |
| C05 | 🟡 | Mailcow is optional and works without its API. Missing: moving the adapter under a provider package boundary, and before/after audit records for Mailcow API writes. |
| C02 | 🟡 | Secrets are sealed, never echoed and kept on blank saves. Missing: secrets from mounted files, and `ENCRYPTION_KEY` rotation with a dual-read window. |

## 4. Domains and DNS (D-series) gaps

| ID | Status | What is missing |
|---|---|---|
| D04 | 🟡 | Requirements follow the actual sender. Missing: provider-specified records (depends on C07); per-domain return-path (bounce) domains — today one installation-wide `BOUNCE_ADDRESS`. |
| D09 | ❌ | DNS state history, TTL display, recheck schedule, and a **domain-claims UI** (claims and transfers are operator API only: `GET /admin/v1/domain-claims`, `POST /admin/v1/domain-claims/transfer`). |
| D10 | ❌ | DKIM selector rotation and revocation UI (pairs with E11). |
| — | 🟡 | The Domains page shows the records saved by the last check; after changing delivery mode or provider, "Check now" is needed to see the new requirements (it is not invalidated automatically). |

## 5. Setup and dashboard (B-series) gaps

| ID | Status | What is missing |
|---|---|---|
| B07 | 🟡 | Kiswahili exists for the new installation screens only; older strings untranslated. No full keyboard / screen-reader test pass; the DNS record table is not yet reworked for phones. |
| B08 | ❌ | Recovery and migration UX (guided mode switch with pending-queue review and rollback, warnings when settings change). The switch planner exists as an API only — there is no switch screen. |
| B09 | 🟡 | A health banner exists. Missing: the unified "Delivery infrastructure" health card on the dashboard (queue depth, deferred count, signer status for managed; connection/TLS state for existing SMTP). |
| — | 🟡 | Screens for the new APIs: transport profile list/edit/retire, activation plan and switch, installer receipts, delivery-evidence ingestion status. |

## 6. Reliability, security, operations (F-series) gaps

| ID | Status | What is missing |
|---|---|---|
| F02 | 🟡 | Operators can resolve unknown outcomes with a required note (audited, recorded as evidence). Missing: suggesting the outcome from MTA log evidence / queue ID, and a dedicated reconciliation queue screen. |
| F07 | 🟡 | `email.delivery_*` events are documented (`docs/WEBHOOKS.md`). Missing: event schema versioning and retry/idempotency tests for the new events. |
| F08 | ❌ | Metrics and alerts for transport health, queue growth, deferred rate and signer availability; alert channels for them. |
| F09 | ❌ | Operator role separation (installation operator vs project roles) and MFA or an equivalent admin-access control. All admins are equal today. |
| F10 | ❌ | Retention/export/delete for delivery evidence and installer receipts; encryption at rest for inbound attachments (still plain files on disk). |
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
2. **Safety net:** G02 (firewall/HTTPS defaults); repeat the G03 restore drill on the production host.
3. **Truthful authentication:** C07 provider metadata, C08 bounce visibility.
4. **Operator UI:** B08 switch screen, B09 health card, D09 claims UI, E10 queue controls, D10/E11 rotation.
5. **Hardening and release:** F05 review, F09 roles/MFA, F07/F08/F10, C01 cut-over, G06, G08–G10.

## Done since the first checkpoint

- **G03** backup and restore of the managed host, with a restore drill (database, queued message, byte-identical keys, permissions,
  `managed_e2e` green after restore).
- **F03** bounded Live queue (`503 queue_full`), worker circuit breaker on mail-server outages, `TRANSPORT_FAILING` readiness.
- **D06** DKIM signature verification of received messages (tested against OpenDKIM 2.11.0), proof recorded per domain from a pasted
  message or the project's own inbox, `njiwa-mail dkim-verify`.
- Webhook docs for `email.delivery_*`; the signer-down case is diagnosed as `SMTP_SERVER_TEMP_REFUSED`.

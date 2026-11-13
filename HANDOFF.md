# Njiwa-Mail — handoff: everything still to do

**Date:** 2026-11-12 · **Written for:** continuing the work from the office PC (which can reach the server).
**Current branch:** `main`.

The `main` and `claude/happy-brahmagupta-pbfu6r` branches were rewritten on 2026-11-12 to assign the 33 former
Claude-authored commits to `Dadi Nasser Utenga <dadyutenga@yahoo.com>`. Existing clones must fetch and reconcile their local
branches with the new history before pushing; old commit links and PR commit references may still show the previous hashes.

Start here when continuing. This file is self-contained: it lists every open item, how to set up the PC, the exact server runbook, and what "done" means for
each item. The detailed history of what is already done (with test evidence) is in [`STATUS.md`](STATUS.md); the short list of
gaps is [`REMAINING_WORK.md`](docs/REMAINING_WORK.md); operating instructions are in [`OPERATIONS.md`](docs/OPERATIONS.md); the security
review is [`SECURITY_REVIEW.md`](docs/SECURITY_REVIEW.md).

**Update on 2026-11-12 (long autonomous session):** every item in section 4 that does not need a real mail host is now implemented, tested and
pushed, one commit per objective: C01 verified, F09 (operator permission + optional TOTP), F10 (sealed attachments, retention, export/delete),
F07 (webhook `schema_version`; it also fixed `email.delivery_*` events being impossible to subscribe to), F02 (reconciliation queue), C02
(key rotation, `rekey`), F06 (signed VERP return path), E10 (queue agent + dashboard; real-host run pending), D04 (per-domain return path),
C05 (provider boundary, audited writes), B07/B09 (Kiswahili for 722 strings with a coverage test, accessibility, Mail host page, profile
editing), A01/G08 (generated endpoint/schema docs, admin OpenAPI, examples, troubleshooting), A02/G07 (smoke, Playwright e2e, reproducible
build + SBOM, release workflow; `govulncheck` found and we fixed Go stdlib, brotli and Fiber advisories; both it and `managed-mta` are now
blocking and green on GitHub) and G10 (release report with compatibility table and rollback runbook).

**What is NOT done, and why it cannot be done by an assistant:** everything in section 3 (real server) and G09 (a non-author install).
`rida-server` turned out to be a live multi-service host: Mailcow owns ports 25/465/587 and the installer correctly refuses such a host, so
managed mode (G05) and the other real-delivery items need a **clean VPS** plus DNS and Gmail/Outlook mailboxes. Also still open: a manual
screen-reader/phone pass and a native-speaker review of the Kiswahili; Fiber was migrated from v2 to v3 afterwards, and one-click Cloudflare DNS
setup was added after that (STATUS.md, `docs/DNS_SETUP.md`). **DNS setup has only run against an in-memory DNS host and test servers:** before
trusting it, a person must run it once against a real Cloudflare zone they own (token connect, analyze, approve, apply, verify, rollback), and
register a Cloudflare OAuth client (and, if wanted, a Domain Connect template) before enabling "Sign in with Cloudflare".
The public documentation portal (`documentation/`, `docs/public-portal/`) was added after that; it is built and tested but **not published**: pick a host, set
`DOCS_BASE_URL`, deploy per `docs/public-portal/DEPLOY.md`, then run `docs/public-portal/RELEASE_CHECKLIST.md` (the "Open in …" URL formats need a real-browser check).
How the work was verified: Go and frontend suites, Playwright and a Compose smoke stack were run in throwaway Docker containers on
`rida-server` (nothing installed on the workstation, no production container touched; the throwaway containers, volumes and images were
removed afterwards), and by GitHub Actions on every push.

---

## 0. Muhtasari (Kiswahili)

- **Kilichokamilika:** API moja na dashboard, njia mbili za kutuma (`managed_mta` = Postfix + OpenDKIM kwenye VPS yako,
  `existing_smtp` = server yako ya barua au Mailcow), installer yenye plan/apply/rollback, DKIM rotation, renewal ya certificates,
  ukaguzi wa SPF/DKIM/DMARC/PTR, ushahidi wa delivery, rate limits, metrics na alerts, security matrix, backup/restore.
- **Kilichobaki kinagawanyika mara mbili:**
  1. **Kinahitaji server halisi** (sehemu ya 3): G05 (managed mode kwenye VPS + Gmail/Outlook), G04 (existing SMTP halisi),
     E06/E07 (delivery na bounce halisi), E12 (Debian/Ubuntu tofauti), G06 (kubadilisha mode na barua kwenye foleni), systemd path,
     CI jobs kuwa blocking.
  2. **Hakihitaji server** (sehemu ya 4): **yote yamekamilika na kupimwa** (F09, F10, F07, F02, C02, F06, E10, D04, C05, B07/B09,
     A01/G08, A02/G07, G10). Kinachobaki ni G09 (mtu mwingine afanye usakinishaji kwa kutumia nyaraka tu), pamoja na majaribio ya
     screen reader na mapitio ya Kiswahili na mzungumzaji wa asili.
- **Muhimu:** `rida-server` ni seva ya kazi yenye Mailcow (port 25/465/587): usakinishaji wa managed mode haufai hapo. Tumia VPS safi.
- **Anza hivi ofisini:** clone → fuata sehemu ya 1 (setup) → sehemu ya 3.1 (G05) hatua kwa hatua.
- Ukitumia Claude Code kwenye PC, mpe maelekezo ya sehemu ya 7.

---

## 1. Setting up the office PC

### 1.1 Get the code

```bash
git clone https://github.com/dadyutenga/Njiwa-Mail.git
```

```bash
cd Njiwa-Mail && git checkout main && git pull
```

Everything is on `main`. Start new work on a new branch (`git checkout -b <name>`).

### 1.2 Tools

| Tool | Version | Used for |
|---|---|---|
| Go | 1.26 (`go.mod`: `go 1.26.0`) | backend, CLI, installer, tests |
| Node.js | ≥ 24 (`frontend/package.json` engines) | dashboard |
| PostgreSQL | 16 | tests need a throwaway database |
| Docker + Compose v2 | recent | running the stack, `docker compose config` checks |
| ssh | any | reaching the server |

### 1.3 Run the test suites (all must stay green)

A **throwaway** database is required: the integration suite drops and recreates the `public` schema. Never point it at real data.

```bash
docker run -d --name njiwa-test-pg -e POSTGRES_PASSWORD=pw -p 127.0.0.1:5433:5432 postgres:16
```

```bash
export TEST_DATABASE_URL="postgres://postgres:pw@localhost:5433/postgres?sslmode=disable"
```

```bash
set -o pipefail; go vet ./... && go test ./... -race -count=1 -timeout 500s
```

Frontend (from `frontend/`):

```bash
cd frontend && npm ci && npm run typecheck && npx vitest run && npm run lint && npm run build
```

Expected today: Go packages all `ok`; frontend 55 tests pass, lint 0 errors (24 warnings are pre-existing).

**Always use `set -o pipefail`** when piping test output (`| tail`, `| grep`): without it a failing test can look green.

### 1.4 Managed end-to-end tests (Linux host with Postfix + OpenDKIM only)

These run against a real Postfix that the installer configured (they are what CI's `managed-mta` job runs):

```bash
go test -tags managed_e2e ./internal/installer/managed -run E2E -count=1 -v
```

Environment knobs: `NJIWA_E2E_SELECTOR` (default `njiwa`), `NJIWA_E2E_CAPTURE` (file to save the received message),
`NJIWA_E2E_CLIENT_DIR` (directory with another `client.crt`/`client.key`, e.g. a replaced certificate).

---

## 2. Where things are (map of the code)

| Area | Path |
|---|---|
| CLI (all commands, usage text) | `internal/cli/cli.go` |
| HTTP API, routes | `internal/api/` (`app.go`, `installation.go`, …) |
| Business logic | `internal/service/` (`transport.go`, `installation.go`, `managed.go`, `domains.go`, `dns.go`, `evidence.go`, `ops.go`, `facts.go`, `claims.go`, `dkimproof.go`) |
| Readiness model (pure) | `internal/installation/installation.go` |
| Transport interface | `internal/transport/` |
| SMTP client, diagnosis, SSRF guard | `internal/mailer/` (`smtp.go`, `diagnose.go`) |
| Worker | `internal/worker/` |
| SPF/DMARC/DKIM | `internal/dnsauth/` |
| Managed installer (Postfix/OpenDKIM/PKI/firewall/rotation) | `internal/installer/managed/` |
| Host preflight | `internal/installer/preflight/` |
| Migrations (0001–0015) | `internal/db/migrations/` |
| Integration tests | `test/` (helpers in `helpers_test.go`, `v2_test.go`, `transport_test.go`) |
| Dashboard | `frontend/src/features/` (`installation/`, `workspace/pages/`) |
| Deployment | `compose.yaml`, `compose.mailcow.yaml`, `Dockerfile`, `deploy/` (nginx, systemd units, Prometheus rules), `scripts/backup.sh`, `scripts/restore.sh` |
| CI | `.github/workflows/ci.yml` (jobs: `test`, `frontend`, `image`, `govulncheck`, `managed-mta`) |

---

## 3. Work that needs the real server

Do these in order. Record the evidence for each (commands, outputs, headers, screenshots) and add a row to `STATUS.md` when done.

### 3.0 Server requirements (check before starting)

- [ ] Debian 12, Ubuntu 22.04 or Ubuntu 24.04, root access, systemd.
- [ ] **Outbound port 25 open.** Many providers block it; ask them to unblock it. Test: `nc -vz gmail-smtp-in.l.google.com 25`.
- [ ] A static public IPv4 and a **PTR** (reverse DNS) set at the provider to the mail hostname, e.g. `mailout.example.com`.
- [ ] DNS you control for: the mail hostname (A record → server IP), the sending domain (e.g. `notify.example.com`), and a bounce
      domain (e.g. `bounce.example.com`).
- [ ] At least 2 GB RAM and 10 GB free disk; correct time (NTP).
- [ ] A Gmail and an Outlook/Hotmail mailbox to receive test mail.
- [ ] No other mail server on the host (Exim, Mailcow, a Postfix not set up by Njiwa); the installer refuses them.

### 3.1 G05 — managed mode end to end (P0 release blocker)

**Goal:** a fresh server sends Live mail to Gmail and Outlook through the managed Postfix, DKIM-signed and passing SPF/DKIM/DMARC.

1. **Install packages** (Postfix: choose "No configuration"):

   ```bash
   apt-get update && apt-get install -y postfix opendkim nftables docker.io docker-compose-v2 git
   ```

2. **Get the code and build the binary on the host** (the installer runs on the host, not in a container):

   ```bash
   git clone https://github.com/dadyutenga/Njiwa-Mail.git /opt/njiwa-mail && cd /opt/njiwa-mail
   ```

   ```bash
   docker build -t njiwa-mail:local . && docker create --name nm-tmp njiwa-mail:local && docker cp nm-tmp:/usr/local/bin/njiwa-mail /usr/local/bin/njiwa-mail && docker rm nm-tmp
   ```

   (Or install Go 1.26 and `go build -o /usr/local/bin/njiwa-mail .`.) Check the binary path inside the image with
   `docker run --rm --entrypoint sh njiwa-mail:local -c 'command -v njiwa-mail'` if the copy fails.

3. **Preflight** (read-only):

   ```bash
   njiwa-mail preflight -hostname mailout.example.com
   ```

   Every line must be `pass` or understood. `cannot_confirm` means it could not be proven; fix or note why. Key checks: hostname A,
   PTR/FCrDNS, outbound 25, existing MTAs, `PRIVATE_PORT_PUBLIC`.

4. **Configure `.env`** (copy from `.env.example`):
   - `POSTGRES_PASSWORD`, `ENCRYPTION_KEY` (`openssl rand -base64 32`), `HMAC_KEY` (`openssl rand -base64 48`).
     **Back these up** — losing them makes queued mail unreadable.
   - `BOUNCE_ADDRESS=bounces@bounce.example.com`, `MAIL_HOSTNAME=mailout.example.com`, `OUTBOUND_IP=<server IP>`,
     `DKIM_SELECTOR=njiwa` (or what you pass to the installer), `PUBLIC_URL=https://mail-api.example.com`, `TRUST_PROXY=true`,
     `METRICS_TOKEN=<16+ chars>`, `INBOUND_ALLOWED_SOURCES=172.30.0.1` (host Postfix relaying bounces).
   - Leave `SMTP_HOST` empty (it would override the managed profile).

5. **Start the stack** (with the inbound listener for bounces):

   ```bash
   docker compose --profile inbound up -d --build
   ```

   Check `curl -s 127.0.0.1:8080/health/ready`. Create the first admin: `docker compose exec api njiwa-mail user create -email you@example.com -role owner` (password on stdin).

6. **Let the host CLI reach the database for `-record`.** PostgreSQL is not published, but it has a fixed address on Njiwa's
   network (`NJIWA_DB_IP`, default `172.30.0.10`, task T3.1), so this URL survives restarts and reboots:

   ```bash
   set -a; . /opt/njiwa-mail/.env; set +a; export DATABASE_URL="postgres://mail:${POSTGRES_PASSWORD}@${NJIWA_DB_IP:-172.30.0.10}:5432/mail?sslmode=disable"
   ```

   Put the same `DATABASE_URL`, `ENCRYPTION_KEY` and `HMAC_KEY` in `/etc/njiwa-mail/env` for the `njiwa-mail-mta-log` unit.
   Before step 7 run the read-only scan (`sh scripts/discover.sh -record`, see `docs/INSTALL.md`); it must recommend managed mode.

7. **Choose managed mode** in the dashboard (Installation page → "Send from my VPS"), then add the project, the sending domain
   `notify.example.com`, publish its ownership TXT and let the check verify it (domain claim). Then:

   ```bash
   njiwa-mail preflight -hostname mailout.example.com -record
   ```

8. **Install the mail service** — the worker container reaches host Postfix at the Compose gateway:

   ```bash
   njiwa-mail installer plan -hostname mailout.example.com -domains notify.example.com -bounce-domain bounce.example.com -selector njiwa -submission 172.30.0.1:10587
   ```

   Read the plan. Then apply with the printed digest:

   ```bash
   njiwa-mail installer apply -hostname mailout.example.com -domains notify.example.com -bounce-domain bounce.example.com -selector njiwa -submission 172.30.0.1:10587 -approve <digest> -record
   ```

   This is the **first run of the systemd path** (`systemctl enable/restart opendkim`, `reload-or-restart postfix`, the firewall
   unit). Check: `systemctl status postfix@- opendkim njiwa-mail-firewall`, `nft list table inet njiwa_mail`,
   `njiwa-mail installer status`.

9. **Publish DNS** (values printed by apply and shown on the Domains page):
   - `njiwa._domainkey.notify.example.com TXT "v=DKIM1; k=rsa; p=…"`
   - SPF on `notify.example.com`: `v=spf1 mx ip4:<server IP> -all` (the Domains page shows the exact record).
   - DMARC: `_dmarc.notify.example.com TXT "v=DMARC1; p=none; rua=mailto:…"` (start with `p=none`).
   - Bounce domain MX: `bounce.example.com MX 10 mailout.example.com`.
   - **SPF on the bounce domain**: `bounce.example.com TXT "v=spf1 mx ip4:<server IP> -all"`. Mail leaves with the signed return path
     `bounces+<id>.<tag>@bounce.example.com`, so receivers check SPF against the bounce domain; without this record they report
     `spf=none` (found in the QEMU lab; DMARC still passes through DKIM). `installer apply` now prints it.
   - `mailout.example.com A <server IP>`; PTR at the provider.
   Then "Check now" on the Domains page until everything passes.

10. **Connection test and activation:** Installation page → run the connection test on the managed profile (must pass) → review
    the switch plan → activate. Readiness must show all required steps ready.

11. **Send Live mail** to Gmail and Outlook (a Live API key: `njiwa-mail key create -project <p> -mode live`; a Live sender on
    `notify.example.com`). Use the dashboard's transport test or `curl` to `/v1/emails`.

12. **Record evidence** for each provider: "Show original" (Gmail) / message source (Outlook). Save the `Received:` chain and
    `Authentication-Results:` — need `spf=pass`, `dkim=pass header.d=notify.example.com header.s=njiwa`, `dmarc=pass`. Paste the
    message into the Domains page "Check signature" (records DKIM proof). Check inbox vs spam.

13. **Run the e2e suite on the server** (`go test -tags managed_e2e …`, needs Go) or at least confirm by hand:
    port 25 refuses relaying (`swaks --to someone@gmail.com --server mailout.example.com` → `554 5.7.1`), submission without the
    client certificate is refused.

14. **Start the log collector** and confirm delivery evidence appears on the message page:

    ```bash
    cp deploy/njiwa-mail-mta-log.service /etc/systemd/system/ && systemctl daemon-reload && systemctl enable --now njiwa-mail-mta-log
    ```

    The unit runs as `njiwa-mail` and reads `/etc/njiwa-mail/env`: create that user (`useradd -r njiwa-mail`) and env file
    (`DATABASE_URL`, `ENCRYPTION_KEY`, `HMAC_KEY` from step 6), or adapt the unit. First real run of this unit — record any fix.

15. **HTTPS:** install nginx + certbot, use `deploy/nginx.conf`, `certbot certonly --webroot -w /var/www/certbot -d mail-api.example.com`.

**Done when:** Gmail and Outlook both show SPF/DKIM/DMARC pass with the headers saved in `docs/` (e.g. `docs/evidence/G05.md`,
addresses removed), the systemd path worked (or was fixed), and `STATUS.md` has a G05 row.

### 3.2 E06 — direct-to-MX delivery and queue durability

- [ ] Send to 3+ external providers (Gmail, Outlook, Yahoo or a corporate domain); `njiwa-mail mta-log` shows `remote_accepted`.
- [ ] Queue survives restart: block outbound 25 temporarily (`nft add rule …` or provider firewall), send, see `deferred` in
      `postqueue -p`, reboot the host, unblock, confirm delivery.
- [ ] Queue survives an upgrade: `apt-get upgrade postfix` with mail in the queue.
- **Done when:** evidence recorded in `STATUS.md` (E06 row).

### 3.3 E07 — real bounce through managed Postfix

- [ ] Send Live mail to a non-existent address at Gmail (`does-not-exist-xyz@gmail.com`).
- [ ] The DSN arrives at `bounces+<id>.<tag>@bounce.example.com` (signed return path, F06) → port 25 → relayed to `127.0.0.1:2525` → inbound listener.
- [ ] The message shows `bounced`, the recipient is suppressed (hard bounce), a `email.bounced` webhook fires, the Installation
      page `bounce_reports` step shows "Bounce reports are received" with the time.
- **Done when:** recorded in `STATUS.md` (E07 row).

### 3.4 E05 remainder — certificate and HELO

- [ ] Optional: a publicly trusted certificate for inbound port 25 (today it uses the installer's private CA; fine for
      opportunistic TLS). If done, add an installer flag such as `-smtpd-cert`/`-smtpd-key`.
- [ ] Check the HELO remote servers see (`Received: from mailout.example.com`) matches the PTR.

### 3.5 E12 — clean-install matrix

Repeat 3.1 steps 1–13 on fresh Debian 12, Ubuntu 22.04 and Ubuntu 24.04 (cheap throwaway VPSes are fine). Record Postfix/OpenDKIM
versions and any differences in a compatibility table (also needed for G10).

### 3.6 G04 + C06 — existing SMTP for real

- [ ] **Mailcow:** follow `docs/MAILCOW_SETUP.md`; profile type Mailcow; connection test; DKIM key pasted (sending facts); send to
      Gmail/Outlook; headers recorded. With and without the Mailcow API key.
- [ ] **Generic Postfix or Exim server** (not managed by Njiwa): profile type generic; enter sending facts (egress IP / SPF
      include / DKIM selector and key) on the Installation page; check that the Domains page asks for exactly the right SPF/DKIM;
      send; headers recorded.
- [ ] C06: turn the generic-server run into a repeatable staging test (a script or a `//go:build staging` test using the same API).

### 3.7 G06 — mode switch with a non-empty queue

- [ ] Start in existing SMTP with mail queued (stop the SMTP server so mail waits in `retry_wait`).
- [ ] Switch to managed: the plan shows pending counts; activation needs `migrate_pending`; after acknowledging, queued mail goes
      out through managed Postfix, each message pinned to the profile that sent it.
- [ ] Roll back to the previous profile and repeat in the other direction.
- [ ] Legacy upgrade: install an old release with a `mail_server` row, upgrade, confirm migration `0010` made the active profile and
      sending continues.

### 3.8 Backups, restore drill and firewall on the real host (G03/G02 on production)

- [ ] `scripts/backup.sh` (with `BACKUP_QUEUE=1`), copy off the host, restore on a fresh VPS with `scripts/restore.sh`, run e2e.
- [ ] From another machine: port 25 open; 2525, 5432, 8080, 3000, 8891, 10587 not reachable (`nmap -p …`).

### 3.10 QEMU/KVM lab (simulated) and what still needs the real world

Run in a throwaway lab (`lab/qemu/`, evidence in `docs/evidence/lab/`; **simulated network, not production validation**). It found seven defects, all fixed
with tests (list in `docs/evidence/lab/README.md`). Classification of the items above:

- **Done in the lab (simulated):** G05 steps 1-8, 10, 11, 13 and 14 (verdicts from a lab verifier, not Gmail/Outlook); E06 queue durability across reboot and power
  loss; E07/F06 real Postfix bounce (webhook HTTP delivery not verifiable offline); E05 HELO equals PTR; E10 queue controls; G03/G02 restore on a fresh host and the
  closed-port scan; rollback; generic existing SMTP and the G06 mode switch (last full run predates the final fix, re-run needed).
- **Testable in a lab but not yet run:** E12 (Debian 12, Ubuntu 22.04), D04 per-domain return path, the 0010 legacy upgrade, Mailcow (needs its own instance), a G06 re-run.
- **Genuinely needs external infrastructure:** real Gmail/Outlook headers (G05 step 12), public port 25 and a real PTR, three or more real providers (E06), a publicly
  trusted certificate (E05) and HTTPS with certbot (G05 step 15), real Cloudflare DNS, real Mailcow, G09 (a person who did not write the code), and the external penetration test.

### 3.9 CI and supply chain (G07)

- [x] `govulncheck` and `managed-mta` are green on GitHub Actions and blocking (2026-11-12); the advisories found were fixed.

---

## 4. Work that does NOT need the server

Each item: what to build, where, and acceptance criteria. Suggested order: F09 → C01 → F10 → E10 → F07/F02 → docs → B07.

### F09 — operator roles and MFA — **done**

- Installation-operator permission and optional TOTP are implemented (migration `0016`); see `docs/SECURITY_REVIEW.md` section 6 and
  `docs/OPERATIONS.md`. Existing users are all operators, so upgrading removes no access.

### C01 — worker cut-over to `transport.Transport` — **done**

- Production worker dispatch uses `transport.Transport`; `StoredTransport` pins the active profile revision; environment SMTP
  wraps the same failover client. Verified on 2026-11-12: full Go suite on PostgreSQL 16 (Docker on the server), including
  `TestWorkerUsesActiveProfileAndPinsMessages`, `TestUnknownOutcomeIsPinnedAndNotResent`,
  `TestTransportOutageBreakerAndDegradedReadiness`.

### F10 — retention, export, delete; encryption at rest — **done**

- See `docs/OPERATIONS.md` (Retention) and `docs/SECURITY_REVIEW.md` section 7. Existing deployments: after upgrading, run
  `njiwa-mail attachments encrypt` (or wait for the retention job) to seal old attachment files.

### E10 — MTA queue controls in the dashboard — **code done; real run pending**

- Agent, API, dashboard page, unit and compose override are done and tested with a fake runner (`docs/OPERATIONS.md`, "Mail queue controls").
- **Still to do on the managed server (with E06):** install the agent unit, open Admin → Mail queue, block outbound 25 to create a deferred
  message, hold / release / requeue / delete it, and confirm the timeline note and the audit rows. Record it in `STATUS.md`.

### F07 — webhook event versioning — **done**

- `schema_version` in every event; see `docs/WEBHOOKS.md` (Schema versions). Retry and idempotency tests added.

### F02 — reconciliation queue — **done**

- Unknown queue screen suggests an outcome from the evidence; the operator confirms. Rules are in `internal/service/reconcile.go`.

### F06 — stronger bounce correlation — **code done; real-host check pending (with E07)**

- Signed return path implemented and tested; see `docs/OPERATIONS.md`. During E07 also confirm `correlated_by = verp` on the real
  bounce and that Postfix relays `bounces+<id>.<tag>@bounce-domain` to the inbound listener.

### C02 — secrets — **done**

- Key rotation procedure is in `docs/OPERATIONS.md` ("Rotating ENCRYPTION_KEY"). HMAC_KEY rotation is intentionally unsupported.

### C05 — Mailcow adapter boundary — **done**

- `internal/provider` (interface, Mailcow adapter); writes audited with before/after. See `docs/SECURITY_REVIEW.md` section 12.

### D04 — per-domain return path — **done (code); real-host check with E07**

- See `docs/OPERATIONS.md` (Per-domain return path). Installer accepts `-bounce-domain a,b`.

### B07 / B09 / UI gaps — mostly done; manual accessibility pass remains

Done: Kiswahili for every static string (with a coverage test), labelled/described form fields, focus after navigation, Mail host page, queue page, rotation state, profile editing, managed health on the card. **Still to do by a person:** a pass with a real screen reader and on a phone, and a native-speaker review of the Kiswahili (it was written by an AI assistant; technical terms such as DNS, SPF, DKIM, webhook were kept in English on purpose).

Original list:

- Kiswahili for all older strings (`frontend/src/features/i18n`), keyboard and screen-reader pass, DNS record table usable on
  phones.
- Health card: managed details (Postfix deferred count, signer status) — needs E10's agent or the metrics.
- Screens: installer receipts list, delivery-evidence ingestion status (last `mta-log` event time), edit an existing transport
  profile in place, a DKIM rotation status screen (CLI exists: `installer dkim-rotate status`).
- After a new domain is verified in managed mode, show "run the installer again to create its DKIM key" (today a manual step).

### A01 / G08 — documentation — **done**

- `docs/ENDPOINTS.md` (generated; keep current with `UPDATE_DOCS=1 go test ./test -run TestEndpointsAndSchemaDoc`), `docs/openapi-admin.yaml`,
  `docs/EXAMPLES.md`, `docs/TROUBLESHOOTING.md`.

### A02 / G07 — CI — **done** (all jobs green on GitHub, govulncheck and managed-mta blocking)

Done: `smoke`, `e2e` (Playwright), `supply-chain` (reproducibility + SBOM), `release.yml`, `managed-mta` blocking, and the vulnerabilities govulncheck found are fixed. Verified on GitHub (run 43276949239): every job green. Fiber was later migrated to v3 (v3.7.0).

Original list:

- Container smoke test in CI: `docker compose up -d`, wait for `/health/ready`, `docker compose down -v`.
- SBOM (`syft` or `go version -m`), reproducible tagged build, browser end-to-end tests (Playwright) for login, installation
  wizard, domains.

### G09 / G10 — release — G10 written; G09 needs a person

- G09: someone who did not write the code installs both modes from a clean server using only the docs; note every place they got
  stuck.
- G10: `docs/RELEASE_REPORT.md` for the version (it predates this work), rollback runbook, compatibility table (distros, Postfix,
  OpenDKIM, PostgreSQL versions from E12).

---

## 5. Known limitations (by design — keep documented, do not "fix" silently)

- Rollback boundary: an older binary ignores the new tables; Mail server edits made by an old binary after a rollback are not seen
  by the transport profile until re-saved.
- `SMTP_HOST` in the environment overrides saved settings at runtime; its mail is not pinned to a profile.
- Mode switch: jobs queued before a switch go out via the new profile only after `migrate_pending` is acknowledged; a worker may
  finish only a send already in progress through the previous profile (pinned to it); the next send uses the new profile (T3.3).
- Accepted messages are not re-checked against domain claims at dispatch.
- Strict domain gate: Live mail from unregistered domains is refused; `ALLOW_UNREGISTERED_LIVE_DOMAINS=true` is a temporary bridge.
- Fixed Compose subnet `172.30.0.0/24` (`NJIWA_SUBNET`); Live rate limits default 600/min, 20 000/h (installation), 300/min,
  10 000/h (per domain); `LIVE_QUEUE_MAX` default 50 000.
- The installer does not install packages; new sending domains need another `installer plan/apply -record`; the CA (10 years) is
  replaced by hand.
- SMTP to loopback/private addresses is allowed (a relay on the same host is normal); link-local/multicast is refused.

---

## 6. Rules the work has followed (keep them)

- Never say "delivered": a remote server accepting mail is `remote_accepted`, not inbox placement.
- Readiness must be honest: `cannot_confirm` instead of a guessed pass; optional steps never block; no green without evidence.
- Every installer change: plan → digest approval → backup → validate (`postfix check`, `opendkim -n`, `nft -c`) → reload →
  receipt → automatic rollback on failure.
- Secrets are sealed, never returned or logged (`TestNoSecretInAnyAdminResponse`, `TestSecretsNeverLoggedOrReturned`).
- Every new route must pass `TestEveryRouteRequiresAuthAndCSRF` (add it to a group, or to `publicRoutes` with a reason).
- Migrations are additive; tests re-run them on legacy snapshots.
- A ticket is done only when its predicate is tested; real-host claims need recorded evidence.
- One PR per round; never push to a branch whose PR is already merged — start a new branch from `main`.

---

## 7. Prompt for Claude Code on the office PC

Copy this as the first message in a Claude Code session started inside the cloned `Njiwa-Mail` folder:

> Read docs/HANDOFF.md, STATUS.md, docs/REMAINING_WORK.md and docs/OPERATIONS.md. We are continuing from the handoff. The
> server is reachable with `ssh <user>@<server>`. Start with section 3.1 (G05), step by step: run each command, show me the
> output, and stop before anything that changes DNS or sends Live mail so I can confirm. Record evidence as the handoff
> describes. Do not change code without running the suites in section 1.3.

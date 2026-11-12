# Operations runbook

## Preflight (before first live traffic)

1. `njiwa-mail smtp-check` — connects to Mailcow, **verifies TLS**, authenticates, sends nothing. Must pass.
2. Mailcow: sender address allowed for the SMTP mailbox; SPF/DKIM/DMARC pass for the sending domain; reverse DNS matches `SMTP_HELO_NAME`;
   port 587/465 reachable from the app host. Record observations in the release report. Do not alter staff mailbox routing or global Mailcow config.
3. API behind HTTPS (`deploy/nginx.conf`); `POSTGRES_PASSWORD`, `ENCRYPTION_KEY`, `HMAC_KEY` set and **backed up separately from the DB backup**.
4. Send one live message to a controlled inbox; check headers (DKIM/SPF results), record placement.

## Start / stop / restart

`docker compose up -d` (add `--profile inbound` once Mailcow routing exists) · `docker compose restart api worker scheduler` · `docker compose logs -f worker`.

| Role | Command | Does |
|---|---|---|
| api | `serve` | Application API, admin API, `/metrics`, health |
| worker | `worker` | Outbound dispatch and webhook delivery |
| scheduler | `scheduler` | Retention, lease recovery, daily DNS check, bounce-rate alerts |
| inbound | `inbound` | Private SMTP listener and the inbound/bounce parser |

Every role is safe to restart at any time and safe to run more than once.
State is in PostgreSQL; restarting loses nothing. On worker death mid-send the message becomes `unknown` after the lease (90s) expires — it is **never auto-resent**.

Health: `GET /health/live` (process), `GET /health/ready` (DB+schema), `njiwa-mail role-health -role worker|scheduler|inbound` (heartbeats; used by the compose healthchecks).

Metrics: set `METRICS_TOKEN` and scrape `GET /metrics` on `127.0.0.1:8080` with `Authorization: Bearer <token>` (Prometheus text format):
queue depth, oldest queued age, messages per state, bounce counts, webhook backlog and dead deliveries, inbound spool, open alerts, role liveness.

Alert on: any role down, `njiwa_mail_oldest_queued_age_seconds` rising, any `unknown`, rising `failed`/`retry_wait`,
`njiwa_mail_open_alerts > 0`, `njiwa_mail_webhook_dead > 0`, `njiwa_mail_inbound_spool{state="failed"} > 0`.

## Admin users

There is no signup page. Create the first admin on the server; any admin can add the rest in the UI.

```bash
njiwa-mail user create -email you@example.com -role admin     # password is read from stdin (min 12 characters)
njiwa-mail user password -email you@example.com               # reset a password; ends that user's sessions
njiwa-mail user disable -email someone@example.com
```
There is one user role: `admin`. Every enabled user can manage users, mail-server settings and recipient privacy, and read inbound bodies and sandbox captures. Existing owners are migrated to admin; old `owner` inputs are accepted as an alias for admin.
The last enabled user cannot be disabled. Logins are limited per account and per IP (15-minute windows; see Settings).
Every write from the UI, the CLI or an API key is recorded in `audit_log` (UI: Audit log). The admin API uses password-based sessions without MFA; keep `/admin/v1` behind HTTPS and an IP allowlist (`deploy/nginx.conf`).

## Alerts

Alerts are rows in the `alerts` table, shown on the Overview page, counted in `/metrics` and logged as `ALERT`. There is no e-mail or chat notification yet: wire your monitoring to the metric.

| Kind | Meaning | What to do |
|---|---|---|
| `policy_bounce` | A recipient server rejected us for policy (5.7.x) | Run the DNS check; look up the IP on blocklists. The recipient is **not** suppressed. |
| `policy_bounce_rate`, `hard_bounce_rate` | Rate over the last hour passed the threshold in Settings | Pause the offending project (`project disable`) and find the cause before sending more. |
| `webhook_endpoint_disabled` | An endpoint failed repeatedly and was switched off | Fix the receiver, enable the endpoint again, replay dead deliveries. |

## DNS health check

`njiwa-mail dns-check` (or "check now" in the UI; the scheduler also runs it daily). It reads SPF, DKIM, DMARC and MX for each live sending domain,
MX for the inbound domain and the PTR of `OUTBOUND_IP`, and shows the expected value next to what it observed. It never changes DNS.
Set `MAIL_HOSTNAME`, `OUTBOUND_IP` and `DKIM_SELECTOR` so the expected values are exact. The SPF check is a heuristic (it does not expand `include:`): read the observed record yourself.

## Managed delivery: host preflight

Before choosing "Send from my VPS", run the read-only preflight on the server itself. It changes nothing on the host and needs no
database:

```
njiwa-mail preflight -hostname mailout.example.com -ip 203.0.113.10
```

It checks the distribution (Debian 12, Ubuntu 22.04/24.04), systemd, disk and inodes for the queue, memory, clock synchronisation,
whether the sending IP is on this host, the mail hostname's A record, reverse DNS (PTR) forward-confirmed, outbound port 25 (it
connects to a large provider's MX and reads the greeting; no mail is sent) and existing mail servers that must not be overwritten.
A check it cannot verify is reported as `CANNOT_CONFIRM`, never as ready. It exits non-zero until every required check passes.

With the database configured, `njiwa-mail preflight -record` stores the result; the Delivery infrastructure page shows it as the
"Check this server can send directly" step. Managed mode still cannot become ready until the host installer has run.

## Managed delivery: installing the mail service

The installer is a root-only command on the mail host; the Njiwa web process never changes host configuration. Install the packages
first (`apt-get install postfix opendkim`, choosing "No configuration" for Postfix), then:

```
njiwa-mail installer plan  -hostname mailout.example.com -domains notify.example.com -bounce-domain bounce.example.com
njiwa-mail installer apply -hostname mailout.example.com -domains notify.example.com -bounce-domain bounce.example.com -approve <digest> -record
```

- `plan` prints every file it will create or change, the keys it will generate, and a digest. `apply` runs only when `-approve`
  matches the digest of the plan recomputed at that moment, so nothing changes that was not reviewed.
- What it configures: Postfix delivers directly to recipient MX servers; port 25 accepts only mail for the bounce domain and relays
  it to the Njiwa inbound listener (no open relay); the Njiwa worker submits on a private listener (default `127.0.0.1:10587`,
  never `0.0.0.0`) that requires the client certificate the installer issued to it (mutual TLS) and accepts only the signing
  domains and the bounce domain as envelope senders. OpenDKIM signs mail from that listener only; if the signer is down the listener
  refuses mail (fail closed) rather than sending it unsigned.
- Keys: a 2048-bit DKIM key per domain under `/etc/opendkim/keys/<domain>/<selector>.private` (0600, opendkim); a private CA, the
  server certificate and the worker's client certificate. Existing keys are never replaced by a re-run. Publish the DKIM records
  `apply` prints.
- Safety: existing files are backed up to `/var/lib/njiwa-mail/installer/backups/<receipt>`; `postfix check` and `opendkim -n`
  validate the result, and any failure restores every file automatically. A Postfix configuration the installer did not write, Exim
  or Mailcow on the host is refused unless `-adopt` is given (Mailcow and Exim: never).
- `-record` stores the receipt, the worker's credentials (sealed) and the DKIM records in the database. Then run the connection test
  on the Delivery infrastructure page; managed mode can be activated only after it passes.
- `njiwa-mail installer status` shows the latest receipt; `njiwa-mail installer rollback [-receipt ID] -record` restores the files
  from that apply's backup.
- When projects add sending domains later, run `plan`/`apply` again (with `-record` the domain list defaults to the installation's
  claimed domains).
- Verification on a host: `go test -tags managed_e2e ./internal/installer/managed -run E2E -v` (as root, after apply with the test
  domains in that file) checks no open relay, mTLS-only submission, sender restrictions and DKIM signing end to end.

## Delivery evidence from the mail server log

A message's state says what Njiwa-Mail did: `submitted` means your mail server (managed Postfix or an existing SMTP server)
accepted it. What happens afterwards is recorded separately as delivery evidence, each item with its source, and shown on the
message page:

- the SMTP reply (with the MTA queue ID, from "queued as …"),
- the mail server log (`deferred`, `sent` = the recipient's server accepted it, `bounced`, `expired`),
- bounce reports (DSNs) and feedback-loop complaints,
- operator resolutions of unknown outcomes.

None of these means "delivered to the inbox", and the UI never says so. For managed mode (and any Postfix you can read the log of),
run the collector next to Postfix:

```
njiwa-mail mta-log -follow                     # tails /var/log/mail.log, reopens it after rotation
journalctl -fu postfix | njiwa-mail mta-log -file -
```

It correlates lines to messages by queue ID (messages submitted in the last 7 days), ignores everything else, never stores addresses
or IPs from the log, and records each line once. A permanent MTA bounce marks the message bounced but never suppresses the
recipient; only a bounce report correlated to the message's own recipient does.

## Sending domains and claims

Live mail and Live sender addresses require a sending domain that the project added on the Domains page, verified with its
ownership TXT record, and holds the **installation-wide claim** for. Only one project can hold a claim: the first project whose
ownership check passes. Another project that adds the same domain sees "already used by another project" and cannot use it.

- Names are compared canonically (lowercase, punycode for international names, no trailing dot). A subdomain is a separate claim
  that needs its own DNS proof; owning `example.com` does not grant `notify.example.com`.
- `GET /admin/v1/domain-claims` lists claims. `POST /admin/v1/domain-claims/transfer {"name", "project"}` moves a claim to a project
  that has added the domain. Transfer is audited, resets every project's ownership proof for the name (the new owner re-checks with
  its own token), and the previous owner's senders stop counting as verified immediately.
- Upgrading: a domain verified by more than one project before the upgrade is flagged `review_required` and nobody can send Live
  mail from it until an operator transfers it. Live mail from a domain no project registered is refused with
  `domain_not_registered`. `ALLOW_UNREGISTERED_LIVE_DOMAINS=true` restores the old behaviour for unclaimed names only, as a
  temporary bridge while domains are registered; it never allows a claimed name.

## Inbound mail and bounces

Setup: [MAILCOW_SETUP.md](MAILCOW_SETUP.md). The listener writes each message to `DATA_DIR/spool` and records it before answering `250`; the parser runs afterwards.

- A message that cannot be parsed after 5 attempts stays in `inbound_spool` with state `failed`, and its raw file is kept for review (`DATA_DIR/spool/<id>.eml`) until inbound retention.
- Inbound bodies are encrypted in the database. **Attachments are stored unencrypted** under `DATA_DIR/attachments` with generated names: protect and back up that volume like the database.
- Bounce types: `5.1.x` hard (recipient suppressed, source `hard_bounce`), `4.x.x` soft (recorded), `5.7.x` policy (alert, no suppression), anything else `unknown` (recorded for review; includes `5.2.x` mailbox-full).
- A hard bounce suppresses only when the DSN matches one of our message ids **and** names that message's own recipient.
- To lift a suppression after the user fixed their mailbox: Suppressions → remove (a reason is required and audited).

## Message states

| State | Meaning | Operator action |
|---|---|---|
| `queued` / `retry_wait` | waiting for worker / bounded retry (≈15s, 45s, 120s, ±20%, never past TTL) | none |
| `submitting` | worker holds a lease | none (reaped to `unknown` if the lease expires) |
| `submitted` | Mailcow acknowledged; Mailcow owns onward delivery | investigate bounces in Mailcow logs |
| `failed` | known permanent failure (e.g. 5xx, auth, TLS verification) or retries exhausted | fix cause; the application decides whether to send a new event |
| `unknown` | acceptance may have happened (worker died / ack lost) | **reconcile** (below); do not resend blindly |
| `expired` | deadline passed before a submission started | none |
| `captured` | sandbox only; nothing was sent | — |

## Reconciling `unknown`

```bash
njiwa-mail message list -state unknown
# Find the message in Mailcow's Postfix log by Message-ID  <MESSAGE_UUID@sender-domain>  (also visible in captures/headers)
njiwa-mail message resolve -id ID -as submitted -note "postfix queue id ABC123"   # it was accepted
njiwa-mail message resolve -id ID -as failed    -note "not found in postfix log"  # it was not
```
Resolution only records evidence; it never resends. Tell the owning application the outcome if it matters to the user flow.

## Keys

```bash
njiwa-mail key list -project P
njiwa-mail key rotate -prefix PREFIX -grace 24h    # new key printed once; old key valid for the grace period
njiwa-mail key revoke -prefix PREFIX               # immediate
```
Suspected leak: revoke immediately, rotate, and review `message list` for the project. `project disable -name P` pauses acceptance for a whole project (queued messages still drain).

## Suppression and recipient policy

```bash
njiwa-mail suppress add -project P -mode live -email bad@example.org -reason "hard bounce 550 5.1.1 2026-11-09"
njiwa-mail suppress remove -project P -mode live -email bad@example.org
njiwa-mail env allow -project P -mode live -pattern @example.org      # widen the live allowlist deliberately
njiwa-mail env restrict -project P -mode live -on=false               # remove restriction only after the release gate passes
```
Hard bounces are suppressed automatically once bounce routing is set up; until then, review Mailcow delivery failures and suppress confirmed-invalid recipients by hand.

## Retention

The scheduler (and the worker, harmlessly twice) runs retention every `RETENTION_INTERVAL` (10m): deletes inbound bodies and attachments after the inbound retention period
(Settings, default 30 days), expired admin sessions, finished webhook deliveries, bounces and DNS results past their retention, and expired sandbox captures (24h), stale rate buckets, terminal message metadata older than
`METADATA_RETENTION` (30d), and clears any payload on a non-active row older than `UNKNOWN_PAYLOAD_PURGE` (≤24h; content is normally cleared at the terminal outcome).
One-off: `njiwa-mail retention`. **Backups can retain older encrypted data until the backup's own retention expires.**

## Backups

`scripts/backup.sh [OUT_DIR]` (reads `DATABASE_URL`, `DATA_DIR`) writes, owner-only, with a `MANIFEST.sha256`:

| File | Contents |
|---|---|
| `mail.dump` | PostgreSQL (`pg_dump -Fc`) |
| `data.tgz` | `DATA_DIR`: inbound attachments and unparsed spool |
| `managed.tgz` | managed host only, run as root: Postfix and OpenDKIM configuration, DKIM private keys, the local CA and the worker's certificate, installer receipts |
| `postfix-queue.tgz` | with `BACKUP_QUEUE=1`: the Postfix queue (Postfix is stopped for the few seconds of the copy so the queue is consistent) |

`ENCRYPTION_KEY` and `HMAC_KEY` are **not** in the backup: store them separately and protected. Without them, encrypted payloads in a
restored database cannot be read. The backup itself contains private keys: keep it encrypted and off the host.

`scripts/restore.sh BACKUP_DIR` verifies the manifest first, restores the database and `DATA_DIR`, and on a managed host (as root)
restores the files with their owners and permissions, rebuilds the Postfix lookup tables, runs `postfix check` and `opendkim -n`,
and restarts the services (restores the queue when `postfix-queue.tgz` is present).

**Restore drill (2026-11-12, development container, Postfix 3.8.6 + OpenDKIM):** seeded a database and one queued message, ran
`BACKUP_QUEUE=1 scripts/backup.sh`, deleted the DKIM keys, certificates, Postfix/OpenDKIM configuration, installer receipts, the
queue and the database, then ran `scripts/restore.sh`. Result: the database rows and the queued message were back, the DKIM key and
the worker's client key were byte-identical (same SHA-256), owners and permissions were intact (`opendkim 0750` key directories,
`0600` keys), `installer plan` showed nothing to change, and all `managed_e2e` checks passed. Repeat the drill on the production host
before relying on it.
## Rollback

1. Pause acceptance for the affected project (`project disable`) **or** switch the application's provider flag for *new* requests.
2. Preserve `queued`, `submitted` and `unknown` records. Let queued messages drain, or accept their expiry, before rerouting those application events.
3. Never resend `submitted`/`unknown` events through another provider without reconciliation.
4. Redeploy the previous image tag. Migrations are forward-only. **`0002_v2.sql` renames `messages.message_id_header` to `smtp_message_id`, so the v1 binary does not run against a v2 database**: take a `pg_dump` before the first v2 `migrate`, and restore it if you must go back to v1.
5. Report affected message IDs and outcomes; never include OTPs, tokens or keys.

## Logging

JSON logs contain message ID, state, safe reason code, timings. They never contain bodies, variables, recipients, tokens, `Authorization` headers or SMTP credentials (covered by a test).

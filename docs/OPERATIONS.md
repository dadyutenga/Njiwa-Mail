# Operations runbook

## Preflight (before first live traffic)

1. `ditronics-mail smtp-check` — connects to Mailcow, **verifies TLS**, authenticates, sends nothing. Must pass.
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

Health: `GET /health/live` (process), `GET /health/ready` (DB+schema), `ditronics-mail role-health -role worker|scheduler|inbound` (heartbeats; used by the compose healthchecks).

Metrics: set `METRICS_TOKEN` and scrape `GET /metrics` on `127.0.0.1:8080` with `Authorization: Bearer <token>` (Prometheus text format):
queue depth, oldest queued age, messages per state, bounce counts, webhook backlog and dead deliveries, inbound spool, open alerts, role liveness.

Alert on: any role down, `ditronics_mail_oldest_queued_age_seconds` rising, any `unknown`, rising `failed`/`retry_wait`,
`ditronics_mail_open_alerts > 0`, `ditronics_mail_webhook_dead > 0`, `ditronics_mail_inbound_spool{state="failed"} > 0`.

## Admin users

There is no signup page. Create the first owner on the server; owners add the rest in the UI.

```bash
ditronics-mail user create -email you@example.com -role owner     # password is read from stdin (min 12 characters)
ditronics-mail user password -email you@example.com               # reset a password; ends that user's sessions
ditronics-mail user disable -email someone@example.com
```
Roles: `owner` (everything, including users) and `admin` (everything except users). Both can read the console, including inbound bodies and sandbox captures.
The last enabled owner cannot be demoted or disabled. Logins are limited per account and per IP (15-minute windows; see Settings).
Every write from the UI, the CLI or an API key is recorded in `audit_log` (UI: Audit log). TOTP is not implemented yet: keep `/admin/v1` behind HTTPS and an IP allowlist (`deploy/nginx.conf`).

## Alerts

Alerts are rows in the `alerts` table, shown on the Overview page, counted in `/metrics` and logged as `ALERT`. There is no e-mail or chat notification yet: wire your monitoring to the metric.

| Kind | Meaning | What to do |
|---|---|---|
| `policy_bounce` | A recipient server rejected us for policy (5.7.x) | Run the DNS check; look up the IP on blocklists. The recipient is **not** suppressed. |
| `policy_bounce_rate`, `hard_bounce_rate` | Rate over the last hour passed the threshold in Settings | Pause the offending project (`project disable`) and find the cause before sending more. |
| `webhook_endpoint_disabled` | An endpoint failed repeatedly and was switched off | Fix the receiver, enable the endpoint again, replay dead deliveries. |

## DNS health check

`ditronics-mail dns-check` (or "check now" in the UI; the scheduler also runs it daily). It reads SPF, DKIM, DMARC and MX for each live sending domain,
MX for the inbound domain and the PTR of `OUTBOUND_IP`, and shows the expected value next to what it observed. It never changes DNS.
Set `MAIL_HOSTNAME`, `OUTBOUND_IP` and `DKIM_SELECTOR` so the expected values are exact. The SPF check is a heuristic (it does not expand `include:`): read the observed record yourself.

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
ditronics-mail message list -state unknown
# Find the message in Mailcow's Postfix log by Message-ID  <MESSAGE_UUID@sender-domain>  (also visible in captures/headers)
ditronics-mail message resolve -id ID -as submitted -note "postfix queue id ABC123"   # it was accepted
ditronics-mail message resolve -id ID -as failed    -note "not found in postfix log"  # it was not
```
Resolution only records evidence; it never resends. Tell the owning application the outcome if it matters to the user flow.

## Keys

```bash
ditronics-mail key list -project P
ditronics-mail key rotate -prefix PREFIX -grace 24h    # new key printed once; old key valid for the grace period
ditronics-mail key revoke -prefix PREFIX               # immediate
```
Suspected leak: revoke immediately, rotate, and review `message list` for the project. `project disable -name P` pauses acceptance for a whole project (queued messages still drain).

## Suppression and recipient policy

```bash
ditronics-mail suppress add -project P -mode live -email bad@example.org -reason "hard bounce 550 5.1.1 2026-11-09"
ditronics-mail suppress remove -project P -mode live -email bad@example.org
ditronics-mail env allow -project P -mode live -pattern @example.org      # widen the live allowlist deliberately
ditronics-mail env restrict -project P -mode live -on=false               # remove restriction only after the release gate passes
```
Hard bounces are suppressed automatically once bounce routing is set up; until then, review Mailcow delivery failures and suppress confirmed-invalid recipients by hand.

## Retention

The scheduler (and the worker, harmlessly twice) runs retention every `RETENTION_INTERVAL` (10m): deletes inbound bodies and attachments after the inbound retention period
(Settings, default 30 days), expired admin sessions, finished webhook deliveries, bounces and DNS results past their retention, and expired sandbox captures (24h), stale rate buckets, terminal message metadata older than
`METADATA_RETENTION` (30d), and clears any payload on a non-active row older than `UNKNOWN_PAYLOAD_PURGE` (≤24h; content is normally cleared at the terminal outcome).
One-off: `ditronics-mail retention`. **Backups can retain older encrypted data until the backup's own retention expires.**

## Backups

`pg_dump` the `mail` database on a schedule, back up the `maildata` volume (inbound attachments and unparsed spool) with it, and store `ENCRYPTION_KEY`/`HMAC_KEY` in a separate protected location. A backup without the keys cannot decrypt in-flight payloads.
**No restore test has been performed**; do not claim disaster-recovery readiness until one is done and recorded.

## Rollback

1. Pause acceptance for the affected project (`project disable`) **or** switch the application's provider flag for *new* requests.
2. Preserve `queued`, `submitted` and `unknown` records. Let queued messages drain, or accept their expiry, before rerouting those application events.
3. Never resend `submitted`/`unknown` events through another provider without reconciliation.
4. Redeploy the previous image tag. Migrations are forward-only. **`0002_v2.sql` renames `messages.message_id_header` to `smtp_message_id`, so the v1 binary does not run against a v2 database**: take a `pg_dump` before the first v2 `migrate`, and restore it if you must go back to v1.
5. Report affected message IDs and outcomes; never include OTPs, tokens or keys.

## Logging

JSON logs contain message ID, state, safe reason code, timings. They never contain bodies, variables, recipients, tokens, `Authorization` headers or SMTP credentials (covered by a test).

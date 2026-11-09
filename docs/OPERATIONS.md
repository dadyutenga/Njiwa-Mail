# Operations runbook

## Preflight (before first live traffic)

1. `ditronics-mail smtp-check` — connects to Mailcow, **verifies TLS**, authenticates, sends nothing. Must pass.
2. Mailcow: sender address allowed for the SMTP mailbox; SPF/DKIM/DMARC pass for the sending domain; reverse DNS matches `SMTP_HELO_NAME`;
   port 587/465 reachable from the app host. Record observations in the release report. Do not alter staff mailbox routing or global Mailcow config.
3. API behind HTTPS (`deploy/nginx.conf`); `POSTGRES_PASSWORD`, `ENCRYPTION_KEY`, `HMAC_KEY` set and **backed up separately from the DB backup**.
4. Send one live message to a controlled inbox; check headers (DKIM/SPF results), record placement.

## Start / stop / restart

`docker compose up -d` · `docker compose restart api worker` · `docker compose logs -f worker`.
State is in PostgreSQL; restarting loses nothing. On worker death mid-send the message becomes `unknown` after the lease (90s) expires — it is **never auto-resent**.

Health: `GET /health/live` (process), `GET /health/ready` (DB+schema), `ditronics-mail worker-health` (worker heartbeat; used by the compose healthcheck).
Alert on: worker unhealthy, queue age (`message list -state queued`), any `unknown`, rising `failed`/`retry_wait`.

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

## Suppression and recipient policy (manual until bounce automation exists)

```bash
ditronics-mail suppress add -project P -mode live -email bad@example.org -reason "hard bounce 550 5.1.1 2026-11-09"
ditronics-mail suppress remove -project P -mode live -email bad@example.org
ditronics-mail env allow -project P -mode live -pattern @example.org      # widen the live allowlist deliberately
ditronics-mail env restrict -project P -mode live -on=false               # remove restriction only after the release gate passes
```
During the pilot, review Mailcow delivery failures regularly and suppress confirmed-invalid recipients.

## Retention

The worker runs retention every `RETENTION_INTERVAL` (10m): deletes expired sandbox captures (24h), stale rate buckets, terminal message metadata older than
`METADATA_RETENTION` (30d), and clears any payload on a non-active row older than `UNKNOWN_PAYLOAD_PURGE` (≤24h; content is normally cleared at the terminal outcome).
One-off: `ditronics-mail retention`. **Backups can retain older encrypted data until the backup's own retention expires.**

## Backups

`pg_dump` the `mail` database on a schedule and store `ENCRYPTION_KEY`/`HMAC_KEY` in a separate protected location. A backup without the keys cannot decrypt in-flight payloads.
**No restore test has been performed**; do not claim disaster-recovery readiness until one is done and recorded.

## Rollback

1. Pause acceptance for the affected project (`project disable`) **or** switch the application's provider flag for *new* requests.
2. Preserve `queued`, `submitted` and `unknown` records. Let queued messages drain, or accept their expiry, before rerouting those application events.
3. Never resend `submitted`/`unknown` events through another provider without reconciliation.
4. Redeploy the previous image tag (migrations are additive/forward-only; this release has a single initial migration).
5. Report affected message IDs and outcomes; never include OTPs, tokens or keys.

## Logging

JSON logs contain message ID, state, safe reason code, timings. They never contain bodies, variables, recipients, tokens, `Authorization` headers or SMTP credentials (covered by a test).

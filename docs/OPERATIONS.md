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
There is one user role, `admin`, plus an **installation-operator** permission. Operators manage delivery infrastructure (transport profiles, mail server, domain ownership), users and settings; other admins keep the project work (messages, templates, keys, domains of a project) and read inbound bodies and sandbox captures. Every existing user and every new user is an operator unless created otherwise (`user create -operator=false`, or `installation_operator: false` in the API); `user operator -email E -on=false` changes it. The first user is always an operator and the last enabled operator cannot be demoted or disabled. Old `owner` inputs are accepted as an alias for admin.

**Two-step sign-in (TOTP).** Each user can turn it on under Profile (authenticator app; ten one-time recovery codes are shown once). To require it for everyone, tick *Require two-step sign-in for every admin* in Settings (`require_mfa`): admins without it can then only enrol or sign out. Lost device and codes: an operator uses *Reset two-step* on the Users page, or `njiwa-mail user mfa-reset -email E` on the server.
The last enabled user cannot be disabled. Logins are limited per account and per IP (15-minute windows; see Settings).
Every write from the UI, the CLI or an API key is recorded in `audit_log` (UI: Audit log). The admin API uses password sessions with optional TOTP; keep `/admin/v1` behind HTTPS and an IP allowlist (`deploy/nginx.conf`).

## Alerts

Alerts are rows in the `alerts` table, shown on the Overview page, counted in `/metrics` and logged as `ALERT`. Njiwa-Mail does not
send notifications itself (when sending is broken, so would be that path): scrape `/metrics` with Prometheus and route
[`deploy/prometheus/njiwa-mail-alerts.yml`](../deploy/prometheus/njiwa-mail-alerts.yml) through Alertmanager to e-mail, chat or a pager.

Delivery-health metrics (F08), all derived from the database so every instance reports the same:

| Metric | Meaning |
|---|---|
| `njiwa_mail_installation_ready{mode}`, `njiwa_mail_installation_problem{code}` | readiness of the active delivery profile, and the problem code of each required step that is not ready |
| `njiwa_mail_transport_failing` | 1 while Live mail cannot reach the mail server (`TRANSPORT_FAILING`) |
| `njiwa_mail_transport_retries_15m{reason}` | connection-level retries; `smtp_454` on a managed host means the DKIM signer is down |
| `njiwa_mail_live_backlog`, `njiwa_mail_live_backlog_limit` | Live mail still to hand over, and `LIVE_QUEUE_MAX` |
| `njiwa_mail_last_live_submission_age_seconds` | time since a Live message was last handed over |
| `njiwa_mail_delivery_events_1h{event}` | what the mail server reported in the last hour (deferred, remote_accepted, bounced, ...; needs `mta-log`) |
| `njiwa_mail_client_cert_expiry_seconds` | managed mode: time left on the worker's submission certificate |

The rules file alerts on: API down, transport failing, signer refusing, backlog above 80 % of the limit, a message waiting more than
30 minutes, deferred rate above 20 % (with at least 20 deliveries), readiness incomplete for 30 minutes, certificate within 30 days,
a role without heartbeats, and open operator alerts. It was validated with the Prometheus 0.54 rule parser.

Operator alerts in the `alerts` table:

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

## Rotating DKIM keys (managed mode)

Rotation is staged so mail is never signed with a key receivers cannot find:

```
njiwa-mail installer dkim-rotate prepare  -selector s2026b   <same flags as apply>   # new keys; signing unchanged
#   publish the printed s2026b._domainkey records, keep the old ones
njiwa-mail installer plan -selector s2026b <same flags>                             # review, note the digest
njiwa-mail installer dkim-rotate activate -selector s2026b -approve <digest> -record <same flags>
#   wait at least 7 days (messages in transit and retries), then
njiwa-mail installer dkim-rotate retire   -selector <old selector>                  # deletes the old private keys
```

- `activate` refuses until DNS publishes exactly the prepared key for every domain, then switches signing through a normal approved
  apply (backup, validation, receipt, rollback); `-record` updates the Domains page to the new selector.
- `retire` refuses for the selector in use. `cancel` abandons a prepared rotation and deletes its keys; `status` shows the signing
  selector and any prepared rotation.
- Verified in the development container against OpenDKIM 2.11: before activation mail stayed signed `s=njiwa`; after the switch a
  received message carried a cryptographically verified signature `s=njiwa2`; switching back verified `s=njiwa` again.

## Renewing the submission certificates (managed mode)

The worker hands mail to Postfix over mutual TLS with certificates from the installer's local CA (server and worker certificates:
5 years; CA: 10 years). `njiwa-mail installer status` lists each certificate and its expiry.

- **Automatic with every apply:** a plan made within 60 days of a leaf certificate's expiry includes `renew` lines; apply it with
  `-record` as usual. Run `installer plan` from a monthly timer or calendar reminder so this happens in time.
- **On demand:** `njiwa-mail installer plan -renew-certs` (same flags), then `installer apply -renew-certs -approve <digest> -record`.
- **No interruption:** after a renewal Postfix keeps accepting the replaced worker certificate for 7 days
  (`njiwa-worker-previous` in `/etc/postfix/njiwa/relay_clientcerts`), so a worker that has not yet picked up the recorded
  certificate keeps sending. The next apply after those 7 days drops it.
- **Readiness:** the Installation page warns `CLIENT_CERT_EXPIRING` 30 days before the recorded worker certificate expires and shows
  `CLIENT_CERT_EXPIRED` after. Live mail is then refused by Postfix and stays queued until a renewal is recorded.
- **The CA is not renewed automatically.** `installer plan` warns in its last year. To replace it, back up (see Backups), move
  `/etc/postfix/njiwa/ca.crt` and `ca.key` aside, and run plan/apply `-record`: a new CA, server and worker certificate are issued
  together (expect a short refusal window until the worker reloads, about 30 s).
- Verified in the development container against Postfix 3.8.6: after `-renew-certs` both the new and the replaced worker certificate
  could send; after the grace period the replaced one was refused `554 5.7.1` and the new one still sent signed mail.

## HTTPS and the host firewall

**HTTPS.** `deploy/nginx.conf` is the reverse proxy: `/` → dashboard (`127.0.0.1:3000`), `/v1/` and `/admin/v1/` → API
(`127.0.0.1:8080`); `/metrics` and `/health/*` answer 404 from outside (scrape them on `127.0.0.1:8080`); HTTP redirects to HTTPS
except the ACME challenge path, so `certbot certonly --webroot -w /var/www/certbot -d <host>` and its renewals work. It sends HSTS,
`nosniff`, `X-Frame-Options: DENY`, a referrer policy and a permissions policy, and hides the nginx version. Verified with
`nginx -t` and requests through it on nginx 1.24 (Ubuntu 24.04) with HTTP/2. Restrict `/admin/v1/` and `/` to office/VPN ranges
where you can (commented `allow`/`deny` lines).

**Firewall (managed mode).** `njiwa-mail installer apply` also installs an nftables table, `inet njiwa_mail`
(`/etc/njiwa-mail/firewall.nft`, loaded at boot by `njiwa-mail-firewall.service`). Its policy is **accept**: it never blocks SSH or
other services. It only drops, from anything but the host itself, the worker submission port (open to the Compose network when the
listener is on its gateway) and Njiwa's private ports 2525, 5432, 8080, 3000 and 8891. Port 25 stays open (bounces; it refuses
relaying). Inspect with `nft list table inet njiwa_mail`; skip with `-no-firewall`. The installer validates it with `nft -c` and
rolls back like any other file. Tested from a separate network namespace: port 25 reachable, port 3000 dropped with the table, open
without it. Your provider's firewall and SSH rules remain yours to manage.

`njiwa-mail preflight` reports any private port listening on a public interface (`PRIVATE_PORT_PUBLIC`).

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

## Bounce reports

Sending works without a bounce path, but then hard bounces are not suppressed automatically and delivery evidence is limited to
what the mail server answered. The Delivery infrastructure page shows an optional step, **Bounce reports reach Njiwa-Mail**:

- **Signed return path (F06, `BOUNCE_VERP`).** With the managed Postfix every message leaves with the envelope sender
  `bounces+<message id>.<signature>@<bounce domain>`; a returned notification is matched to the message by that address (HMAC with
  `HMAC_KEY`), which a remote server cannot rewrite and a third party cannot forge. A bounce whose signature does not verify is stored
  as uncorrelated (`correlated_by=verp_invalid`) and never suppresses anyone, even if its headers name a real message. Bounces
  without a signed address (messages sent before the change, existing SMTP servers) are still matched by the message id in the returned
  headers (`correlated_by=header`). `BOUNCE_VERP=auto` (default) enables it only on the managed transport; set `on` for an existing SMTP
  server **after** confirming `bounces+test@<bounce domain>` is delivered to the bounce mailbox (Mailcow's Postfix accepts `+`
  extensions; an alias or a custom server may not), or `off` to keep the plain address. The installer sets `recipient_delimiter = +`.
  Check on a real managed host: send to a non-existent address and see `correlated_by = verp` in Bounces (E07).
- `BOUNCES_NOT_CONFIGURED`: set `BOUNCE_ADDRESS` (the envelope sender of every Live message) on a domain whose MX reaches a mail
  server that relays it to the Njiwa inbound listener. Managed mode does this for the bounce domain given to the installer; with an
  existing SMTP server, configure that server to relay the bounce domain to the listener (or poll a bounce mailbox with `IMAP_HOST`).
- `BOUNCE_LISTENER_DOWN`: the inbound role has not reported in for 5 minutes; start it (`docker compose --profile inbound up -d`).
- Ready: shows when the last bounce report arrived. Some providers never return DSNs to a custom envelope sender; then this stays
  "none has arrived yet" and bounce visibility is limited by design.

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

## Existing SMTP: what your mail server sends from

Njiwa-Mail cannot see the egress or the DKIM signer of a server it does not run, so with a generic SMTP server the SPF and DKIM
records start as templates. Enter the facts your mail server's operator documents and they become real requirements and checks:

- Delivery infrastructure → **What your mail server sends from**: sending IP addresses or ranges (public only, at most /16 or
  /32 for IPv6), the SPF `include:` domain, and the DKIM selector.
- Each domain's page → **Your mail server's DKIM key for this domain**: the public key the server signs that domain with.

SPF then must authorise every stated address (or, with only an include, reference that include); DKIM must publish exactly that
key at that selector. API: `PUT /admin/v1/installation/transport-profiles/{id}/sending-facts` (`dkim_keys` entries are merged;
an empty value removes one). Facts are audited and never change the connection settings or invalidate a connection test.

## Proof that mail is signed (DKIM)

A DKIM record in DNS only proves a key is published. The Domains page records separate proof that mail from the domain is actually
signed: a received message with a valid DKIM signature whose `d=` aligns with the domain (as DMARC requires).

- Send a test (Delivery infrastructure → Send through the queue) to a mailbox you control, open the original message
  ("Show original" / "View source"), and paste it under **Proof that mail is signed** on the domain's page.
- Or send it to one of the project's own Njiwa inbound addresses: the inbound processor checks it automatically.
- From a shell: `njiwa-mail dkim-verify -file message.eml` prints the result of every signature (pass, fail with the reason,
  permerror, temperror). It needs DNS only.

The verifier implements RFC 6376 (rsa-sha256 and ed25519-sha256, simple and relaxed canonicalization, `l=`, `x=`) and is tested
against a message signed by OpenDKIM. A message from another domain, a changed body or header, a revoked or wrong key, or a
signature by a non-aligned domain is never recorded as proof.

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

## Mail queue controls (E10, managed mode)

The API runs in a container and gets no host privileges. To see and control the Postfix queue from the dashboard (Admin → Mail queue), run the
agent on the mail host:

1. `openssl rand -base64 36 > /etc/njiwa-mail/mta-agent.token && chmod 0600 /etc/njiwa-mail/mta-agent.token`
2. `cp deploy/njiwa-mail-mta-agent.service /etc/systemd/system/ && systemctl daemon-reload && systemctl enable --now njiwa-mail-mta-agent`
   (`-group 10001` makes the socket `root:10001 0660`, the API container's user.)
3. Give the API the socket and the same token: `docker compose -f compose.yaml -f compose.mta-agent.yaml --profile inbound up -d` with
   `MTA_AGENT_TOKEN=<token>` in `.env`.

What it can do: list the queue (`postqueue -j`), hold / release / re-queue / delete one message (`postsuper -h/-H/-r/-d`), retry all deferred
mail (`postqueue -f`), and report whether Postfix and the DKIM signer are running. Nothing else: the agent runs exactly those commands, without a
shell, after checking the queue id (`[0-9A-Za-z]{6,20}`), listens only on the Unix socket, and refuses every request without the token.
In the API these routes are for installation operators only, every action is audited (`mta.queue.*`), **delete requires typing the queue id**,
recipient and sender addresses are masked, and queue ids are matched to our messages (a delete or hold is noted on that message's timeline as
operator evidence; the message state itself never changes). If the agent is not configured the page says so; if it is stopped the API answers
`503 mta_agent_unreachable`. Check on a real host: `curl --unix-socket /run/njiwa-mail/mta-agent.sock -H "Authorization: Bearer $(cat /etc/njiwa-mail/mta-agent.token)" http://agent/status`.

## Rotating `ENCRYPTION_KEY` (C02)

Sealed data (queued payloads, sent copies, drafts, captures, inbound bodies and attachment files, webhook signing secrets, SMTP/Mailcow/managed
credentials, authenticator secrets, campaign recipients) is encrypted with `ENCRYPTION_KEY`. To change it without downtime:

1. Back up the database and the **current** key. Generate the new key: `openssl rand -base64 32`.
2. Set `ENCRYPTION_KEY=<new>` and `ENCRYPTION_KEY_PREVIOUS=<old>` (or the `_FILE` variants) for every container, then restart. The service
   now seals with the new key and still opens everything sealed with the old one.
3. `njiwa-mail rekey -check` counts what still uses the old key. `njiwa-mail rekey` re-seals it (safe while the service runs, repeatable,
   resumable; each row is replaced only if it did not change meanwhile). It exits non-zero if some value cannot be opened by any
   configured key: add the key that sealed it to `ENCRYPTION_KEY_PREVIOUS` and run again.
4. When `rekey -check` says nothing uses a previous key, remove `ENCRYPTION_KEY_PREVIOUS` and restart. **Old backups stay sealed with
   the old key: keep it for as long as you may restore them.** `HMAC_KEY` is not rotated by this procedure (it keys recipient hashes
   and idempotency fingerprints; changing it breaks lookups).

A test fails when a migration adds a sealed column that `rekey` does not know (`TestEverySealedColumnIsRekeyed`).

## Retention

The scheduler (and the worker, harmlessly twice) runs retention every `RETENTION_INTERVAL` (10m): deletes inbound bodies and attachments after the inbound retention period
(Settings, default 30 days), expired admin sessions, finished webhook deliveries, bounces and DNS results past their retention, and expired sandbox captures (24h), stale rate buckets, terminal message metadata older than
`METADATA_RETENTION` (30d), and clears any payload on a non-active row older than `UNKNOWN_PAYLOAD_PURGE` (≤24h; content is normally cleared at the terminal outcome).
Also (F10, Settings): delivery evidence (`delivery_events_retention_days`, default 90) and installer receipts
(`installer_receipts_retention_days`, default 365; the receipt that proves a profile's installation and the newest receipt of each profile are never removed).
Each run also seals up to 200 inbound attachment files written before encryption at rest; `njiwa-mail attachments encrypt` converts all of them now.

**Encryption at rest.** Inbound attachments are stored sealed with `ENCRYPTION_KEY` (AES-GCM, bound to the attachment id) and decrypted on download; losing the key makes them unreadable, like queued mail. The raw spool file (`DATA_DIR/spool`) is plaintext until parsed, normally seconds. Backups copy the sealed files.

**Data-subject requests.** Recipient privacy page (or `POST /admin/v1/w/{project}/{mode}/privacy/export[?format=csv]` and `/privacy/delete`): export lists message metadata, delivery evidence, bounces, inbound mail and suppression state (no bodies, no variables); delete removes them and the attachment files. A single finished message can be exported (`GET .../messages/{id}/export`) or deleted (`DELETE .../messages/{id}`; queued, retrying, scheduled or unknown messages are refused with `message_active`). All are audited.

One-off: `njiwa-mail retention`. **Backups can retain older encrypted data until the backup's own retention expires.**

## When the mail server is down

- Messages are never lost or sent twice: a temporary failure before acceptance is retried with backoff until the message's TTL;
  an ambiguous outcome is marked `unknown` and never resent automatically.
- After 5 consecutive connection-level failures (connect, timeout, TLS, `421`/`454`) the worker stops claiming Live mail for 15 s,
  doubling up to 5 min, so queued messages keep their attempts and TTL instead of each failing against a server that is down. One
  successful hand-over resets it. Sandbox is unaffected.
- The Delivery infrastructure page and every project's banner show "Live mail is going out — Error (TRANSPORT_FAILING)" while
  recent Live attempts fail with no successful submission since; it clears on the next successful send.
- The Live backlog is bounded by `LIVE_QUEUE_MAX` (default 50 000). When it is full, new Live requests get
  `503 queue_full` with `Retry-After`; nothing is stored for them, so clients can retry safely with the same idempotency key.

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

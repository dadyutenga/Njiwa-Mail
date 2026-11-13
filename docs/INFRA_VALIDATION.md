# Infrastructure validation checklist (real-host evidence)

Status: **plan only. Nothing here has been run, provisioned or approved.** Source of every test ID and acceptance criterion: `HANDOFF.md`
sections 3 and 4 (commit `f18cf11`). Where this file adds something (sizing advice, safety steps) it says so.

## 0. Ground rules (apply to every step)

1. **`rida-server` and its Mailcow are never touched.** No managed installer, no Postfix/firewall/DNS/mailbox change, no Mailcow API key or
   sender created there. The managed installer refuses a host with another mail server, and must not be forced.
2. **Approval gates.** Each of these needs an explicit "yes" from the owner at that moment, not earlier: (a) provisioning any VPS or paid
   service, (b) any real DNS change, (c) the first Live message, (d) any further Live batch, (e) destroying the VPS.
3. **Separate names.** Use a throwaway domain, or subdomains of a domain the owner controls that Mailcow does not use. Never publish SPF, DKIM,
   DMARC or MX records on a name Mailcow sends or receives mail for.
4. **Recipients are the owner's own Gmail and Outlook mailboxes only**, plus the deliberate non-existent Gmail address for E07. Low volume.
5. **Never write "delivered"** from SMTP acceptance. Delivery evidence is the received message and its headers.
6. Evidence goes in `docs/evidence/<ID>.md` with addresses removed, and a row in `STATUS.md`.

## 1. Tests and acceptance criteria

| ID | What | Acceptance (from HANDOFF.md) |
|---|---|---|
| **G05** (3.1, P0 blocker) | Managed mode end to end on a fresh server | Gmail **and** Outlook both show `spf=pass`, `dkim=pass header.d=<sending domain> header.s=<selector>`, `dmarc=pass`; headers saved in `docs/evidence/G05.md`; the systemd path worked or was fixed; STATUS row. Also: preflight lines all `pass` or understood; connection test on the managed profile passes; readiness shows every required step ready; port 25 refuses relaying (`554 5.7.1`); submission without the client certificate is refused; "Check signature" records DKIM proof; log collector `njiwa-mail-mta-log` shows delivery evidence on the message page; HTTPS in front of the API |
| **E06** (3.2) | Direct-to-MX delivery, queue durability | 3+ external providers show `remote_accepted` in `njiwa-mail mta-log`; with outbound 25 blocked a message sits `deferred` in `postqueue -p`, survives a reboot, and is delivered after unblocking; the queue survives `apt-get upgrade postfix` with mail queued; STATUS row |
| **E07** (3.3) | Real bounce | Live mail to `does-not-exist-xyz@gmail.com`; the DSN arrives at `bounces+<id>.<tag>@<bounce domain>` -> port 25 -> `127.0.0.1:2525`; the message shows `bounced`, the recipient is suppressed (hard bounce), an `email.bounced` webhook fires, Installation `bounce_reports` shows "Bounce reports are received" with a time; STATUS row |
| **F06** (with E07) | Signed return path on a real bounce | `correlated_by = verp` on the real bounce; Postfix relays `bounces+<id>.<tag>@<bounce domain>` to the inbound listener |
| **D04** (with E07) | Per-domain return path | Installer run with `-bounce-domain a,b`: bounces for each domain correlate (record in the E07 row) |
| **E05 rest** (3.4) | HELO, optional certificate | `Received: from <mail hostname>` as seen by the remote server matches the PTR. A publicly trusted inbound certificate is optional (would need an installer flag) |
| **E10** (with E06) | Queue controls in the dashboard | Agent unit installed; Admin -> Mail queue; with outbound 25 blocked create a deferred message; hold / release / requeue / delete it; timeline note and audit rows present; STATUS row |
| **E12** (3.5) | Clean-install matrix | G05 steps 1-13 repeated on fresh Debian 12, Ubuntu 22.04, Ubuntu 24.04; Postfix/OpenDKIM versions and differences in a compatibility table (also feeds G10) |
| **G04** (3.6) | Existing SMTP for real | (a) Mailcow per `docs/MAILCOW_SETUP.md`, with and without the Mailcow API key: profile type Mailcow, connection test passes, DKIM key pasted as a sending fact, send to Gmail/Outlook, headers recorded. (b) A generic Postfix/Exim server not managed by Njiwa: profile type generic, sending facts entered, the Domains page asks for exactly the right SPF/DKIM, send, headers recorded |
| **C06** | Repeatable staging test | The generic-server run becomes a script or a `//go:build staging` test using the same API |
| **G06** (3.7) | Mode switch with a non-empty queue | Existing SMTP with mail in `retry_wait`; switch to managed: the plan shows pending counts, activation needs `migrate_pending`, after acknowledging queued mail leaves via managed Postfix, each message pinned to the profile that sent it; roll back and repeat in the other direction; legacy upgrade: an old release with a `mail_server` row upgrades, migration `0010` makes the active profile, sending continues |
| **G03 / G02** (3.8) | Backup, restore, firewall on a real host | `scripts/backup.sh` with `BACKUP_QUEUE=1`, copy off host, `scripts/restore.sh` on a fresh VPS, e2e passes; from another machine port 25 is open and 2525, 5432, 8080, 3000, 8891, 10587 are not reachable (`nmap`) |
| **G09** | Non-author install | Someone who did not write the code installs both modes on a clean server using only the docs; every place they got stuck is noted and fixed |
| **G10** | Release report | `docs/RELEASE_REPORT.md` compatibility table filled from E12, rollback runbook exercised |
| G07 (3.9) | CI, supply chain | **Done** (all jobs green on GitHub) |

Also open but not infrastructure: screen-reader and phone pass, native-speaker Kiswahili review, a real Cloudflare run of the DNS setup,
registering a Cloudflare OAuth client, "Open in ..." links checked in a real browser, publishing the docs portal, an external penetration test.

## 2. What needs what

| Needs a **dedicated public VPS** (managed Postfix, real port 25, PTR) | Needs only an SMTP server that is not Mailcow-on-rida-server | Can run locally or in throwaway containers |
|---|---|---|
| G05, E06, E07, F06, D04, E05 rest, E10, E12 (3 VPSes in sequence, or reuse one rebuilt), G06 managed leg, G03/G02 restore drill and nmap check, G09 | G04(b) generic server and C06: any Postfix/Exim/relay the owner runs, with its own sending IP, SPF/DKIM and domain. G04(a) Mailcow: needs **its own** Mailcow instance (not rida-server's; Mailcow documents a much larger RAM minimum than the managed mode, check its docs) | Connection-test and diagnosis logic, transport and profile tests (already in CI against fakes and Mailpit); G06 legacy upgrade with migration `0010` (old release + PostgreSQL in containers); the database and data half of backup/restore; the DNS wizard against fakes (done); portal checks in a browser; Kiswahili and accessibility reviews |

Real Cloudflare DNS setup test needs a Cloudflare account and one throwaway zone, not a VPS.

## 3. Minimum VPS specification and network capabilities

From HANDOFF 3.0 (hard requirements):

- Debian 12, Ubuntu 22.04 or Ubuntu 24.04; root; systemd; correct time (NTP).
- At least 2 GB RAM and 10 GB free disk. **My advice (not tested):** 2 vCPU, 4 GB RAM, 25 GB disk, plus 2 GB swap, because the stack
  (PostgreSQL, API, worker, scheduler, inbound, dashboard) is built with Docker on the host next to Postfix and OpenDKIM.
- A static public IPv4 and a **PTR** (reverse DNS) at the provider set to the mail hostname. Forward and reverse must match (FCrDNS).
- **Outbound TCP 25 open.** Many providers block it. Check before paying: `nc -vz gmail-smtp-in.l.google.com 25`. If blocked, ask the provider to
  unblock it, or pick another provider.
- **Inbound TCP 25 open** (bounces and, later, inbound mail).
- No other mail server on the host (Exim, Mailcow, a Postfix not written by Njiwa).
- Provider must allow editing PTR and must not be on a blocklist (check the IP at a blocklist lookup before use).

## 4. Ports, DNS, firewall, privileges

**Public (reachable from the internet):** 25/tcp (SMTP, bounces), 80/tcp (certbot HTTP-01), 443/tcp (HTTPS to the API/dashboard), 22/tcp (admin,
restrict to the owner's IP if possible).

**Outbound:** 25/tcp to the internet, 443/tcp and 53 (apt, Docker, ACME, Cloudflare API, webhooks), 123/udp (NTP).

**Never public** (the installer's nftables table `inet njiwa_mail` drops them): 2525 (inbound listener), 5432 (PostgreSQL), 8080 (API), 3000 (dashboard),
8891 (DKIM signer), 10587 (submission; allowed only from the Compose subnet, 172.30.0.0/24 by default).

**DNS records** (all on throwaway names; placeholders from HANDOFF):

| Record | Value |
|---|---|
| `mailout.example.com` A | server IPv4 |
| PTR (set at the provider) | `mailout.example.com` |
| `mail-api.example.com` A | server IPv4 (HTTPS, certbot) |
| Domain ownership TXT on `notify.example.com` | shown by the Domains page |
| `njiwa._domainkey.notify.example.com` TXT | `v=DKIM1; k=rsa; p=...` (printed by `installer apply`) |
| `notify.example.com` TXT (SPF) | `v=spf1 ip4:<server IP> -all` (the Domains page shows the exact value) |
| `_dmarc.notify.example.com` TXT | `v=DMARC1; p=none; rua=mailto:<owner mailbox>` (start with `p=none`) |
| `bounce.example.com` MX | `10 mailout.example.com` |

If the VPS also has an IPv6 address, decide first whether mail may leave over it, because the SPF record above lists IPv4 only (verify in preflight).

**What the installer changes on the VPS** (root): `/etc/postfix/main.cf`, `master.cf`, `/etc/postfix/njiwa/` (private CA and certificates),
`/etc/opendkim.conf`, `/etc/opendkim/` (tables and DKIM private keys), `/etc/njiwa-mail/` (firewall ruleset, submission client certificate),
`/etc/systemd/system/njiwa-mail-firewall.service`, nft table `inet njiwa_mail`, receipts under `/var/lib/njiwa-mail/installer`; it enables
and restarts `opendkim` and `postfix`. It refuses to replace a Postfix configuration it did not write. Packages installed by hand:
`postfix opendkim nftables docker.io docker-compose-v2 git` (plus nginx, certbot, swaks, nmap tooling). A system user `njiwa-mail` for the log collector.

**Credentials the owner provides** (never pasted into chat or committed): provider account for the VPS; DNS control for the test zone (a
Cloudflare token scoped to that one zone only, if the DNS wizard is being tested); Gmail and Outlook mailboxes for reading headers.

## 5. Safe procedure

Stop at each gate and wait for approval.

**Phase A: no cost, no network** (can start now). Owner reviews this file. Assistant prepares `docs/evidence/` templates and the C06 staging test.

**Phase B: provision (gate: approval to buy/create).** Create the VPS. Record provider, region, OS image. Confirm outbound 25 and PTR before
anything else. If port 25 is blocked, stop; do not work around it.

**Phase C: read-only checks.** `njiwa-mail preflight -hostname mailout.example.com`; every line `pass` or understood. Nothing installed beyond packages.

**Phase D: install in Sandbox mode** (HANDOFF 3.1 steps 1-6): packages, clone, build the binary from the repo, `.env` with generated secrets
(`ENCRYPTION_KEY`, `HMAC_KEY` backed up off host immediately), `docker compose --profile inbound up -d --build`, first admin. Check `/health/ready`.
No DNS changes yet. Then `installer plan` and read it.

**Phase E: DNS (gate: approval for each record set).** Publish only the records in section 4, on throwaway names. Write down every record
created, for cleanup. Use "Check now" until the Domains page passes.

**Phase F: apply and activate.** `installer apply ... -approve <digest> -record`. Check `systemctl status postfix@- opendkim njiwa-mail-firewall`,
`nft list table inet njiwa_mail`, `njiwa-mail installer status`. If apply fails it rolls back by itself. Run the connection test, review the switch plan, activate.

**Phase G: first Live message (gate: approval).** One message to the owner's Gmail, then Outlook. Save headers. Only then E06/E07/E10.

**Phase H: failure tests, carefully.** To simulate blocked egress use a *separate* nft table that drops only outbound tcp/25, started with an
automatic expiry, for example `systemd-run --on-active=20m nft delete table inet njiwa_block25`, so a mistake cannot persist. SSH is never touched.
Keep a provider console open before any reboot.

**Phase I: restore drill.** `BACKUP_QUEUE=1 scripts/backup.sh`, copy off host, restore on a second throwaway VPS with `scripts/restore.sh`, run e2e.
Run `nmap` from a third machine for the closed ports.

**Cleanup (gate: approval to destroy).**
1. `docker compose down -v`; stop `njiwa-mail-mta-log` and the queue agent.
2. `njiwa-mail installer rollback` (restores the files that apply changed from its backup; the receipt records the state). Remove the nft table
   (`nft delete table inet njiwa_mail`) and disable `njiwa-mail-firewall`. Confirm with `installer status`.
3. Delete every DNS record listed in phase E; remove the PTR.
4. Revoke the Cloudflare token, Live API keys and webhook endpoints created for the test.
5. Remove addresses from evidence files; commit the evidence.
6. Destroy the VPS (this removes packages and keys). If the VPS is kept, say so; do not leave a live open mail host unattended.
7. Check the IP and the domain in a blocklist lookup and note the result.

**Rollback at any point:** `installer apply` rolls itself back on failure; `installer rollback` restores configuration; DNS records are removed
from the list; destroying the VPS is the full reset. None of this involves `rida-server`.

## 6. Evidence needed to call each mode production-ready

**Managed delivery** is ready only when all of these are recorded in `docs/evidence/` and `STATUS.md`:
G05 headers from Gmail and Outlook (SPF, DKIM, DMARC pass, inbox vs spam noted); E06 (3+ providers, reboot and upgrade durability); E07 + F06 + D04
(real bounce, suppression, webhook, `correlated_by = verp`); E10 (queue controls, audit rows); E05 HELO matches PTR; E12 on all three distros
with the compatibility table; the closed-port `nmap` result and the managed e2e suite output from the host; the backup, restore and e2e result
on a second VPS; the systemd-path fixes committed; a rollback demonstrated; G09 notes from a second person.

**Existing SMTP** is ready only when recorded: G04(a) Mailcow on its own instance with and without the API key, and G04(b) a generic server, each with
headers showing the right SPF/DKIM alignment and the Domains page asking for exactly those records; C06 as a repeatable staging test; G06 both
directions plus the legacy-upgrade check (migration `0010`); connection test and sending facts screens verified; G09 notes.

**For both:** the readiness page agrees with the evidence (it must not say ready earlier), no message is described as "delivered" without a received copy,
`docs/RELEASE_REPORT.md` is updated, and the remaining human items (screen reader, Kiswahili review, external penetration test) are either done or listed
as open rather than hidden.

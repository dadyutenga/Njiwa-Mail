# Mailcow setup (operator task)

Njiwa-Mail never changes Mailcow or DNS by itself, never touches Mailcow's database, and never changes global Mailcow settings. This file lists every step the operator does on the Mailcow server, with the screen or command and how to undo it.

> **Check against your installed version first.** Menu paths below are from the current Mailcow UI (2024–2026 releases: *E-Mail → Configuration*, *System → Configuration*). Older releases put the same pages under *Configuration → Mail setup*. Do every step first with a test address. Results recorded in `docs/RELEASE_REPORT.md` say whether they were run against a real Mailcow or only locally.

Replace `example.com` with the real domain.

| Name | Meaning |
|---|---|
| `mail.example.com` | The Mailcow host (MX target, PTR, SMTP submission host). |
| `notify.example.com` | Transactional sending domain. |
| `news.example.com` | Bulk sending domain (announcements only). Optional. |
| `njiwa@notify.example.com` | Dedicated submission mailbox used by Njiwa-Mail. |
| `bounces@notify.example.com` | Envelope sender. DSNs come back to it. |
| `fbl@notify.example.com` | Feedback-loop (spam complaint) reports. |
| `in.example.com` | Receiving subdomain relayed to the Njiwa-Mail listener. |

---

## Part A — Outbound

### A1. Dedicated submission mailbox

| | |
|---|---|
| Do | *E-Mail → Configuration → Mailboxes → Add mailbox*: `njiwa@notify.example.com`, a generated password of 32+ characters, quota 100 MiB (it stores nothing). Do not use an admin or personal account. |
| Then | In Njiwa-Mail *Admin → Mail server*: host `mail.example.com`, port `587`, encryption STARTTLS, username `njiwa@notify.example.com`, the password, test sender `hello@notify.example.com`. Save. (Or set `SMTP_HOST`, `SMTP_USERNAME`, `SMTP_PASSWORD` in `.env`; the environment wins.) |
| Undo | Delete the mailbox in the same screen; clear the Mail server page or the `.env` values. Live mail then stays queued and expires. |

### A2. Allow the mailbox to send as the project's senders

| | |
|---|---|
| Do | *E-Mail → Configuration → Mailboxes →* edit `njiwa@…` → **Sender ACL** ("Allow to send as"): add every sender address you will add in Njiwa-Mail (e.g. `hello@notify.example.com`), the bounce address `bounces@notify.example.com`, and for bulk `news@news.example.com`. A whole domain can be allowed as `@notify.example.com`. |
| Check | *Mail server → Test connection*: step "Sender allowed" passes. Without the ACL Mailcow answers `553 Sender address rejected: not owned by user` and messages end `failed`. |
| Undo | Remove the entries from the Sender ACL. |

### A3. Sending domains and DKIM

| | |
|---|---|
| Do (API) | If the Mailcow API is configured on the Mail server page, open the domain in Njiwa-Mail → *Set up in Mailcow*. It adds the domain and a 2048-bit DKIM key with selector `dkim` (`POST /api/v1/add/domain`, `POST /api/v1/add/dkim`, `GET /api/v1/get/dkim/{domain}`). |
| Do (manual) | *E-Mail → Configuration → Domains → Add domain* `notify.example.com`. Then *System → Configuration → Options → ARC/DKIM keys*: domain `notify.example.com`, selector `dkim`, key length 2048 → Add. |
| DNS | Publish exactly the records shown on the Njiwa-Mail domain page (ownership TXT, SPF, DKIM, DMARC, MX for the bounce domain) and press *Check now*. See the *DNS setup guide* page. |
| Undo | Delete the DKIM key, then the domain (Mailcow refuses while mailboxes exist on it). Remove the DNS records. |

### A4. Rate limit

| | |
|---|---|
| Do | *E-Mail → Configuration → Mailboxes →* edit `njiwa@…` → **Ratelimit**: set it above the sum of the project send limits (Njiwa-Mail *Projects → Limits*) plus the bulk rate, e.g. `1000 / hour`. Record the value in `docs/OPERATIONS.md`. |
| Why | If Mailcow's limit is lower, submissions are refused with 4xx and wait for retry. |
| Undo | Set the rate limit back to the domain default (empty). |

### A5. Submission port and certificate

| | |
|---|---|
| Check | From the Njiwa-Mail host: `openssl s_client -starttls smtp -connect mail.example.com:587 -servername mail.example.com </dev/null | grep -E 'Verify return code|subject='` must show `Verify return code: 0 (ok)`. On the Mail server page all five checks pass. |
| Fix | Mailcow renews its certificate with ACME; make sure `mail.example.com` is in `MAILCOW_HOSTNAME`/`ADDITIONAL_SAN` in `mailcow.conf`, then `docker compose restart nginx-mailcow postfix-mailcow dovecot-mailcow` on the Mailcow host. |
| Undo | Nothing to undo; this is a check. |

---

## Part B — Inbound

The Njiwa-Mail listener has no TLS and no authentication. It must be reachable **only** from Mailcow's Postfix.

### B1. Put the listener on Mailcow's Docker network

| | |
|---|---|
| Do | `docker network ls \| grep mailcow` and `docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' $(docker ps -qf name=postfix-mailcow)`. In Njiwa-Mail `.env`: `INBOUND_DOMAIN=in.example.com`, `BOUNCE_ADDRESS=bounces@notify.example.com`, `MAILCOW_NETWORK=<network>`, `INBOUND_IPV4=<free address, default 172.22.1.200>`, `INBOUND_ALLOWED_SOURCES=<Postfix address>`. Start: `docker compose --profile inbound up -d --build`. |
| Then | Mail server page → *Inbound listener address*: `172.22.1.200:2525`. The status card shows "reachable: yes". |
| Never | Publish port 2525 on the host. The compose file publishes none; do not add one. |
| Undo | `docker compose stop inbound`; remove the `.env` values. |

### B2. Relay the receiving domain

| | |
|---|---|
| Do (API) | Domain page → *Set up in Mailcow* for a domain with purpose *Receiving*: adds it with *Relay all recipients* and the transport (`POST /api/v1/add/transport`, next hop `[listener]:2525`). |
| Do (manual) | *E-Mail → Configuration → Domains → Add domain* `in.example.com`, tick **Relay this domain** and **Relay all recipients**. Then *System → Configuration → Routing → Transport Maps → Add*: destination `in.example.com`, next hop `[172.22.1.200]:2525` (brackets stop an MX lookup), no username/password. |
| DNS | `in.example.com. MX 10 mail.example.com.` On Cloudflare `mail.example.com` must be **DNS only** (grey cloud). |
| Why relay all | The listener decides which addresses exist and rejects the rest at `RCPT TO`, so Mailcow bounces unknown addresses. |
| Undo | Delete the transport map, then the domain, then the MX record. Mail to the domain then bounces; outbound is unaffected. |

### B3. Route bounces and complaints

| | |
|---|---|
| Do | *E-Mail → Configuration → Aliases → Add alias*: `bounces@notify.example.com` → `bounces@in.example.com`, and `fbl@notify.example.com` → `fbl@in.example.com`. The listener treats both local parts on the inbound domain as reports; do not create mailboxes called `bounces` or `fbl` in Njiwa-Mail. |
| Then | Register `fbl@notify.example.com` with the feedback loops you can join (e.g. Microsoft JMRP/SNDS, Yahoo CFL). Gmail has no per-message FBL. |
| Undo | Delete the two aliases. |

---

## Part C — Checks before real traffic

| # | Check | How | Pass |
|---|---|---|---|
| C1 | Reverse DNS | `dig -x <server IP> +short` | equals `mail.example.com.` (set at the hosting provider) |
| C2 | Outbound port 25 | `nc -vz gmail-smtp-in.l.google.com 25` from the Mailcow host | connects (many VPS providers block 25 until asked) |
| C3 | Authentication | Mail server → *Send test email* to a Gmail and an Outlook inbox; open *Show original* / message source | `spf=pass`, `dkim=pass`, `dmarc=pass` |
| C4 | Inbound | Create mailbox `help@in.example.com`; send from an outside account | appears in *Inbox* within seconds, with spam score |
| C5 | Unknown address | Send to `nobody@in.example.com` | sender gets a bounce from Mailcow; nothing stored |
| C6 | Bounce | Send live mail to a non-existent address on a domain you control | *Complaints → Hard bounces* row, recipient under *Suppressions* with cause "hard bounce" |
| C7 | Listener exposure | From another machine: `nc -vz <public IP> 2525` | fails |

Record each result, with date and the Mailcow version (`docker compose exec php-fpm-mailcow cat /web/inc/app_info.inc.php | grep version` or *System → Information*), in `docs/RELEASE_REPORT.md`.

## Mailcow API

Create a **read-write** key at *System → Configuration → Access → API*, restricted to the Njiwa-Mail server IP, and enter URL and key on the Mail server page (stored encrypted, never shown again). The client uses only:

| Call | Used for |
|---|---|
| `GET /api/v1/get/status/version` | connection test |
| `POST /api/v1/add/domain` | add a domain (`relay_all_recipients=1` for receiving-only) |
| `POST /api/v1/add/dkim` | create a 2048-bit key, selector from `DKIM_SELECTOR` |
| `GET /api/v1/get/dkim/{domain}` | read `dkim_txt` for the DKIM record |
| `POST /api/v1/add/transport` | relay a receiving domain to the listener |

These match Mailcow's published `data/web/api/openapi.yaml`; confirm them at `https://mail.example.com/api` for your version. To undo: delete the API key in Mailcow and clear it on the Mail server page.

## If relay routing cannot be configured

Set `IMAP_HOST`, `IMAP_USERNAME`, `IMAP_PASSWORD` (port 993, mailbox `INBOX`) and run the `inbound` role: it polls unseen mail once a minute. Do not expose the SMTP listener publicly as a workaround.

# Mailcow setup for inbound mail and bounces (operator task)

Ditronics Mail never changes Mailcow or DNS itself. These are the steps for the operator.

> **Not yet verified against your Mailcow.** These steps were written from Mailcow's documented behaviour, not run on the production server. Menu names and container addresses differ between Mailcow releases. Check each step against the installed version before changing anything, and do it first with a test address.

Replace `example.com` with the real domain.

| Name | Meaning |
|---|---|
| `notify.example.com` | Sending domain. A normal Mailcow domain (DKIM is signed here). |
| `bounces@notify.example.com` | Envelope sender. DSNs come back to it. |
| `in.example.com` | Inbound subdomain. Every address is relayed to the Go listener. |
| `mail.example.com` | The Mailcow host (MX target, PTR). |

## 1. Network path from Mailcow to the listener

The listener has no TLS and no authentication. It must be reachable **only** from Mailcow's Postfix.

1. Find Mailcow's Docker network and Postfix address:
   ```
   docker network ls | grep mailcow
   docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' $(docker ps -qf name=postfix-mailcow)
   ```
2. Pick a free address in that network for the listener (the compose default is `172.22.1.200`; confirm nothing else uses it).
3. In `.env`:
   ```
   INBOUND_DOMAIN=in.example.com
   BOUNCE_ADDRESS=bounces@notify.example.com
   MAILCOW_NETWORK=<network name from step 1>
   INBOUND_IPV4=<address from step 2>
   INBOUND_ALLOWED_SOURCES=<Postfix address from step 1>
   ```
4. Start it: `docker compose --profile inbound up -d --build`
5. Check: `docker compose exec inbound ditronics-mail role-health -role inbound`

The `inbound` service publishes no host port. Do not add one.

## 2. Relay `in.example.com` to the listener

1. Mailcow UI → E-Mail → Configuration → Domains → Add domain: `in.example.com`.
   Enable **Relay this domain** and **Relay all recipients**. (The Go listener decides which recipients exist and rejects the rest at `RCPT TO`, so Mailcow bounces unknown addresses.)
2. Mailcow UI → System → Configuration → Routing → Transport Maps → add:
   - Destination: `in.example.com`
   - Next hop: `[<INBOUND_IPV4>]:2525` (square brackets stop an MX lookup)
   - No username or password.
3. DNS: `in.example.com. MX 10 mail.example.com.` If DNS is on Cloudflare, `mail.example.com` must be **DNS only** (grey cloud); the proxy does not carry SMTP.

## 3. Route bounces to the listener

DSNs are addressed to `bounces@notify.example.com`, which is on a normal Mailcow domain. Send it on to the relayed domain with an alias:

1. Mailcow UI → E-Mail → Configuration → Aliases → Add alias:
   - Alias address: `bounces@notify.example.com`
   - Goto address: `bounces@in.example.com`
2. Nothing else is needed: the listener treats `bounces@in.example.com` as the bounce mailbox automatically (the local part of `BOUNCE_ADDRESS` on `INBOUND_DOMAIN`). Do **not** create an inbound address called `bounces` in the admin UI.
3. `notify.example.com` needs an MX record too (`MX 10 mail.example.com.`), otherwise remote servers cannot return DSNs.

## 4. Allow the submission mailbox to send as these addresses

The mailbox in `SMTP_USERNAME` must be allowed to use, as sender:

- every approved sender address (for example `security@notify.example.com`), and
- the bounce address `bounces@notify.example.com` (it is the SMTP `MAIL FROM`).

Mailcow UI → mailbox → Edit → **Sender ACL** / "Allow to send as". Without this Mailcow answers `553 Sender address rejected: not owned by user` and messages go to `failed`.

## 5. Test before any real traffic

1. Add an inbound address in the admin UI (Inbound addresses → `support`, project and environment of your choice).
2. From an outside mailbox send a message with an attachment to `support@in.example.com`. It should appear under Inbound within a few seconds, with a spam score.
3. Send to `nobody@in.example.com`. You should get a bounce from Mailcow, and nothing stored.
4. Send a live message through the API to an address that does not exist on a domain you control. Within a minute or two: a row under Bounces of type `hard`, the recipient under Suppressions with source `hard_bounce`, and an `email.bounced` webhook if one is subscribed.
5. On the Mailcow host confirm the listener is not reachable from outside: `nc -vz <public IP> 2525` from another machine must fail.

## If relay routing cannot be configured

The plan names IMAP polling of a catch-all mailbox as the fallback. **It is not implemented.** If step 2 or 3 is impossible on your Mailcow, stop and report it; do not expose the listener publicly as a workaround.

## Rollback

Remove the transport map and the alias, then `docker compose stop inbound`. Mail to `in.example.com` then bounces at Mailcow; outbound sending is unaffected. Messages already spooled stay in the `maildata` volume and are parsed when the service starts again.

# Rida server deployment

Deployed on 2026-11-16 to `rida-server` (`37.60.238.125`).

- Public URL: https://njiwamail.qarmalab.dev
- Source: working tree based on `748b08d476e97bbad0f9203afccf442b110f92ae`, including the subsequent DNS timeout, publishing, navigation, and delivery-choice fixes.
- Application files: `/opt/njiwa-mail`.
- Compose files: `compose.yaml` and `compose.production.yaml` in that directory.
- API: `127.0.0.1:18080`; dashboard: `127.0.0.1:18300`.
- Persistent volumes: `njiwa-mail_pgdata` and `njiwa-mail_maildata`.
- Database and application secrets: server-only `.env`, mode `0600`; preserve this file and back up the volumes together.
- HTTPS: Nginx virtual host `/etc/nginx/sites-available/njiwamail.qarmalab.dev`, with a Let's Encrypt certificate and automatic renewal. The domain is proxied through Cloudflare.
- Host discovery: `njiwa-mail-host-agent.service`, using a token-authenticated Unix socket mounted into the API container. The service has a read-only host filesystem and does not start or install Postfix.
- Test operator: `test@qarmalab.dev`; workspace: `dadi`. The generated password is provided separately, outside the repository.

The scheduler needs a 256 MiB limit: its health check starts a second Go process, and the former 128 MiB limit caused OOM restarts on this host.

## Operations

```bash
ssh rida-server
cd /opt/njiwa-mail
docker compose -f compose.yaml -f compose.production.yaml ps
docker compose -f compose.yaml -f compose.production.yaml logs --tail 100 api worker scheduler
curl -fsS http://127.0.0.1:18080/health/ready
systemctl status njiwa-mail-host-agent
```

Container restart policies and the enabled host-agent service keep the deployment running after reboot. The server source was uploaded from the local working tree; it is not a Git checkout.

## Validation and scope

Verified HTTPS, public browser login, workspace access, and a dashboard-triggered scan that detected the server's existing Mailcow installation. API, PostgreSQL, worker, and scheduler health are checked separately.

This is a fresh application database. Local development data and DNS-provider credentials were not copied. Live SMTP delivery is not configured; connect the existing Mailcow service through Delivery infrastructure when preparing live mail. Existing applications and Mailcow configuration were preserved.

## SMTP onboarding update — 2026-11-17

Uploaded the current working tree and rebuilt the API/worker/scheduler image and
frontend. Applied the Njiwa compose services with health checks; PostgreSQL and
server-only environment/secrets were preserved. Rollback source/database backup
and previous images: `/opt/njiwa-mail-backups/20261117-121246`, image tags
`njiwa-mail:rollback-20261117-121246` and
`njiwa-mail-frontend:rollback-20261117-121246`.

Verified public HTTPS login, authenticated settings and installation endpoints,
the new guided SMTP frontend bundle, and authenticated connect API validation.
API, worker, scheduler and PostgreSQL are healthy. Production SMTP delivery was
not exercised. Mailcow Postfix, Dovecot and Nginx were not restarted or changed;
the certificate repair in `mailcow-tls-repair.md` remains pending explicit approval.

## Local Mailcow DNS facts — 2026-11-17

Deployed the read-only public-facts importer and exporter. The API mounts
`/opt/njiwa-mail/public-mailcow-facts` read-only, with
`LOCAL_MAILCOW_FACTS_FILE=/run/mailcow-public/facts.json`. The
`njiwa-mailcow-dns-facts.timer` refreshes the export hourly. Verified direct
Mailcow egress from inside its Postfix container: IPv4 `37.60.238.125`, IPv6
`2a02:c207:2332:9248::1`; relayhost and sender-dependent relay maps are empty.

Live Cloudflare re-analysis now proposes concrete SPF and MX creates, preserving
existing ownership and DMARC. DKIM remains manual because the signing server has
no key for `notify.ditronics.co.tz`. No DNS records were applied, and no Mailcow
domain/key was created: explicit approval for this production Mailcow change is
pending. Njiwa services remain healthy. Rollback image:
`njiwa-mail:before-dns-facts`; configuration backups are root-only under
`/opt/njiwa-mail-backups/dns-facts-20261117`.

## notify.ditronics.co.tz DKIM repaired — 2026-11-17

Following the request to fix the missing DKIM record, used Mailcow's installed
management functions to add only the missing sending domain and generate a
2048-bit RSA key with selector `dkim`. Preserved the existing parent-domain key
and verified that the new public key matches its private key. Did not create
mailboxes, change relay transports, enable management API credentials, rotate
existing keys, or restart any Mailcow service. A root-only SQL backup and the
idempotent provisioning script are stored on the VPS under
`/opt/njiwa-mail-backups/dkim-notify-20261117`.

Refreshed the public-facts exporter, re-analyzed through Njiwa, and reviewed a
plan containing exactly one create: the missing DKIM TXT. Approved and applied
that exact plan through Njiwa's existing Cloudflare workflow. Existing ownership,
SPF, inherited DMARC and MX records were unchanged. Public DNS and the Njiwa
verification endpoint subsequently verified all five records. The domain view
now reports DKIM pass with the actual public key. This validates DNS publication;
a received test message is still needed to prove end-to-end signing/delivery.

## OTP sender authorization repaired — 2026-11-17

The first Live Compose attempts failed before DATA with SMTP 553: the SMTP login
`njiwa@ditronics.co.tz` did not own the From address
`otp@notify.ditronics.co.tz`. Reproduced with Njiwa's connection test, which passed
DNS/TCP/TLS/AUTH and failed only the sender envelope check.

Backed up Mailcow's sender ACL table under the root-only VPS path
`/opt/njiwa-mail-backups/otp-sender-20261117/sender-acl.sql`. Added exactly one
native Mailcow send-as permission from that mailbox to that exact OTP address;
no wildcard or domain-wide permission. Confirmed both the mailbox and sending
domain were active. Did not create aliases, disable sender ownership checks or
restart services. Re-ran Njiwa's envelope test using the OTP sender: all five
checks passed without sending DATA. Saved the tested OTP identity as Njiwa's
connection-test sender. Previously failed messages were not retried automatically.

## One Live test completes project onboarding — 2026-11-17

The project checklist now requires one Live message accepted by the SMTP server
(`submitted`). A Sandbox capture is optional. Queued/failed messages do not
complete the step, and another project's history cannot complete it. Updated
the dashboard guidance to match. Confirmed the dadi project already has one
submitted Live message, so no additional email needs to be sent. Backend
regression tests cover Live-only completion, queued state and project isolation.

## Public developer documentation published — 2026-11-17

The docs site (`apps/docs`, Fumadocs static export) is public under the app domain:
`https://njiwamail.qarmalab.dev/docs`, plus `/llms.txt`, `/llms-full.txt`,
`/openapi.{yaml,json}`, `/skills/…`, `/versions.json`, `/api/search` and every page
as `.md`. No DNS or certificate change was needed.

- Container: `docker compose --env-file .env.docs -p njiwa-mail-docs -f compose.docs.yaml up -d`
  (image `njiwa-mail-docs:748b08d-wip`, `127.0.0.1:18400`, read-only, no secrets).
  `.env.docs` holds `NJIWA_DOCS_PORT=18400`, `DOCS_BASE_URL`, `DOCS_COMMIT`.
- Host nginx: one regex `location` for the docs paths in the njiwamail.qarmalab.dev
  vhost, proxied to 18400. Backup: `/opt/njiwa-mail-backups/nginx-njiwamail.qarmalab.dev.before-docs`.
- Dashboard: `VITE_DOCS_URL=https://njiwamail.qarmalab.dev` in `.env` (backup
  `env.before-docs-url`), so Developers › Open the documentation links to it.
- Checks: `SHARED_ORIGIN=1 sh apps/docs/scripts/smoke.sh https://njiwamail.qarmalab.dev`
  passed; the container itself passed the strict (API isolation) smoke on 127.0.0.1:18400.

Rollback: restore the nginx backup over `/etc/nginx/sites-available/njiwamail.qarmalab.dev`,
`nginx -t && systemctl reload nginx`, then `docker compose -p njiwa-mail-docs -f compose.docs.yaml down`.
Dashboard link: restore `env.before-docs-url` and retag `njiwa-mail-frontend:before-docs-link`.

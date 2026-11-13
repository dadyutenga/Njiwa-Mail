# Installing Njiwa-Mail

Njiwa-Mail installs on a **new or an existing server**. A dedicated or freshly installed server is **not** required. On a server that
already runs a mail service (Mailcow, Postfix, Exim and so on), Njiwa-Mail **reuses** that service over SMTP and leaves it untouched.

Both delivery modes give you the same dashboard, REST API, domains workflow, templates, API keys, queue, analytics and monitoring.
Only how mail leaves the server differs:

| Mode | What it does | Use it when |
|---|---|---|
| **Use my existing mail server** (`existing_smtp`) | Njiwa-Mail submits mail over authenticated SMTP to a server you already have (Mailcow, Postfix, Exim, Microsoft 365, any provider). Nothing on that server is installed or changed. | You already have a mail server or provider, your host blocks port 25, or the server runs other mail software. |
| **Send from my VPS** (`managed_mta`) | Njiwa-Mail's installer sets up Postfix and DKIM signing on this host, after you review and approve the exact changes. | The host has no mail service of its own, a public IP with reverse DNS (PTR), and outbound port 25. |

---

## 1. What you need

### Both modes

- A Linux server with Docker Engine and the Compose plugin; about 1 GiB of memory and 2 GiB of disk for Njiwa-Mail itself.
- A domain you can add DNS records to (by hand, or through a supported DNS provider after you approve the changes).
- HTTPS in front of the dashboard and API. Reuse the reverse proxy you already have (nginx, Caddy, Traefik, Apache): add the locations
  from `deploy/nginx.conf`. Njiwa-Mail never replaces your proxy or its certificates.

### Existing mail server mode, in addition

- The server's SMTP host name (the one on its TLS certificate), port (587 STARTTLS or 465 TLS) and a login allowed to send as your
  domain. Mailcow: create a mailbox for Njiwa-Mail. The Mailcow API key is optional.
- Nothing else: no public IP, PTR, port 25 or empty host is needed.

### Managed mode, in addition

- One of these, with systemd, run as root for the installer:
  - Debian 12; Ubuntu 22.04, 24.04 or 26.04 (Ubuntu 26.04 is checked on every change by CI);
  - AlmaLinux, Rocky Linux or RHEL 9 or 10, or Fedora 41 or later. AlmaLinux 9 and 10, Rocky Linux 9 and the current Fedora are
    checked on every change by CI, with the distribution's own Postfix and OpenDKIM (see "RHEL family" below).
- A public IPv4 with a PTR record to your mail host name, and outbound port 25 (many providers block it; ask them).
- No other mail service on the host. If one exists, the installer refuses and changes nothing (see section 4).

### RHEL family (AlmaLinux, Rocky Linux, RHEL, Fedora)

The installer detects the distribution and adapts; the plan shows every difference before anything changes:

- **Packages** come from dnf. OpenDKIM is in EPEL on AlmaLinux, Rocky Linux and RHEL: the plan installs `epel-release` and, on
  version 9, enables the CodeReady Builder repository (`crb`), where OpenDKIM's libraries are. Each is its own line in the plan, is
  recorded in the receipt, and is undone by a failed apply or by `installer rollback -remove-packages`. A dnf dry run comes first;
  if it would remove any package or cannot resolve a dependency, nothing is installed.
- **RHEL itself:** EPEL and CodeReady Builder are subscription repositories, so enable them yourself first (the plan prints the
  commands) and run `plan` again.
- **Postfix 3.5** (version 9) gets `compatibility_level = 2`; Fedora and version 10 use LMDB tables (`postfix-lmdb` is installed when
  missing) because Berkeley DB was removed there.
- **SELinux** stays enforcing. The plan labels the submission port `smtp_port_t` (`semanage port`) and restores file labels with
  `restorecon`; rollback removes the port label.
- **firewalld** is never changed. If it runs, the plan warns that port 25 (bounce reports) must be opened by you:
  `firewall-cmd --permanent --add-service=smtp && firewall-cmd --reload`.
- `scripts/discover.sh` passes `--security-opt label=disable` so SELinux lets the read-only scan read the host.

SELinux enforcing and firewalld cannot run in CI containers; they are covered by unit tests and still need one run on a real
AlmaLinux or Rocky Linux server.

---

## 2. Install, step by step

The dashboard shows the same ten steps (Delivery infrastructure → "Set up Njiwa-Mail") with the state of each one. You can stop at
any point and continue later from any device, because every step is read from the server's real state.

1. **Install Njiwa-Mail.**

   ```bash
   git clone https://github.com/dadyutenga/Njiwa-Mail.git /opt/njiwa-mail && cd /opt/njiwa-mail
   ```

   ```bash
   cp .env.example .env
   ```

   Fill in `POSTGRES_PASSWORD`, `ENCRYPTION_KEY` (`openssl rand -base64 32`) and `HMAC_KEY` (`openssl rand -base64 48`) and back the
   two keys up. Then build the image (or pull a released one):

   ```bash
   docker build -t njiwa-mail:local .
   ```

2. **Detect what is already on the server** (read-only; changes nothing):

   ```bash
   sh scripts/discover.sh
   ```

   It reports any Mailcow or Compose stacks, Postfix, Exim, OpenDKIM and Dovecot (and whether they are in use), and which programs own
   ports 25, 465, 587, 993 and 995. It also lists reverse proxies, certificates, Docker networks, CPU, memory, disk and public addresses.
   Then it recommends a mode and explains anything that blocks managed mode. It reads only `MAILCOW_HOSTNAME` from Mailcow's
   configuration, never its passwords. If it reports a port or network conflict for Njiwa-Mail itself, set the variable it names in
   `.env` (`NJIWA_API_PORT`, `NJIWA_DASHBOARD_PORT`, `NJIWA_INBOUND_PORT` or `NJIWA_SUBNET`/`NJIWA_IP_RANGE`/`NJIWA_DB_IP`).

   Start the stack and create the first user:

   ```bash
   docker compose --profile inbound up -d
   ```

   ```bash
   docker compose exec api njiwa-mail user create -email you@example.com
   ```

   Store the scan for the dashboard:

   ```bash
   sh scripts/discover.sh -record
   ```

3. **Choose how mail is sent.** On the Delivery infrastructure page the recommended mode is marked. If managed mode is not possible
   on this host, the page says why.

4. **Connect or provision.**
   - *Existing mail server:* click "Use these settings" next to a server the scan found, or enter any SMTP server. Then enter the
     login, save, run "Test connection" and the test send. TLS is always verified. If the server uses a private CA, see
     `docs/MAILCOW_SETUP.md`.
   - *Managed:* on the host, as root:

     ```bash
     njiwa-mail preflight -hostname mail.example.com -ip 203.0.113.10
     ```

     ```bash
     njiwa-mail installer plan -hostname mail.example.com -domains notify.example.com -bounce-domain bounce.example.com -submission 172.30.0.1:10587
     ```

     Read the plan. It lists every file, the packages to install, the firewall table and the keys. Then apply exactly that plan:

     ```bash
     njiwa-mail installer apply -hostname mail.example.com -domains notify.example.com -bounce-domain bounce.example.com -submission 172.30.0.1:10587 -approve <digest> -record
     ```

     In managed mode set `INBOUND_ALLOWED_SOURCES=172.30.0.1` (the gateway of Njiwa's network, from which host Postfix relays
     bounces); the Installation page warns `INBOUND_SOURCES_WRONG` otherwise.
     Host commands reach the database at `postgres://mail:<POSTGRES_PASSWORD>@172.30.0.10:5432/mail` (`NJIWA_DB_IP`). Put that and
     the two keys from `.env` into `/etc/njiwa-mail/env` for `-record` and the `njiwa-mail-mta-log` unit.

5. **Add and verify a sending domain** (Domains page): publish the ownership TXT record.
6. **Publish SPF, DKIM and DMARC.** Do it by hand from the Domains page, or connect a DNS provider: you review the exact changes and
   approve them. Existing MX and SPF records are never changed without your approval (`TestDNSExistingMailAndSPFArePreserved`).
7. **Create a sender address** on the verified domain (Senders page).
8. **Create an API key** for your application (API keys page).
9. **Send a test email** from the dashboard. New projects accept at most 3 messages per minute and 10 per hour to the same recipient
   (abuse protection); raise them on the Projects page or with `njiwa-mail project quotas` for tests and demos. Check the received message's headers for `spf=pass`, `dkim=pass` and `dmarc=pass`.
   Njiwa-Mail reports that the mail server *accepted* the message; delivery is proven by the received mail.
10. **Open the dashboard.** Messages, delivery evidence, analytics and alerts are the same in both modes.

---

## 3. What the installer will never do

The managed installer and the setup flow are restricted by design. They never:

- stop, replace or reconfigure a mail service that is in use (Mailcow, a Postfix with mail in its queue, relay or virtual domains or
  remote deliveries in its log, Exim, sendmail, OpenSMTPD, Dovecot, or an OpenDKIM that signs for another server);
- take over a port another program owns (25, 465, 587, the submission port, the DKIM milter port);
- change MX records without your explicit approval;
- change your firewall rules. Managed mode adds its own table, `inet njiwa_mail`, with policy *accept*, listed in the plan you
  approve. Skip it with `-no-firewall`;
- remove Docker containers or volumes, or replace certificates that belong to other services;
- install a second mail server next to an existing one, or remove a package to make room (apt is checked first);
- give the web application the Docker socket or root on the host. The installer is a separate root-only command, and the web process
  only reads what it records.

Every infrastructure change has a preview (the plan and its digest), an audit trail (a receipt on the host and an `installer.apply`
audit record) and a recovery procedure (section 5).

---

## 4. When managed mode is blocked

`njiwa-mail installer plan` and the dashboard print each blocker with its code, what was found and the fix:

| Code | Meaning |
|---|---|
| `MAILCOW_PRESENT` | Mailcow runs here. Connect it as an existing mail server. |
| `POSTFIX_IN_USE` | Postfix here already handles mail. Connect it instead. |
| `OTHER_MTA_PRESENT` | Exim, sendmail or OpenSMTPD is installed. |
| `OPENDKIM_IN_USE` | OpenDKIM already signs for another server. |
| `MAILBOX_SERVER_PRESENT` | Dovecot runs here: this is a production mail server. |
| `MAIL_PORT_IN_USE` | Another program owns a mail port. |
| `INSTALLER_PORT_IN_USE` | The submission or milter port is taken; pass another `-submission` port or free it. |
| `HOST_OS_UNSUPPORTED`, `HOST_NO_SYSTEMD`, `HOST_MEMORY_LOW`, `HOST_DISK_LOW` | The host cannot run the managed service. |

A blocked plan exits with status 3, and `apply` refuses even with `-adopt` and an approved digest. No command runs and no file
changes. `-adopt` only replaces an **unused** default configuration (for example the one `apt install postfix` leaves), after
backing it up.

---

## 5. Retry, rollback and cleanup

- **Retry:** run `plan` and `apply` again. The installer is idempotent: existing keys and certificates are kept, unchanged files are
  not rewritten, and a failed apply has already restored every file it touched.
- **Roll back the last apply:** `njiwa-mail installer rollback -record` restores every file from the apply's backup. Add
  `-remove-packages` to also remove exactly the packages that apply installed.
- **Undo the firewall table only:** `systemctl disable --now njiwa-mail-firewall` (or `nft delete table inet njiwa_mail`).
- **Remove Njiwa-Mail itself:** `docker compose down`. Your data stays in the volumes. Delete them yourself with `docker compose
  down -v` only after a backup (`scripts/backup.sh`).
- **Upgrade:** `git pull`, `docker compose up -d --build`. When upgrading an installation made before the fixed database address
  (`NJIWA_DB_IP`, `NJIWA_IP_RANGE`), recreate the network once so those take effect: `docker compose down` (without `-v`: data stays),
  then `docker compose up -d`. Migrations are additive and keep every setting. Upgrades are tested by
  re-running migrations on a legacy snapshot.

---

## 6. Installation requirements vs release validation

What **you** need is section 1. What the **project** needs to validate a release is different, and is documented in
[`INFRA_VALIDATION.md`](INFRA_VALIDATION.md):

- Clean-system installer tests run in disposable QEMU/KVM virtual machines (`lab/qemu/`). They are never a prerequisite for anyone
  installing Njiwa-Mail.
- Existing-SMTP mode is validated on hosts that already run other services (an isolated Mailcow or Postfix), never on a production
  mail server.
- Only tests that need an independent public IP, PTR, outbound port 25 or delivery to real providers (Gmail, Outlook) use a real
  public VPS.

### Scenarios covered by automated tests

| Scenario | Test |
|---|---|
| Fresh Ubuntu host → managed recommended, installer plans and applies | `TestFreshHostRecommendsManaged`, `TestApplyNeedsTheApprovedDigestAndIsIdempotent` |
| Existing server with Mailcow → existing SMTP, Mailcow offered, installer blocked | `TestMailcowHostRecommendsExistingSMTP`, `TestProductionMailServerIsNeverReplaced/mailcow` |
| Existing server with other applications and a generic SMTP provider | `TestBusyHostWithoutMailServer`, `TestSetupJourneyResumesFromServerState` |
| Conflicting mail ports → managed safely blocked, host unchanged | `TestProductionPostfixBlocksManaged`, `TestUnknownProgramOnPort25`, `TestProductionMailServerIsNeverReplaced`; also run on a real host (below) |
| Existing installation upgraded without losing settings | `TestLegacyMailServerMigratesToActiveProfile`, `TestOneRoleMigrationPreservesAccountsAndSessions`, migration 0024 is additive |
| Failed setup resumed without duplicating services or DNS records | `TestFailedSetupResumesWithoutDuplicates`, `TestSetupJourneyResumesFromServerState`, `TestDNSLostResponseDoesNotCreateADuplicate`, `TestDNSPartialFailureThenRecovery`, `TestDNSTwoAppliesCannotRunTogether` |

**Real-host check (development container, 2026-11-13).** A program listening on port 25 made `installer plan` print
`BLOCKED [MAIL_PORT_IN_USE] Port 25 is already used by python3` and exit 3. An `apply` with that plan's digest and `-adopt` was
refused. `/etc/postfix/main.cf` and `/etc/opendkim.conf` stayed byte-identical and no receipt was written.

### Limits

- `scripts/discover.sh` was not run here, because the development container has no Docker daemon. The scan itself (`njiwa-mail
  discover`) was run on the real host. Run the script once on a real server and record the result.
- Without root, discovery cannot see which program owns each port and says so. Run it as root (the script does).
- Discovery cannot see a mail service that runs on another machine. It recognises Mailcow by its directory or its containers.

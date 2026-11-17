# Connect an existing mailbox

Open Delivery infrastructure → Connect Existing Mail Server. Choose Mailcow or
Generic SMTP, enter the SMTP hostname, full mailbox username and password, and
press Connect and test. Defaults are port 587 and STARTTLS. Advanced connection
settings allow implicit TLS (usually 465), another port, and an authorized sender
when it differs from the login. Certificate verification is always enabled.

The API tests DNS, TCP, TLS certificate trust/hostname/validity, authentication,
and the sender envelope in order. Later checks are skipped after a failure.
The envelope check uses MAIL FROM and RCPT TO the mailbox, followed by RSET;
it sends no message and cannot guarantee final delivery or acceptance after DATA.
Successful candidates are saved encrypted and select existing SMTP mode. Failed
candidates leave saved settings untouched. Existing advanced/inbound settings are
retained. Environment-controlled SMTP and active managed transports require their
existing operator configuration/switch workflow.

After connecting, enter a test recipient. A submitted result means SMTP accepted
the message; check the inbox/spam folder for actual delivery. This direct setup
probe records installation readiness. The existing queued transport test remains
in Advanced Settings for testing the normal API/worker/tracking path. Public Live
sending still uses existing sender/domain authorization rules.

No listener, Docker network, bounce address, SPF range or DKIM selector is needed
to connect an existing mailbox. DNS deliverability remains the sending server's
responsibility. Discovery, profiles, managed infrastructure, Mailcow API helpers,
sending facts, queue testing and legacy forms are retained under Advanced Settings.

## Optional inbound and bounce processing

Prefer the existing IMAP ingestion service for Mailcow when a dedicated mailbox
can receive the required messages/reports. Configure the Njiwa inbound service:

```
IMAP_HOST=mail.example.com
IMAP_PORT=993
IMAP_USERNAME=njiwa-inbound@example.com
IMAP_PASSWORD=<secret supplied through deployment configuration>
IMAP_MAILBOX=INBOX
```

The poller uses verified TLS, polls unseen messages and marks successfully spooled
messages seen. Use a dedicated mailbox. Existing inbound routing and bounce
classification rules still apply: polling an arbitrary mailbox does not configure
return paths or make reports arrive there. The existing private SMTP ingestion
listener remains an alternative. Outbound setup does not change Mailcow Postfix
configuration or publish competing submission ports.

## API

`POST /admin/v1/mail-server/connect` (authenticated operator and CSRF protected):

```json
{"smtp_host":"mail.example.com","smtp_username":"mailbox@example.com","smtp_password":"secret","provider_type":"mailcow"}
```

Optional overrides: `smtp_port`, `smtp_tls_mode` (`starttls` or `implicit`),
`test_sender`. A blank password reuses the saved password only for the same host
and username. Responses contain `ok`, `saved` and five ordered `steps`; failed
steps include a stable `code` and actionable `detail`. Diagnostic failures return
200 with `ok:false,saved:false`; validation/configuration errors use 4xx.
Passwords are never returned. Existing PUT and test APIs remain supported.

## Automatic DNS facts for co-located Mailcow

For a Mailcow instance sharing the VPS, `deploy/mailcow/export-dns-facts.py`
exports public DKIM keys/selectors from Mailcow Redis, its Postfix hostname and
operator-verified public egress IPs. It does not change Mailcow or export private
keys/passwords. It refuses upstream relay configurations, hostname mismatches and
mixed DKIM selectors. Run it periodically through an operator-owned systemd timer.
Mount its output directory read-only into the Njiwa API and set
`LOCAL_MAILCOW_FACTS_FILE` to the JSON file path. No Docker socket or Mailcow
credentials are exposed to Njiwa.

Njiwa uses exports younger than 24 hours only when their SMTP hostname exactly
matches the active existing-SMTP profile. Explicit profile facts retain priority.
The records appear automatically in domain requirements and Cloudflare analysis;
the existing conflict checks, approval, apply, rollback and audit remain in force.
Re-analyze an old DNS plan after importing facts. A missing domain key stays
unpublishable: create that domain/key on the real signing Mailcow instance first;
Njiwa never invents a key or substitutes a parent domain's key.

## Prepare a missing signing key from the DNS wizard

The Cloudflare review groups missing values under Sending server setup needed.
For DKIM it offers Njiwa's sending-domain preparation action when the Mailcow
management API is connected, or a link to configure that one-time connection.
`POST /admin/v1/w/:project/:mode/domains/:id/prepare-sending` requires a sending
purpose and verified domain ownership. It adds the domain only if absent,
generates DKIM only if no key exists, and returns its public record. Provider
writes are audited. It never adds inbound transports or restarts services.
After preparation, re-analysis reads the actual key and creates the Cloudflare
plan for normal review/approval/apply. No placeholder is published.

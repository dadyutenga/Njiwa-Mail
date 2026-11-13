# One-click DNS setup (Cloudflare)

Journey: **Add domain → Connect Cloudflare → Select zone → Analyze → Review → Approve → Apply → Verify → Ready**.

This page is the design and the operator guide. What was tested and what still needs a real
Cloudflare account is listed at the end.

## Manual setup (any DNS provider, or when you do not connect Cloudflare)
Without a connected DNS provider Njiwa-Mail changes nothing in DNS. Publish these records, then use Domains → Verify in the operator UI or `POST /admin/v1/domains/{id}/verify`.

Replace `notify.example.com` with the sending domain and `203.0.113.10` with the Mailcow outbound IP.

1. TXT at `notify.example.com`: `njiwa-mail-verify=<token>` (the token is shown once when the domain is added).
2. TXT SPF: `v=spf1 ip4:203.0.113.10 -all`
3. TXT DKIM at `dkim._domainkey.notify.example.com`: the public key Mailcow generated for selector `dkim`.
4. TXT DMARC at `_dmarc.notify.example.com`: `v=DMARC1; p=none; rua=mailto:dmarc@example.com`
5. MX for the inbound domain `in.example.com`: `10 mail.example.com`
6. PTR for `203.0.113.10`: `mail.example.com`

Live sending from a domain that has been added stays blocked until the TXT challenge, SPF, DKIM and DMARC all pass. A domain that was never added is not blocked, so existing projects keep working until you register the domain.

Mail hostnames must be DNS-only at Cloudflare. The proxy does not carry SMTP.

The wizard's last step shows the same records, built from your actual delivery mode, with a copy button per record.

## Layering

| Layer | Package / file | Knows about Cloudflare? |
|---|---|---|
| Record requirements (what a domain needs) | `internal/service/domains.go` `expectedRecords` (existing) | No. Derived from the active delivery mode (managed MTA, Mailcow, generic SMTP + sending facts) |
| DNS provider interface | `internal/dnsprovider` (`Provider`, `Record`, `Zone`, typed errors) | No |
| Cloudflare adapter | `internal/dnsprovider/cloudflare.go` | Yes, the only place |
| Plan engine (pure function: requirements + existing zone → change set) | `internal/service/dnsplan.go` | No |
| Connections, OAuth, setups, apply, rollback, verify | `internal/service/dnsconn.go`, `dnssetup.go` | No (uses the interface) |
| HTTP | `internal/api/dnssetup.go` | No |
| Wizard | `frontend/src/features/workspace/pages/DnsSetup.tsx` | No |

A second provider means one new adapter and one line in `dnsprovider.New`.

## Data (migration 0022, additive)

* `dns_connections`: per project; `token_enc` / `refresh_enc` sealed with `secure.Box` (AAD binds the row id);
  `status` active | expired | revoked | disconnected | error. Tokens are never returned or logged.
* `dns_oauth_states`: single-use, hashed state, sealed PKCE verifier, bound to project + user, 10-minute life.
* `dns_setups`: one wizard run: zone, plan (JSON), plan digest, approval (who/when/acknowledged codes), status.
* `dns_changes`: append-only ledger, one row per record change: record id, previous value, new value,
  actor, time, outcome. Written **before** the provider call (as `pending`) and finished after it.
* Every write also goes to `audit_log`.

## Authentication

* **Scoped API token (works everywhere).** The user creates a token in Cloudflare with `Zone → DNS → Edit` and
  `Zone → Zone → Read`, restricted to the one zone. Njiwa-Mail proves it works (`/user/tokens/verify`; account-owned tokens, which
  that endpoint does not know, are proven by listing a zone), refuses one that is not active, and warns when the token can reach
  several zones (least privilege says to restrict it to one). Every operation re-checks that the chosen zone is visible to the
  token. Global API Keys and passwords are never accepted: the form takes a token only, and a 37-hex-character Global API Key
  shape is rejected by the adapter.
* **OAuth 2.0 authorization code + PKCE (S256).** Enabled only when the operator sets `CLOUDFLARE_OAUTH_CLIENT_ID`,
  `CLOUDFLARE_OAUTH_CLIENT_SECRET`, `CLOUDFLARE_OAUTH_AUTH_URL`, `CLOUDFLARE_OAUTH_TOKEN_URL` and `PUBLIC_URL`
  (the redirect URI is `PUBLIC_URL/admin/v1/dns/oauth/callback`, compared exactly). The endpoints come from operator
  configuration, never from a user; they must be https. State is random, stored hashed, single-use, expires in 10 minutes
  and is bound to the starting user and project; a replayed or foreign state is refused.
* **Domain Connect: not available.** It needs a template registered with the Domain Connect project and a signing key, which is an
  external step. The capability flag is always `false` and nothing else is built; use the API token or OAuth.
* Disconnect: tokens are wiped from the database (and revoked at the provider for OAuth). DNS records already
  published stay; the change history stays so the user can see what was added and remove it by hand.
* Rotation: replace the token; it is validated against the same zone before the old one is overwritten.

## Safety rules, as implemented

The plan engine never produces a delete. Items are:

| Item action | Meaning |
|---|---|
| `create` | the name/type does not exist yet |
| `unchanged` | an equivalent record already exists |
| `modify` | only a safe SPF merge: one existing SPF record gets the missing mechanism before its `all` term (existing TTL kept; needs extra confirmation) |
| `conflict` | something incompatible exists (other MX, different DKIM key at the selector, CNAME at the name, two SPF records, `redirect=`/`+all` SPF). Never changed; the user resolves it by hand and re-analyzes |
| `manual` | Njiwa-Mail cannot know the value (for example a generic SMTP server without declared sending addresses or DKIM key). Never guessed |

* MX: never replaced. Created only where none exists. An MX that points elsewhere is a conflict and a warning that existing
  mail would be disrupted.
* A/AAAA/CNAME: never created or modified. A proxied record that an MX target depends on is reported as Needs Action.
* DKIM: only the Njiwa selector; another selector is never touched; a different key at the same selector is a conflict.
* SPF: one record per name, merged only when safe, never `+all`, never a second record.
* DMARC: an existing policy is left exactly as it is and explained; one is created (`p=none`) only when none exists.
* DNSSEC, CAA, Email Routing: read and reported, never changed.
* TTL: created records use Cloudflare automatic TTL; modified records keep theirs. Created MX/TXT are `proxied:false`.
* The plan recommends a sending subdomain whenever the apex already carries mail.
* Existing infrastructure is recognised (Google Workspace, Microsoft 365, Zoho, Cloudflare Email Routing, Mailcow/custom)
  from MX and SPF content, and a warning explains the risk.

## Approval, apply, rollback

1. **Analyze** creates a plan and a digest (SHA-256 of the actions plus a fingerprint of every affected existing record).
2. **Approve** takes the digest and the list of acknowledgement codes the plan requires. A stale digest is refused.
3. **Apply** re-fetches the zone, rebuilds the plan and refuses (`409 dns_changed_concurrently`, status back to *needs review*)
   if the digest differs: someone changed DNS between approval and apply. Each change is recorded `pending` with the previous
   value, then done. A create first checks whether the record is already there (idempotent, retry-safe); an ambiguous failure
   is resolved by listing, not by repeating. Transient errors and rate limits retry with bounded exponential backoff and honour
   `Retry-After`. Updates are never retried blindly. Partial application leaves status **partial**, never *applied*.
4. **Rollback** (Njiwa-managed changes only): a created record is deleted only if it still exists and is unchanged; a modified
   SPF record is restored only if it still equals what Njiwa-Mail wrote. Anything changed by someone else is skipped and
   listed for manual recovery. Records Njiwa-Mail adopted (already present) are never removed.

## Verification

Applying is not readiness. After apply the existing checker (`CheckDomain`, the 10-minute background re-check, and the
bounded wizard polling) decides each record: **Pending** (applied but not visible yet, first 24 hours), **Verified**,
**Failed** (visible but wrong) or **Needs action**. The domain is *Ready* only when ownership, SPF, DKIM and DMARC pass, plus
(managed delivery) the PTR / forward-confirmed reverse DNS check of the sending IP. DKIM signing is a separate proof
(a received test message); DNS verification never claims inbox delivery.

## Who can use a connection

A connection belongs to one project and is never visible to another. Inside a project, access follows Njiwa-Mail's existing model:
every signed-in administrator can work in every project (there is no per-project membership), so every administrator of the
installation can use, rotate or disconnect that project's DNS connection. If that is too broad, restrict who has an admin
account; a per-project membership model would be a separate piece of work.

## Security

Backend authorisation on every route (the route matrix test covers all of them). Connections, setups and changes are always
filtered by project. A domain claimed by another project cannot be set up. Provider requests go only to the fixed
Cloudflare API host (or operator-configured OAuth endpoints over https), refuse redirects to other hosts, cap response
size and time out. Per-user and per-domain rate limits apply to analyze and apply. Zone names, record content and error
text are validated and escaped; provider error bodies are not echoed to clients.

## Tests

`internal/dnsprovider/cloudflare_test.go` (httptest Cloudflare), `internal/service/dnsplan_test.go` (plan rules),
`test/dns_setup_test.go` (full flows on PostgreSQL with an in-memory provider), frontend `DnsSetup.test.tsx`.

## Blockers (cannot be closed from this repository)

* A real Cloudflare account run: token verify, zone listing, record create/rollback against the live API.
* Cloudflare OAuth client registration (and Domain Connect template registration) with their programs.
* Real-host managed delivery checks (PTR, outbound port 25) remain the existing handoff items.

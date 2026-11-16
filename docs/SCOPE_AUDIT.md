# Scope audit: the implementation against the corrected product scope

Historical assessment. The current implementation assessment and corrective tasks are in [`SCOPE_AUDIT_CURRENT.md`](SCOPE_AUDIT_CURRENT.md).

Audited: branch `dev` at `e77dec2` (after PR #27). It does **not** include the open PRs #28 to #37.
Source of truth for the scope: the owner's "Product scope correction" statement, kept in [`PRODUCT_SCOPE.md`](PRODUCT_SCOPE.md).

**Method and limits.** Read the code, migrations, tests, CI workflow, docs and the lab scripts; ran targeted searches; checked the test
suite status that CI reports. Nothing was run against a real host, DNS zone, mail server or the owner's production machine. A status
below says what the repository *shows*; it is not a production-readiness claim. Where something was not re-read in this audit it says so.

Status words: **Implemented** (code and tests exist), **Partial** (the missing part is named), **Not verified** (built, never run in a real
environment), **Missing**, **Misdesigned** (works but contradicts the scope).

## 1. Verdict

- The core of the scope is **built**: two delivery modes behind one transport interface, one API and dashboard, a host-inspecting managed
  installer with a plan, approval digest, receipts and rollback, domain and DNS checks that evaluate SPF/DKIM/DMARC, a Cloudflare setup flow
  with preview, approval and rollback, a durable queue with idempotency and uncertain-outcome handling, encrypted secrets, a public OpenAPI
  that a test ties to the code, and agent-readable documentation.
- **Nothing is proven on real infrastructure.** Every real-host test in [`INFRA_VALIDATION.md`](INFRA_VALIDATION.md) (managed delivery to
  Gmail and Outlook, real bounce, queue durability across reboot, Mailcow and generic existing SMTP, mode switch, restore on a fresh host,
  a real Cloudflare run) is still open. Do not call either mode production-ready.
- **No decision found that makes Njiwa-Mail require Mailcow, an empty VPS or one SMTP implementation.** Mailcow is behind
  `provider.Provider` and used only when its API is configured; the managed installer refuses a host with another mail server; custom
  domain is not a mode. There are leftovers (section 3): Mailcow wording in generic existing-SMTP messages, stale statements in
  `REMAINING_WORK.md`, one host name and one client-app name in public-facing files.
- **A large amount of functionality outside the scope exists** (section 4): marketing contacts and campaigns, SSO, forms, live chat, an
  SMTP relay, daily report e-mail, announcements, and (in open PRs) more of the same. The scope says not to expand unrelated functionality at
  the expense of the main platform. The work on the main platform has not been blocked by it, but the open PRs #29, #30, #33 and #36 are
  such expansion and should be held unless you want them.

## 2. Requirement by requirement

### 2.1 Two delivery modes, one platform

| Requirement | Status | Evidence | Gap |
|---|---|---|---|
| Exactly two primary outbound modes | Implemented | `transport_profiles.mode CHECK IN ('managed_mta','existing_smtp')` (migration 0010); `discover.ModeManaged/ModeExisting`; no code path for a third mode (a search for "custom domain" as a mode found none) | none |
| Both modes behind one interface, API unaware of transport | Implemented | `internal/transport` (`Transport`, `StoredTransport` pins the profile revision); worker dispatch goes through it (C01: `TestWorkerUsesActiveProfileAndPinsMessages`, `TestUnknownOutcomeIsPinnedAndNotResent`); public OpenAPI names no transport except one sentence in its description | the response and states are identical, but the *evidence* differs (2.7) |
| Same dashboard, projects, environments, templates, domains, senders, keys, queue, tracking, webhooks, analytics in both modes | Implemented | one workspace router and service layer; mode switch plan with `migrate_pending` (G06 logic tests) | mode switch with real transports and a non-empty queue is **Not verified** (G06) |

### 2.2 Mode 1: Njiwa-managed delivery

| Requirement | Status | Evidence | Gap |
|---|---|---|---|
| Provisions Postfix and DKIM signing | Implemented | `internal/installer/managed` (plan, apply, receipts, rollback); Postfix + OpenDKIM; mTLS submission; nftables table; installs missing packages on Debian/Ubuntu (`apt`) and the RHEL family (`dnf`, EPEL/CRB as visible steps) (`plan.go` `MissingPackages`) | `REMAINING_WORK.md` still says the installer does not install packages: stale (CT-02) |
| Preflight, conflict detection, safe | Implemented | `preflight` checks: OS, systemd, disk, memory, clock, local IP, hostname A, PTR/FCrDNS, outbound 25, conflicts, private ports. `discover` problem codes: Mailcow present, other MTA, Postfix or OpenDKIM in use, mail port in use, installer port, mailbox server, reverse proxy, subnet conflict, public IP not on host, Docker missing; managed blockers "can never be overridden" | real-host results not recorded |
| Never overwrites or disrupts existing production mail | Implemented in code | refuses to replace a Postfix configuration it did not write; plan shown first, applied only with its digest; `installer apply` rolls itself back on failure; firewall table is a separate `inet njiwa_mail` table; firewalld is never changed | **Not verified** on a real host with another service (G05, E12) |
| DNS guidance, queue monitoring, security controls, rollback, diagnostics | Implemented | DNS requirements per transport; Mail queue page and host agent (E10); metrics and alert rules (F08); `installer rollback`; typed SMTP diagnostics (C03) | E10 never run on a real managed host |
| New domains get a DKIM key | Partial | after a domain is verified the dashboard shows the exact `installer plan/apply` commands (T7.2, Mail host page) | manual hand-off, no automatic key creation (CT-03) |
| Distros | Implemented in CI, Not verified on hosts | Debian 12, Ubuntu 22.04/24.04/26.04, AlmaLinux/Rocky/RHEL 9-10, Fedora 41+; CI jobs `managed-mta` and `managed-mta-rhel` (containers) | E12 clean-install matrix not run on real VMs |

### 2.3 Mode 2: existing mail server

| Requirement | Status | Evidence | Gap |
|---|---|---|---|
| Connects to any SMTP submission service (Mailcow, Postfix, Exim, other) | Implemented | `provider_type` `generic` or `mailcow`; connection test; sending facts (egress IPs, SPF include, DKIM key) per profile (C07); tested against a fake server and Mailpit in CI | real Mailcow and real generic server runs are open (G04, C06) |
| Does not install a competing MTA | Implemented | the installer is a separate command that is not part of this mode; the managed blockers refuse a host that runs a mail server | none |
| Mailcow API provisioning optional, never required | Implemented | all Mailcow calls go through `provider.Provider` and are audited (C05); `docs/MAILCOW_SETUP.md` shows manual steps first | wording leftovers (3) |
| Fresh VPS not required | Implemented | `INSTALL.md` section 1 and 6 separate installation from validation; compose ports bind to loopback and are configurable (`NJIWA_API_PORT`, `NJIWA_DASHBOARD_PORT`, `NJIWA_INBOUND_PORT`, `NJIWA_SUBNET`); discovery warns on port, subnet and proxy conflicts | the application itself needs Docker Compose and a fixed-purpose subnet (configurable) |

### 2.4 Domains, DNS, Cloudflare

| Requirement | Status | Evidence | Gap |
|---|---|---|---|
| Register and verify domains, ownership claims | Implemented | domain claims and transfer (D09), per-project verification | none |
| Transport-specific DNS records, SPF/DKIM/DMARC evaluated | Implemented | `internal/dnsauth` (SPF evaluation, DKIM key validation, DMARC syntax and alignment), D05 to D08; per-domain return-path domain (D04); staged DKIM rotation (D10) | |
| Sender identities | Implemented | senders with verification and approval | |
| Cloudflare: preview, approval, no destructive conflicts, verify, manual fallback | Implemented against fakes | `internal/dnsprovider` (Cloudflare and in-memory), `service/dnssetup.go`: `StartDNSSetup` analyses and returns a plan, `ApproveDNSSetup` needs the plan digest and acknowledgements, `ApplyDNSSetup`, `RollbackDNSSetup`, `ManualDNSRecords`; token and OAuth connection | conflict rules in `dnsplan.go` were not re-read here; **no real Cloudflare run, OAuth client not registered** (CT-10) |

### 2.5 Developer API and compatibility

| Requirement | Status | Evidence | Gap |
|---|---|---|---|
| Project-scoped keys, reliable submission | Implemented | scoped keys per environment, idempotency, sender approval, domain gate | |
| Documented contract preserved | Implemented | `test/public_openapi_test.go`: routes, scopes, states, error codes, schemas and examples are all checked against the server and Go types; webhook `schema_version` (F07) | no automated check that the current contract stays compatible with the last released one (CT-07) |
| Backward compatibility, explicit versioned migration | Partial | a written policy exists (`apps/docs/content/docs/versions.mdx`: a breaking change to `/v1` would be `/v2`, additive changes are not breaking; webhook `schema_version`); additive database migrations; `docs/RELEASE_REPORT.md` states the rollback boundary | the policy is not enforced by a test (CT-07) |

### 2.6 Reliable sending

Implemented: durable PostgreSQL queue and worker, retry with backoff, idempotency keys, uncertain-outcome handling and the unknown queue
(F02), suppression list, bounce parsing with signed VERP return path (F06), accurate states ("submitted" is never "delivered"), queue
backpressure and a circuit breaker for transport outages (F03). Tests are named in `docs/planning/STATUS.md`. **Not verified:** a real bounce
end to end, durability across a reboot or `apt upgrade postfix` (E06, E07).

### 2.7 Delivery tracking is the same shape but not the same depth

Managed mode records MTA queue ids and `remote_accepted`/`deferred` evidence from the Postfix log (`mta-log` collector, migration 0013,
source `mta_log`). Existing-SMTP mode records the SMTP reply and the queue id in it (`queued as ...`), and bounces only if the operator
relays them to the inbound listener or an IMAP mailbox (C08 readiness step). That is an honest limit of the mode, but "common delivery
tracking" is only true up to `submitted`. Candidate (CT-04): an opt-in, read-only evidence adapter for a generic Postfix host.

### 2.8 Security

| Requirement | Status | Evidence | Gap |
|---|---|---|---|
| Encrypt stored secrets | Implemented | `secure.Box` (AES-GCM with AAD), key rotation `rekey` guarded by `TestEverySealedColumnIsRekeyed`; inbound attachments are sealed at rest | |
| Authorization boundaries | Partial | installation-operator permission (F09), optional TOTP, security regression matrix over every route (F05), SSRF guards | **every signed-in user can open every project** until PR #28 (project membership and roles) is merged |
| Prevent unauthorized sender or domain use | Implemented | domain claims, sender approval, Live domain gate | |
| No open relay | Implemented in code, Not verified | managed port 25 is configured to refuse relaying and submission needs a client certificate (G05 acceptance lists the real-host check); the optional SMTP relay requires `AUTH` over TLS (`internal/relay`) | real-host `554 5.7.1` check open; the SMTP relay is extra attack surface outside the core (CT-09) |
| Audit | Implemented | audit log on every write, provider writes audited | |
| Protect production infrastructure | Implemented in code | see 2.2 | |

### 2.9 Documentation

Implemented: documentation portal (`apps/docs/`), generated agent-readable Markdown for every page, `llms.txt`, `llms-full.txt`,
`openapi.yaml/json`, `skills/`, `versions.json`, all from one source with one version and commit; content tests keep docs equal to the
code; SDK material: Go client (`client/`, `njiwamail/`), TypeScript client (`clients/typescript`), curl/Go/Node/Python examples
(`docs/EXAMPLES.md`), troubleshooting by error code. A lint rule forbids the Ditronics name in public pages. **Not done:** the portal is
not published; no SDK is published to a package registry (not claimed anywhere).

### 2.10 Operations

| Requirement | Status | Evidence | Gap |
|---|---|---|---|
| Install | Implemented | `INSTALL.md` | non-author install (G09) not done |
| Upgrade | Partial | `INSTALL.md` has two lines; `RELEASE_REPORT.md` has a deploy/upgrade/rollback run of the container images | no dedicated upgrade runbook (CT-06) |
| Monitoring and health | Implemented | `/health/ready` with honest readiness, metrics, Prometheus rules, alerts | |
| Backup, restore, disaster recovery | Implemented in lab | `scripts/backup.sh`, `restore.sh`, restore drill in the QEMU lab (`t05`) | restore on a real second VPS open (G03) |
| Safe rollback | Implemented | installer rollback, DNS setup rollback, application rollback boundary documented | rollback of a real managed host not demonstrated |

### 2.11 Testing honesty

Good: `INFRA_VALIDATION.md` separates "what a user needs to install" from "what the project needs to validate a release", lists which
tests need a public VPS and which do not, and says nothing in it has been run. `lab/qemu` carries a header "SIMULATED networking only:
results are NOT production evidence". CI runs the managed installer in containers and on hosted runners, existing-SMTP tests against
fakes and Mailpit, a container smoke test, govulncheck, an SBOM and a reproducible-build check. **Not done:** all rows of the
real-infrastructure table. `REMAINING_WORK.md` and `docs/planning/STATUS.md` agree on that.

### 2.12 Definition of done, walked through

| Step | Status |
|---|---|
| Install on a compatible server | Implemented, not independently verified (G09) |
| Connect an existing SMTP server | Implemented; real Mailcow and generic runs open |
| Or safely provision managed delivery | Implemented; real-host run open (G05) |
| Register a domain, configure DNS (manual or Cloudflare) | Implemented; real Cloudflare run open |
| Generate an API key, send, inspect status | Implemented |
| Manage everything in the same interface | Implemented |
| API caller does not need to know the transport | Implemented for sending and states; evidence depth differs (2.7) |
| A production mail service is not disrupted | Implemented in code; not verified on a real host with other services |

## 3. Decisions that tie the product to Mailcow, a clean VPS or one SMTP implementation

None is blocking. All are cheap to fix and are listed so they are not forgotten.

| Where | What | Severity |
|---|---|---|
| `internal/service/dns.go:259`, `:319` | In existing-SMTP mode the DNS check text says "MX pointing at the Mailcow host" and "inbound mail is relayed by Mailcow" for any server | low, misleading for Postfix/Exim users |
| `internal/service/dns.go:32`, `config.go:68,86`, `inbound/imap.go`, `service/setup.go:59` | comments and operator-facing wording assume Mailcow as the typical server (`OUTBOUND_IP`, `INBOUND_ALLOWED_SOURCES`, IMAP fallback) | low |
| `compose.mailcow.yaml` in the repo root | an optional override that joins Mailcow's Docker network | none (documented as optional); consider `deploy/` |
| `docs/INFRA_VALIDATION.md`, `docs/planning/HANDOFF.md`, `docs/planning/task.md`, `RELEASE_REPORT.md`, `public-portal/DEPLOY.md` | the owner's production host name `rida-server` appears in tracked files of an open-source repository | medium for privacy; keep only in a private note |
| `apps/dashboard/.../Layout.tsx:110` | the "new project" placeholder is `sacas`, a client application's name | low; the scope says the product is not application-specific |
| `docs/REMAINING_WORK.md:39` and the "installer does not install packages" row | statements that code no longer matches | low, but it is the document a reader trusts for "what is not done" |
| managed installer | Debian/Ubuntu/RHEL family with systemd and Postfix + OpenDKIM only | by design (scope names Postfix and DKIM signing) |
| application stack | Docker Compose, fixed-purpose private subnet `172.30.0.0/24` | by design, configurable, conflict-checked by discovery |

## 4. Functionality outside the core scope

On `dev` today (already merged by the owner): contacts, lists, segments and campaigns with unsubscribe and tracking; OpenID Connect
sign-in; website forms and live chat (both **off** unless `FORMS_ENABLED` / `CHAT_ENABLED`); an SMTP relay for applications; daily report
e-mail; announcements and notifications; shared inbox, mailboxes and automations for inbound mail. None of them is required to send
transactional mail. I did not find an on/off switch for contacts and campaigns or SSO; that is worth verifying (CT-08).

Open pull requests, with a recommendation (yours to decide; nothing is merged or closed by me):

| PR | What | Relation to the scope | Recommendation |
|---|---|---|---|
| #35 | fix a timing race in a chat test | CI health | merge |
| #37 | CI test timeout 280 s to 600 s | CI health | merge |
| #28 | project members and roles | **authorization boundaries** (2.8) | merge, it closes a real gap |
| #31 | templates: any language, stylesheets, import/export | template engine is core | merge after review |
| #32 | account lifecycle (password, sessions, deletion) | security hygiene | merge after review |
| #34 | platform page: jobs, live health, opt-in update check | monitoring and operations | merge after review |
| #33 | bulk recipient delete with dry run | privacy tooling | optional |
| #36 | S3-compatible attachment storage | extra infrastructure, off by default | hold |
| #29 | live chat extras | outside the scope | hold |
| #30 | form routing rules, tags, export | outside the scope | hold |

## 5. Corrective engineering tasks

Each task keeps working functionality and existing data; none touches production mail, firewall or DNS. "Needs approval" marks anything that
needs the owner's yes at that moment (a paid VPS, a real DNS change, a real Live message).

**CT-01 Neutral wording for existing-SMTP mode.**
Files: `internal/service/dns.go`, `internal/config/config.go`, `internal/service/setup.go`, `internal/inbound/imap.go`,
`docs/MAILCOW_SETUP.md` (keep), `docs/INSTALL.md`. Migrations: none. Dependencies: none.
Acceptance: a test builds the DNS requirements for a `generic` profile and fails if any message contains "Mailcow"; the Mailcow profile
keeps its wording.

**CT-02 Documentation truth pass and de-identification.**
Files: `docs/REMAINING_WORK.md`, `docs/INFRA_VALIDATION.md`, `docs/planning/HANDOFF.md`, `docs/planning/task.md`, `docs/RELEASE_REPORT.md`,
`docs/public-portal/DEPLOY.md`, `apps/dashboard/src/features/workspace/Layout.tsx`. Migrations: none.
Acceptance: extend the guard in `test/docs_content_test.go` (today it forbids "ditronics" and "njiwa.co.tz" in public docs) with the
production host name and the client app name; a test that `REMAINING_WORK.md` does not contradict `installer plan` (package installation).

**CT-03 Managed mode: DKIM key for a newly verified domain without re-running the installer by hand.**
Design decision needed first: the key must be created on the host, so it needs the existing queue/host agent or a second, narrowly
scoped agent command; the application must never get root. Files: `internal/service/managed.go`, `internal/installer/managed`,
`internal/mtaagent`, `deploy/njiwa-mail-mta-agent.service`. Migrations: possibly a `domain_dkim_keys` state column (additive).
Acceptance: lab test (extends `lab/qemu/tests/t06-managed-e2e.sh`): verify a second domain, trigger the hand-off, DKIM signs for it, rollback
removes the key; `installer plan` still shows the same change before it is applied. Real-host check with E12.

**CT-04 Existing-SMTP delivery evidence (opt-in, read-only).**
Reuse the `mta-log` parser for a generic Postfix host the operator already runs, shipped as a separate binary or flag that only reads a
log file. Files: `internal/mtalog` (or equivalent), `internal/service/evidence.go`, docs. Migrations: none (source `mta_log` exists).
Acceptance: fixture logs from Postfix and Exim produce `deferred`, `remote_accepted` and `bounced` evidence; nothing is installed or
changed on the existing server; Mailcow users keep working without it.

**CT-05 Real-infrastructure verification program.**
Follow `INFRA_VALIDATION.md` phases A to I: G05, E06, E07/F06/D04, E10, E12, G04(a)(b), C06, G06, G03/G02, G09, a real Cloudflare run.
Needs approval at each gate (VPS, DNS, first Live message). Acceptance: an evidence file per ID under `docs/evidence/` and a `docs/planning/STATUS.md` row;
the readiness page never says ready earlier. Until then the README and `RELEASE_REPORT.md` state "not verified on real infrastructure".

**CT-06 Application upgrade and rollback runbook.**
Files: new `docs/UPGRADE.md`, `docs/INSTALL.md` (link), `lab/qemu/tests/t07-rollback.sh`. Migrations: none.
Acceptance: the runbook is exercised in the lab (backup, upgrade, smoke, rollback with the previous image, restore if a migration was
applied) and the test fails if a documented command no longer exists.

**CT-07 Product-scope document and contract guard.**
Files: `docs/PRODUCT_SCOPE.md` (this PR), `README.md`, `test/docs_content_test.go`. Add a test that the public OpenAPI of the current
commit is a compatible superset of the last released one (no removed path, field or enum value) using a stored copy per release.
Acceptance: removing a documented field fails the test; adding one does not.

**CT-08 Isolation of out-of-scope modules.**
Verify and, where missing, add feature switches for contacts and campaigns and for SSO (forms and chat already have them); hide their
navigation and routes when off. Files: `internal/config/config.go`, `internal/api`, `apps/dashboard/src/features/workspace/nav.ts`.
Migrations: none. Acceptance: with every optional module off, the core flow (register domain, create key, send, inspect, webhook) passes
and the optional routes answer `404 <module>_disabled`; the security matrix test lists them.

**CT-09 Security review of the SMTP relay.**
Files: `internal/relay`, `test/`. Acceptance (named tests): unauthenticated `MAIL FROM` is refused (530); `AUTH` is refused without TLS; a
key without the scope or for another sender or an unverified domain is refused exactly as on the HTTP API; the relay port is bound to
loopback by default; a recipient limit applies. If any is missing, add it; if the relay is not wanted, remove it behind a flag.

**CT-10 Real Cloudflare run.**
Needs a Cloudflare account and one throwaway zone and the owner's yes. Register the OAuth client or use a zone-scoped token. Acceptance:
preview, approve, apply and rollback on the throwaway zone, an existing conflicting record is refused, the manual fallback produces the same
records; evidence in `docs/evidence/`.

**CT-11 Merge order and scope of the open pull requests** (section 4). Acceptance: the owner's decision; `docs/planning/task.md` updated to match.

## 6. What must still be reported as a limitation

1. Neither mode has been run on real infrastructure. All real-host rows in `INFRA_VALIDATION.md` are open.
2. `lab/qemu` uses simulated networking; it proves installer and restore logic, not public delivery, PTR or outbound port 25.
3. Existing-SMTP mode tracks a message up to "submitted" by itself; later evidence needs the operator to relay bounces (or CT-04).
4. New sending domains in managed mode need a manual installer run for their DKIM key (until CT-03).
5. Until PR #28 is merged, every signed-in user can open every project.
6. The documentation portal and SDKs are not published; the Kiswahili text needs a native review; a screen-reader pass, an external
   penetration test and a non-author installation (G09) have not been done.

## 7. Rules observed in this audit

No production host, mail server, firewall rule, DNS record or external resource was touched. Nothing was provisioned, paid for or sent. No
data or migration was changed. Open pull requests were neither merged nor closed.

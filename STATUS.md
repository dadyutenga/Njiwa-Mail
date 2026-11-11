# Implementation status

Design reference: the Njiwa-Mail implementation pack (`15_IMPLEMENTATION_BACKLOG`, master spec).
This file is the live tracker the backlog asks for. A ticket is Done only when its backend predicate is tested.

Baseline: `cd129e2` — `go test ./...` green on PostgreSQL 16 (throwaway DB; the suite resets `public`).

## Done (this branch)

| ID | What | Evidence |
|---|---|---|
| A02 | Go suite reproduced on a throwaway Postgres 16 | `go test ./... -race` |
| A03 | `internal/transport`: `Transport`, `Outcome`, `Capability`, `Receipt`, `Fake`, `ExistingSMTPTransport` wrapper | `internal/transport/transport_test.go` |
| A04 | `internal/installation`: pure readiness model, status + problem codes, fresh/stale/expired logic; `GET /admin/v1/installation` | `internal/installation/installation_test.go`, `test/transport_test.go` |
| A05 | Migration `0010_transport_profiles.sql` (additive): `installation_settings`, `transport_profiles`, `installation_checks`, `installer_receipts`, `messages.transport_profile_id/revision` | `TestLegacyMailServerMigratesToActiveProfile` (re-runs migration on a legacy snapshot) |
| A06 | Legacy `mail_server` backfilled to the active `existing_smtp` profile, sealed secrets copied byte for byte; env `SMTP_HOST` adopted by `migrate` when no profile exists; legacy row kept | same + `TestEnvSMTPAdoptedOnceAndNeverOverwrites` |
| A07 | Profile registry, one active (partial unique index), per-profile checks bound to `config_revision`, audited activation, submitted messages pinned to profile+revision | `TestStatusIsTiedToProfileRevision`, `TestProfileActivationPlanAndGates`, `TestWorkerUsesActiveProfileAndPinsMessages` |
| A08 | Switch plan + gates: unknown jobs always block; pending jobs need explicit `migrate_pending`; unverified/incomplete/uninstalled target blocked; previous profile kept for rollback | `TestProfileActivationPlanAndGates` |
| C02 (part) | Secrets never echoed; blank password keeps stored one; rotation bumps revision | tests above |

| A02 (part) | Frontend CI called `npm run typecheck`, which did not exist (the job failed on the baseline); script added | `npm run typecheck` |
| B01 | `DeliveryModeCard`: exactly `managed_mta` / `existing_smtp`, accessible radio group; choice saved only on Continue | `Installation.test.tsx` |
| B02 | `PUT /admin/v1/installation/wizard` persists the confirmed mode in `installation_settings.wizard`; resumes after refresh/redeploy; never switches an active transport (`mode_switch_requires_plan`); audited | `TestDeliveryModeWizardPersistsAndNeverSwitches` |
| B03 | Readiness steps show plain status text, stable problem code, the fix for each code, last checked time and "Run checks again"; welcome states `ready_to_configure` / `legacy_detected` / `in_progress` / `degraded` / `completed` | `Installation.test.tsx`, `TestLegacyInstallIsDetectedThenConfirmed` |
| B04 | Mailcow wording removed from the generic screens (Delivery infrastructure, Domains, Senders, temporary inboxes, DNS guide); Mailcow panel and API fields only when the operator picks provider Mailcow; `provider_type` stored on the profile; choosing generic deletes stored Mailcow API settings | `TestProviderTypeIsOptIn`, `Installation.test.tsx` |
| C05 (part) | Mailcow provider works with no Mailcow API key | `TestProviderTypeIsOptIn` |
| B05 | `/checklist` returns project-only steps (`scope: project`) plus a separate installation summary; mail-server and global "project exists" steps removed from the project list; no profile config leaks into workspaces | `TestChecklistSeparatesProjectFromInstallation` |
| B06 | Step count and progress computed by the backend; frontend uses `progress.required` (no `/7`); mailbox optional and never blocks | same + `Installation.test.tsx` |
| B07 (part) | Kiswahili strings for the new installation UI; status text not colour-only; `aria-live`, labelled progress bars | — |
| B09 (part) | Installation health banner on the project dashboard and Getting started page when delivery is not ready | `Installation.test.tsx` |
| D01 | Installation-wide `domain_claims` (migration 0011): the first project whose ownership check passes holds the name; others cannot verify, create Live senders or send; audited operator transfer resets every project's proof; deleting the domain releases the claim; names verified by several projects before the upgrade are flagged `review_required`, not given to one | `test/domain_claims_test.go` |
| D02 | One Live gate (`requireLiveDomain`) for `/v1/emails`, batch, compose, scheduled and bulk acceptance and for Live sender creation on every path (CLI, admin API, workspace). No grandfathering: unregistered → `domain_not_registered`, other project/review → `domain_claimed_elsewhere`, unverified or failing DNS → `domain_not_ready`. Temporary `ALLOW_UNREGISTERED_LIVE_DOMAINS` bridge for unclaimed names only | `TestDomainGateAndSearch`, `TestDomainClaimIsInstallationWide`, `TestUnregisteredDomainEscapeHatchIsNarrow` |
| D03 | Canonical names (lowercase, IDNA/punycode, no trailing dot) on every create path; explicit policy: subdomains are separate claims needing their own proof. Fixed CLI `CreateDomain` still using the retired `inbound` purpose | `TestDomainNamesAreCanonical` |
| D04 (part) | DNS requirements follow the sender that actually sends: with a generic SMTP server SPF and DKIM are marked templates (`placeholder: true`, no Copy action) and never authorise `OUTBOUND_IP`/`MAIL_HOSTNAME`; managed and Mailcow keep the configured values; MX/return-path guidance names exactly the host the check compares, priority 10 | `TestDNSRecordsFollowTheActualSender`, `Domains.test.tsx` |
| D05 | `internal/dnsauth` RFC 7208 SPF evaluation (include, a, mx, ip4/ip6 CIDR, exists, redirect; 10-lookup and void limits; loops and duplicates are permerror). Known sending IP (managed/Mailcow) must evaluate to pass; generic SMTP needs a valid record and is told only a test proves it. Health check uses the same evaluator | `internal/dnsauth`, `TestDomainCheckEvaluatesSPFAndDMARC`, `TestDNSHealthCheck` |
| D06 (part) | DKIM key records parsed and decoded (v/k/p/t, base64, PKIX/PKCS1, ed25519, revoked, ≥1024-bit RSA, fingerprint match with the signer's key); a valid key is reported as published, not as proof of signing | `TestDKIMKeyParsing`, `TestDomainCheckValidatesDKIMKey` |
| D07 | DMARC parsed (RFC 7489 tags and values), organisational-domain fallback via the public suffix list, strict/relaxed alignment; DMARC passes only when DKIM or SPF on the envelope (bounce) domain can align, and explains misalignment | `TestDMARCParse`, `TestDMARCLookupAndAlignment`, `TestDomainCheckEvaluatesSPFAndDMARC` |
| D08 | PTR of the sending address forward-confirmed (FCrDNS), only when Njiwa-Mail knows the sender; bounce MX check names the exact host; sending-only domains never ask for an apex MX | `TestPTRIsForwardConfirmed`, `TestDNSRecordsFollowTheActualSender` |
| C03 | Every failed diagnosis step has a stable provider-neutral code (DNS, refused vs timed-out port, TLS mode mismatch, missing STARTTLS, untrusted certificate, auth, sender) with a fix in the UI; the test refuses link-local/metadata/multicast targets and dials the vetted address | `TestDiagnoseReportsTypedProblemCodes` |
| C04 | `POST /admin/v1/w/{project}/live/transport-test` sends a controlled Live message through the normal accept pipeline, queue and worker; its final outcome becomes the `test_send` check of the profile revision that sent it (server-set flag, migration 0012) | `TestQueuedTransportTestRecordsTheRealOutcome` |
| E01 | `njiwa-mail preflight`: read-only host checks (OS, systemd, disk/inodes, memory, clock, sending IP on host, hostname A, FCrDNS PTR, outbound 25, existing MTAs), `cannot_confirm` instead of green; runs before a database exists; `-record` drives the managed readiness step | `internal/installer/preflight`, `TestManagedPreflightRecordingDrivesReadiness` |

New admin routes (operator-only): `GET /admin/v1/installation`, `PUT /admin/v1/installation/wizard`, `GET|POST /admin/v1/installation/transport-profiles`, `GET|PUT|DELETE .../{id}` (DELETE retires), `POST .../{id}/test`, `GET .../{id}/activation-plan`, `POST .../{id}/activate`, `GET /admin/v1/domain-claims`, `POST /admin/v1/domain-claims/transfer`.

## Review pass (gaps found and fixed)

| Gap | Fix | Evidence |
|---|---|---|
| A test on environment SMTP (or racing a save) could be recorded as a pass for different saved settings | Tests carry the exact profile revision they exercised; env results bind only when env and profile are identical | `TestEnvironmentTestBindsOnlyToIdenticalProfile` |
| Messages were stamped with whichever profile was active at commit time; unknown outcomes were not stamped | The transport reports the revision it used; submitted and unknown outcomes record it; unattributable sends stay NULL | `TestWorkerUsesActiveProfileAndPinsMessages`, `TestUnknownOutcomeIsPinnedAndNotResent` |
| Switch plan counted sandbox mail and ignored scheduled live mail | Live only; scheduled included | `TestProfileActivationPlanAndGates` |
| Activating an API-created profile left the Mail server page (and rollback row) showing other settings | Activation and edits of the active profile mirror into `mail_server`; inbound listener kept | `TestProfileUpdateRetireAndLegacyMirror` |
| Checklist read the global `last_test` | Newer of the revision-bound check and legacy record decides | `TestChecklistMailServerStepFollowsActiveRevision` |
| Profiles could not be edited or retired | `PUT` / `DELETE` routes; retire drops secrets, refuses the active profile | `TestProfileUpdateRetireAndLegacyMirror` |
| Problem codes doubled the prefix (`SMTP_SMTP_…`) | Normalised | `TestStatusIsTiedToProfileRevision` |

## API contract changes

- `GET /admin/v1/w/{project}/{mode}/checklist` now returns `{scope, steps, complete, progress, installation}`. Steps no longer include `mail_server` or `project`; each step has `required`. Clients must use `progress.required` instead of counting steps.
- `GET|PUT /admin/v1/mail-server` gained `provider_type` (`generic` default, `mailcow`). Older clients that omit it keep the current provider.

## Upgrade notes (D-series)

- **Breaking for installs that sent Live mail from unregistered domains:** those sends now get `403 domain_not_registered`. Register and verify each sending domain before upgrading, or set `ALLOW_UNREGISTERED_LIVE_DOMAINS=true` temporarily (unclaimed names only). See `docs/OPERATIONS.md`.
- A domain verified by two projects before the upgrade blocks Live mail from both until an operator transfers it.
- `VerifyDomain` (the older `/admin/v1/domains/{id}/verify` route) now runs the same evaluation and claim rules as the Domains page.
- Stricter DNS evaluation can turn previously "passing" domains red on the next check (for example SPF that does not cover the known sending IP, a truncated DKIM key, an invalid DMARC record). That is the intended correction; the hint on each record says what to change.
- The Domains page shows the records saved by the last DNS check; after changing the delivery mode or provider, run "Check now" to see the new requirements.
- Messages already accepted are not re-checked at dispatch; a revoked or transferred domain stops *new* Live mail only.

## Compatibility / rollback notes

- The Mail server page keeps working: `SaveMailServer` dual-writes `mail_server` and the active existing-SMTP profile in one transaction; the worker reads the active profile and falls back to the legacy row only when no profile exists.
- **Rollback boundary:** an older binary ignores the new tables. If it edits `mail_server` after rollback, the profile will not see those edits; re-save on the Mail server page after returning to this version.
- `SMTP_HOST` in the environment still wins at runtime (unchanged precedence). `GET /admin/v1/installation` reports `environment_override`. Mail sent through environment SMTP is not pinned to a profile.
- A worker caches the active settings for up to 30 seconds, so after a switch it may finish sends through the previous profile; those messages are pinned to the profile that really sent them.
- Jobs queued before a switch are sent by the new profile only after the operator acknowledges `migrate_pending`; there is no per-message hold at dispatch.
- The checklist's legacy fallback still trusts `last_test` for environment SMTP across a restart with changed env values (pre-existing behaviour).
- SMTP host values are validated for shape only; there is no private-network (SSRF) guard on operator-entered hosts yet (F05).

## Not done / blockers

- B07 remainder (full keyboard/screen-reader pass, responsive DNS table, Kiswahili for all older strings), B08 (recovery/migration UX), B09 remainder (unified health summary with queue metrics).
- Managed mode is selectable and honest (it shows "installer not available" and cannot become ready or active) but the installer itself (E-series) does not exist yet.
- `docs/MAILCOW_SETUP.md` and `docs/openapi.yaml` still describe Mailcow as the default path (G08).
- A01 endpoint/schema map doc, A02 container smoke CI.
- C01 full cutover of `StoredTransport` to `transport.Transport`, C03–C08.
- D, E (managed MTA installer — managed profiles can be created but can never activate without a verified installer receipt), F, G.

## Next task

E02–E09: the privileged host installer (plan/apply/validate/rollback receipts, Postfix with no open relay, DKIM signer, DSN route, queue/log collector). It must be built and validated on a real supported VPS; nothing here proves direct delivery yet. Also open: D06 signed-mail evidence (verifying DKIM on a received test message), C01 full worker cutover to `transport.Transport`, F-series (delivery evidence model, unknown reconciliation workflow, abuse limits, security review) and G-series (deployment packages, backups, CI security gates, OpenAPI). Domain-claims UI (D09) is not built: claims are operator API only.

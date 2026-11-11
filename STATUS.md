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

| B05 (part) | Project checklist's mail-server step follows the active profile's current revision; project step scoped to the project | `TestChecklistMailServerStepFollowsActiveRevision` |

New admin routes (operator-only): `GET /admin/v1/installation`, `GET|POST /admin/v1/installation/transport-profiles`, `GET|PUT|DELETE .../{id}` (DELETE retires), `POST .../{id}/test`, `GET .../{id}/activation-plan`, `POST .../{id}/activate`.

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

## Compatibility / rollback notes

- The Mail server page keeps working: `SaveMailServer` dual-writes `mail_server` and the active existing-SMTP profile in one transaction; the worker reads the active profile and falls back to the legacy row only when no profile exists.
- **Rollback boundary:** an older binary ignores the new tables. If it edits `mail_server` after rollback, the profile will not see those edits; re-save on the Mail server page after returning to this version.
- `SMTP_HOST` in the environment still wins at runtime (unchanged precedence). `GET /admin/v1/installation` reports `environment_override`. Mail sent through environment SMTP is not pinned to a profile.
- A worker caches the active settings for up to 30 seconds, so after a switch it may finish sends through the previous profile; those messages are pinned to the profile that really sent them.
- Jobs queued before a switch are sent by the new profile only after the operator acknowledges `migrate_pending`; there is no per-message hold at dispatch.
- The checklist's legacy fallback still trusts `last_test` for environment SMTP across a restart with changed env values (pre-existing behaviour).
- SMTP host values are validated for shape only; there is no private-network (SSRF) guard on operator-entered hosts yet (F05).

## Not done / blockers

- B-series UI (wizard, mode card, Mailcow copy removal, dynamic step count). B05 remainder: the checklist still mixes the installation step into the project list and the frontend still hard-codes the step count.
- A01 endpoint/schema map doc, A02 container smoke CI.
- C01 full cutover of `StoredTransport` to `transport.Transport`, C03–C08.
- D, E (managed MTA installer — managed profiles can be created but can never activate without a verified installer receipt), F, G.

## Next task

B01/B02 + rest of B05/B06: persisted wizard API, mode-choice card, and a checklist response that separates installation from project steps with a computed count.

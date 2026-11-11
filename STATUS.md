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

New admin routes (operator-only): `GET /admin/v1/installation`, `GET|POST /admin/v1/installation/transport-profiles`, `GET .../{id}`, `POST .../{id}/test`, `GET .../{id}/activation-plan`, `POST .../{id}/activate`.

## Compatibility / rollback notes

- The Mail server page keeps working: `SaveMailServer` dual-writes `mail_server` and the profile in one transaction; the worker reads the active profile and falls back to the legacy row only when no profile exists.
- **Rollback boundary:** an older binary ignores the new tables. If it edits `mail_server` after rollback, the profile will not see those edits; re-save on the Mail server page after returning to this version.
- `SMTP_HOST` in the environment still wins at runtime (unchanged precedence). `GET /admin/v1/installation` reports `environment_override`.
- Pinning records the *active* profile at submission time. With an environment override that is the adopted profile, which is accurate only while the env and profile agree.
- Not yet enforced: re-checking the pinned profile at dispatch time (jobs queued before a switch are sent by the new profile once the operator acknowledges `migrate_pending`).

## Not done / blockers

- B-series UI (wizard, mode card, Mailcow copy removal, dynamic step count) and B05 (`Checklist()` still reads global `last_test`; project readiness still not separated from installation readiness).
- A01 endpoint/schema map doc, A02 container smoke CI.
- C01 full cutover of `StoredTransport` to `transport.Transport`, C03–C08.
- D, E (managed MTA installer — managed profiles can be created but can never activate without a verified installer receipt), F, G.

## Next task

B05 + B01/B02: separate `Checklist()` from installation readiness, then the persisted wizard API and mode-choice card.

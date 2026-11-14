# Public documentation portal: contract audit and plan

Source commit audited: `531c7b4` (2026-11-13). Inputs read: `internal/api/{api,app,product,workspace}.go`, `internal/service/{accept,auth,product,status,adminviews,inbound}.go`,
`docs/{openapi,INTEGRATION,WEBHOOKS}.yaml/md`, `client/`, `njiwamail/`, `clients/typescript/index.ts`, `frontend/src/features/workspace/pages/Developers.tsx`,
`.github/workflows/ci.yml`, `README.md`, `docs/RELEASE_REPORT.md`.

**Package note.** The brief refers to `07_IMPLEMENTATION_BACKLOG.md` and other package documents. Only `09_CODING_AGENT_MASTER_TASK.md` was supplied, so
backlog IDs and dependencies could not be followed; the stages below follow the brief's own execution order (contract → scaffold → content → UI → agent
assets → tests → deployment).

## 1. Public route / source / auth matrix

"Public API" means what an application integrates against. `/admin/v1`, `/console/*` and `/metrics` are not part of it.

| Method | Path | Handler | Auth | Scope | Success | Tested by |
|---|---|---|---|---|---|---|
| POST | `/v1/emails` | `api.go postEmail` | Bearer key, key IP allowlist | `emails:send` (template) or `emails:send_raw` (raw) | 202 `{id,status}` | `test/api_test.go` (through the `post` helper), `complete_test.go` |
| POST | `/v1/emails/batch` | `product.go postBatch` | Bearer key | per item, as above | 200 `{results:[{index,id,status,state,code}]}` | `test/complete_test.go` |
| GET | `/v1/emails/{id}` | `api.go getEmail` | Bearer key | `emails:read` | 200 status object | `test/api_test.go` |
| POST | `/v1/emails/{id}/cancel` | `product.go cancelEmail` | Bearer key | `emails:send` or `emails:send_raw` | 200 `{status:"cancelled"}`; 409 `not_cancellable` | `test/complete_test.go`, `test/authz_test.go` |
| GET | `/v1/inbound` | `app.go` | Bearer key | `inbound:read` | 200 page | `test/v2_test.go` |
| POST | `/v1/inbound/ingest/{env}` | `api.go ingestInbound` | HMAC signature (environment ingest secret) | | 202 `{id,recipients}`; 200 `{duplicate:true}`; 401 `invalid_signature` | `test/ingest_test.go` |
| GET | `/v1/inbound/{id}` | `app.go` | Bearer key | `inbound:read` | 200 message | `complete_test.go`, `lifecycle_test.go`, `v2_test.go` |
| GET | `/v1/inbound/{id}/attachments/{aid}` | `app.go` | Bearer key | `inbound:read` | 200 bytes, always `application/octet-stream` | `rekey_test.go`, `lifecycle_test.go`, `complete_test.go` |
| POST | `/v1/addresses/verify` | `api.go verifyAddresses` | Bearer key | `addresses:verify`, `emails:send` or `emails:send_raw` | 200 `{results:[AddressCheck]}`; 422 over 100 | `test/verify_test.go` |
| GET | `/v1/suppressions` | `app.go` | Bearer key | `suppressions:manage` | 200 page | `test/v2_test.go` |
| POST | `/v1/suppressions` | `app.go` | Bearer key | `suppressions:manage` | 200 `{id}` | `v2_test.go` |
| DELETE | `/v1/suppressions/{id}` | `app.go` | Bearer key | `suppressions:manage` | 200 `{ok:true}`; `{id}` may be the address | `v2_test.go` |
| GET | `/v1/unsubscribe/{token}` | `workspace.go` | none (token in URL) | | 200 HTML confirm page | `workspace_test.go`, `security_matrix_test.go` |
| POST | `/v1/unsubscribe/{token}` | `workspace.go` | none (token in URL) | | 200 / 404 HTML | `workspace_test.go` |
| GET | `/v1/t/o/{token}` | `workspace.go` | none (signed token) | | 200 GIF always; first open counted | `marketing_test.go`, `security_matrix_test.go` |
| GET | `/v1/t/c/{token}` | `workspace.go` | none (signed token) | | 302 to a stored campaign link; 404 HTML | `marketing_test.go`, `security_matrix_test.go` |
| GET | `/v1/view/{token}` | `workspace.go` | none (signed token in URL) | | 200 sandboxed HTML; 404 HTML when altered or expired | `webview_test.go`, `security_matrix_test.go` |
| GET | `/health/live` | `api.go` | none | | 200 `{status:"ok"}` | `api_test.go`, CI smoke |
| GET | `/health/ready` | `api.go` | none | | 200 `{status:"ready"}` / 503 `{status:"unavailable"}` | `api_test.go`, CI smoke |

Out of the public contract: `/metrics` (bearer metrics token, 404 when unset), `/console/*` (legacy console session), `/admin/v1/*` (operator API, documented in
`openapi-admin.yaml` and `ENDPOINTS.md`, never in the public site).

Error envelope: `{"error":{"code":"…","message"?:"…"}}`; `401` adds `WWW-Authenticate: Bearer realm="njiwa-mail"`; `429`/`503` add `Retry-After`.
Request size: `POST /v1/emails` accepts up to **6 MiB** as shipped (`EmailMaxBytes`, so attachments fit); `MAX_BODY_BYTES` (32 KiB) applies to other bodies.
Batch body ≤ 6 MiB and ≤ 100 messages; attachments ≤ 5 files, ≤ 5 MiB total, types pdf/png/jpeg/plain.

## 2. Discrepancies found (existing docs vs source)

| # | Where | Claim | Reality | Fix |
|---|---|---|---|---|
| D1 | `openapi.yaml` | only 3 operations | 14 public operations exist | rewritten (stage 1), parity test |
| D2 | `openapi.yaml` SendRequest | `template`, `template_version`, `variables` required; template enum of 3; `additionalProperties:false`; `template_version` enum `[1]` | template or raw (`subject`+`html`/`text`); custom templates allowed; `template_version` optional (newest published); extra fields: `locale`, `reply_to`, `cc`, `bcc`, `priority`, `send_at`, `attachments`, `headers` | rewritten |
| D3 | `openapi.yaml` Status enum | no `scheduled`, `cancelled` | both exist | rewritten |
| D4 | `openapi.yaml` 413 | "32 KiB max" | 6 MiB on this route as shipped | rewritten |
| D5 | `openapi.yaml` Error | only `code` | optional `message` | rewritten |
| D6 | `openapi.yaml` | no `scheduled` 202 state, no batch/cancel/inbound/suppression/unsubscribe | present | rewritten |
| D7 | `INTEGRATION.md` §4.7 | "Live keys submit to Mailcow" | Live keys submit through the active transport (managed Postfix or existing SMTP; Mailcow optional) | fix in stage 3 |
| D8 | `INTEGRATION.md` §3 | `GET /v1/emails/{id}` "never includes … the recipient" | true (status has no recipient) | none |
| D9 | `clients/typescript/index.ts` | `status()` returns `Accepted` | returns the status object | typing fixed in stage 3; not a published package |
| D10 | Go client | docs imply full API coverage | `client.Client` has `Send` and `Status` only (no batch, cancel, inbound, suppressions) | docs say so; do not invent methods |
| D11 | `INTEGRATION.md` §5 | `njiwamail` import | exists: `github.com/njiwa-tz/mail/njiwamail` (alias of `client`) | keep |
| D12 | docs | no published npm/PyPI/Go proxy package names are claimed anywhere | none are published | portal says "copy the file from the repository" |

## 3. Framework comparison (brief)

| Option | For | Against |
|---|---|---|
| **Fumadocs + Next.js + MDX** (chosen) | React/TypeScript like the dashboard, MDX with typed components, built-in static search, TOC, tabs, code blocks, i18n-ready, static export | younger API, moves fast: versions pinned and lockfile committed |
| Docusaurus | mature versioning, large ecosystem | heavier theming, separate toolchain feel, React 18/webpack |
| Astro Starlight | very fast, little JS | different component model from the dashboard |
| Mintlify / hosted | polished | hosted SaaS, not self-hostable, conflicts with "no central endpoint, no third-party hosting" |

No repository constraint mandates an alternative. Output is a **static export** served by nginx: no server runtime, no secrets in the image, no proxy to `/admin/v1`.

## 4. Stages (each independently testable, one commit each)

| Stage | Scope | Done when |
|---|---|---|
| 0 | this audit | reviewed |
| 1 Contract | complete `docs/openapi.yaml` (public), Go parity tests (routes, scopes, states, error codes, request/response examples against the live handlers) | `go test` fails on any drift |
| 2 Scaffold | `documentation/` Next.js + Fumadocs, static export, lockfile, lint, typecheck, CSP-safe | `npm run build` produces `out/` |
| 3 Content | real MDX for every page in the brief, snippets as compiled files, fixes D7/D9 | content lint + snippet tests pass |
| 4 UI | landing, header actions (Copy Markdown, Open ▾), version selector, search, 404 | Playwright passes |
| 5 Agent assets | `llms.txt`, `llms-full.txt`, per-page `.md`, OpenAPI JSON/YAML, `skills/njiwa-mail-integration` | export parity tests pass |
| 6 CI + deploy | CI job, separate deploy workflow, Dockerfile + nginx, smoke, rollback demonstration | CI green; deployment smoke run |
| 7 Docs of docs | contribution/style guide, version policy, migration guide, README/dashboard/release-report links | links check passes |
| 8 (optional) | documentation-only MCP tools | only after 1–7 |

## 5. Status

Stages 0 to 7 are done; stage 8 (documentation MCP tools) is deliberately not built. Exact commands, results and what is not done: `docs/RELEASE_REPORT.md` ("Public documentation portal"). Operating it: `CONTRIBUTING.md`, `VERSIONING.md`, `MIGRATION.md`, `DEPLOY.md`, `RELEASE_CHECKLIST.md` in this folder.

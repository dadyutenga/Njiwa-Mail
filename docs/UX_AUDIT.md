# Njiwa-Mail UX audit

Date: 11 November 2026. Baseline: commit `f55f39e` (before the workspace rebuild).
This audit was written before any change. Each finding has an ID; the **Fix** column says
what the rebuild does with it. "Fixed" means the item is resolved in the rebuild described
in `docs/PROGRESS.md`.

## 1. Frontend routes

All operator routes were flat (`/messages`, `/keys`, …). None carried the project or environment.

| ID | Route | State | Notes | Fix |
|---|---|---|---|---|
| R1 | `/` | works | Welcome page; signed-in users go to `/dashboard` | Signed-in users go to `/app` (workspace resolver) |
| R2 | `/login`, `/forgot-password`, `/reset-password` | works | After login goes to `/dashboard` | After login goes to `/app` |
| R3 | `/dashboard` | partly | Global counters; numbers do not link to anything; not project aware | Replaced by `/p/{project}/{env}/dashboard`; every counter links to its filtered list |
| R4 | `/messages` | partly | State filter only, not in URL; no project or environment filter; no recipient, sender or subject | Replaced by Message log and Sent, filters in URL |
| R5 | `/messages/:id` | partly | No links to template, sender, domain, bounce, suppression, webhook deliveries | Rebuilt message detail with all related links |
| R6 | `/projects` | partly | Generic table + inline create form; quota edit uses `window.prompt` | Rebuilt Projects page; create selects the project and opens the checklist |
| R7 | `/projects/:name` | works | Environment settings | Kept, linked from Projects and the switcher |
| R8 | `/keys` | partly | Form asks for **project and environment as free text** | Scoped page; no project field |
| R9 | `/senders` | partly | Form asks for project and environment as free text; no domain check | Scoped page; only verified sending domains |
| R10 | `/templates` | partly | Form asks for project as free text | Scoped list + editor |
| R11 | `/templates/:id` | works | Draft, preview, publish, test send (test send asks for project) | Editor uses the selected project |
| R12 | `/inbound` | partly | Global table of inbound mail; no reader layout, no read state | Replaced by Inbox |
| R13 | `/inbound/:id` | partly | Raw dump of the message | Replaced by the Inbox reader |
| R14 | `/inbound-addresses` | partly | Form asks for project, environment, webhook endpoint **ID** as free text | Replaced by Mailboxes |
| R15 | `/webhooks` | partly | Project as free text; events typed comma-separated | Scoped page with checkboxes |
| R16 | `/webhook-deliveries` | partly | Not linked from an endpoint; not filterable | Endpoint detail shows its deliveries with replay |
| R17 | `/suppressions` | partly | Project as free text; removal reason via `window.prompt`; no link to the bounce | Scoped page; links to the cause |
| R18 | `/bounces` | works | Global list | Folded into Suppressions and Complaints, and message detail |
| R19 | `/alerts` | works | | Kept under Admin |
| R20 | `/alert-channels` | works | | Kept under Admin → Alerts |
| R21 | `/domains` | partly | Project as free text; "verify" gives no record-level result; no DNS records per domain | Rebuilt Domains + domain page |
| R22 | `/dns` | partly | Global check table, not linked to domains | Replaced by domain pages + DNS setup guide |
| R23 | `/sandbox`, `/sandbox/:id` | works | Global; not scoped | Scoped Sandbox page |
| R24 | `/analytics` | works | Global only | Kept under Admin |
| R25 | `/audit` | works | | Kept under Admin |
| R26 | `/settings` | works | | Kept under Admin |
| R27 | `/profile` | works | Only reachable from the user menu | Kept |
| R28 | `/privacy` | works | Asks for project and environment as free text | Uses the selected project |
| R29 | `/users` | works | Owner only | Kept under Admin |
| R30 | Inbox, Compose, Sent, Scheduled, Bulk send, Temporary inboxes, Mailboxes, DNS guide, Automations, Complaints, Unknown queue, API docs, Mail server, System, Setup checklist | **missing** | Not implemented | Built |
| R31 | `/records`, `/reports`, `/example` template pages | dead | Example-template code under `features/example-crud`, `features/reports`, `pages/Records.tsx`; not routed | Removed |

## 2. Sidebar and in-page links

| ID | Link | Where it went | Problem | Fix |
|---|---|---|---|---|
| L1 | Sidebar (27 items, flat, alphabetical by feature) | each flat route | No grouping; Inbound, Bounces, DNS checks, Alert channels and Webhook deliveries are separate top-level items | Grouped sidebar per task section 2 |
| L2 | Logo | `/dashboard` | | `/p/{project}/{env}/dashboard` |
| L3 | Breadcrumb | Home → page | Detail pages show "Page" because the lookup matches exact paths only | project → section → item |
| L4 | Dashboard "View messages →" | `/messages` | Not filtered | Every counter links to the filtered list |
| L5 | User menu → Profile, Settings | `/profile`, `/settings` | | Kept (scoped paths) |
| L6 | Search palette | navigates to route list only | Does not search data | Global search endpoint (messages, domains, senders, templates, keys) |
| L7 | Status page "Return to Dashboard" | `/dashboard` | | `/app` |
| L8 | Resources: project name → `/projects/:name`; template slug → `/templates/:id`; inbound subject → `/inbound/:id`; sandbox id → `/sandbox/:id` | | Only links in the app; no other record links to its related records | Related-record links per task table |

## 3. Project selection

| ID | Place | Behaviour | Stored where | Fix |
|---|---|---|---|---|
| P1 | Keys, Senders, Templates, Inbound addresses, Webhooks, Suppressions, Domains create forms | Free-text "Project" field (and "Environment" select) | Component state only | Removed; from the workspace context |
| P2 | Template test send | Project + environment fields | Component state | From context |
| P3 | Privacy export/delete | Project + environment fields | Component state | From context |
| P4 | Projects page | Inline "Add new" form | — | One of the two places allowed ("New project" in switcher and Projects page) |
| P5 | Messages, Inbound, Sandbox, Analytics | No project filter at all; shows every project | — | Scoped by URL |
| P6 | Anywhere | No current project concept; nothing remembered | — | URL `/p/{project}/{env}/…` + last-used in browser storage |

## 4. Backend endpoints and callers (before)

| Endpoint | Called by |
|---|---|
| `POST /admin/v1/auth/login`, `GET /auth/me`, `POST /auth/logout`, `POST /auth/forgot`, `POST /auth/reset` | auth.tsx, AuthForm |
| `GET /auth/sessions`, `DELETE /auth/sessions/{id}` | Profile |
| `GET /overview` | Dashboard |
| `GET /alerts`, `POST /alerts/{id}/ack` | Resources(alerts), Toolbar bell |
| `GET /messages`, `GET /messages/{id}`, `POST /messages/{id}/resolve` | Messages, MessageDetail |
| `GET /bounces` | Resources(bounces) |
| `GET /inbound`, `GET /inbound/{id}` | Resources(inbound), ResourceDetail |
| `GET /inbound/{id}/html`, `GET /inbound/{id}/attachments/{aid}` | **no page** (HTML and attachments never shown) |
| `GET/POST/PATCH/DELETE /inbound-addresses` | Resources(inbound-addresses) |
| `GET/POST /projects`, `PATCH /projects/{name}` | Resources(projects) |
| `PATCH /projects/{name}/environments/{mode}` | ProjectDetail |
| `GET/POST /keys`, `POST /keys/{prefix}/rotate|revoke` | Resources(keys) |
| `GET/POST /senders`, `DELETE /senders/{id}` | Resources(senders) |
| `GET /dns`, `POST /dns/check` | Resources(dns) |
| `GET/POST /templates`, `GET /templates/{id}`, `PUT /templates/{id}/draft`, `POST …/publish`, `POST …/preview`, `POST …/test-send` | Resources(templates), TemplateDetail |
| `PATCH /templates/{id}` (archive, TTLs) | **no page** |
| `GET /template-meta` | **no page** |
| `GET/POST/PATCH/DELETE /webhooks`, `POST /webhooks/{id}/rotate-secret` | Resources(webhooks) |
| `GET /webhook-deliveries`, `POST …/replay` | Resources(webhook-deliveries) |
| `GET/POST/DELETE /suppressions` | Resources(suppressions) |
| `GET /sandbox`, `GET /sandbox/{id}` | Resources(sandbox), ResourceDetail |
| `GET/POST/PATCH /users` | Resources(users) |
| `GET /audit` | Resources(audit) |
| `GET/PUT /settings` | Settings |
| `GET /analytics` | Analytics |
| `GET/POST /domains`, `POST /domains/{id}/verify` | Resources(domains) |
| `GET/POST /alert-channels` | Resources(alert-channels) |
| `POST /privacy/export`, `POST /privacy/delete` | Privacy |
| `GET /admin/v1/docs` | **no page** |
| `/console/*` (legacy console) | **no page** (kept for the CLI-era console tests) |

Pages that call nothing: none. Endpoints that are missing for the task: drafts, compose (admin send),
inbox read/archive/delete/block, unread counts, campaigns, temporary inboxes, automations,
mail-server settings and tests, setup checklist, global search, per-domain DNS records,
any endpoint scoped to a project **and** environment.

| ID | Backend scoping gap | Fix |
|---|---|---|
| B1 | `GET /senders`, `/templates`, `/webhooks`, `/inbound-addresses`, `/domains`, `/sandbox`, `/bounces`, `/webhook-deliveries`, `/dns` return every project | New scoped API `/admin/v1/w/{project}/{env}/…`: every list filters by project and environment from the path |
| B2 | Domain purpose only `sending` or `inbound`; no `both` | Migration adds `receiving` and `both` |
| B3 | Mailboxes only on the single `INBOUND_DOMAIN` | Mailboxes on any verified receiving domain of the project, or the inbound domain |

## 5. Forms

| ID | Form | Submits | Success | Error | Problems | Fix |
|---|---|---|---|---|---|---|
| F1 | Login | `POST /auth/login` | go to `/dashboard` | message under form | — | lands in workspace |
| F2 | Resources "Add new" (generic) | `POST {path}` | closes form, reloads list, shows secret if any | message under form | Free-text project; no field-level validation; no success message; does not open the new record; entered data is cleared on close | Dedicated forms per page, field validation, success toast, land on the new record |
| F3 | Row actions (rotate, revoke, remove, enable/disable, verify, replay, ack) | various | reloads list silently | message above table | `window.confirm("Confirm remove?")` does not name what is lost; quota/role edits use `window.prompt` | Confirmation dialog naming the record; toasts; real forms |
| F4 | Template draft/publish/preview/test send | template endpoints | inline status | inline | Test send asks for project | Scoped |
| F5 | Environment settings | `PATCH …/environments/{mode}` | "Saved." | inline | — | Kept |
| F6 | Settings | `PUT /settings` | "Saved." | inline | — | Kept |
| F7 | Privacy export/delete | `POST /privacy/*` | shows JSON / message | inline | Free-text project | Scoped |
| F8 | Resolve unknown | `POST /messages/{id}/resolve` | reloads | inline | No success message | Toast |

## 6. Cross-cutting

| ID | Finding | Fix |
|---|---|---|
| X1 | Filters, tabs and pagination are component state; back and refresh lose them | URL search params |
| X2 | Loading is plain text "Loading…"; no skeletons; errors have no retry | Shared `DataState` with skeleton, retry and next-step empty state |
| X3 | Empty tables say "No records yet." with no next step | Empty states name the next step with one button |
| X4 | Times use `toLocaleString()` with no UTC value | `<Time>` shows local time, UTC on hover |
| X5 | No Live marker anywhere | Live badge in the header on every page |
| X6 | No copy buttons for IDs, DNS values or code | Copy buttons |

# Progress

The public API is `/v1` and the operator API is `/admin/v1`. The workspace API is `/admin/v1/w/{project}/{env}/…`.

## Workspace and UX task (`NJIWA_MAIL_WORKSPACE_AND_UX_TASK.md`), 11 November 2026

| Step | Work | State |
|---|---|---|
| 1 | Audit | Done: `docs/UX_AUDIT.md` |
| 2 | Project context and switcher | Done: URL `/p/{project}/…`, one Live workspace, project switcher with search + New project, last project remembered (browser storage), first-run screen, auto-select of a single project, "Project not found". Sending mode is local to Compose, bulk sending, template tests and API keys; Sandbox captures have a dedicated page. Legacy environment URLs redirect. |
| 3 | Navigation, linking, empty states | Done: grouped sidebar, breadcrumbs, related-record links, empty states with next step, filters/pagination in URL, global search (Ctrl K) |
| 4 | Mail server page + Mailcow | Done in app (encrypted settings, step-by-step test, test send, status, Mailcow API client checked against Mailcow openapi.yaml). `docs/MAILCOW_SETUP.md` rewritten with exact screens and undo per step. Real Mailcow run still pending. |
| 5 | Domains, DNS guide, Senders | Done: per-record checks, host shown both ways, auto re-check, delete guard, senders only on verified domains, default sender |
| 6 | Setup checklist | Done (landing page until steps 1–7 pass) |
| 7 | Mailboxes, Inbox, Compose, Sent, Scheduled | Done: two-pane inbox, read/archive/delete/block, threaded reply, compose with idempotency key, drafts autosave, suppression check |
| 8 | Templates, Automations | Done: editor with EN/SW, preview, publish, duplicate, archive; rules with order, dry-run test, run history, loop protection |
| 9 | Bulk send, Temporary inboxes | Done: campaigns with recipient check, bulk-only domain, pacing outside the transactional quota, List-Unsubscribe + one-click endpoint `/v1/unsubscribe/{token}`, auto-pause on complaint / >5% hard bounces; temporary inboxes with expiry job |
| 10 | UX pass | Done locally: 32 pages × 4 widths × both themes walked headless, no crashes or horizontal page scroll. Kiswahili added for navigation, titles, actions and empty states; long help text is English. |

Tests: `go test ./...` (6 new tests in `test/workspace_test.go`), `npm run lint`, `npm test`, `npm run build` all pass.

## Remaining

1. Run the Mailcow checks in `docs/MAILCOW_SETUP.md` Part C against the real server and record them in `docs/RELEASE_REPORT.md` (only local results exist).
2. Last project is remembered in the browser only, not server-side per user.

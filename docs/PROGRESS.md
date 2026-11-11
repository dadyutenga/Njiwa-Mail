# Progress

The public API is `/v1` and the operator API is `/admin/v1`. The workspace API is `/admin/v1/w/{project}/{env}/…`.

## Workspace and UX task (`NJIWA_MAIL_WORKSPACE_AND_UX_TASK.md`), 11 November 2026

| Step | Work | State |
|---|---|---|
| 1 | Audit | Done: `docs/UX_AUDIT.md` |
| 2 | Project context and switcher | Done: URL `/p/{project}/{env}/…`, one `WorkspaceProvider`, switcher with search + New project, last workspace remembered (browser storage), first-run screen, auto-select of a single project, "Project not found", Live badge, per-page project fields removed |
| 3 | Navigation, linking, empty states | Done: grouped sidebar, breadcrumbs, related-record links, empty states with next step, filters/pagination in URL, global search (Ctrl K) |
| 4 | Mail server page + Mailcow | Done in app: encrypted settings, step-by-step connection test, test send, status card, Mailcow API client (endpoints checked against Mailcow's published openapi.yaml), worker uses saved settings when `SMTP_HOST` is unset. **Not done:** `docs/MAILCOW_SETUP.md` still has the older text; it needs the new outbound steps (dedicated mailbox, sender ACL, rate limit, `fbl@` routing) with exact screens and undo steps. No real Mailcow run. |
| 5 | Domains, DNS guide, Senders | Done: per-record checks, host shown both ways, auto re-check, delete guard, senders only on verified domains, default sender |
| 6 | Setup checklist | Done (landing page until steps 1–7 pass) |
| 7 | Mailboxes, Inbox, Compose, Sent, Scheduled | Done: two-pane inbox, read/archive/delete/block, threaded reply, compose with idempotency key, drafts autosave, suppression check |
| 8 | Templates, Automations | Done: editor with EN/SW, preview, publish, duplicate, archive; rules with order, dry-run test, run history, loop protection |
| 9 | Bulk send, Temporary inboxes | Done: campaigns with recipient check, bulk-only domain, pacing outside the transactional quota, List-Unsubscribe + one-click endpoint `/v1/unsubscribe/{token}`, auto-pause on complaint / >5% hard bounces; temporary inboxes with expiry job |
| 10 | UX pass | Done locally: 32 pages × 4 widths (360/768/1024/1440) × both themes walked headless, no crashes or horizontal page scroll. Not done: Kiswahili strings for the new pages (they fall back to English). |

Tests: `go test ./...` (6 new tests in `test/workspace_test.go`), `npm run lint`, `npm test`, `npm run build` all pass.

## Remaining (first unfinished item first)

1. Rewrite `docs/MAILCOW_SETUP.md` per task section 5.2 (exact screens, undo per step).
2. Run Mailcow connection test, test send, inbound and bounce tests against a real Mailcow and record them in `docs/RELEASE_REPORT.md` (only local results exist).
3. Kiswahili translations for the workspace pages.
4. `docs/UX_AUDIT.md`: mark each item fixed.
5. Last workspace is remembered in the browser only, not server-side per user.

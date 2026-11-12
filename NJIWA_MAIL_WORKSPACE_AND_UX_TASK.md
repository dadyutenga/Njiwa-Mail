# Njiwa-Mail — Mail Workspace, Mailcow Setup and UX Repair: Task File

Date: 11 November 2026
Scope: four things that are missing or broken in the current build.

1. The mail workspace (inbox, compose, sent, bulk send, mailboxes, domains, DNS guide, senders, templates, automations) is not implemented.
2. Mailcow is not configured and the app has no way to connect to it or check it.
3. Project selection is broken: pages ask the user to create a new project instead of using the one already selected.
4. Pages are not linked to each other, and the user flows for data and operations are poor.

Reference for feature coverage: the MailAfrica workspace (`app.mailafrica.online/apps/...`). Match what it lets a user do. Do not copy its layout, colours, text or logo; use the Njiwa-Mail design tokens and the existing frontend template.

Read with: `DITRONICS_MAIL_IMPLEMENTATION_PLAN.md` (backend rules) and `NJIWA_MAIL_WELCOME_PAGE_TASK.md` (design tokens, light and dark mode).

---

## 0. Audit first

Before changing anything, write `docs/UX_AUDIT.md` with:

- [ ] Every route in the frontend, and for each: works, partly works, dead link, or missing.
- [ ] Every sidebar and in-page link, and where it actually goes.
- [ ] Every place a project is chosen, created or assumed, and where that value is stored.
- [ ] Every backend endpoint, and which pages call it. List endpoints no page uses and pages that call nothing.
- [ ] Every form: what it submits, what happens on success, what happens on error.

Fix from this list. Do not guess.

---

## 1. Project and environment context (fix this first)

**Problem:** every page asks for a project again, or offers only "create new project".

**Rule:** the project and environment are chosen once, in one place, and every page uses that choice.

- [ ] One **workspace switcher** at the top of the sidebar: shows the current project and environment (Sandbox or Live), lists all projects the user can access, has search, and has "New project" as its last item.
- [ ] The current project and environment live in the URL: `/p/{projectSlug}/{env}/...`. A link shared with a colleague opens the same project.
- [ ] The last used project and environment are remembered and restored on the next sign-in.
- [ ] One shared context provider supplies the project and environment to every page and every API call. No page keeps its own copy.
- [ ] Remove every per-page project picker and every inline "create project" step from forms (API keys, senders, templates, domains, webhooks, inbound addresses, compose).
- [ ] "New project" exists in exactly two places: the switcher, and the Projects page.
- [ ] Creating a project selects it, creates its Sandbox and Live environments, and lands on the setup checklist (section 3).
- [ ] Switching project keeps the user on the same page type (Messages stays on Messages) and reloads the data.
- [ ] Zero projects: show the first-run screen (section 3), not an empty table with a create form.
- [ ] One project: select it automatically.
- [ ] Unknown or forbidden project in the URL: show "Project not found" with a link back, not a create form.
- [ ] Live environment is visibly marked on every page (badge in the header), so nobody sends real mail by mistake.
- [ ] Backend: every list endpoint filters by the project and environment from the request; none returns data across projects.

---

## 2. Navigation and linking

Sidebar, in this order:

| Group | Items |
|---|---|
| (top) | Workspace switcher, **Compose** button |
| Overview | Dashboard, Setup checklist (hidden once complete) |
| Mail | Inbox, Sent, Scheduled, Bulk send, Temporary inboxes, Sandbox |
| Workspace | Domains, DNS setup guide, Mailboxes, Sender addresses, Templates, Automations, Suppressions, Complaints |
| Developers | API keys, Webhooks, Message log, Unknown queue, API docs |
| Admin | Projects, Users, Mail server, Analytics, Alerts, System, Audit log, Settings |

- [ ] Every sidebar item opens a working page. No dead items, no "coming soon".
- [ ] The active item is highlighted; breadcrumbs show project → section → item.
- [ ] Every record links to its related records:

| From | Links to |
|---|---|
| Message | Template version, sender, domain, recipient's suppression, bounce or complaint, webhook deliveries, reply thread |
| Inbound message | Mailbox, the outbound message it replies to, automation that handled it |
| Domain | Its DNS records, senders, mailboxes, recent messages |
| Sender address | Its domain, recent messages |
| Template | Versions, messages sent with it |
| API key | Messages sent with it |
| Webhook endpoint | Deliveries, failed deliveries with replay |
| Suppression | The bounce or complaint that caused it |
| Dashboard counters | The filtered list behind each number |

- [ ] Every empty state names the next step and has one button that goes there (no domains → "Add domain"; no senders → "Add a sender", or "Verify a domain first" if none is verified).
- [ ] Every page that needs something else first says so and links to it, instead of showing a form that will fail.
- [ ] After create or edit, the user lands on the new record with a success message. After delete, back on the list.
- [ ] Browser back and refresh work on every page; filters, tabs and pagination are in the URL.
- [ ] Global search (keyboard shortcut) finds messages by ID or recipient, domains, senders, templates and API keys.

---

## 3. First-run and setup checklist

One guided path from empty account to first email. Each step shows done or not done from real data, and links to its page.

| Step | Done when |
|---|---|
| 1. Connect mail server | Mailcow connection test passes (section 5) |
| 2. Create a project | At least one project exists |
| 3. Add a domain | A domain is added |
| 4. Publish DNS records | SPF, DKIM, DMARC (and MX for receiving) all pass |
| 5. Add a sender address | One sender on a verified domain |
| 6. Create an API key | One active key; secret shown once |
| 7. Send a test email | A sandbox message is captured, then one live message is `submitted` |
| 8. Create a mailbox (optional) | One receiving address exists |

- [ ] The checklist is the landing page until steps 1 to 7 are done, then it collapses into the Dashboard.
- [ ] Each step can be opened directly; none is locked except by its real prerequisite.

---

## 4. Mail workspace pages

### 4.1 Inbox

- [ ] Two-pane layout: message list on the left, reader on the right; single column on mobile.
- [ ] Mailbox selector (all mailboxes, or one address), unread filter with count, search (sender, subject, body).
- [ ] List row: sender, subject, snippet, time, unread mark, attachment mark.
- [ ] Reader: sender, recipients, time, text view by default, sandboxed HTML view, attachments, headers on demand, spam score.
- [ ] Actions: reply, forward, mark read or unread, archive, delete (with confirmation), block sender.
- [ ] Reply opens Compose with the thread linked (`In-Reply-To`, `References`) and the mailbox's sender preselected.
- [ ] Opening a message marks it read; the unread count updates everywhere.
- [ ] Empty state: "No messages" plus, if no mailbox exists, a button to create one.
- [ ] Server-side pagination; new mail appears without a full reload.

### 4.2 Compose

- [ ] Opens as a panel from the sidebar button, from Reply and Forward, and from Sent ("send again").
- [ ] Fields: from (verified senders of this project only), to, cc, bcc, subject, body (rich text with a plain-text version generated), attachments, optional template with variables, optional send time.
- [ ] Validates recipients against suppressions before sending and names any blocked address.
- [ ] Sends through the same API and queue as apps do (raw send with an idempotency key generated when the panel opens, so a double click cannot send twice).
- [ ] Drafts are saved automatically and listed under Compose.
- [ ] In Sandbox the button reads "Send to sandbox" and nothing leaves the server.
- [ ] No verified sender: the panel explains why and links to Sender addresses.

### 4.3 Sent

- [ ] Table of messages sent from this project: recipient, subject or template, sender, state, time.
- [ ] States use the backend names (`queued`, `submitted`, `failed`, `unknown`, `expired`, `scheduled`, `cancelled`). No "Delivered".
- [ ] Filters: state, sender, date, source (UI, API, SMTP).
- [ ] Row opens the message detail: timeline, attempts, bounce or complaint, retry button where the rules allow.

### 4.4 Scheduled

- [ ] Messages waiting for their send time, with cancel and "send now".

### 4.5 Bulk send (announcements)

For service announcements to people who already use the product. Not for marketing to bought or scraped lists.

- [ ] Form: verified sending domain and sender, recipients (paste or CSV upload), subject, body or template with per-recipient variables, optional send time.
- [ ] Shows the count of unique valid addresses, and lists duplicates, invalid addresses and suppressed addresses before sending.
- [ ] Every bulk message carries `List-Unsubscribe` and `List-Unsubscribe-Post` headers and a visible unsubscribe link; unsubscribes go to a bulk-only suppression list for that project.
- [ ] Bulk mail goes out as `normal` priority behind transactional mail, at a configurable rate (default 60 per minute), and never delays OTPs.
- [ ] Bulk mail uses its own sending subdomain (for example `news.<domain>`) so problems there do not damage the reputation of transactional mail.
- [ ] Limits: maximum recipients per campaign and per day, set per project; `owner` or `admin` role required.
- [ ] A campaign pauses itself if its hard-bounce rate passes 5% or any complaint arrives, and raises an alert.
- [ ] Campaign page: totals for accepted, failed, bounced, unsubscribed; pause, resume and cancel.
- [ ] Each recipient is one message with its own idempotency key (`campaignId:recipient`), so a restart never sends twice.

### 4.6 Temporary inboxes

Disposable receiving addresses for testing sign-up and reset flows.

- [ ] One click creates a random address under the inbound domain with a lifetime (1 hour, 1 day, 7 days).
- [ ] Mail to it appears in a simple reader; the address and its mail are deleted when it expires.
- [ ] Limit on active temporary inboxes per project.

### 4.7 Mailboxes

- [ ] List of receiving addresses: address, domain, unread count, last received, enabled state.
- [ ] Create: local part plus a domain that is verified for receiving; optional display name; optional catch-all.
- [ ] No receiving domain yet: show "Connect a domain first" with the button, not the create form.
- [ ] Disable, enable, delete (delete warns that stored mail goes too).
- [ ] Each mailbox links to its inbox view and its automations.

### 4.8 Domains

- [ ] List with search and a "Needs attention" filter: domain, purpose (sending, receiving, both), status per record, last checked.
- [ ] Add domain: enter the name, choose purpose, get the ownership TXT record.
- [ ] Domain page: every DNS record as a row with type, host, value, a copy button, and pass or fail with the value actually found.
- [ ] "Check now" button; automatic re-check while records are pending.
- [ ] Shows the host both ways (full name and the short label most DNS panels want).
- [ ] Deleting a domain is blocked while mailboxes or senders use it, and says which ones.

### 4.9 DNS setup guide

- [ ] One page explaining each record in plain words: DKIM, SPF (edit the existing record, never publish two), DMARC, MX, return path, ownership TXT.
- [ ] Short steps for common DNS providers, including the Cloudflare rule that mail hostnames must be DNS-only, not proxied.
- [ ] A troubleshooting list: record not found yet, wrong host, value cut off, two SPF records.
- [ ] Linked from Domains, from each failing record, and from the setup checklist.

### 4.10 Sender addresses

- [ ] List: address, display name, domain, status, last used.
- [ ] Create only on a verified sending domain; otherwise explain and link to Domains.
- [ ] Set a default sender per project; Compose and Bulk send preselect it.

### 4.11 Templates

- [ ] List on the left, editor on the right: name, subject, plain-text body, HTML body, variables with `{{name}}` placeholders, English and Kiswahili tabs.
- [ ] Live preview with sample values; missing variables are flagged.
- [ ] Save as draft, publish (published versions are immutable), version history, duplicate, archive.
- [ ] "Send test" to sandbox or to an approved test address.
- [ ] Usable from Compose, Bulk send and the API by slug.

### 4.12 Automations

Rules that act on incoming mail.

- [ ] Rule = mailbox (or all) + condition (sender, subject contains, has attachment) + action.
- [ ] Actions: forward to an email address, call a webhook, auto-reply from a template, label, archive.
- [ ] Rules run in order; each has an on/off switch and a run history with result.
- [ ] Auto-replies never answer automatic mail (`Auto-Submitted`, bounces, mailing lists) and are limited to one per sender per day, to prevent mail loops.
- [ ] Test a rule against a recent message before turning it on.

### 4.13 Developers

- [ ] API keys, Webhooks, Message log, Unknown queue and API docs as in the implementation plan, each using the selected project and environment.
- [ ] A "Quick start" panel with a ready-to-copy request that already contains the project's API host and a sender address.

---

## 5. Mailcow configuration

### 5.1 In the app: Mail server page (Admin)

- [ ] Form: SMTP host, port, encryption mode, username, password; inbound listener address; optional Mailcow API URL and API key. Secrets are stored encrypted and never shown again after saving.
- [ ] **Test connection** button runs and reports each check separately: DNS resolves, TCP connects, TLS certificate valid, login accepted, sender allowed.
- [ ] **Send test email** button sends one message to an address the admin types, and shows the result state.
- [ ] Status card: outbound connection, inbound listener reachable, last inbound message time, breaker state, queue depth.
- [ ] If the Mailcow API is configured, the app uses it for: adding the domain, creating and reading the DKIM key, and adding the relay and transport entries for inbound. Check each call against the API documentation of the installed Mailcow version before use.
- [ ] If the API is not configured, the Domains page shows the manual steps for each of those actions instead.
- [ ] The app never touches Mailcow's database and never changes global Mailcow settings.

### 5.2 On the server: operator steps (write up in `docs/MAILCOW_SETUP.md`)

Outbound:

- [ ] Create a dedicated mailbox for submission (for example `njiwa@<domain>`) with a strong password; do not use an admin or personal account.
- [ ] Allow that mailbox to send as the project's sender addresses (sender ACL / "send as").
- [ ] Add each sending domain and generate its DKIM key; publish the DKIM, SPF and DMARC records.
- [ ] Raise or set the mailbox's rate limit to fit expected volume, and note the value.
- [ ] Confirm submission works on the chosen port with a valid certificate.

Inbound:

- [ ] Add the receiving domain or subdomain as a relay domain.
- [ ] Add a transport entry that sends mail for it to the Njiwa-Mail inbound listener.
- [ ] Put the Njiwa-Mail inbound container on a Docker network Mailcow can reach; do not publish its port to the internet.
- [ ] Publish the MX record for the receiving domain pointing at the Mailcow host.
- [ ] Route `bounces@` and `fbl@` on the sending domain to the same listener.

Checks:

- [ ] Reverse DNS of the server IP matches the Mailcow hostname.
- [ ] Outbound port 25 is open.
- [ ] Send to a Gmail and an Outlook test inbox; confirm SPF, DKIM and DMARC pass in the headers.
- [ ] Send from outside to a mailbox address; confirm it appears in the Inbox.
- [ ] Send to an address that does not exist; confirm the bounce is recorded and the address suppressed.

Every step in this file must show the exact screen or command for the installed Mailcow version, with a way to undo it.

---

## 6. UX standards for every page

- [ ] One clear primary action per page, top right.
- [ ] Loading skeletons, empty states with a next step, and error states with a retry, on every list and form.
- [ ] Forms validate as the user types, show errors next to the field, keep entered data on failure, and disable the submit button while sending.
- [ ] Confirmation for every destructive action, naming what will be lost.
- [ ] Success and error messages for every operation; no silent failures.
- [ ] Tables: server pagination, sortable columns, filters in the URL, row click opens the record.
- [ ] Copy buttons on IDs, API hosts, DNS values and code.
- [ ] Secrets are shown once, with a warning, and never again.
- [ ] Role checks: hide what the user cannot do; the backend still refuses it.
- [ ] Works at 360, 768, 1024 and 1440 pixels; sidebar collapses on small screens.
- [ ] Light and dark mode on every page, from the shared tokens.
- [ ] Keyboard access and visible focus everywhere; dialogs trap focus and close on Escape.
- [ ] Times in the user's timezone with the UTC value on hover.

---

## 7. Backend work this needs

Check each against the implementation plan; add what is missing.

- [ ] Mailboxes: read state, archive, delete, block sender; unread counts per mailbox.
- [ ] Drafts: save, list, delete.
- [ ] Campaigns: create, recipients import and validation, pause, resume, cancel, totals; bulk suppression list and unsubscribe endpoint.
- [ ] Temporary inboxes: create with expiry, expiry job.
- [ ] Automations: rules, ordered execution, run history, loop protection.
- [ ] Mail server settings: encrypted storage, connection test, test send, optional Mailcow API client.
- [ ] Setup checklist endpoint that returns each step's real state.
- [ ] Global search endpoint.
- [ ] Project context: every endpoint takes and enforces project and environment.

---

## 8. Build order

| Step | Work | Exit check |
|---|---|---|
| 1 | Audit (section 0) | `docs/UX_AUDIT.md` complete |
| 2 | Project context and switcher (section 1) | No page asks for or creates a project on its own |
| 3 | Navigation, linking, empty states (section 2) | No dead link in the audit list |
| 4 | Mail server page and Mailcow setup (section 5) | Connection test and test email pass |
| 5 | Domains, DNS guide, Senders | A domain goes from added to verified in the UI |
| 6 | Setup checklist (section 3) | A new account reaches a first sent email without leaving the path |
| 7 | Mailboxes, Inbox, Compose, Sent, Scheduled | Receive, read, reply and see the reply in Sent |
| 8 | Templates, Automations | A rule auto-replies from a template |
| 9 | Bulk send, Temporary inboxes | A campaign sends, one recipient unsubscribes, and is skipped next time |
| 10 | UX pass on every page (section 6) | Screenshots in both themes at four widths |

---

## 9. Done when

- [ ] A new user can go from sign-in to a first sent email and a first received email using only the UI, with no page asking them to create a project again.
- [ ] Every sidebar item works and every item in `docs/UX_AUDIT.md` is fixed or removed.
- [ ] Every page uses the selected project and environment.
- [ ] Mailcow connection test, test send, inbound test and bounce test all pass and are recorded in `docs/RELEASE_REPORT.md`.
- [ ] No mock data, no placeholder pages, no TODOs.
- [ ] Lint, type check, tests and production build pass.

---

## 10. Prompt for the coding agent

> You are repairing and completing the **Njiwa-Mail** web app. Follow this file in the order of section 8. Start with the audit in section 0 and write `docs/UX_AUDIT.md` before changing code.
>
> Fix the project context first (section 1): the project and environment are chosen once in the sidebar switcher, stored in the URL, and used by every page and API call. Remove every other project picker and every inline "create project" step.
>
> Then make the app connected (section 2): every sidebar item works, every record links to its related records, every empty state leads to the next step.
>
> Build the mail workspace pages in section 4 and the Mail server page in section 5, with the backend work in section 7. Follow the backend rules in `DITRONICS_MAIL_IMPLEMENTATION_PLAN.md`: idempotency keys on every send, no "Delivered" status, safe retry, project and environment scoping. Use the design tokens and theme rules from `NJIWA_MAIL_WELCOME_PAGE_TASK.md`. The MailAfrica workspace is a reference for what users can do, not for how it looks; do not copy its design, text or logo.
>
> You cannot change Mailcow or DNS yourself. Write the exact operator steps into `docs/MAILCOW_SETUP.md`, and test against the local mail server in the development Compose file. Label local results as local.
>
> Apply section 6 to every page. After each step, run the app and walk the flow as a new user in both themes; fix what is confusing before moving on. Keep `docs/PROGRESS.md` updated and continue from the first unfinished item each session.
>
> Finish only when every item in section 9 is true. Report files changed, commands to run, test results, and anything not finished with the reason.

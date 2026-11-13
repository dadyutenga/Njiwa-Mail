# task.md: work the agent still has to complete

**Branch rule:** all development happens on `dev`. `main` only receives work that has been tested together (owner + assistant). Do not push
unfinished work to `main`.

**How we finish:** the agent implements the items below on `dev`, one commit per item, and runs the automated tests itself. Real-host and
manual checks are done later by the owner and the assistant together; each item says what that joint test is.

**Rules that stay in force** (from `HANDOFF.md` section 6):
- never say a message was "delivered" because SMTP accepted it; delivery is shown by received mail and its headers
- readiness must be honest: a check that could not be proven is `cannot_confirm`, never green
- every new route goes into the security matrix test (`TestEveryRouteRequiresAuthAndCSRF`) and `docs/ENDPOINTS.md` is regenerated
  (`UPDATE_DOCS=1 go test ./test -run TestEndpointsAndSchemaDoc`)
- migrations are additive; every new sealed column is added to `sealedColumns` in `internal/service/rekey.go`
- every new `phrase("...")` string needs a Kiswahili entry (the coverage test fails otherwise)
- never touch the production Mailcow host (`rida-server`): no managed installer, firewall, DNS or mailbox change there
- no real DNS change, no Live message to real recipients, no paid infrastructure without the owner's explicit "yes" at that moment
- never put the owner's email address or personal data in requests to outside services

Legend: P0 = blocks a real release, P1 = should be done before release, P2 = improvement.

---

## 1. MFA and session work (P0): finish and review

The owner was changing MFA and session handling when it was committed by accident inside `f08eff8`. Files: `internal/service/mfa.go`,
`internal/service/users.go`, `internal/service/users_test.go`, `internal/api/admin.go`, `internal/db/migrations/0023_auth_version.sql`
(`auth_version` on `users` and `sessions`), `test/mfa_test.go`. It builds, vets and passes the full Go suite, but nobody has reviewed it.

- [ ] **T1.1** Ask the owner what the fix is meant to cover; write it down here as acceptance criteria. Unknown today.
- [ ] **T1.2** Review the diff of `f08eff8` for those files. Confirm: sessions issued before a password change, MFA enrolment/reset, recovery-code use
  or role change are rejected afterwards; the existing replay protection and lockout still hold.
- [ ] **T1.3** Frontend: if the API now returns new error codes or states (for example "sign in again"), show them with a Kiswahili string and a test.
- [ ] **T1.4** Document the behaviour in `docs/SECURITY_REVIEW.md` and `docs/OPERATIONS.md` (what ends sessions, what does not).
- Joint test: sign in on two browsers, change the password on one, confirm the other is signed out; reset MFA with `njiwa-mail user mfa-reset`.

## 2. Re-run and finish what the QEMU lab started (P0)

The lab (`lab/qemu/`, evidence in `docs/evidence/lab/`) found seven defects, all fixed with tests. These parts were not finished.

- [ ] **T2.1** Re-run `lab/qemu/tests/t08-existing-smtp.sh` after the fix in `f08eff8` (mode switch holds waiting mail 35 s). Last full run was 16/20.
  Known test-script issues to fix first: check the receiver's `ESMTPSA` line, count messages by `created_at`, wait 35 s after every activation.
  Expected: queued mail moves to the new profile in both directions and is pinned to the profile that sent it.
- [ ] **T2.2** E12: clean-install matrix on Debian 12 and Ubuntu 22.04 (images were still downloading). Repeat the documented install (HANDOFF 3.1 steps 1-13)
  and tests t01-t03 on each; record Postfix/OpenDKIM/Docker versions in the compatibility table in `docs/RELEASE_REPORT.md`.
  Likely problems to check: the package list in HANDOFF (`docker-compose-v2` may not exist on Debian 12), `systemd-resolved` on Debian.
- [ ] **T2.3** D04 on a real Postfix: per-domain return path with `-bounce-domain a,b`; a bounce for the second domain correlates and is suppressed.
- [ ] **T2.4** The 0010 legacy upgrade (an old release with a `mail_server` row upgrades; migration 0010 creates the active profile and sending continues).
  Needs an old release build; the existing Go test covers the migration, not the full upgrade.
- [ ] **T2.5** Webhook HTTP delivery of `email.bounced` (the lab could only prove the delivery row is queued, because the SSRF guard rightly refuses private
  addresses and the lab has no public HTTPS). Test against a real public HTTPS endpoint later.
- [ ] **T2.6** Mailcow (G04 part a) on its own Mailcow instance, never the production one: profile type Mailcow, with and without the API key, DKIM pasted,
  headers recorded. Mailcow needs far more RAM than the managed mode.
- [ ] **T2.7** Re-run the whole Go suite and the lab once after T1 and T2.1, then update `docs/evidence/lab/README.md`.
- Joint test: recreate the lab with `lab/qemu/lab.sh init` and run `lab/qemu/tests/t01..t08`. It needs a Linux host with KVM and about 8 GB free RAM.

## 3. Product follow-ups the lab exposed (not yet implemented)

- [ ] **T3.1 (P1)** Host CLI database access. `-record` and the `njiwa-mail-mta-log` unit need `DATABASE_URL` with PostgreSQL's container IP, which can
  change after a reboot or restart and silently breaks the collector. Give PostgreSQL a fixed address on the Compose network (or publish it on
  `172.30.0.1` only, which the firewall table already covers) and use it in `compose.yaml`, `.env.example`, HANDOFF 3.1 step 6 and the unit's env file.
- [ ] **T3.2 (P1)** SPF for the default bounce domain. The Domains page lists SPF at the bounce domain only for a per-domain return path
  (`return_path_spf`); for the installation's default bounce domain nothing is listed, so receivers report `spf=none` (DMARC still passes via DKIM).
  `installer apply` now prints the record and HANDOFF lists it. Decide whether the Domains page and the DNS wizard should also list and apply it
  (`test/returnpath_test.go` currently asserts it is absent).
- [ ] **T3.3 (P1)** Profile switch without a 30 s window. The worker re-reads the active profile every 30 s; the fix holds waiting mail back 35 s.
  Better: make the worker notice a switch immediately (for example compare the profile revision on each claim) and drop the delay.
- [ ] **T3.4 (P2)** `njiwa-mail installer status` as a non-root user prints "permission denied" on the receipt file. Say "run as root".
- [ ] **T3.5 (P2)** `scripts/restore.sh` expects the Compose volume for `DATA_DIR` to exist; the first restore on a fresh host finds none. Create it or
  say so in `docs/OPERATIONS.md`. Add a CI or lab check for `backup.sh` and `restore.sh` (none exists today).
- [ ] **T3.6 (P2)** New projects start with recipient limits of 3 per minute and 10 per hour. Correct for abuse protection but surprising in tests and demos;
  show the limits on the project page and in the quickstart docs.
- [ ] **T3.7 (P2)** After a file-level queue restore Postfix renames queue files ("QUEUE FILE NAMES WERE CHANGED TO MATCH INODE NUMBERS"); queue ids change,
  so messages lose their `mta_queue_id` link. Document it; consider re-linking by Message-ID from the log collector.
- [ ] **T3.8 (P2)** The preflight cannot see public DNS when `/etc/hosts` maps the host's own name to `127.0.1.1`; it now says "cannot confirm". Optionally
  query the DNS servers directly so it can confirm.
- [ ] **T3.9 (P2)** Existing-SMTP profiles trust only the system roots. An operator with a private CA must mount a bundle and set `SSL_CERT_FILE` (the lab did).
  Either add an optional CA field to the profile or document this in `docs/MAILCOW_SETUP.md`/`INTEGRATION.md`.

## 4. Real-world evidence (P0): needs external infrastructure, cannot be simulated

Do these with the owner. Nothing here may be marked done from lab results.

- [ ] **T4.1 G05** Managed mode on a fresh public VPS: Gmail and Outlook show `spf=pass`, `dkim=pass header.d=<domain> header.s=<selector>`, `dmarc=pass`;
  headers saved in `docs/evidence/G05.md` with addresses removed; "Check signature" records the DKIM proof.
- [ ] **T4.2 E06** Delivery to three or more real providers; queue survives a reboot and `apt-get upgrade postfix` on the real host.
- [ ] **T4.3 E07 / F06 / D04** A real bounce from Gmail to `does-not-exist-xyz@gmail.com`: DSN reaches the bounce address, `correlated_by = verp`, recipient suppressed,
  `email.bounced` webhook delivered over real HTTPS.
- [ ] **T4.4 E05** HELO equals the PTR as seen by Gmail; decide on a publicly trusted certificate for inbound port 25 (installer flag `-smtpd-cert/-smtpd-key`).
- [ ] **T4.5 E10** Queue controls on the real host with outbound 25 temporarily blocked.
- [ ] **T4.6 G04** Existing SMTP for real: a generic Postfix/Exim server and Mailcow, with headers recorded and the Domains page asking for the right SPF/DKIM.
- [ ] **T4.7 G05 step 15** HTTPS with nginx and certbot (`deploy/nginx.conf`); ACME automation for the dashboard certificate.
- [ ] **T4.8 G03/G02** Restore drill and `nmap` check on the real host (the lab proved the procedure, not the host).
- [ ] **T4.9 G09** Someone who did not write the code installs both modes from a clean server using only the docs; every stuck point is written down and fixed.
- [ ] **T4.10 G10** Fill the compatibility table in `docs/RELEASE_REPORT.md` from T2.2 and T4.1, then re-decide the release status honestly.
- [ ] **T4.11** External penetration test.
- Needs: a VPS with port 25 open and a PTR, a throwaway domain, your Gmail and Outlook mailboxes. Checklist: `docs/INFRA_VALIDATION.md`.

## 5. DNS setup (Cloudflare) (P1)

Built and tested against fakes only (`docs/DNS_SETUP.md`).
- [ ] **T5.1** Run the wizard against a real Cloudflare account and one throwaway zone (token scoped to that zone only); record what differs from the fake.
- [ ] **T5.2** Register a Cloudflare OAuth client (the OAuth flow is built but needs an operator-registered client id).
- [ ] **T5.3** Domain Connect is not built (the flag is always false); decide whether to build it.
- [ ] **T5.4** Access is per project, not per user; decide whether per-user connections are needed.
- [ ] **T5.5** Native-speaker review of the Kiswahili in `frontend/src/features/i18n/sw-dns.ts`.

## 6. Public documentation portal (P1)

Built and tested (`documentation/`, `docs/public-portal/`), not published anywhere.
- [ ] **T6.1** Choose a host and domain; set `DOCS_BASE_URL`; deploy with `docs/public-portal/DEPLOY.md`; run `documentation/scripts/smoke.sh` against it.
- [ ] **T6.2** Check the "Open in ChatGPT / Scira / T3 Chat" links in a real browser (the `q` prefill formats came from community sources, not the providers);
  Claude's link copies the prompt instead.
- [ ] **T6.3** A second documentation version so the version selector has something to switch (the logic is unit-tested with two).
- [ ] **T6.4** Remove `'unsafe-inline'` from the docs CSP if the Next.js export can be given script hashes or a nonce.
- [ ] **T6.5** Screen-reader and phone pass, and a cold read of the quickstart by someone new; run a coding agent against `skills/njiwa-mail-integration`.
- [ ] **T6.6** Documentation-only MCP tools (planned page `planned/documentation-mcp`); only after the baseline above is accepted.
- [ ] **T6.7** The brief's other package documents (`07_IMPLEMENTATION_BACKLOG.md` and others) were never supplied; reconcile the stages with them when available.

## 7. Dashboard and UX (P1)

- [ ] **T7.1** Screen-reader and phone pass of the dashboard (B07); native-speaker review of the Kiswahili (written by an AI assistant).
- [ ] **T7.2** After a new domain is verified in managed mode, show "run the installer again to create its DKIM key" (today a manual step; no hand-off
  from "domain verified" to "key generated").
- [ ] **T7.3** `INBOUND_ALLOWED_SOURCES` must be set by hand; derive or check it from the managed install and warn when wrong.

## 8. Repository housekeeping (P2)

- [ ] **T8.1** `docs/REMAINING_WORK.md` and `STATUS.md` still name the deleted branch `claude/happy-brahmagupta-pbfu6r`; point them at `dev`/`main` and
  fold their open rows into this file or delete the duplicates.
- [ ] **T8.2** `lab/qemu` scripts are kept in the repo but the VMs and images were deleted; decide whether to keep the lab in the repo.
- [ ] **T8.3** Add `dev` to the CI triggers in `.github/workflows/ci.yml` (check what branches it listens to) and consider branch protection on `main`.
- [ ] **T8.4** Update `HANDOFF.md` after each item above; it is the source of truth for the next session.

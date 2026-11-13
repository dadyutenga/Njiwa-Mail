# Security review and regression matrix (F05)

**Date:** 2026-11-12 · **Scope:** the API (`/v1`, `/admin/v1`, `/console`), the worker's outbound connections, the managed
installer's Postfix/OpenDKIM configuration and stored secrets.

Each row is a property the code must keep, and the test that fails if it stops holding. "Matrix" tests enumerate the
application's own route table, so a route added later without protection fails CI without anyone remembering to add a test.

## 1. Authentication and CSRF (route matrix)

`TestEveryRouteRequiresAuthAndCSRF` (`test/security_matrix_test.go`) walks every route Fiber has registered (194 today) and:

| Group | Check |
|---|---|
| `/admin/v1/*` (including every workspace route `/admin/v1/w/:project/:mode/...`) | no session → `401` |
| `/admin/v1/*` writes (POST/PUT/PATCH/DELETE) | valid session cookie but no CSRF header → `403 csrf_token_invalid` |
| `/v1/*` | no API key → `401` |
| `/console/*` | no console session → refused |
| anything else | the test fails until the route is put in a group or in the public list with a reason |

Public by design, each with its reason in the test's `publicRoutes`: `POST /admin/v1/auth/login|forgot|reset`,
`POST /console/login`, `GET /health/live|ready`, `GET /metrics` (bearer token, 404 without it — `TestMetricsOverviewAndHealth`),
and the signed unsubscribe link `GET|POST /v1/unsubscribe/:token`.

Related, pre-existing: role boundaries and audit (`TestAdminRolesCSRFAndAudit`, `TestAuthorizationBoundaries`), login throttling
with generic errors (`TestLoginThrottlingAndGenericErrors`), the console is metadata-only and off without a password
(`TestConsoleIsMetadataOnly`, `TestConsoleDisabledWithoutPassword`).

## 2. Secrets

| Property | Test |
|---|---|
| No `GET` route of the operator API (75 today; 56 answer `200` with data in the test) returns an SMTP password, the Mailcow API key, or `PRIVATE KEY` material, with a Mailcow server, an extra profile and a managed install all configured | `TestNoSecretInAnyAdminResponse` |
| Secrets are sealed at rest; blank saves keep the stored secret; switching provider deletes Mailcow API settings | `TestMailServerSettingsSecrets`, `TestProviderTypeIsOptIn`, `TestProfileUpdateRetireAndLegacyMirror` |
| Managed client key is stored sealed, never in clear | `TestManagedInstallRecordingAndActivation` |
| Secrets are never logged or returned | `TestSecretsNeverLoggedOrReturned`; in addition the whole integration suite's output (`go test ./test -v`, 153 log lines) was searched for every test secret: no match |

## 3. Outbound connections (SSRF)

| Path | Rule | Test |
|---|---|---|
| Webhooks | never to loopback, private, link-local, CGNAT or other special ranges; checked on the address actually dialled | `TestWebhookSSRF` |
| SMTP connection test | never to link-local (cloud metadata `169.254.169.254`), multicast or unspecified; dials the vetted address, so DNS rebinding cannot redirect it | `TestDiagnoseReportsTypedProblemCodes` |
| SMTP **sends** (new in this review) | the same rule in a `net.Dialer` `Control` hook, which runs on the connected address after DNS; a refused target is `smtp_host_forbidden` | `TestSendRefusesForbiddenTargets` |
| Sending facts | egress IPs must be public (no loopback/private/link-local) | `TestSendingFactsDriveSPFAndDKIM` |

Loopback and private ranges stay allowed for SMTP on purpose: a relay on the same host or private network is a normal deployment,
and only an administrator can set the mail server.

**Finding fixed in this review:** before, only the connection test applied the link-local guard; the worker's real sends dialled
whatever the saved host resolved to.

## 4. Mail relay (managed mode)

Verified against real Postfix 3.8.6 and OpenDKIM 2.11.0 (`go test -tags managed_e2e ./internal/installer/managed`):

| Property | Test |
|---|---|
| Port 25 is not an open relay (`554 5.7.1` for a foreign recipient) | `TestE2ENoOpenRelayOnPort25` |
| Submission needs the worker's client certificate (mutual TLS) | `TestE2ESubmissionNeedsTheClientCertificate` |
| Envelope senders are limited to claimed domains | `TestE2EForeignEnvelopeSenderIsRejected` |
| Mail is DKIM-signed, and signing failure refuses mail rather than sending it unsigned | `TestE2EAcceptedMailIsDKIMSigned` (signature verified cryptographically) |

## 5. Abuse limits

Live rate limits per installation and per sending domain (`TestInstallationAndDomainLiveLimits`, `TestParallelFloodIsBounded`), a
bounded Live queue (`TestLiveQueueIsBounded`), the strict domain-claim gate (`TestDomainClaimIsInstallationWide`), and suppression only
for DSNs correlated to our own message (`TestDeliveryEvidenceTimeline`).

## 6. Operator access (F09)

- **Installation-operator permission.** `users.installation_operator` (migration 0016; every existing user is backfilled `true`, so an
  upgrade removes nobody's access). Installation, transport profile, domain-claim, mail-server, user and settings-write routes answer
  `403 operator_required` to other admins; project work (messages, templates, keys, domains of a project) is unchanged. The first user
  is always an operator and the last enabled operator cannot be demoted or disabled (`last_operator`). A permission change ends that
  user's sessions. Evidence: `TestInstallationOperatorPermission`, `TestFirstUserIsAlwaysOperator`.
- **Optional TOTP (RFC 6238).** Secret sealed with `ENCRYPTION_KEY`, pending until a code is confirmed; a time step is accepted once
  (replay), five wrong codes lock the second factor for 15 minutes on top of the login throttle; ten recovery codes, single use,
  stored only as keyed hashes; enabling ends other sessions. Evidence: `TestTOTPMatchesRFC6238Vectors`, `TestMFAEnrolLoginReplayAndRecovery`,
  `TestMFALockoutAndDisable`.
- **Policy `require_mfa`.** When on, every admin without a second factor can only enrol or sign out (`403 mfa_enrollment_required`)
  and nobody can switch MFA off. An operator resets another user's lost device (`mfa.reset`, audited, ends their sessions); the CLI
  has `user mfa-reset`. Evidence: `TestRequireMFAPolicyAndOperatorReset`.

### Session invalidation (T1)

| Event | Other sessions | Test |
|---|---|---|
| Password change or reset | ended | `TestAccountChangesEndSessions`, `TestAuthorizationBoundaries` (reset) |
| Account disabled, operator access granted or removed (an unchanged value is not a change) | ended | `TestAccountChangesEndSessions` |
| Two-step sign-in enabled, disabled, or reset by an operator | ended (the enabling session is kept) | `TestMFAEnrolLoginReplayAndRecovery`, `TestRequireMFAPolicyAndOperatorReset` |
| Sign-in with a recovery code | ended; only the new session is valid | `TestRecoveryCodeSignInEndsOtherSessions` |
| Normal sign-in (password, TOTP) | kept | `TestRecoveryCodeSignInEndsOtherSessions` |

Every session row stores the account's `auth_version` when it was issued, and a request is accepted only while it equals the user's.
TOTP checks, replay protection and lockout run in one transaction with the user row locked (`TestMFAConcurrentFailuresLockAccount`).

## 7. Data lifecycle (F10)

- Inbound attachments are sealed with `ENCRYPTION_KEY` (AAD = attachment id, so a file cannot be swapped between attachments); legacy
  plaintext files are converted by the retention job or `attachments encrypt`, writing the sealed copy first and switching the row before
  removing the plaintext. Evidence: `TestAttachmentsAreEncryptedAtRestAndLegacyOnesConverted`.
- Retention for delivery evidence and installer receipts (never the receipt that proves an installation): `TestEvidenceAndReceiptRetention`.
- Export and delete per message and per recipient, including attachment files, bounces and delivery evidence, audited:
  `TestExportAndDeletePerMessageAndRecipient`. The raw spool file is plaintext until parsed (seconds); not covered.

## 8. Key rotation (C02)

`ENCRYPTION_KEY_PREVIOUS` lets a process open data sealed with an older key (it never seals with it); `njiwa-mail rekey` re-seals every
sealed column and attachment file, reports values no key can open without touching them, and a schema test keeps the column list
complete. Evidence: `TestEncryptionKeyRotation` (every kind of sealed value readable with both keys, converted, and readable with only
the new key: MFA login, attachment download, queued payload sent, webhook secret), `TestRekeyReportsUnreadableAndChangesNothing`,
`TestEverySealedColumnIsRekeyed`, `TestPreviousKeysOpenButNeverSeal`. `HMAC_KEY` rotation is not supported.

## 9. Bounce correlation (F06)

Return paths are signed (`bounces+<id>.<HMAC-64>@domain`). A valid signature wins over headers; an invalid one is never trusted and its
headers are ignored; no signature falls back to the header method; suppression still needs the report's recipient to be the message's
own. Evidence: `TestSignedReturnPathCorrelation` (envelope sender, correlation, header mismatch, forged tags, fallback, unrelated
recipient, routing), installer test for `recipient_delimiter`.

## 10. Mail queue agent (E10)

Root-level actions live in a separate host process, not in the API: Unix socket (root:group 0660), bearer token compared in constant time,
five fixed commands without a shell, queue ids validated before use, one command at a time. The API side is operator-only, audited, masks
addresses, and requires the queue id typed back to delete. Evidence: `TestAgentRequiresTheToken`, `TestAgentRunsOnlyTheFixedCommands`
(injection attempts never reach a command), `TestQueueIDValidation`, `TestAgentStatusAndSocketClient`, `TestMTAQueueControls`.
Not yet exercised against a real Postfix (needs the managed host, E06).

## 11. Return-path domains (D04)

A return-path domain must equal the sending domain or be a subdomain of it, so no project can capture bounces for a name it does not hold; it
may not be the inbound domain or the default bounce domain. Changing it withdraws the SPF/DMARC pass until rechecked. Evidence:
`TestPerDomainReturnPath` (alignment before and after, validation, cross-project refusal, routing, envelope sender plain and signed, signed
DSN correlation), `TestSeveralBounceDomains`.

## 12. Provider boundary (C05)

The Mailcow API is reached only through `provider.Provider`. Each write (domain, DKIM key, relay transport) records `provider.mailcow.<op>`
in the audit log with the object's state before and after (existence, activity, a fingerprint of the DKIM record), whether anything
changed, the actor, and a bounded error text from which the API key is removed. The key and the public DKIM value are never stored in
audit details. Evidence: `TestProviderWritesAreAuditedWithBeforeAndAfter` (includes a scan of the whole audit log for the key and an SMTP
password), `TestMailcowAdapterReadsAndWrites`.

## Not covered yet

- An external penetration test and the dependency scan (`govulncheck` runs in CI but has not yet had a green run on GitHub).

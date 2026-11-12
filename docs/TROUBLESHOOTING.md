# Troubleshooting

Problems are reported with a stable **code**: in the dashboard (Installation, Delivery infrastructure), in `GET /admin/v1/installation` (`problem_code`) and in `njiwa-mail preflight`. The fix text below is the one the dashboard shows; it is kept in `frontend/src/features/installation/Installation.tsx` (`problemFixes`). Codes returned by API calls are in the second half.

## Readiness and connection problems

### Choosing and testing a delivery mode

| Code | What to do |
|---|---|
| `TRANSPORT_NOT_SELECTED` | Choose how this installation sends mail. |
| `SMTP_CONFIG_INCOMPLETE` | Enter the SMTP host, port, encryption, username and password, then save. |
| `CHECK_NOT_RUN` | Run the check. |
| `CHECK_STALE` | The settings changed or the result is old. Run the check again. |
| `CHECK_FAILED` | Fix the problem shown in the test result, then run the check again. |
| `TRANSPORT_FAILING` | Live mail is not reaching the mail server right now. Messages stay queued and are retried, nothing is lost. Run the connection test to see what is wrong. |

### Existing SMTP server: connection test

| Code | What to do |
|---|---|
| `SMTP_EGRESS_BLOCKED` | Outbound port 25 is blocked. Ask your VPS provider to open it, or connect an existing mail server instead. |
| `SMTP_DNS_FAILED` | The host name does not resolve. Check the spelling, or its DNS record. |
| `SMTP_TCP_FAILED` | Could not connect. Check the port, and that a firewall allows this server to reach it. |
| `SMTP_TLS_FAILED` | TLS failed. Use port 587 with STARTTLS or port 465 with TLS, and check the server's certificate matches its host name. |
| `SMTP_LOGIN_FAILED` | The login was rejected. Check the username and enter the password again. |
| `SMTP_AUTH_FAILED` | The login was rejected. Check the username and enter the password again. |
| `SMTP_SENDER_FAILED` | This login may not send as the test sender. Allow it on your mail server, or use another sender. |
| `SMTP_DNS_NOT_FOUND` | The host name does not resolve. Check the spelling, or its DNS record. |
| `SMTP_HOST_FORBIDDEN` | The host resolves to a link-local or metadata address. Enter the mail server's real host name. |
| `SMTP_TCP_REFUSED` | Nothing is listening on that port. Check the port number and that the mail server is running. |
| `SMTP_TCP_TIMEOUT` | No answer on that port. A firewall or your hosting provider may block it; ask them to allow it. |
| `SMTP_CONNECTION_FAILED` | Could not connect. Check the port, and that a firewall allows this server to reach it. |
| `SMTP_TLS_MODE_MISMATCH` | The encryption setting does not match the port. Use STARTTLS with port 587, or TLS with port 465. |
| `SMTP_STARTTLS_UNSUPPORTED` | The server does not offer STARTTLS on this port. Use port 465 with TLS, or enable STARTTLS on the server. |
| `SMTP_TLS_CERT_INVALID` | The server's certificate is not trusted or does not match its host name. Use the host name on the certificate. |
| `SMTP_SERVER_TEMP_REFUSED` | The server is temporarily refusing mail. On a managed host this usually means the DKIM signer is stopped: start opendkim (systemctl start opendkim) and test again. |
| `SMTP_NO_GREETING` | The server did not answer like a mail server. Check the host and port. |
| `SMTP_AUTH_MISSING` | Enter the SMTP username and password. |
| `SMTP_AUTH_REJECTED` | The login was rejected. Check the username and enter the password again. |
| `SMTP_SENDER_REJECTED` | This login may not send as the test sender. Allow it on your mail server, or use another sender. |
| `SMTP_OUTCOME_UNKNOWN` | The server may or may not have accepted the test. Check its logs before testing again; nothing is resent automatically. |
| `SMTP_TLS_VERIFICATION_FAILED` | The server's certificate is not trusted or does not match its host name. Use the host name on the certificate. |

### Managed mode (your own VPS)

| Code | What to do |
|---|---|
| `INSTALLER_NOT_AVAILABLE` | Run the host installer on this server as root: njiwa-mail installer plan, then installer apply -approve <digest> -record. Until then, mail cannot be sent directly from this VPS. |
| `CLIENT_CERT_EXPIRING` | On the mail host run the installer again (njiwa-mail installer plan, then installer apply -record with the new digest). It renews the certificate and records it; mail keeps flowing during the change. |
| `CLIENT_CERT_EXPIRED` | On the mail host run njiwa-mail installer plan -renew-certs, then installer apply -record with the new digest. Live mail stays queued until the new certificate is recorded. |
| `HOST_OS_UNSUPPORTED` | Use Debian 12 or Ubuntu 22.04/24.04 for managed delivery, or connect an existing mail server. |
| `HOST_NO_SYSTEMD` | Managed delivery needs systemd to run the mail service. |
| `HOST_DISK_LOW` | Free at least 2 GiB and 100,000 inodes for the mail queue and logs. |
| `HOST_MEMORY_LOW` | Provide at least 512 MiB of free memory. |
| `HOST_CLOCK_NOT_SYNCED` | Enable NTP time synchronisation (timedatectl set-ntp true). |
| `HOST_IP_NOT_LOCAL` | The sending IP is not on this host. Check OUTBOUND_IP, or confirm with your provider that mail leaves from it. |
| `HOSTNAME_NO_ADDRESS` | Add an A record for the mail hostname pointing at the sending IP. |
| `PTR_MISSING` | In your VPS provider's panel, set the reverse DNS (PTR) of the sending IP to the mail hostname. |
| `PTR_MISMATCH` | The reverse DNS names another host. Change the PTR of the sending IP to the mail hostname. |
| `FCRDNS_FAILED` | The mail hostname must resolve back to the sending IP. Fix its A record. |
| `MTA_CONFLICT` | Another mail server runs on this host. Remove it, or connect it as an existing mail server instead. |
| `PREFLIGHT_PARAMETERS_MISSING` | Set MAIL_HOSTNAME and OUTBOUND_IP, then run the preflight again. |

### Bounces

| Code | What to do |
|---|---|
| `BOUNCES_NOT_CONFIGURED` | Set BOUNCE_ADDRESS to an address on a domain whose MX reaches Njiwa-Mail (your mail server relays it to the inbound listener), and run the inbound role. Sending works without it, but hard bounces are not suppressed automatically. |
| `BOUNCE_LISTENER_DOWN` | Start the inbound role (docker compose --profile inbound up -d) so bounce reports can be received. |

## API error codes

Errors are `{"error": {"code": "...", "message": "..."}}`. The HTTP status is in the first column.

### Sending mail (`/v1`)

| Status | Code | Meaning and fix |
|---|---|---|
| 401 | `invalid_api_key` | The key is wrong, revoked, expired, or from the other mode. Create or rotate a key. |
| 403 | `missing_scope` | The key lacks the scope (`emails:send`, `emails:read`, `inbound:read`). |
| 403 | `ip_not_allowed` | The key has an IP allowlist and the request came from another address. |
| 403 | `sender_not_approved` | The From address is not an approved sender of this environment. Add it under Senders. |
| 403 | `project_disabled` | The project or environment is switched off. |
| 403 | `domain_not_registered` | Live mail needs a registered sending domain. Add and verify it (temporary bridge: `ALLOW_UNREGISTERED_LIVE_DOMAINS`). |
| 403 | `domain_claimed_elsewhere` | Another project holds this domain (or it needs operator review). An operator transfers it under Domain ownership. |
| 403 | `domain_not_ready` | The domain's ownership, SPF, DKIM or DMARC check is not passing. Open the domain and run Check now; after changing the return-path domain it must be rechecked. |
| 422 | `recipient_suppressed` | The recipient hard-bounced or complained. Remove the suppression only if the reason no longer applies. |
| 409 | `idempotency_conflict` | The same Idempotency-Key was used with a different request. |
| 429 | `rate_limited` | A send limit was hit; wait for `Retry-After`. |
| 503 | `queue_full` | The Live backlog limit is reached; nothing was stored. Retry later. |
| 413/422 | `body_too_large`, `attachments_too_large`, `too_many_recipients` | Reduce the body, attachments (5 MB total) or recipients (10). |

### Admin API (`/admin/v1`)

| Status | Code | Meaning and fix |
|---|---|---|
| 401 | `unauthenticated` | No or expired session. Sign in again. |
| 401 | `invalid_credentials` | Wrong email or password (or disabled account); the answer is the same on purpose. |
| 401 | `mfa_required` | The account uses two-step sign-in: send `code` (authenticator code or a recovery code) with the same login request. |
| 401 | `invalid_mfa_code` | The code is wrong, expired, or already used. |
| 429 | `mfa_locked`, `too_many_attempts` | Too many wrong codes or logins; wait the time in `Retry-After`. |
| 403 | `csrf_token_invalid` | A write needs the `X-CSRF-Token` returned at login. |
| 403 | `operator_required` | Installation-level action; ask an installation operator or have the permission granted. |
| 403 | `mfa_enrollment_required` | The operator requires two-step sign-in and this account has none: enrol on the Profile page. |
| 409 | `mfa_required_by_policy` | MFA cannot be turned off while the operator requires it. |
| 409 | `last_operator`, `last_user` | The last enabled operator/user cannot be demoted or disabled. |
| 409 | `mode_switch_requires_plan`, `pending_jobs_require_decision`, `resolve_unknown_first` | Changing delivery mode goes through the activation plan; acknowledge pending mail, and resolve unknown messages first. |
| 409 | `managed_not_installed`, `managed_mode_not_selected`, `mail_server_not_configured` | Run the host installer (managed) or save the SMTP settings first. |
| 409 | `profile_active`, `profile_not_smtp` | The active profile cannot be retired; only SMTP profiles have SMTP settings. |
| 409 | `message_active` | A queued, retrying, scheduled or unknown message cannot be deleted; cancel or resolve it first. |
| 422 | `confirmation_required` | Deleting from the mail queue needs the queue id typed back (`confirm`). |
| 409 | `mta_agent_not_configured` | Set `MTA_AGENT_SOCKET`/`MTA_AGENT_TOKEN` and run `njiwa-mail mta-agent` on the host. |
| 503 | `mta_agent_unreachable` | The queue agent is stopped or the socket is not mounted into the API container. |
| 502 | `mta_agent_rejected_token`, `mta_agent_failed` | Token mismatch between API and agent, or `postqueue`/`postsuper` failed (see the agent's log). |
| 422 | `return_path_not_under_domain` | A return-path domain must be the domain or a subdomain of it. |
| 422 | `return_path_is_default`, `return_path_conflicts_inbound` | Use the installation's default by clearing the field; the inbound domain cannot be a return path. |
| 409 | `bounces_not_configured` | Set `BOUNCE_ADDRESS` before naming per-domain return paths. |
| 422 | `unsupported_schema_version` | A webhook schema version this server does not offer (see WEBHOOKS.md). |
| 409 | `mailcow_api_not_configured` | Enter the Mailcow API URL and key, or follow the manual steps. |

### Command line

| Message | Meaning |
|---|---|
| `rekey` exits 3 with `-check` | Some values still use a previous key: run `rekey`. |
| `rekey: N values cannot be opened with any configured key` | Add the key that sealed them to `ENCRYPTION_KEY_PREVIOUS`. |
| `installer apply` refuses `-approve` | The plan changed since you read it; run `installer plan` again and approve the new digest. |
| `installer dkim-rotate activate`: "mail would fail DKIM" | A new DKIM record is not published yet; the dashboard's Mail host page shows which. |

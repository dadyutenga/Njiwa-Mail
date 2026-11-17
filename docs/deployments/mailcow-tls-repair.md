# Mailcow SMTP certificate diagnosis and proposed repair

Read-only inspection on rida-server, 2026-11-16. No Mailcow configuration, files
or services were changed or restarted. Applying the procedure below requires the
owner's explicit approval and a maintenance window.

## Evidence

Both STARTTLS on 587 and implicit TLS on 465 serve the same self-signed Mailcow
certificate: CN `mail.ditronics.co.tz`, no Subject Alternative Name extension.
Valid June 10, 2026 through June 10, 2027. SHA-256 fingerprint:

`A4:90:F5:7D:44:75:08:92:2F:8E:06:C9:D9:20:F0:3D:59:8D:1E:DE:29:20:FF:12:49:A5:70:11:BA:08:9B:66`

It matches `/opt/mailcow-dockerized/data/assets/ssl/cert.pem`, mounted into
Postfix as `/etc/ssl/mail`. `MAILCOW_HOSTNAME=mail.ditronics.co.tz` and
`SKIP_LETS_ENCRYPT=y`. This explains the legacy Common Name error: Go requires
SAN hostname coverage. The certificate also lacks public trust; merely adding a
SAN to another self-signed certificate will not resolve both problems.

The host reverse proxy has a separate Let's Encrypt YE1 certificate at
`/etc/letsencrypt/live/mail.ditronics.co.tz/fullchain.pem`, with the correct DNS
SAN and October 8, 2026–January 6, 2027 validity. Its fingerprint is:

`10:F0:79:6A:01:8E:F2:3B:49:6F:A5:A6:C8:CF:0C:BD:6C:92:34:C5:D6:5E:E5:EE:CF:21:31:1F:FB:8C:53:B6`

HTTPS success does not prove that SMTP serves this certificate.

## Safe corrective procedure (not executed)

1. Recheck the current certificates and expiry, hostname coverage, full-chain
   trust, and that the corresponding private key has the same public-key digest.
   Never display or transmit the private key. Verify the hostname resolves to the
   SMTP origin; an HTTP reverse proxy does not terminate SMTP STARTTLS.
2. Back up Mailcow's current certificate/key together in a root-only directory,
   recording ownership/modes and fingerprints. Confirm the current installed
   Mailcow compose service names and existing certificate renewal arrangement.
3. Use the existing external ACME certificate lifecycle. Stage copies of the
   validated full chain and matching private key in Mailcow's SSL directory,
   preserving required ownership and restricted key permissions. Replace
   `cert.pem` and `key.pem` together. Do not use symlinks into `/etc/letsencrypt`:
   their targets are outside Mailcow's certificate mount.
4. Only after explicit approval, restart the affected Mailcow services in its
   compose project: `postfix-mailcow`, `dovecot-mailcow`, `nginx-mailcow`.
   Do not restart the entire stack or change submission ports/Postfix routing.
5. Verify the served certificates on 587, 465 and 993 with hostname verification,
   system CA trust and verification errors treated as failures. Re-run Njiwa's
   five checks and send a controlled test message. If service health regresses,
   restore the backed-up pair and restart only those affected services.
6. Install and test an external ACME renewal deploy hook that validates and copies
   renewed certificate/key material and restarts affected services, with logging
   and failure reporting. Do not enable a competing ACME client while maintaining
   the existing external renewal owner.

This follows [Mailcow's own-certificate procedure](https://docs.mailcow.email/post_installation/firststeps-ssl/#how-to-use-your-own-certificate).
Never set InsecureSkipVerify, disable certificate verification, or change to
plaintext SMTP to bypass this failure.

Read-only verification example from the server:

```sh
openssl s_client -starttls smtp -connect 127.0.0.1:587 \
  -servername mail.ditronics.co.tz -verify_hostname mail.ditronics.co.tz \
  -verify_return_error </dev/null
```

Repeat from the Njiwa deployment network using the configured SMTP hostname to
also validate DNS/network reachability. No successful authentication or end-to-end
production email delivery was asserted by this certificate inspection.

## Repair applied with owner approval — 2026-11-17

After explicit approval, validated the existing external Let's Encrypt certificate
chain, hostname/SAN, remaining validity, and matching certificate/private-key
public digests. Installed copies of the full chain and matching key, preserving
existing ownership and modes. Original pair and permissions are backed up in the
root-only `/opt/mailcow-certificate-backup-zMdGc6fm` directory on rida-server.

Restarted only Mailcow `postfix-mailcow`, `dovecot-mailcow`, `nginx-mailcow`.
Verified STARTTLS 587, implicit TLS 465 and IMAP TLS 993 on the server using system
CA trust, hostname validation and fatal verification errors. All three services
are running and the Njiwa readiness endpoint returns ready.

Installed root-only `/usr/local/sbin/sync-mailcow-certificate` and the Certbot
copy `/etc/letsencrypt/renewal-hooks/deploy/mailcow-certificate-sync`. The hook
filters the certificate lineage, validates chain/SAN/expiry/key matching, locks
concurrent runs, backs up replaced pairs, preserves file permissions, restarts
only affected services and verifies all three ports. It restores the original
pair if deployment verification fails. Re-running it against the current pair
was a successful no-op; the unrelated Njiwa certificate lineage was ignored.
The existing certbot timer is active. No forced certificate issuance was needed.
SMTP authentication and actual email delivery still require the user's mailbox
credentials and are separate from these successful certificate checks.

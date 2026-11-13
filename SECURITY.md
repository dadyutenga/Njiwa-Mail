# Security policy

Njiwa Mail sends mail, holds API keys and DKIM private keys, and its installer changes servers, so security reports are taken
seriously.

## Reporting a vulnerability

**Please do not open a public issue.** Report it privately through GitHub:
[Security → Report a vulnerability](https://github.com/dadyutenga/Njiwa-Mail/security/advisories/new).

Include what you found, how to reproduce it, the version or commit, and the impact you expect. You will get an acknowledgement,
a fix or a plan, and credit in the advisory if you want it.

## In scope

- The API and admin API (authentication, sessions, CSRF, API key handling, tenant isolation between projects).
- Sending: open relay, sender spoofing across projects or domains, header injection.
- Webhooks (signature, SSRF controls) and inbound mail parsing.
- The managed installer and discovery: anything that could change a host without an approved plan, or expose Postfix or OpenDKIM.
- Secrets at rest (encryption keys, DKIM keys, SMTP credentials).

## Supported versions

Njiwa Mail is pre-1.0. Fixes go to the `dev` branch and the next release; run the latest release.

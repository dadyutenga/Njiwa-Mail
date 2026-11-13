# Playbook: managed Postfix vs existing SMTP

Same application API, different operator setup.

| | Managed Postfix | Existing SMTP |
|---|---|---|
| Needs | Debian 12 / Ubuntu 22.04 or 24.04, systemd, public IPv4, PTR, outbound port 25, no other mail server | SMTP host, port 587 STARTTLS or 465 TLS, dedicated submission account |
| Setup | `njiwa-mail preflight`, then `installer plan` and `installer apply -approve <digest> -record` (root, on the host) | Enter the settings in the dashboard, run the connection test (`njiwa-mail smtp-check`) |
| DKIM/SPF | Generated | Entered by the operator so they are checked |

Status of managed mode: implemented, not yet verified on a clean production VPS. Mailcow is one optional example of an existing SMTP server; never require it. Do not run the managed installer on a host with another mail server.

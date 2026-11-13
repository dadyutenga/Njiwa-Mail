# Playbook: DNS and domain readiness

Live sending needs a verified sending domain. Diagnose in this order, using the dashboard's Domains page (it shows the exact records and what was found):

1. **Ownership** TXT `njiwa-mail-verify=<token>` at the domain. One project per instance holds a domain (`domain_claimed_elsewhere`).
2. **SPF**: exactly one `v=spf1` record at the name that authorises the sending server. Managed: your VPS. Existing SMTP: the operator must enter the provider's sending IPs or `include:`; until then SPF is a template.
3. **DKIM** at `<selector>._domainkey.<domain>`: the exact public key. A published key does not prove mail is signed.
4. **DMARC** at `_dmarc.<domain>`, start at `p=none`.
5. Managed only: PTR of the sending IP names the mail hostname and resolves back; outbound port 25 works.

API symptoms: `403 domain_not_registered`, `domain_claimed_elsewhere`, `domain_not_ready`. Pending means DNS has not spread or the record differs from the one shown. Ready never guarantees inbox delivery. Mail hostnames must be DNS only (not proxied). Do not edit someone's MX or website records to "make setup pass".

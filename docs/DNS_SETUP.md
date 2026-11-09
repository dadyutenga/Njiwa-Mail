# DNS setup

The service does not change DNS. Publish these records, then use Domains → Verify in the operator UI or `POST /admin/v1/domains/{id}/verify`.

Replace `notify.example.com` with the sending domain and `203.0.113.10` with the Mailcow outbound IP.

1. TXT at `notify.example.com`: `ditronics-mail-verify=<token>` (the token is shown once when the domain is added).
2. TXT SPF: `v=spf1 ip4:203.0.113.10 -all`
3. TXT DKIM at `dkim._domainkey.notify.example.com`: the public key Mailcow generated for selector `dkim`.
4. TXT DMARC at `_dmarc.notify.example.com`: `v=DMARC1; p=none; rua=mailto:dmarc@example.com`
5. MX for the inbound domain `in.example.com`: `10 mail.example.com`
6. PTR for `203.0.113.10`: `mail.example.com`

Live sending from a domain that has been added stays blocked until the TXT challenge, SPF, DKIM and DMARC all pass. A domain that was never added is not blocked, so existing projects keep working until you register the domain.

Mail hostnames must be DNS-only at Cloudflare. The proxy does not carry SMTP.

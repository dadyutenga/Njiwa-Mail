# QEMU/KVM lab evidence (SIMULATED networking, not production validation)

Every file here comes from `lab/qemu/` (Ubuntu 24.04 cloud image in KVM, a second VM as DNS and mail receiver, a private network with no
route to the internet). Verdicts come from software in the lab (Postfix, an independent DKIM/SPF/DMARC verifier written for the lab), not from
Gmail or Outlook. Nothing here replaces the real-host checks in HANDOFF.md section 3. The lab was deleted afterwards; `lab/qemu` can recreate it.

| File | What it shows |
|---|---|
| t01 | reboot and hard power loss with mail queued: services, firewall table, Postfix queue and API recover (19/19) |
| t02 | ports seen from another host, firewall drops proven with listeners, relay refusal, client certificate required, HELO equals PTR (21/21) |
| t03 | real Postfix bounce: DSN, signed return path (`correlated_by = verp`), suppression, webhook row queued (webhook HTTP not verified) |
| t04 | queue agent hold/release/requeue/delete with typed confirmation, audit rows, log-collector evidence (14/14) |
| t05 | backup on one host, restore on a fresh clone: database, queue, DKIM key, units, reboot (13/14; the 14th was a test-script bug) |
| t06 | the managed end-to-end suite on a clean host: 4/4 |
| t07 | manual rollback and automatic rollback of a failed apply (11/11) |
| t08 | existing SMTP (generic server), mode switch both directions: **last full run 16/20 before the fix in f08eff8; not re-run** |

## Defects the lab found (each fixed with a regression test that fails without the fix)
1. Preflight failed on the documented install order (Postfix binary present) and on Ubuntu's `127.0.1.1` hosts entry.
2. The worker replaced the managed profile's private CA with the system roots: every managed send failed `smtp_tls_verification_failed`.
3. Postfix did not start after any reboot when `-submission` is the Docker bridge address (installer now writes a boot-ordering drop-in).
4. Postfix 3.8 bounce reports (`message/global-delivery-status`, RFC 6533) were not parsed: bounces never took effect.
5. `backup.sh`/`restore.sh` needed `pg_dump` on the host and left out the firewall unit and boot-ordering drop-in.
6. No SPF record was listed for the bounce domain (SPF result `none`); `installer apply` now prints it and HANDOFF lists it.
7. After an acknowledged profile switch, waiting mail could still go through the old profile for up to 30 s and exhaust its attempts.

## Not run in the lab
E12 (Debian 12, Ubuntu 22.04 images were still downloading), D04, Mailcow, the 0010 legacy upgrade, a full Go suite re-run after the last two fixes
(the relevant tests passed), and a re-run of t08.

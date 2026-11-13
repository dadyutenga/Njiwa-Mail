#!/usr/bin/env bash
# Installer rollback and automatic rollback on a failed apply (HANDOFF G02/G10 rollback runbook), on the clean host from t06. SIMULATED network.
set -uo pipefail; . "$(dirname "$0")/lib.sh"
mx() { "$LAB/lab.sh" ssh mxf "$@"; }
H="-hostname mailout.example.test -domains notify.example.test -bounce-domain bounce.example.test -selector njiwa"
echo "## t07 rollback ($(date -u +%FT%TZ))"; echo
check "precondition: managed configuration is installed" mx 'sudo grep -q "Managed by the Njiwa-Mail installer" /etc/postfix/main.cf'
mx 'sudo cp /etc/postfix/master.cf /tmp/master.cf.afterapply; sudo njiwa-mail installer status | head -n 1 | cut -c1-120' | sed 's/^/    /'
echo "### A. Manual rollback of the last apply"
mx 'sudo njiwa-mail installer rollback 2>&1 | tail -n 4 | cut -c1-160' | sed 's/^/    /'
check "main.cf written by the installer is gone (it did not exist before the apply)" mx '! test -f /etc/postfix/main.cf'
check "Njiwa firewall unit file removed" mx '! test -f /etc/systemd/system/njiwa-mail-firewall.service'
check "firewall file removed" mx '! test -f /etc/njiwa-mail/firewall.nft'
check "DKIM private key removed" mx '! test -f /etc/opendkim/keys/notify.example.test/njiwa.private'
mx 'sudo njiwa-mail installer status 2>&1 | head -n 2 | cut -c1-140' | sed 's/^/    status: /'
mx 'sudo njiwa-mail installer rollback 2>&1 | tail -n 1 | cut -c1-120' | grep -q 'already rolled back' && ok "a second rollback is refused (already rolled back)" || bad "second rollback not refused"
echo "### B. A failed apply rolls back by itself"
mx 'sudo mkdir -p /etc && sudo touch /etc/opendkim.conf && sudo chattr +i /etc/opendkim.conf'
D=$(mx "sudo njiwa-mail installer plan $H | grep 'Plan digest' | awk '{print \$3}'")
OUT=$(mx "sudo njiwa-mail installer apply $H -approve $D 2>&1 | tail -n 8 | cut -c1-170"); echo '```'; echo "$OUT"; echo '```'
echo "$OUT" | grep -qiE 'fail|rolled|error|denied|immutable|not permitted' && ok "apply failed as provoked (opendkim.conf made immutable)" || bad "apply did not fail"
check "no managed main.cf left behind after the failed apply" mx '! test -f /etc/postfix/main.cf'
check "no installer DKIM key left behind" mx '! test -f /etc/opendkim/keys/notify.example.test/njiwa.private'
mx 'sudo njiwa-mail installer status 2>&1 | head -n 2 | cut -c1-140' | sed 's/^/    status: /'
mx 'sudo chattr -i /etc/opendkim.conf'
echo "### C. A clean apply works again after both rollbacks"
D=$(mx "sudo njiwa-mail installer plan $H | grep 'Plan digest' | awk '{print \$3}'")
mx "sudo njiwa-mail installer apply $H -approve $D 2>&1 | grep -E 'Result'" | grep -q 'Result: ok' && ok "re-apply succeeds" || bad "re-apply failed"
mx 'systemctl is-active postfix@- opendkim | tr "\n" " "' | grep -q 'active active' && ok "services active again" || bad "services not active"
summary

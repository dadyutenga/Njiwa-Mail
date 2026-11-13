#!/usr/bin/env bash
# Service persistence, restart recovery and queue durability (HANDOFF E06 durability items, G05 systemd path). SIMULATED network.
set -uo pipefail; . "$(dirname "$0")/lib.sh"
block()   { mx 'sudo nft add table inet njiwa_block25 && sudo nft add chain inet njiwa_block25 out "{ type filter hook output priority 0; policy accept; }" && sudo nft add rule inet njiwa_block25 out tcp dport 25 reject with tcp reset && sudo systemd-run --quiet --on-active=40m nft delete table inet njiwa_block25'; }
unblock() { mx 'sudo nft delete table inet njiwa_block25' 2>/dev/null; }
qlen()    { mx 'sudo postqueue -p | grep -c "^[0-9A-F]\{8,\}[*!]\?\s"'; }
echo "## t01 persistence and restart recovery ($(date -u +%FT%TZ))"; echo; echo "Mode: QEMU/KVM guest Ubuntu 24.04, isolated lab network (**simulated**)"; echo
net 'sudo lab-receiver clear'; B=$(inbox_count)
echo "### A. Clean reboot with deferred mail"
block; check "outbound port 25 blocked by a temporary nft table (auto-expires in 40 min)" mx 'sudo nft list table inet njiwa_block25'
for i in 1 2 3; do mx "send alice@rcv-a.test 'durability reboot $i'" >/dev/null 2>&1; done; sleep 6
Q=$(qlen); [ "${Q:-0}" -ge 3 ] && ok "3 messages accepted by Njiwa and held as deferred in Postfix (postqueue: $Q)" || bad "expected 3 deferred, got $Q"
mx 'sudo postqueue -p | grep -E "Connection refused|reset" | head -n 1 | cut -c1-140'
mx 'sudo systemctl reboot' >/dev/null 2>&1; sleep 15; wait_ssh && ok "VM came back after reboot" || bad "VM did not come back"
for u in postfix@- opendkim njiwa-mail-firewall docker; do check "unit $u active after boot" mx "systemctl is-active --quiet $u"; done
check "firewall table inet njiwa_mail loaded at boot" mx 'sudo nft list table inet njiwa_mail'
check "temporary block table is gone after reboot (it was not persistent)" bash -c "! $LAB/lab.sh ssh mx 'sudo nft list table inet njiwa_block25' >/dev/null 2>&1"
for _ in $(seq 1 40); do mx 'curl -fs 127.0.0.1:8080/health/ready' >/dev/null 2>&1 && break; sleep 3; done
check "Njiwa API ready after reboot (compose restart policy)" mx 'curl -fs 127.0.0.1:8080/health/ready'
mx 'cd /opt/njiwa-mail && sudo docker compose ps --format "{{.Service}} {{.Status}}" | cut -c1-60'
Q=$(qlen); [ "${Q:-0}" -ge 3 ] && ok "queue survived the reboot ($Q messages)" || bad "queue lost after reboot ($Q)"
mx 'sudo postqueue -f'; sleep 15; A=$(inbox_count); [ $((A-B)) -ge 3 ] && ok "all 3 delivered to the receiver after the block was gone (inbox +$((A-B)))" || bad "delivered only $((A-B))"
echo; echo "### B. Hard power loss (QEMU process killed, no shutdown)"
B=$(inbox_count); block
for i in 1 2; do mx "send alice@rcv-a.test 'durability powerloss $i'" >/dev/null 2>&1; done; sleep 5
Q=$(qlen); [ "${Q:-0}" -ge 2 ] && ok "2 messages deferred before power loss ($Q)" || bad "expected 2 deferred, got $Q"
"$LAB/lab.sh" kill mx >/dev/null; "$LAB/lab.sh" up mx isolated >/dev/null 2>&1; wait_ssh && ok "VM restarted after power loss" || bad "VM did not restart"
for _ in $(seq 1 30); do mx 'systemctl is-active --quiet postfix@-' 2>/dev/null && break; sleep 2; done
check "Postfix active after power loss" mx 'systemctl is-active --quiet postfix@-'
mx 'sudo postfix check' >/dev/null 2>&1 && ok "postfix check passes after power loss" || bad "postfix check reports problems"
Q=$(qlen); [ "${Q:-0}" -ge 2 ] && ok "queue survived power loss ($Q messages)" || bad "queue lost after power loss ($Q)"
mx 'sudo postqueue -f'; sleep 15; A=$(inbox_count); [ $((A-B)) -ge 2 ] && ok "both delivered after recovery (inbox +$((A-B)))" || bad "delivered only $((A-B))"
for _ in $(seq 1 40); do mx 'curl -fs 127.0.0.1:8080/health/ready' >/dev/null 2>&1 && break; sleep 3; done
check "Njiwa API ready after power loss" mx 'curl -fs 127.0.0.1:8080/health/ready'
mx 'cd /opt/njiwa-mail && sudo docker compose exec -T postgres psql -U mail -d mail -Atc "select state,count(*) from messages group by 1 order by 1"' 2>/dev/null | sed 's/^/    message states: /'
summary

#!/usr/bin/env bash
# Backup on the source host, restore on a fresh host (HANDOFF 3.8 / G03, G02). SIMULATED network; real scripts, real Postfix, real PostgreSQL.
# The fresh host is a clone of the mx "pkgs" snapshot (OS + packages + Docker + repo clone, nothing configured), taking mx's IP and name.
set -uo pipefail; . "$(dirname "$0")/lib.sh"
P() { mx "cd /opt/njiwa-mail; sudo docker compose exec -T postgres psql -U mail -d mail -Atc \"$1\""; }
KIT=/tmp/lab/restore-kit.tgz
echo "## t05 backup and restore on a fresh host ($(date -u +%FT%TZ))"; echo
VARS=/tmp/lab/t05.vars
if [ "${SKIP_A:-0}" = 1 ]; then . $VARS; echo "### A. skipped (re-run of the restore half; source values from the first run: messages=$M0 users=$U0 api_keys=$K0)"; else
echo "### A. Source host: queued mail, backup, copy off the host"
net 'sudo lab-receiver clear'
mx 'sudo nft add table inet njiwa_block25 2>/dev/null; sudo nft add chain inet njiwa_block25 out "{ type filter hook output priority 0; policy accept; }" 2>/dev/null; sudo nft add rule inet njiwa_block25 out tcp dport 25 reject with tcp reset' >/dev/null 2>&1
for i in 1 2; do mx "send alice@rcv-a.test 'restore drill $i'" >/dev/null 2>&1; done; sleep 5
Q=$(mx 'sudo postqueue -j | wc -l'); [ "${Q:-0}" -ge 2 ] && ok "source has $Q deferred messages in the Postfix queue" || bad "queue: $Q"
M0=$(P "select count(*) from messages"); U0=$(P "select count(*) from users"); K0=$(P "select count(*) from api_keys"); echo "    source: messages=$M0 users=$U0 api_keys=$K0"
mx '. /usr/local/share/mxenv; cd /opt/njiwa-mail; sudo rm -rf /root/bk2; export DATA_DIR=$(sudo docker volume inspect njiwa-mail_maildata -f "{{.Mountpoint}}"); BACKUP_QUEUE=1 nmroot env DATA_DIR=$DATA_DIR sh scripts/backup.sh /root/bk2 2>&1 | tail -n 1' | grep -q 'backup written' && ok "backup.sh with BACKUP_QUEUE=1 completed (using pg_dump from the PostgreSQL image: the host has no client tools)" || bad "backup failed"
mx 'sudo tar -C / -czf - root/bk2 opt/njiwa-mail/.env' > $KIT 2>/dev/null; [ -s $KIT ] && ok "backup and the separately kept secrets copied off the host ($(du -h $KIT | cut -f1))" || bad "copy off failed"
mx 'sudo nft delete table inet njiwa_block25' 2>/dev/null
mx 'sudo tar -tzf /root/bk2/managed.tgz | grep -E "njiwa-mail-firewall.service|postfix@.service.d|njiwa-mail/firewall.nft|opendkim/keys" | sort | head -n 6' | sed 's/^/    managed.tgz has: /'
"$LAB/lab.sh" down mx >/dev/null && ok "source host stopped (the fresh host takes over its IP and name)"
echo "M0=$M0 U0=$U0 K0=$K0" > $VARS
fi
echo; echo "### B. Fresh host from the pkgs snapshot (OS, packages, Docker, repository; nothing configured)"
"$LAB/lab.sh" fresh pkgs >/dev/null && "$LAB/lab.sh" up mxf online >/dev/null 2>&1 && ok "fresh host booted" || { bad "fresh host did not boot"; summary; exit 1; }
mx() { "$LAB/lab.sh" ssh mxf "$@"; }
# lab-only conveniences (not part of the product): operator API helper, send helper, env loader
"$LAB/lab.sh" scp "$LAB/guest/nm" "$LAB/guest/send" "$LAB/guest/mxenv" mxf:/tmp/ >/dev/null 2>&1
mx 'sudo install -m755 /tmp/nm /tmp/send /usr/local/bin/; sudo install -m644 /tmp/mxenv /usr/local/share/mxenv'

check "no Postfix configuration exists yet" mx '! test -f /etc/postfix/main.cf'
check "no Njiwa data exists yet" mx '! test -d /etc/njiwa-mail'
tar -C /home/dadi/Projects/ditronics-mail --exclude=.git --exclude=node_modules --exclude=documentation --exclude=lab -czf /tmp/lab/src.tgz .
"$LAB/lab.sh" scp /tmp/lab/src.tgz $KIT mxf:/tmp/ >/dev/null 2>&1
mx 'cd /opt/njiwa-mail && sudo tar -xzf /tmp/src.tgz -C . && sudo tar -xzf /tmp/restore-kit.tgz -C / && sudo docker build -q -t njiwa-mail:local . >/dev/null 2>&1; sudo docker create --name nm-tmp njiwa-mail:local >/dev/null && sudo docker cp nm-tmp:/usr/local/bin/njiwa-mail /usr/local/bin/njiwa-mail && sudo docker rm nm-tmp >/dev/null; sudo docker compose pull -q postgres 2>/dev/null; ls -l /usr/local/bin/njiwa-mail | cut -c1-60' | sed 's/^/    /'
mx 'sudo docker images njiwa-mail:local -q | head -n1' | grep -q . && ok "current code built on the fresh host (online step, as HANDOFF 3.1 step 2)" || bad "build failed"
"$LAB/lab.sh" down mxf >/dev/null; "$LAB/lab.sh" up mxf isolated >/dev/null 2>&1 && ok "fresh host restarted in the isolated lab network" || bad "isolated restart failed"
echo; echo "### C. Restore (scripts/restore.sh as documented)"
mx 'cd /opt/njiwa-mail; sudo docker compose up -d --no-build postgres migrate 2>&1 | tail -n 2 >/dev/null; sudo docker volume create njiwa-mail_maildata >/dev/null; sleep 10' >/dev/null
mx '. /usr/local/share/mxenv; cd /opt/njiwa-mail; export DATA_DIR=$(sudo docker volume inspect njiwa-mail_maildata -f "{{.Mountpoint}}"); nmroot env DATA_DIR=$DATA_DIR sh scripts/restore.sh /root/bk2 2>&1 | tail -n 6 | cut -c1-200'
mx 'systemctl is-active postfix@- opendkim | tr "\n" " "' | grep -q 'active active' && ok "Postfix and OpenDKIM running after restore" || bad "Postfix/OpenDKIM not both active"
mx 'cd /opt/njiwa-mail; sudo docker compose --profile inbound up -d --no-build postgres migrate api worker scheduler inbound 2>&1 | tail -n 1 >/dev/null; for i in $(seq 1 40); do curl -fs 127.0.0.1:8080/health/ready >/dev/null && exit 0; sleep 3; done; exit 1' && ok "Njiwa API ready on the restored data" || bad "API not ready"
M1=$(P "select count(*) from messages"); U1=$(P "select count(*) from users"); K1=$(P "select count(*) from api_keys")
[ "$M1/$U1/$K1" = "$M0/$U0/$K0" ] && ok "database restored: messages=$M1 users=$U1 api_keys=$K1 equal the source" || bad "counts differ: $M1/$U1/$K1 vs $M0/$U0/$K0"
mx 'nm GET /admin/v1/auth/me | head -n1 | cut -c1-90' | grep -q operator@lab.test && ok "the operator can sign in with the restored account" || bad "operator login failed"
echo; echo "### D. Queue and signing continue on the restored host"
sleep 20; B=$(inbox_count); [ "${B:-0}" -ge 2 ] && ok "the 2 restored queued messages were delivered (inbox $B)" || bad "restored queue not delivered (inbox $B)"
mx 'cp /dev/null $HOME/.livekey' ; K=$(mx 'cd /opt/njiwa-mail; sudo docker compose exec -T api njiwa-mail key create -project lab -mode live -scopes emails:send,emails:send_raw,emails:read -label restored 2>&1 | grep -o "dmk_liv_[A-Za-z0-9_-]*"'); mx "echo $K > \$HOME/.livekey"
mx 'send alice@rcv-b.test "after restore"' | tail -n1 | grep -q '"status":"submitted"' && ok "a new message is accepted by the restored managed Postfix" || bad "new message not submitted"
sleep 6; V=$(net 'f=$(sudo ls -1t /home/alice/Maildir/new | head -n1); [ -n "$f" ] && sudo lab-verify /home/alice/Maildir/new/$f | head -n1'); echo "    independent verifier: $V"
echo "$V" | grep -q 'dkim=pass' && ok "restored DKIM private key still matches the published DNS key (dkim=pass)" || bad "DKIM does not verify after restore"
echo; echo "### E. Services persist across a reboot of the restored host"
mx 'sudo systemctl reboot' >/dev/null 2>&1; sleep 15; for _ in $(seq 1 60); do mx true 2>/dev/null && break; sleep 2; done
for _ in $(seq 1 20); do mx 'systemctl is-active --quiet postfix@-' 2>/dev/null && break; sleep 3; done
check "Postfix active after reboot (boot-ordering drop-in restored)" mx 'systemctl is-active --quiet postfix@-'
check "njiwa_mail firewall table loaded after reboot (firewall unit restored and enabled)" mx 'sudo nft list table inet njiwa_mail'
summary

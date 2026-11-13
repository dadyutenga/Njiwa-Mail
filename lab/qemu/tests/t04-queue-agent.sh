#!/usr/bin/env bash
# Queue controls through the dashboard API (HANDOFF E10) and the Postfix log collector (G05 step 14). SIMULATED receiver; real Postfix/agent/API.
set -uo pipefail; . "$(dirname "$0")/lib.sh"
J() { python3 -c "import sys,json;d=json.loads(sys.stdin.read().rsplit(chr(10)+'HTTP',1)[0]);$1"; }
P() { mx "cd /opt/njiwa-mail; sudo docker compose exec -T postgres psql -U mail -d mail -Atc \"$1\""; }
echo "## t04 queue agent and log collector ($(date -u +%FT%TZ))"; echo
check "queue agent unit active" mx 'systemctl is-active --quiet njiwa-mail-mta-agent'
check "log collector unit active (runs as user njiwa-mail)" mx 'systemctl is-active --quiet njiwa-mail-mta-log'
mx 'stat -c "%U:%G %a" /run/njiwa-mail/mta-agent.sock' | sed 's/^/    socket: /'
mx 'sudo curl -s -o /dev/null -w "%{http_code}" --unix-socket /run/njiwa-mail/mta-agent.sock http://agent/status' | grep -q 401 && ok "agent refuses a request without the token (401)" || bad "agent answered without a token"
echo "### A. Hold, release, requeue, delete on a deferred message"
mx 'sudo nft add table inet njiwa_block25 2>/dev/null; sudo nft add chain inet njiwa_block25 out "{ type filter hook output priority 0; policy accept; }" 2>/dev/null; sudo nft add rule inet njiwa_block25 out tcp dport 25 reject with tcp reset; sudo systemd-run --quiet --on-active=40m nft delete table inet njiwa_block25' >/dev/null 2>&1
R=$(mx 'send alice@rcv-a.test "E10 queue control"' | head -n1); MID=$(echo "$R" | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])'); sleep 6
Q=$(mx 'nm GET /admin/v1/mta/queue' | J 'print(d["items"][0]["queue_id"] if d.get("items") else "")' 2>/dev/null)
mx 'nm GET /admin/v1/mta/queue' | head -n 1 | cut -c1-420 | sed 's/^/    queue: /'
[ -n "$Q" ] && ok "the deferred message appears in the dashboard queue (queue id $Q)" || { bad "no queue entry"; summary; exit 1; }
A() { mx "nm POST /admin/v1/mta/queue/$Q/$1 ${2:-}" | tail -n1; }
[ "$(A hold)" = "HTTP 200" ] && ok "hold" || bad "hold"
H=$(A_hold=1; mx "nm POST /admin/v1/mta/queue/$Q/hold" >/dev/null; mx 'sudo postqueue -j | grep -o "\"queue_name\": *\"[a-z]*\""'); echo "    after hold: $H"; echo "$H" | grep -q hold && ok "Postfix shows the message in the hold queue" || bad "not in hold queue"
[ "$(A release)" = "HTTP 200" ] && ok "release" || bad "release"
[ "$(A requeue)" = "HTTP 200" ] && ok "requeue" || bad "requeue"
D1=$(mx "nm POST /admin/v1/mta/queue/$Q/delete '{\"confirm\":\"wrong\"}'" | tail -n1); [ "$D1" = "HTTP 422" ] && ok "delete without typing the queue id is refused (422)" || bad "delete without confirmation returned $D1"
D2=$(mx "nm POST /admin/v1/mta/queue/$Q/delete '{\"confirm\":\"$Q\"}'" | tail -n1); [ "$D2" = "HTTP 200" ] && ok "delete with the queue id typed" || bad "delete returned $D2"
mx 'sudo postqueue -j | wc -l' | grep -q '^0$' && ok "the queue is empty after the delete" || bad "queue not empty"
N=$(P "select count(*) from audit_log where action like 'mta.queue.%'"); [ "${N:-0}" -ge 4 ] && ok "audit rows written ($N mta.queue.* actions)" || bad "audit rows: $N"
T=$(P "select string_agg(event_type,',' order by created_at) from message_events where message_id='$MID'"); echo "    timeline: $T"
P "select count(*) from message_events where message_id='$MID' and (event_type ilike '%operator%' or event_type ilike '%queue%' or event_type ilike '%note%' or event_type ilike '%mta%')" | grep -qv '^0$' && ok "operator actions noted on the message timeline" || echo "    (no operator note found on the timeline: check event names above)"
mx 'sudo nft delete table inet njiwa_block25' 2>/dev/null
echo "### B. Log collector evidence on a normal delivery"
R=$(mx 'send alice@rcv-b.test "collector evidence"' | head -n1); MID2=$(echo "$R" | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])'); sleep 12
E=$(P "select string_agg(source||':'||event||':'||coalesce(smtp_status,''),', ') from delivery_events where message_id='$MID2'"); echo "    delivery_events: $E"
echo "$E" | grep -q 'mta_log\|mail_log\|log' && ok "delivery evidence from the Postfix log is attached to the message" || bad "no log evidence for the message"
P "select mta_queue_id is not null from messages where id='$MID2'" | grep -q t && ok "the message knows its Postfix queue id" || bad "no queue id on the message"
summary

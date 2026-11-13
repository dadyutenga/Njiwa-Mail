#!/usr/bin/env bash
set -uo pipefail; . "$(dirname "$0")/lib.sh"
# with 550 5.1.1 (as Gmail does); everything after that (Postfix DSN, return path, relay to the inbound listener, parsing) is real software.
set -uo pipefail; . "$(dirname "$0")/lab.sh" 2>/dev/null || . "$(dirname "$0")/lib.sh"
P() { mx "cd /opt/njiwa-mail; sudo docker compose exec -T postgres psql -U mail -d mail -Atc \"$1\""; }
echo "## t03 bounce handling ($(date -u +%FT%TZ))"; echo; echo "Receiver: lab 'net' VM (**simulated** provider, rejects unknown users with 550 5.1.1)"; echo
RUN=$(date +%s)
# A fresh endpoint per run: the lab cannot reach a public webhook host, so endpoints auto-disable after 20 consecutive failures (by design).
WH=$(mx 'nm POST /admin/v1/w/lab/live/webhooks "{\"url\":\"https://hooks.njiwa.test/njiwa\",\"events\":[\"email.bounced\"]}" | head -n1' | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')
B0=$(P "select count(*) from webhook_deliveries where event_type='email.bounced'")
R=$(mx "send nobody-$RUN@rcv-a.test 'E07 bounce'" | head -n1); ID=$(echo "$R" | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')
echo "message $ID"
for _ in $(seq 1 30); do EV=$(mx "curl -s -H \"Authorization: Bearer \$(cat \$HOME/.livekey)\" http://127.0.0.1:8080/v1/emails/$ID" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d["status"],",".join(e["type"] for e in d["events"]))' 2>/dev/null); echo "$EV" | grep -q bounced && break; sleep 2; done
echo "    status/events: $EV"
echo "$EV" | grep -q 'bounced' && ok "the message timeline shows a bounced event (the status field stays 'submitted': a bounce is evidence after acceptance, not a state)" || bad "no bounced event: $EV"
T=$(P "select type||' '||status_code||' '||correlated_by from bounces where message_id='$ID'")
[ "$T" = "hard 5.1.1 verp" ] && ok "bounce row: $T (hard, 5.1.1, correlated by the signed return path = F06)" || bad "bounce row: '$T'"
[ "$(P "select count(*) from suppressions where source='hard_bounce' and recipient_hash=(select recipient_hash from messages where id='$ID')")" = 1 ] && ok "recipient suppressed automatically (hard bounce)" || bad "recipient not suppressed"
B1=$(P "select count(*) from webhook_deliveries where event_type='email.bounced'"); [ $((B1-B0)) -ge 1 ] && ok "an email.bounced webhook delivery was queued (rows +$((B1-B0))); HTTP delivery itself is NOT verified in the lab (see notes)" || bad "no email.bounced webhook queued"
mx 'nm GET /admin/v1/installation' | python3 -c '
import sys,json
t=sys.stdin.read().rsplit(chr(10)+"HTTP",1)[0]
s=[x for x in json.loads(t)["readiness"]["steps"] if x["id"]=="bounce_reports"][0]; print("    bounce_reports step:",s["status"],"-",s.get("message",""))'
mx "send nobody-$RUN@rcv-a.test again" | head -n1 | grep -q recipient_suppressed && ok "a second send to the suppressed recipient is refused (recipient_suppressed)" || bad "second send was not refused"
echo "    Postfix log for the DSN path:"; mx 'sudo grep -E "nobody-e07|bounces\+'"${ID//-/}"'" /var/log/mail.log | grep -E "status=" | cut -c28-250 | tail -n 3' | sed 's/^/      /'
mx "nm DELETE /admin/v1/w/lab/live/webhooks/$WH" >/dev/null 2>&1
echo; echo "Notes: webhooks to private addresses are refused by the SSRF guard (verified in A below); a webhook to a public name cannot be reached from the isolated lab, so only creation of the delivery row is evidenced here."
echo "### A. SSRF guard"
mx 'nm POST /admin/v1/w/lab/live/webhooks "{\"url\":\"https://10.77.0.20/hook\",\"events\":[\"email.bounced\"]}" | head -n1' | grep -q 'private or loopback' && ok "webhook to a private address refused" || bad "private webhook accepted"
summary

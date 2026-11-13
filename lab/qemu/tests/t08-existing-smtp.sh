#!/usr/bin/env bash
# Existing SMTP mode with a generic server (HANDOFF G04(b)), the repeatable staging script (C06) and mode switching with a non-empty queue
# (G06), both directions. The "existing server" is the lab Postfix on net:587 (STARTTLS + AUTH, lab CA) - SIMULATED, not a provider.
# This script only uses the public admin API, so it can be pointed at any staging server by changing the SMTP_* values below.
set -uo pipefail; . "$(dirname "$0")/lib.sh"
SMTP_HOST=${SMTP_HOST:-mx.rcv.test}; SMTP_PORT=${SMTP_PORT:-587}; SMTP_USER=${SMTP_USER:-relayuser}; SMTP_PASS=${SMTP_PASS:-relaypass-lab-12345}; SENDER=${SENDER:-hello@notify.njiwa.test}
P() { mx "cd /opt/njiwa-mail; sudo docker compose exec -T postgres psql -U mail -d mail -Atc \"$1\""; }
J() { python3 -c "import sys,json;d=json.loads(sys.stdin.read().rsplit(chr(10)+'HTTP',1)[0]);$1" 2>/dev/null; }
api() { mx "nm $1 $2 ${3:+'$3'}"; }
echo "## t08 existing SMTP, staging script and mode switch ($(date -u +%FT%TZ))"; echo; echo "Existing server: $SMTP_HOST:$SMTP_PORT (**simulated** generic Postfix with SASL; certificate from the lab CA)"; echo
MG=$(api GET /admin/v1/installation/transport-profiles | J 'print([p["id"] for p in d["profiles"] if p["mode"]=="managed_mta" and not p["label"].startswith("old")][0])')
echo "managed profile: $MG"
echo "### A. Create and test an existing-SMTP profile (generic server)"
R=$(api POST /admin/v1/installation/transport-profiles "{\"mode\":\"existing_smtp\",\"label\":\"Lab generic SMTP\",\"provider_type\":\"generic\",\"smtp\":{\"host\":\"$SMTP_HOST\",\"port\":$SMTP_PORT,\"tls_mode\":\"starttls\",\"username\":\"$SMTP_USER\",\"password\":\"$SMTP_PASS\",\"test_sender\":\"$SENDER\"}}")
EX=$(echo "$R" | J 'print(d["id"])'); [ -n "$EX" ] && ok "profile created ($EX); password stored sealed, never returned" || { bad "profile not created: $R"; summary; exit 1; }
echo "$R" | grep -q "$SMTP_PASS" && bad "password echoed in the response" || ok "the API response does not contain the SMTP password"
T=$(api POST /admin/v1/installation/transport-profiles/$EX/test)
echo "$T" | J 'print("ok",d["ok"]); [print("   ",s["key"],s["ok"],s.get("skipped"),str(s.get("detail",""))[:90]) for s in d["steps"]]' | sed 's/^/    /'
echo "$T" | J 'print(d["ok"])' | grep -q True && ok "connection test passes: DNS, TCP, TLS (lab CA verified), AUTH, sender" || bad "connection test failed"
echo "### B. Sending facts: the Domains page must ask for exactly the right SPF and DKIM"
api PUT /admin/v1/installation/transport-profiles/$EX/sending-facts '{"egress_ips":["93.184.216.35"],"spf_include":"","dkim_selector":"gen"}' | tail -n1 | grep -q 'HTTP 200' && ok "sending facts saved (the validator refuses private egress IPs, so a public-looking lab address 93.184.216.35 stands in)" || bad "sending facts rejected"
api PUT /admin/v1/installation/transport-profiles/$EX/sending-facts '{"egress_ips":["10.77.0.20"]}' | head -n1 | grep -q 'not a public sending address' && ok "a private egress IP is refused as a sending address" || bad "private egress IP accepted"
echo "### C. G06 existing -> managed (we start in managed; first activate the existing profile with an empty queue)"
SW=35  # the worker re-reads the active profile at most every 30 s
A=$(api POST /admin/v1/installation/transport-profiles/$EX/activate '{}'); echo "$A" | J 'print(d["target_mode"],"already_active",d["already_active"])' | sed 's/^/    /'
echo "$A" | tail -n1 | grep -q 'HTTP 200' && ok "existing_smtp profile activated" || bad "activation failed: $A"; sleep $SW; T0=$(P "select now()")
net 'sudo lab-receiver clear'; B=$(inbox_count)
mx 'send alice@rcv-a.test "G06 via existing smtp"' | tail -n1 | grep -q '"status":"submitted"' && ok "a message goes out through the existing server" || bad "send via existing server failed"
sleep 4; H=$(net 'f=$(sudo ls -1t /home/alice/Maildir/new | head -n1); sudo grep -A1 -E "^Received:" /home/alice/Maildir/new/$f | head -n 4 | tr "\n" " " | cut -c1-300'); echo "    $H"
echo "$H" | grep -q 'ESMTPSA' && ok "the receiver recorded an authenticated (SASL) TLS submission straight from the worker: the existing-server path" || bad "no authenticated submission recorded: $H"
echo "--- stop the existing server so mail waits in retry_wait"
net 'sudo systemctl stop postfix'; T1=$(P "select now()"); for i in 1 2 3; do mx "send alice@rcv-a.test 'queued while existing server down $i'" >/dev/null 2>&1; done; sleep 12
W=$(P "select count(*) from messages where created_at > '$T1' and state in ('retry_wait','queued','submitting')"); [ "${W:-0}" -ge 3 ] && ok "$W messages are waiting in Njiwa (retry_wait/queued)" || bad "waiting messages: $W"
api POST /admin/v1/installation/transport-profiles/$MG/test | tail -n1 | grep -q 'HTTP 200' && ok "managed profile re-tested (a switch needs a fresh passing check for its current revision)" || bad "managed profile test failed"
PL=$(api GET /admin/v1/installation/transport-profiles/$MG/activation-plan); echo "$PL" | J 'print({k:d[k] for k in ("queued","retry_wait","submitting","unknown","needs_decision","verified")}, d.get("blockers"))' | sed 's/^/    plan: /'
echo "$PL" | J 'print(d["needs_decision"])' | grep -q True && ok "the switch plan shows the pending mail and requires a decision" || bad "plan did not require a decision"
N=$(api POST /admin/v1/installation/transport-profiles/$MG/activate '{}' | tail -n1); [ "$N" != "HTTP 200" ] && ok "activation without acknowledging the queue is refused ($N)" || bad "switched without acknowledging pending mail"
R=$(api POST /admin/v1/installation/transport-profiles/$MG/activate '{"migrate_pending":true}'); echo "$R" | tail -n1 | grep -q 'HTTP 200' && ok "activation with migrate_pending acknowledged" || bad "activation failed: $(echo "$R" | head -n1 | cut -c1-200)"
sleep $SW; net 'sudo systemctl start postfix'; sleep 30; A2=$(inbox_count)
[ $((A2-B)) -ge 4 ] && ok "queued mail left through the managed Postfix once the receiver was back (inbox +$((A2-B)))" || bad "only $((A2-B)) delivered"
PIN=$(P "select count(*) from messages m join transport_profiles p on p.id=m.transport_profile_id where p.mode='managed_mta' and m.created_at > '$T1' and m.state='submitted'")
[ "${PIN:-0}" -ge 3 ] && ok "the 3 queued messages are pinned to the managed profile that sent them" || bad "pinned to managed: $PIN"
echo "### D. G06 managed -> existing (roll back to the previous profile; the other direction)"
T2=$(P "select now()"); mx 'sudo systemctl stop postfix@-'; for i in 1 2; do mx "send alice@rcv-b.test 'queued while managed down $i'" >/dev/null 2>&1; done; sleep 12
W=$(P "select count(*) from messages where created_at > '$T2' and state in ('retry_wait','queued','submitting')"); [ "${W:-0}" -ge 2 ] && ok "$W messages waiting while the managed Postfix is down" || bad "waiting: $W"
B=$(inbox_count)
R=$(api POST /admin/v1/installation/transport-profiles/$EX/activate '{"migrate_pending":true}'); echo "$R" | tail -n1 | grep -q 'HTTP 200' && ok "switched back to the existing-SMTP profile with the queue acknowledged" || bad "switch back failed: $(echo "$R" | head -n1 | cut -c1-200)"
sleep $((SW+25)); A2=$(inbox_count); [ $((A2-B)) -ge 2 ] && ok "queued mail left through the existing server (inbox +$((A2-B)))" || bad "only $((A2-B)) delivered"
PIN=$(P "select count(*) from messages m where m.transport_profile_id='$EX' and m.created_at > '$T2' and m.state='submitted'"); [ "${PIN:-0}" -ge 2 ] && ok "those messages are pinned to the existing-SMTP profile" || bad "pinned to existing: $PIN"
mx 'sudo systemctl start postfix@-'
echo "### E. Restore the managed profile as the active one"
api POST /admin/v1/installation/transport-profiles/$MG/test >/dev/null; api POST /admin/v1/installation/transport-profiles/$MG/activate '{"migrate_pending":true}' | tail -n1 | grep -q 'HTTP 200' && ok "managed profile active again" || bad "could not restore managed"
summary

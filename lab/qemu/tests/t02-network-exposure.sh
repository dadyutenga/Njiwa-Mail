#!/usr/bin/env bash
# Firewall behaviour, relay and submission refusal, three receiver domains, HELO vs PTR (HANDOFF 3.8 nmap, G05 step 13, E06, E05). SIMULATED network.
set -uo pipefail; . "$(dirname "$0")/lib.sh"
echo "## t02 network exposure and relay controls ($(date -u +%FT%TZ))"; echo; echo "Scanner: the lab 'net' VM at 10.77.0.20 against mx 10.77.0.10 (**simulated** network, not the internet)"; echo
echo "### A. Ports as seen from another host"
S=$(net 'sudo nmap -Pn -n -p 25,2525,3000,5432,8080,8891,10587 10.77.0.10 2>&1 | grep -E "^[0-9]+/tcp"')
echo '```'; echo "$S"; echo '```'
echo "$S" | grep -q '^25/tcp *open' && ok "25/tcp is open" || bad "25/tcp not open"
for p in 2525 3000 5432 8080 8891 10587; do echo "$S" | grep -qE "^$p/tcp *(closed|filtered)" && ok "$p/tcp not reachable" || bad "$p/tcp reachable"; done
echo; echo "### B. The firewall rule itself (services deliberately started on the private ports, all interfaces)"
mx 'for p in 2525 3000 5432 8080 8891; do (nohup python3 -m http.server $p --bind 0.0.0.0 >/dev/null 2>&1 &) ; done; sleep 2; sudo ss -ltn | grep -cE ":(2525|3000|5432|8080|8891) .*0.0.0.0|\\*:(2525|3000|5432|8080|8891)"' | sed 's/^/    listeners on 0.0.0.0: /'
S2=$(net 'sudo nmap -Pn -n -p 2525,3000,5432,8080,8891 10.77.0.10 2>&1 | grep -E "^[0-9]+/tcp"'); echo '```'; echo "$S2"; echo '```'
for p in 2525 3000 5432 8080 8891; do echo "$S2" | grep -qE "^$p/tcp *filtered" && ok "$p/tcp dropped by the njiwa_mail table although a service listens" || bad "$p/tcp NOT dropped"; done
mx 'sudo nft list ruleset | grep -E "counter packets [1-9]" | head -n 2'
mx 'pkill -f "http.server" ; true' >/dev/null 2>&1
echo; echo "### C. Submission listener (172.30.0.1:10587)"
mx 'cd /opt/njiwa-mail; sudo docker run --rm --network njiwa-mail_internal alpine:3.20 sh -c "nc -zv -w3 172.30.0.1 10587" 2>&1 | tail -n 1' | sed 's/^/    from the Compose network: /'
check "reachable from the Compose subnet (worker path)" mx 'cd /opt/njiwa-mail; sudo docker run --rm --network njiwa-mail_internal alpine:3.20 nc -z -w3 172.30.0.1 10587'
R=$(mx 'swaks --server 172.30.0.1:10587 --tls --from bounces@bounce.njiwa.test --to alice@rcv-a.test --quit-after RCPT 2>&1 | tail -n 6' | cut -c1-170)
echo '```'; echo "$R"; echo '```'
echo "$R" | grep -qiE "(<-|\*\*\*).*(5[0-9][0-9]|TLS|certificate|handshake)|alert|error" && ok "submission without the worker's client certificate is refused" || bad "submission without a client certificate was NOT refused"
echo; echo "### D. Port 25 is not an open relay"
R=$(net 'swaks --server 10.77.0.10 --from probe@example.org --to someone@gmail.test --quit-after RCPT 2>&1 | grep -E "^<\*\*|^<-  (4|5)" | tail -n 2' | cut -c1-170)
echo '```'; echo "$R"; echo '```'
echo "$R" | grep -q '554 5.7.1' && ok "relay to a foreign domain refused with 554 5.7.1" || bad "relay not refused with 554 5.7.1"
R=$(net 'swaks --server 10.77.0.10 --from probe@example.org --to hello@notify.njiwa.test --quit-after RCPT 2>&1 | grep -E "^<\*\*|^<-  (4|5)" | tail -n 2' | cut -c1-170)
echo "    unknown local recipient: $(echo "$R" | tail -n1)"
echo; echo "### E. Three receiver domains (stand-ins for three providers) and HELO"
net 'sudo lab-receiver clear'; B=$(inbox_count)
for d in rcv-a.test rcv-b.test rcv-c.test; do mx "send alice@$d 'three providers $d'" | tail -n1 | grep -q '"status":"submitted"' && ok "accepted by managed Postfix for $d" || bad "not accepted for $d"; done
sleep 12; A=$(inbox_count); [ $((A-B)) -ge 3 ] && ok "all three arrived at the receiver (inbox +$((A-B)))" || bad "only $((A-B)) arrived"
H=$(net 'f=$(sudo ls -1t /home/alice/Maildir/new | head -n1); sudo grep -m1 "^Received: from mailout" /home/alice/Maildir/new/$f')
echo "    $H"; echo "$H" | grep -q 'from mailout.njiwa.test (mailout.njiwa.test \[10.77.0.10\])' && ok "HELO seen by the remote side equals the PTR name (forward-confirmed)" || bad "HELO/PTR mismatch"
T=$(net 'f=$(sudo ls -1t /home/alice/Maildir/new | head -n1); sudo grep -m1 -A1 "by mx.rcv.test" /home/alice/Maildir/new/$f | tr -d "\n"'); echo "    $T"; echo "$T" | grep -q 'with UTF8SMTPS' && ok "the receiver recorded a TLS session (UTF8SMTPS) from Postfix" || bad "no TLS session recorded by the receiver"
mx 'sudo postconf -h myhostname smtp_helo_name smtp_tls_security_level 2>/dev/null' | sed 's/^/    postconf: /'
summary

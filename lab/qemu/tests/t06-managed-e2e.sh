#!/usr/bin/env bash
# HANDOFF G05 step 13: the managed end-to-end suite (go test -tags managed_e2e) against a real Postfix + OpenDKIM configured by the installer
# on a CLEAN host (clone of mx at snapshot pkgs). Parameters are the ones the suite documents. SIMULATED network (the suite needs none).
set -uo pipefail; . "$(dirname "$0")/lib.sh"
echo "## t06 managed end-to-end suite on a clean Ubuntu 24.04 host ($(date -u +%FT%TZ))"; echo
mx() { "$LAB/lab.sh" ssh mxf "$@"; }
"$LAB/lab.sh" down mx >/dev/null 2>&1; "$LAB/lab.sh" down mxf >/dev/null 2>&1
"$LAB/lab.sh" fresh pkgs >/dev/null && "$LAB/lab.sh" up mxf online >/dev/null 2>&1 && ok "clean host booted from the pkgs snapshot (OS + packages only)" || { bad boot; summary; exit 1; }
check "no Postfix configuration yet" mx '! test -f /etc/postfix/main.cf'
tar -C /home/dadi/Projects/ditronics-mail --exclude=.git --exclude=node_modules --exclude=documentation --exclude=lab -czf /tmp/lab/src.tgz .
"$LAB/lab.sh" scp /tmp/lab/src.tgz mxf:/tmp/ >/dev/null 2>&1
mx 'cd /opt/njiwa-mail && sudo tar -xzf /tmp/src.tgz -C . && sudo docker build -q -t njiwa-mail:local . >/dev/null 2>&1; sudo docker create --name nm-tmp njiwa-mail:local >/dev/null && sudo docker cp nm-tmp:/usr/local/bin/njiwa-mail /usr/local/bin/njiwa-mail && sudo docker rm nm-tmp >/dev/null' && ok "binary built on the host (HANDOFF 3.1 step 2)" || bad "build failed"
H="-hostname mailout.example.test -domains notify.example.test -bounce-domain bounce.example.test -selector njiwa"
D=$(mx "sudo njiwa-mail installer plan $H | grep 'Plan digest' | awk '{print \$3}'")
mx "sudo njiwa-mail installer apply $H -approve $D 2>&1 | grep -E 'Result|FAIL'" | sed 's/^/    /' | grep -q 'Result: ok' && ok "installer apply on a clean host, defaults for -submission and -inbound (plan digest $D)" || bad "installer apply failed"
mx 'systemctl is-active postfix@- opendkim | tr "\n" " "' | grep -q 'active active' && ok "Postfix and OpenDKIM active" || bad "services not active"
echo "    compiling the e2e suite in the Go image (online step)"
mx 'cd /opt/njiwa-mail && sudo mkdir -p out && sudo docker run --rm -v /opt/njiwa-mail:/src -w /src -e GOFLAGS=-mod=mod golang:1.26.11-alpine go test -c -tags managed_e2e -o /src/out/managed.e2e ./internal/installer/managed 2>&1 | tail -n 3; ls -l out/managed.e2e | cut -c1-50' | sed 's/^/    /'
"$LAB/lab.sh" down mxf >/dev/null; "$LAB/lab.sh" up mxf isolated >/dev/null 2>&1 && ok "restarted isolated (no outside network for the suite)" || bad "isolated restart failed"
for _ in $(seq 1 20); do mx 'systemctl is-active --quiet postfix@-' 2>/dev/null && break; sleep 3; done
OUT=$(mx 'cd /opt/njiwa-mail && sudo NJIWA_E2E_SELECTOR=njiwa NJIWA_E2E_CAPTURE=/tmp/e2e-capture.eml ./out/managed.e2e -test.run E2E -test.v -test.timeout 10m 2>&1')
echo '```'; echo "$OUT" | grep -E '^(=== RUN|--- |PASS|FAIL|ok|panic)|    ' | cut -c1-200 | tail -n 60; echo '```'
echo "$OUT" | grep -q '^PASS' && ok "the managed end-to-end suite passed on the clean host" || bad "the managed end-to-end suite did not pass"
P=$(echo "$OUT" | grep -c '^--- PASS'); F=$(echo "$OUT" | grep -c '^--- FAIL'); S=$(echo "$OUT" | grep -c '^--- SKIP'); echo "    suite tests: pass=$P fail=$F skip=$S"
mx 'sudo head -c 1500 /tmp/e2e-capture.eml 2>/dev/null | head -n 14' | sed 's/^/    capture: /'
summary

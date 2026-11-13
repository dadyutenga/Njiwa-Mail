# Shared helpers for the lab tests. SIMULATED networking: every result printed here is lab evidence, not production validation.
LAB=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
mx()  { "$LAB/lab.sh" ssh mx "$@"; }
net() { "$LAB/lab.sh" ssh net "$@"; }
PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); echo "- [x] $*"; }
bad()  { FAIL=$((FAIL+1)); echo "- [ ] **FAIL** $*"; }
check() { local d=$1; shift; if "$@" >/dev/null 2>&1; then ok "$d"; else bad "$d"; fi; }
wait_ssh() { for _ in $(seq 1 90); do mx true 2>/dev/null && return 0; sleep 2; done; return 1; }
inbox_count() { net 'sudo ls -1 /home/alice/Maildir/new | wc -l' 2>/dev/null; }
summary() { echo; echo "Result: $PASS passed, $FAIL failed"; [ "$FAIL" -eq 0 ]; }

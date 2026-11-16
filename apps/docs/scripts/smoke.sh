#!/bin/sh
# Deployment smoke test for the documentation container.
#   scripts/smoke.sh http://127.0.0.1:8088 [expected-commit]
# Exits non-zero on the first failed check. Needs only curl.
set -u
BASE="${1:?usage: smoke.sh BASE_URL [commit]}"
WANT="${2:-}"
fail=0
ok() { printf 'ok   %s\n' "$1"; }
bad() { printf 'FAIL %s\n' "$1"; fail=1; }
status() { curl -s -o /dev/null -w '%{http_code}' "$BASE$1"; }
ctype() { curl -s -o /dev/null -D - "$BASE$1" | tr -d '\r' | awk -F': ' 'tolower($1)=="content-type"{print tolower($2)}'; }
header() { curl -s -o /dev/null -D - "$BASE$1" | tr -d '\r' | awk -F': ' -v h="$2" 'tolower($1)==tolower(h){print $2}'; }

for p in / /docs /docs/quickstart /docs/api/send-email /docs/webhooks /docs/setup/managed-postfix /docs/setup/existing-smtp; do
  [ "$(status $p)" = 200 ] && ok "200 $p" || bad "$p is not 200"
done
case "$(ctype /docs/quickstart.md)" in text/markdown*) ok "quickstart.md is text/markdown";; *) bad "quickstart.md content type: $(ctype /docs/quickstart.md)";; esac
case "$(ctype /llms.txt)" in text/plain*) ok "llms.txt is text/plain";; *) bad "llms.txt content type: $(ctype /llms.txt)";; esac
case "$(ctype /llms-full.txt)" in text/plain*) ok "llms-full.txt is text/plain";; *) bad "llms-full.txt content type";; esac
case "$(ctype /openapi.yaml)" in application/yaml*) ok "openapi.yaml is application/yaml";; *) bad "openapi.yaml content type: $(ctype /openapi.yaml)";; esac
case "$(ctype /openapi.json)" in application/json*) ok "openapi.json is application/json";; *) bad "openapi.json content type";; esac
case "$(ctype /api/search)" in application/json*) ok "search index is application/json";; *) bad "search index content type: $(ctype /api/search)";; esac
[ "$(status /skills/njiwa-mail-integration/SKILL.md)" = 200 ] && ok "skill served" || bad "skill missing"

# Unknown pages are a real 404 with the site's own page.
[ "$(status /docs/does-not-exist)" = 404 ] && ok "404 for an unknown page" || bad "unknown page is not 404"
curl -s "$BASE/docs/does-not-exist" | grep -q "This page does not exist" && ok "custom 404 page" || bad "404 page missing"

# The docs host is never an API: these must not reach anything.
for p in /admin/v1/auth/me /admin/v1/mta/status /v1/emails /v1/emails/batch /console/me /metrics /health/live /.env /.git/config; do
  [ "$(status $p)" = 404 ] && ok "404 $p" || bad "$p answered $(status $p), want 404"
done
[ "$(curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Authorization: Bearer x' "$BASE/v1/emails")" = 404 ] && ok "POST /v1/emails is 404" || bad "POST /v1/emails is reachable"

# Security headers on pages and on the agent files.
for p in /docs/quickstart /docs/quickstart.md /llms.txt /api/search; do
  [ -n "$(header $p X-Content-Type-Options)" ] && ok "nosniff on $p" || bad "no X-Content-Type-Options on $p"
  [ -n "$(header $p Content-Security-Policy)" ] && ok "CSP on $p" || bad "no CSP on $p"
done
header /docs/quickstart Content-Security-Policy | grep -q "frame-ancestors 'none'" && ok "frame-ancestors none" || bad "frame-ancestors missing"
header /docs/quickstart Content-Security-Policy | grep -q "object-src 'none'" && ok "object-src none" || bad "object-src missing"
[ -z "$(header /docs/quickstart Server | grep -E '[0-9]\.[0-9]')" ] && ok "no server version" || bad "server version is exposed"

# One version and commit everywhere.
v="$(curl -s "$BASE/versions.json")"
commit="$(printf '%s' "$v" | sed -n 's/.*"commit": *"\([^"]*\)".*/\1/p')"
[ -n "$commit" ] && ok "versions.json commit=$commit" || bad "versions.json has no commit"
curl -s "$BASE/llms.txt" | grep -q "source commit $commit" && ok "llms.txt carries the same commit" || bad "llms.txt commit differs"
curl -s "$BASE/docs/quickstart.md" | grep -q "commit $commit)" && ok "quickstart.md carries the same commit" || bad "quickstart.md commit differs"
if [ -n "$WANT" ]; then [ "$commit" = "$WANT" ] && ok "serving the expected commit $WANT" || bad "serving $commit, expected $WANT"; fi

[ "$fail" = 0 ] && echo "SMOKE PASSED" || { echo "SMOKE FAILED"; exit 1; }

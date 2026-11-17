#!/bin/sh
# Continuous deployment entrypoint for rida-server (production).
#
# Installed as /usr/local/sbin/njiwa-deploy and reached only through a forced
# command in root's authorized_keys (see deploy/rida-server/README.md), so the
# CI key can run this script and nothing else:
#
#   command="/usr/local/sbin/njiwa-deploy",restrict ssh-ed25519 AAAA… njiwa-mail-cd
#
# The SSH command is the commit to deploy (40 hex characters). The commit must
# already be on origin/main, so the key cannot deploy arbitrary code. Only the
# services whose inputs changed are rebuilt. A failed health check puts the
# previous images back. Database migrations are forward-only, so a dump is taken
# before any deploy that changes them.
set -eu

APP=/opt/njiwa-mail
SRC=/opt/njiwa-mail-src
BACKUPS=/opt/njiwa-mail-backups
REPO=https://github.com/dadyutenga/Njiwa-Mail.git
PUBLIC=https://njiwamail.qarmalab.dev
COMPOSE="docker compose -f compose.yaml -f compose.production.yaml"
DOCS="docker compose --env-file .env.docs -p njiwa-mail-docs -f compose.docs.yaml"

log() { printf '%s %s\n' "$(date -u +%H:%M:%S)" "$*"; }
die() { log "ERROR: $*"; exit 1; }

sha=${SSH_ORIGINAL_COMMAND:-${1:-}}
case $sha in
  *[!0-9a-f]* | "") die "expected a full commit SHA, got '${sha}'" ;;
esac
[ ${#sha} -eq 40 ] || die "expected a full 40-character commit SHA"

exec 9>/run/njiwa-deploy.lock
flock -w 900 9 || die "another deploy is still running"

# --- source -----------------------------------------------------------------
[ -d "$SRC/.git" ] || git clone --quiet "$REPO" "$SRC"
git -C "$SRC" fetch --quiet --prune origin main
git -C "$SRC" merge-base --is-ancestor "$sha" origin/main || die "$sha is not on origin/main"
git -C "$SRC" -c advice.detachedHead=false checkout --quiet --force "$sha"
short=$(git -C "$SRC" rev-parse --short=7 "$sha")

prev=$(cat "$APP/.deployed-commit" 2>/dev/null || true)
if [ "$prev" = "$sha" ]; then log "$short is already deployed"; exit 0; fi
if [ -n "$prev" ] && git -C "$SRC" cat-file -e "$prev^{commit}" 2>/dev/null; then
  changed=$(git -C "$SRC" diff --name-only "$prev" "$sha")
else
  changed=ALL
  log "no previous deploy record: rebuilding everything"
fi

needs() { [ "$changed" = ALL ] || printf '%s\n' "$changed" | grep -Eq "$1"; }
api=0; frontend=0; docs=0; migrations=0
# The Go image ignores apps/ and docs/ (.dockerignore); docs, CI and clients never reach it.
if [ "$changed" = ALL ] || printf '%s\n' "$changed" |
  grep -Ev '^(apps/|docs/|skills/|clients?/|lab/|\.github/|deploy/)|\.md$|^compose\.docs\.yaml$' | grep -q .; then
  api=1
fi
needs '^(apps/dashboard/|compose\.ya?ml$|compose\.production\.yaml$)' && frontend=1
needs '^(apps/docs/|docs/openapi\.yaml$|skills/|clients/typescript/|compose\.docs\.yaml$)' && docs=1
needs '^internal/db/migrations/' && migrations=1
log "deploying $short (previous ${prev:-none}): api=$api frontend=$frontend docs=$docs migrations=$migrations"

# --- sync files (server-only .env, .env.docs, secrets and data are never tracked, never touched) ---
git -C "$SRC" ls-files -z | rsync -a --from0 --files-from=- "$SRC/" "$APP/"
cd "$APP"

# --- back up what a rollback needs --------------------------------------------
mkdir -p "$BACKUPS"
docker image inspect njiwa-mail:local >/dev/null 2>&1 && docker image tag njiwa-mail:local njiwa-mail:rollback
docker image inspect njiwa-mail-frontend:latest >/dev/null 2>&1 && docker image tag njiwa-mail-frontend:latest njiwa-mail-frontend:rollback
cp .env.docs "$BACKUPS/env.docs.rollback"
if [ "$migrations" = 1 ]; then
  dump="$BACKUPS/db-before-$short.sql.gz"
  $COMPOSE exec -T postgres pg_dump -U mail -d mail | gzip > "$dump"
  log "database dumped to $dump"
fi

rollback() {
  log "health check failed: restoring the previous images"
  docker image inspect njiwa-mail:rollback >/dev/null 2>&1 && docker image tag njiwa-mail:rollback njiwa-mail:local
  docker image inspect njiwa-mail-frontend:rollback >/dev/null 2>&1 && docker image tag njiwa-mail-frontend:rollback njiwa-mail-frontend:latest
  cp "$BACKUPS/env.docs.rollback" .env.docs
  $COMPOSE up -d --no-build --wait --wait-timeout 180 || true
  $DOCS up -d --no-build --wait || true
  die "deploy of $short failed and was rolled back (files on disk are $short; images are the previous ones)"
}

# --- build and start --------------------------------------------------------
build=""
[ "$api" = 1 ] && build="$build api"
[ "$frontend" = 1 ] && build="$build frontend"
if [ -n "$build" ]; then
  log "building:$build"
  # shellcheck disable=SC2086
  $COMPOSE build $build > "$BACKUPS/deploy-$short.log" 2>&1 || die "build failed (see $BACKUPS/deploy-$short.log); nothing was restarted"
  $COMPOSE up -d --no-build --wait --wait-timeout 180 || rollback
fi
if [ "$docs" = 1 ]; then
  log "building docs"
  sed -i "s/^DOCS_COMMIT=.*/DOCS_COMMIT=$short/" .env.docs
  $DOCS build > "$BACKUPS/deploy-docs-$short.log" 2>&1 || { cp "$BACKUPS/env.docs.rollback" .env.docs; die "docs build failed; nothing was restarted"; }
  $DOCS up -d --no-build --wait || rollback
fi

# --- verify -----------------------------------------------------------------
ok=0
for _ in $(seq 1 30); do
  if curl -fsS -o /dev/null http://127.0.0.1:18080/health/ready && curl -fsS -o /dev/null http://127.0.0.1:18300/; then ok=1; break; fi
  sleep 2
done
[ "$ok" = 1 ] || rollback
if [ "$docs" = 1 ]; then
  SHARED_ORIGIN=1 sh apps/docs/scripts/smoke.sh "$PUBLIC" > "$BACKUPS/smoke-$short.log" 2>&1 || rollback
fi

echo "$sha" > .deployed-commit
docker image prune -f --filter "until=168h" >/dev/null 2>&1 || true
log "deployed $short"

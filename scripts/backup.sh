#!/bin/sh
# Backup PostgreSQL and the attachment directory. Reads DATABASE_URL and DATA_DIR.
set -eu
out=${1:-"./backup-$(date -u +%Y%m%dT%H%M%SZ)"}
mkdir -p "$out"
pg_dump --format=custom --file="$out/mail.dump" "$DATABASE_URL"
if [ -n "${DATA_DIR:-}" ] && [ -d "$DATA_DIR" ]; then
  tar -C "$DATA_DIR" -czf "$out/data.tgz" .
fi
echo "backup written to $out"

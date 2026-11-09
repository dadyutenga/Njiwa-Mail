#!/bin/sh
# Restore a backup created by backup.sh. This replaces the database named in DATABASE_URL.
set -eu
src=${1:?usage: restore.sh BACKUP_DIR}
pg_restore --clean --if-exists --dbname="$DATABASE_URL" "$src/mail.dump"
if [ -n "${DATA_DIR:-}" ] && [ -f "$src/data.tgz" ]; then
  mkdir -p "$DATA_DIR"
  tar -C "$DATA_DIR" -xzf "$src/data.tgz"
fi
echo "restored from $src"

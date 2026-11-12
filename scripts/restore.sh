#!/bin/sh
# Restore a backup made by scripts/backup.sh. Reads DATABASE_URL and DATA_DIR.
#
#   scripts/restore.sh BACKUP_DIR
#
# Replaces the database named in DATABASE_URL and the contents of DATA_DIR.
# On a managed-mode host (as root) it also restores the Postfix and OpenDKIM
# configuration, DKIM keys, certificates and installer receipts, rebuilds the
# Postfix lookup tables, validates the configuration and restarts the services;
# with postfix-queue.tgz present it restores the queue too. The checksum
# manifest is verified first and the restore stops if anything differs.
set -eu
src=${1:?usage: restore.sh BACKUP_DIR}
( cd "$src" && sha256sum -c --quiet MANIFEST.sha256 ) || { echo "backup files do not match MANIFEST.sha256; not restoring" >&2; exit 1; }

pg_restore --clean --if-exists --dbname="$DATABASE_URL" "$src/mail.dump"
if [ -n "${DATA_DIR:-}" ] && [ -f "$src/data.tgz" ]; then
  mkdir -p "$DATA_DIR"
  tar -C "$DATA_DIR" -xzf "$src/data.tgz"
fi

if [ -f "$src/managed.tgz" ]; then
  if [ "$(id -u)" -ne 0 ]; then
    echo "managed.tgz present but not running as root: managed-mode files were NOT restored" >&2
    exit 1
  fi
  running=0
  postfix status >/dev/null 2>&1 && running=1
  [ "$running" = 1 ] && postfix stop >/dev/null
  tar -C / --numeric-owner -xzpf "$src/managed.tgz"
  if [ -f "$src/postfix-queue.tgz" ]; then
    tar -C / --numeric-owner -xzpf "$src/postfix-queue.tgz"
  fi
  for m in senders transport relay_clientcerts; do
    [ -f "/etc/postfix/njiwa/$m" ] && postmap "hash:/etc/postfix/njiwa/$m"
  done
  postfix check
  opendkim -n -x /etc/opendkim.conf
  if [ -d /run/systemd/system ]; then
    systemctl restart opendkim
    systemctl restart postfix
  else
    postfix start >/dev/null
    echo "no systemd: restart opendkim yourself (opendkim -x /etc/opendkim.conf)" >&2
  fi
fi
echo "restored from $src"

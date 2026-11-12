#!/bin/sh
# Back up Njiwa Mail. Reads DATABASE_URL and DATA_DIR.
#
#   scripts/backup.sh [OUT_DIR]
#
# Always: the PostgreSQL database (pg_dump) and DATA_DIR (inbound attachments,
# spool). On a managed-mode host, run as root, it also saves what the
# installer created: Postfix and OpenDKIM configuration, the DKIM private keys,
# the local CA and the worker's certificate, and the installer receipts.
# BACKUP_QUEUE=1 also copies the Postfix queue; Postfix is stopped for the copy
# (seconds) so the queue files are consistent, then started again.
#
# The output contains private keys and encrypted message payloads: it is
# written owner-only. Store it encrypted and off-host. ENCRYPTION_KEY and
# HMAC_KEY are NOT in the backup; keep them separately or the data cannot be
# decrypted after a restore.
set -eu
umask 077
out=${1:-"./backup-$(date -u +%Y%m%dT%H%M%SZ)"}
mkdir -p "$out"

pg_dump --format=custom --file="$out/mail.dump" "$DATABASE_URL"
if [ -n "${DATA_DIR:-}" ] && [ -d "$DATA_DIR" ]; then
  tar -C "$DATA_DIR" -czf "$out/data.tgz" .
fi

# Managed-mode host files, only those that exist.
managed=""
for p in /etc/postfix/main.cf /etc/postfix/master.cf /etc/postfix/njiwa /etc/opendkim.conf /etc/opendkim \
         /etc/njiwa-mail /var/lib/njiwa-mail/installer; do
  [ -e "$p" ] && managed="$managed ${p#/}"
done
if [ -n "$managed" ]; then
  if [ "$(id -u)" -ne 0 ]; then
    echo "managed-mode files found but not running as root: they were NOT backed up" >&2
  else
    # shellcheck disable=SC2086
    tar -C / --numeric-owner -czpf "$out/managed.tgz" $managed
    if [ "${BACKUP_QUEUE:-0}" = "1" ] && [ -d /var/spool/postfix ]; then
      running=0
      postfix status >/dev/null 2>&1 && running=1
      [ "$running" = 1 ] && postfix stop >/dev/null
      tar -C / --numeric-owner --warning=no-file-ignored -czpf "$out/postfix-queue.tgz" var/spool/postfix || { [ "$running" = 1 ] && postfix start >/dev/null; exit 1; }
      [ "$running" = 1 ] && postfix start >/dev/null
    fi
  fi
fi

( cd "$out" && sha256sum ./* > MANIFEST.sha256 )
echo "backup written to $out"

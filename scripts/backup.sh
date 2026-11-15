#!/bin/sh
# Back up Njiwa Mail. Reads DATABASE_URL and DATA_DIR (or DATA_VOLUME, the Compose volume name).
#
#   scripts/backup.sh [OUT_DIR]
#
# Always: the PostgreSQL database (pg_dump) and DATA_DIR (inbound attachments,
# spool). On a managed-mode host, run as root, it also saves what the
# installer created (requires BACKUP_MANAGED=1): Postfix and OpenDKIM configuration, the DKIM private keys,
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
# Managed host capture is explicit: application backup never inspects mail keys.
[ ! -e "$out" ] || { echo "backup output must be a new directory" >&2; exit 1; }
[ "${BACKUP_QUEUE:-0}" != 1 ] || [ "${BACKUP_MANAGED:-0}" = 1 ] || { echo "BACKUP_QUEUE=1 requires BACKUP_MANAGED=1" >&2; exit 1; }
if [ "${BACKUP_MANAGED:-0}" = 1 ]; then
  [ "$(id -u)" = 0 ] || { echo "managed backup requires root" >&2; exit 1; }
  python3 "$(dirname "$0")/recovery_guard.py" backup-plan "$out" --managed
else
  python3 "$(dirname "$0")/recovery_guard.py" backup-plan "$out"
fi

# PostgreSQL normally runs in a container and the host has no client tools. Without pg_dump, run the one from the same
# image as the server (PG_IMAGE, on the Compose network PG_NETWORK) so the dump format always matches the server.
if [ "${PG_CLIENT:-}" != docker ] && command -v pg_dump >/dev/null 2>&1; then
  pg_dump --format=custom --file="$out/mail.dump" "$DATABASE_URL"
else
  docker run --rm --network "${PG_NETWORK:-njiwa-mail_internal}" "${PG_IMAGE:-postgres:16.4-alpine}" \
    pg_dump --format=custom "$DATABASE_URL" > "$out/mail.dump"
fi
if [ -n "${DATA_VOLUME:-}" ]; then
  # Compose installs keep DATA_DIR in a named volume (njiwa-mail_maildata):
  # read it through a throwaway container instead of Docker's internal path.
  # A missing volume is an error: "docker run -v" would silently create an
  # empty one and back that up.
  docker volume inspect "$DATA_VOLUME" >/dev/null 2>&1 || { echo "volume $DATA_VOLUME does not exist (see: docker volume ls)" >&2; exit 1; }
  docker run --rm -v "$DATA_VOLUME:/data:ro" "${PG_IMAGE:-postgres:16.4-alpine}" tar -C /data -czf - . > "$out/data.tgz"
elif [ -n "${DATA_DIR:-}" ] && [ -d "$DATA_DIR" ]; then
  tar -C "$DATA_DIR" -czf "$out/data.tgz" .
fi

# The inventory contains only receipt-owned files, not whole /etc mail trees.
if [ "${BACKUP_MANAGED:-0}" = 1 ]; then
  tar -C / --numeric-owner --no-recursion -czpf "$out/managed.tgz" -T "$out/managed-paths.txt"
  if [ "${BACKUP_QUEUE:-0}" = 1 ] && [ -d /var/spool/postfix ]; then
    running=0
    postfix status >/dev/null 2>&1 && running=1
    restart_queue() { [ "$running" != 1 ] || postfix start >/dev/null; }
    trap restart_queue EXIT
    trap 'exit 1' HUP INT TERM
    [ "$running" != 1 ] || postfix stop >/dev/null
    tar -C / --numeric-owner --warning=no-file-ignored -czpf "$out/postfix-queue.tgz" var/spool/postfix
    restart_queue
    running=0
    trap - EXIT HUP INT TERM
  fi
fi

( cd "$out" && sha256sum ./* > MANIFEST.sha256 )
echo "backup written to $out"

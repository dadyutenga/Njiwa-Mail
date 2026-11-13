#!/bin/sh
# Restore a backup made by scripts/backup.sh. Reads DATABASE_URL and DATA_DIR, or DATA_VOLUME
# (the Compose volume, njiwa-mail_maildata), which is created when missing.
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

if [ "${PG_CLIENT:-}" != docker ] && command -v pg_restore >/dev/null 2>&1; then
  pg_restore --clean --if-exists --dbname="$DATABASE_URL" "$src/mail.dump"
else
  docker run --rm -i --network "${PG_NETWORK:-njiwa-mail_internal}" "${PG_IMAGE:-postgres:16.4-alpine}" \
    pg_restore --clean --if-exists --dbname="$DATABASE_URL" < "$src/mail.dump"
fi
if [ -n "${DATA_VOLUME:-}" ] && [ -f "$src/data.tgz" ]; then
  # Compose installs: the named volume may not exist yet on a fresh host; it
  # is created (empty) and filled through a throwaway container. Owner 10001
  # is the user the Njiwa-Mail image runs as.
  # Created with Compose's labels (<project>_<volume>), so a later
  # "docker compose up" adopts it instead of warning that it is foreign.
  if ! docker volume inspect "$DATA_VOLUME" >/dev/null 2>&1; then
    project=${DATA_VOLUME%%_*} volname=${DATA_VOLUME#*_}
    docker volume create --label "com.docker.compose.project=$project" --label "com.docker.compose.volume=$volname" \
      "$DATA_VOLUME" >/dev/null
  fi
  docker run --rm -i -v "$DATA_VOLUME:/data" "${PG_IMAGE:-postgres:16.4-alpine}" \
    sh -c 'tar -C /data -xzf - && chown -R 10001:10001 /data' < "$src/data.tgz"
elif [ -n "${DATA_DIR:-}" ] && [ -f "$src/data.tgz" ]; then
  mkdir -p "$DATA_DIR"
  tar -C "$DATA_DIR" -xzf "$src/data.tgz"
elif [ -f "$src/data.tgz" ]; then
  echo "data.tgz present but neither DATA_VOLUME nor DATA_DIR is set: attachments and spool were NOT restored" >&2
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
    systemctl daemon-reload # the restored firewall unit, Postfix boot-ordering drop-in and agent/collector units
    if [ -f /etc/njiwa-mail/firewall.nft ]; then
      nft -f /etc/njiwa-mail/firewall.nft
      systemctl enable njiwa-mail-firewall >/dev/null 2>&1 || true
    fi
    systemctl restart opendkim
    systemctl restart postfix
  else
    postfix start >/dev/null
    echo "no systemd: restart opendkim yourself (opendkim -x /etc/opendkim.conf)" >&2
  fi
fi
echo "restored from $src"

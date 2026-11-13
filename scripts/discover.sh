#!/bin/sh
# Read-only discovery of this server, run from the Njiwa-Mail image before
# anything is installed on the host. The host filesystem is mounted read-only;
# the container sees the host's processes and network only to read them.
#
#   docker build -t njiwa-mail:local .        # or pull a released image
#   sh scripts/discover.sh                    # human-readable report
#   sh scripts/discover.sh -json              # JSON
#   sh scripts/discover.sh -record            # store it for the setup wizard
#                                             # (after "docker compose up", reads .env)
#
# Run it from the Njiwa-Mail directory: when .env exists, its NJIWA_* port and
# network settings are used, so a port you already moved is not reported again.
set -eu
IMAGE=${NJIWA_IMAGE:-njiwa-mail:local}
set -- discover -root /host -docker=false "$@"

# Percent-encode every byte, so a password with @ : / ? # % works in a URL.
urlencode() { printf '%s' "$1" | od -An -tx1 -v | tr -d ' \n' | sed 's/../%&/g'; }

ENVARGS=""
if [ -f .env ]; then
	# Only Njiwa's own placement settings are needed for a scan.
	for k in NJIWA_SUBNET NJIWA_API_PORT NJIWA_DASHBOARD_PORT NJIWA_INBOUND_PORT; do
		v=$(sed -n "s/^$k=//p" .env | tail -1)
		[ -n "$v" ] && ENVARGS="$ENVARGS -e $k=$v"
	done
fi
case " $* " in
*" -record "*)
	[ -f .env ] || { echo "-record needs .env (run from the Njiwa-Mail directory after docker compose up)" >&2; exit 2; }
	PW=$(sed -n 's/^POSTGRES_PASSWORD=//p' .env | tail -1)
	DBIP=$(sed -n 's/^NJIWA_DB_IP=//p' .env | tail -1)
	ENVARGS="$ENVARGS --env-file .env -e DATABASE_URL=postgres://mail:$(urlencode "$PW")@${DBIP:-172.30.0.10}:5432/mail?sslmode=disable"
	;;
esac
# Reading which process owns each port means reading /proc/PID/fd of host
# processes (ptrace read access). Docker's default AppArmor profile denies
# that, so the container runs unconfined; it has only the two read
# capabilities below, no new privileges, and the host mounted read-only.
# shellcheck disable=SC2086
exec docker run --rm --pid=host --network=host --user 0 \
	--cap-drop ALL --cap-add DAC_READ_SEARCH --cap-add SYS_PTRACE \
	--security-opt no-new-privileges --security-opt apparmor=unconfined \
	-v /:/host:ro $ENVARGS --entrypoint njiwa-mail "$IMAGE" "$@"

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
set -eu
IMAGE=${NJIWA_IMAGE:-njiwa-mail:local}
set -- discover -root /host -docker=false "$@"
DBARGS=""
case " $* " in
*" -record "*)
	[ -f .env ] || { echo "-record needs .env (run from the Njiwa-Mail directory after docker compose up)" >&2; exit 2; }
	PW=$(sed -n 's/^POSTGRES_PASSWORD=//p' .env | tail -1)
	DBIP=$(sed -n 's/^NJIWA_DB_IP=//p' .env | tail -1)
	DBARGS="--env-file .env -e DATABASE_URL=postgres://mail:${PW}@${DBIP:-172.30.0.10}:5432/mail?sslmode=disable"
	;;
esac
# shellcheck disable=SC2086
exec docker run --rm --pid=host --network=host --user 0 \
	--cap-drop ALL --cap-add DAC_READ_SEARCH --cap-add SYS_PTRACE --security-opt no-new-privileges \
	-v /:/host:ro $DBARGS --entrypoint njiwa-mail "$IMAGE" "$@"

#!/usr/bin/env bash
# Runs ON mx (no internet needed): HANDOFF 3.1 step 4-5. Generates secrets, writes .env, starts the stack from the locally built image.
set -euo pipefail
cd /opt/njiwa-mail
if [ ! -f .env ]; then
  umask 077
  sudo sh -c "cat > .env" <<E
POSTGRES_PASSWORD=$(openssl rand -hex 16)
ENCRYPTION_KEY=$(openssl rand -base64 32)
HMAC_KEY=$(openssl rand -base64 48)
BOUNCE_ADDRESS=bounces@bounce.njiwa.test
MAIL_HOSTNAME=mailout.njiwa.test
OUTBOUND_IP=10.77.0.10
DKIM_SELECTOR=njiwa
PUBLIC_URL=http://mail-api.njiwa.test
METRICS_TOKEN=$(openssl rand -hex 12)
INBOUND_ALLOWED_SOURCES=172.30.0.1
E
fi
sudo docker compose --profile inbound up -d --no-build postgres migrate api worker scheduler inbound 2>&1 | tail -n 12
for i in $(seq 1 40); do curl -fs 127.0.0.1:8080/health/ready >/dev/null && break; sleep 2; done
curl -s 127.0.0.1:8080/health/ready; echo
sudo docker compose ps --format '{{.Service}} {{.Status}}'

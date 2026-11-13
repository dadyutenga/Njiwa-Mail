#!/usr/bin/env bash
# Runs ON mx: HANDOFF E10 (queue agent, docs/OPERATIONS.md "Mail queue controls") and G05 step 14 (njiwa-mail-mta-log collector), as documented.
set -euo pipefail
. /usr/local/share/mxenv
cd /opt/njiwa-mail
sudo install -d -m 0755 /etc/njiwa-mail
[ -f /etc/njiwa-mail/mta-agent.token ] || { openssl rand -base64 36 | sudo tee /etc/njiwa-mail/mta-agent.token >/dev/null; sudo chmod 0600 /etc/njiwa-mail/mta-agent.token; }
sudo cp deploy/njiwa-mail-mta-agent.service /etc/systemd/system/
TOKEN=$(sudo cat /etc/njiwa-mail/mta-agent.token)
sudo grep -q '^MTA_AGENT_TOKEN=' .env || echo "MTA_AGENT_TOKEN=$TOKEN" | sudo tee -a .env >/dev/null
# collector: system user, env file, unit
id njiwa-mail >/dev/null 2>&1 || sudo useradd -r -s /usr/sbin/nologin njiwa-mail
sudo sh -c "umask 077; printf 'DATABASE_URL=%s\nENCRYPTION_KEY=%s\nHMAC_KEY=%s\n' '$DATABASE_URL' '$ENCRYPTION_KEY' '$HMAC_KEY' > /etc/njiwa-mail/env"
sudo chown root:njiwa-mail /etc/njiwa-mail/env; sudo chmod 0640 /etc/njiwa-mail/env
sudo cp deploy/njiwa-mail-mta-log.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now njiwa-mail-mta-agent njiwa-mail-mta-log
sudo docker compose -f compose.yaml -f compose.mta-agent.yaml --profile inbound up -d --no-build postgres migrate api worker scheduler inbound 2>&1 | grep -E "Recreate|Error" || true
sleep 8
systemctl is-active njiwa-mail-mta-agent njiwa-mail-mta-log

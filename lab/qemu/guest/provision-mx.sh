#!/usr/bin/env bash
# Runs ON the "mx" VM (online mode): HANDOFF 3.1 steps 1-2 exactly as documented (packages, clone, build the binary on the host).
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
echo "postfix postfix/main_mailer_type select No configuration" | sudo debconf-set-selections
sudo apt-get update -qq
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq postfix opendkim opendkim-tools nftables docker.io docker-compose-v2 git swaks netcat-openbsd dnsutils >/dev/null
sudo systemctl enable --now docker
sudo rm -rf /opt/njiwa-mail
sudo git clone -q "${REPO:-https://github.com/dadyutenga/Njiwa-Mail.git}" /opt/njiwa-mail
cd /opt/njiwa-mail
[ -n "${REF:-}" ] && sudo git checkout -q "$REF"
sudo docker build -q -t njiwa-mail:local . >/dev/null
sudo docker create --name nm-tmp njiwa-mail:local >/dev/null
sudo docker cp nm-tmp:/usr/local/bin/njiwa-mail /usr/local/bin/njiwa-mail
sudo docker rm nm-tmp >/dev/null
sudo docker compose --profile inbound pull -q 2>/dev/null || true
njiwa-mail version 2>/dev/null || true
echo provisioned-mx "$(git rev-parse --short HEAD)"

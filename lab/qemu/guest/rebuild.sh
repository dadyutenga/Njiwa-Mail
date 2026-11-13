#!/usr/bin/env bash
# Offline rebuild on mx from /opt/njiwa-mail using the cached Go module layer (image njiwa-build = `docker build --target build`).
# Replaces the binary in the njiwa-mail:local image and on the host. Lab convenience only: releases use the repository Dockerfile.
set -euo pipefail
cd /opt/njiwa-mail
sudo mkdir -p out
sudo docker run --rm -v /opt/njiwa-mail:/src -w /src -e GOPROXY=off -e GOFLAGS=-mod=mod -e CGO_ENABLED=0 njiwa-build \
  go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o /src/out/njiwa-mail ./cmd/njiwa-mail
printf 'FROM njiwa-mail:local\nCOPY out/njiwa-mail /usr/local/bin/njiwa-mail\nCOPY out/njiwa-mail /usr/local/bin/dmctl\n' | sudo tee out/Dockerfile.lab >/dev/null
sudo docker build -q -f out/Dockerfile.lab -t njiwa-mail:local . >/dev/null
sudo install -m755 out/njiwa-mail /usr/local/bin/njiwa-mail
sudo docker compose --profile inbound up -d --no-build postgres migrate api worker scheduler inbound 2>&1 | grep -E "Recreate|Error" || true
sleep 6; echo rebuilt

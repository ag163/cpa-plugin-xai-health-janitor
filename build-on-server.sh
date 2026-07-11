#!/usr/bin/env bash
set -euo pipefail
cd /home/ubuntu/xai-health-janitor
docker run --rm \
  -v /home/ubuntu/xai-health-janitor:/src \
  -w /src/go \
  golang:1.26-bookworm \
  bash -lc 'set -e; apt-get update -qq; DEBIAN_FRONTEND=noninteractive apt-get install -y -qq gcc libc6-dev >/tmp/apt.log; go mod tidy; CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -buildmode=c-shared -o /src/xai-health-janitor.so .; ls -la /src/xai-health-janitor.so'
rm -f /home/ubuntu/xai-health-janitor/xai-health-janitor.h
ls -la /home/ubuntu/xai-health-janitor/xai-health-janitor.so

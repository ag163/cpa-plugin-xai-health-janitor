#!/usr/bin/env bash
set -euo pipefail
cp /home/ubuntu/xai-health-janitor/xai-health-janitor.so /opt/cliproxyapi/plugins/linux/arm64/xai-health-janitor.so
chmod 755 /opt/cliproxyapi/plugins/linux/arm64/xai-health-janitor.so
ls -la /opt/cliproxyapi/plugins/linux/arm64/
cd /opt/cliproxyapi
docker compose restart cli-proxy-api
sleep 7
docker logs --tail 40 cli-proxy-api

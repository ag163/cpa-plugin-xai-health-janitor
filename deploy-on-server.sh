#!/usr/bin/env bash
set -euo pipefail

BASE=/opt/cliproxyapi
TS=$(date +%Y%m%d%H%M%S)
: "${CPA_MANAGEMENT_KEY:?set CPA_MANAGEMENT_KEY to the CPA remote-management plaintext key}"
export CPA_MANAGEMENT_KEY

mkdir -p "$BASE/backups" "$BASE/plugins/linux/arm64"
cp -a "$BASE/config.yaml" "$BASE/backups/config.yaml.bak-xai-janitor-$TS" || true
cp -a "$BASE/docker-compose.yml" "$BASE/backups/docker-compose.yml.bak-xai-janitor-$TS" || true

# ensure plugin binary present
if [[ -f /home/ubuntu/xai-health-janitor/xai-health-janitor.so ]]; then
  cp -f /home/ubuntu/xai-health-janitor/xai-health-janitor.so "$BASE/plugins/linux/arm64/xai-health-janitor.so"
fi
chmod 755 "$BASE/plugins/linux/arm64/"*.so || true

cat >"$BASE/docker-compose.yml" <<'YAML'
services:
  cli-proxy-api:
    image: ${CLI_PROXY_IMAGE:-eceasy/cli-proxy-api:latest}
    container_name: cli-proxy-api
    restart: unless-stopped
    env_file:
      - .env
    environment:
      DEPLOY: ${DEPLOY:-docker}
    ports:
      - "8317:8317"
      - "127.0.0.1:8085:8085"
    volumes:
      - ./config.yaml:/CLIProxyAPI/config.yaml
      - ./auths:/root/.cli-proxy-api
      - ./logs:/CLIProxyAPI/logs
      - ./plugins:/CLIProxyAPI/plugins

  cpa-usage-keeper:
    image: ghcr.io/willxup/cpa-usage-keeper:latest
    container_name: cpa-usage-keeper
    restart: unless-stopped
    depends_on:
      - cli-proxy-api
    env_file:
      - keeper.env
    ports:
      - "8080:8080"
    volumes:
      - ./keeper:/data
YAML

python3 - <<'PY'
from pathlib import Path
import os
import re
p = Path('/opt/cliproxyapi/config.yaml')
text = p.read_text(encoding='utf-8')
management_key = os.environ['CPA_MANAGEMENT_KEY'].replace('\\', '\\\\').replace('"', '\\"')
plugin_block = f'''plugins:
  enabled: true
  dir: "plugins"
  configs:
    keeper:
      enabled: true
      store:
        id: keeper
        name: CPA Usage Keeper
        description: Adds a CPAMC entry for opening CPA Usage Keeper from the management center. Requires CPA Usage Keeper v1.12.2 or later.
        author: Willxup
        version: 0.1.0
        release-tag: v0.1.0
        repository: https://github.com/Willxup/cpa-plugin-usage-keeper
        homepage: https://github.com/Willxup/cpa-plugin-usage-keeper
        license: MIT
        tags:
          - Management
          - Keeper
        source-id: official
        source-name: Official
        source-url: https://raw.githubusercontent.com/router-for-me/CLIProxyAPI-Plugins-Store/main/registry.json
        install:
          type: github-release
      keeper_url: http://3.139.57.236:8080/
    xai-health-janitor:
      enabled: true
      priority: 1
      interval_seconds: 600
      model: "grok-4.5"
      cli_version: "0.1.220"
      management_base: "http://127.0.0.1:8317"
      management_key: "{management_key}"
      probe_enabled: false
      auto_delete: true
      dry_run: false
      concurrency: 1
      providers: ["xai"]
      require_user_traffic: true
      hard_failure_confirmations: 2
'''
new_text, n = re.subn(r'(?ms)^plugins:\n.*?(?=^[a-zA-Z0-9_-]+:|\Z)', plugin_block + '\n', text, count=1)
if n != 1:
    raise SystemExit(f'failed to replace plugins section, n={n}')
p.write_text(new_text, encoding='utf-8')
print('config rewritten ok')
PY

cd "$BASE"
docker compose up -d cli-proxy-api
sleep 6
docker ps --filter name=cli-proxy-api --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'
docker exec cli-proxy-api sh -lc 'ls -la /CLIProxyAPI/plugins/linux/arm64'
docker logs --tail 100 cli-proxy-api

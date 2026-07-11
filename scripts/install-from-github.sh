#!/usr/bin/env bash
set -euo pipefail

# Install xai-health-janitor from GitHub Release into a CPA host directory.
#
# Usage:
#   REPO=ag163/cpa-plugin-xai-health-janitor TAG=v0.1.0 \
#   CPA_DIR=/opt/cliproxyapi bash scripts/install-from-github.sh

REPO="${REPO:-ag163/cpa-plugin-xai-health-janitor}"
TAG="${TAG:-latest}"
CPA_DIR="${CPA_DIR:-/opt/cliproxyapi}"
ARCH="$(uname -m)"

case "$ARCH" in
  aarch64|arm64) ASSET="xai-health-janitor-linux-arm64.so" ;;
  x86_64|amd64)  ASSET="xai-health-janitor-linux-amd64.so" ;;
  *) echo "unsupported arch: $ARCH"; exit 1 ;;
esac

PLUGIN_DIR="$CPA_DIR/plugins/linux/arm64"
if [[ "$ASSET" == *amd64* ]]; then
  PLUGIN_DIR="$CPA_DIR/plugins/linux/amd64"
fi

TMP="$(mktemp -d)"
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

echo "repo=$REPO tag=$TAG asset=$ASSET"
if [[ "$TAG" == "latest" ]]; then
  URL=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | python3 -c "import sys,json; d=json.load(sys.stdin);
print(next(a['browser_download_url'] for a in d.get('assets',[]) if a['name']=='$ASSET'))")
else
  URL=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/tags/$TAG" | python3 -c "import sys,json; d=json.load(sys.stdin);
print(next(a['browser_download_url'] for a in d.get('assets',[]) if a['name']=='$ASSET'))")
fi

echo "download $URL"
curl -fsSL "$URL" -o "$TMP/$ASSET"
mkdir -p "$PLUGIN_DIR"
install -m 0755 "$TMP/$ASSET" "$PLUGIN_DIR/xai-health-janitor.so"
echo "installed: $PLUGIN_DIR/xai-health-janitor.so"
ls -la "$PLUGIN_DIR/xai-health-janitor.so"
echo "done. ensure plugins config enabled and restart/reload CPA if needed."

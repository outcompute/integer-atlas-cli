#!/bin/sh
# Integer Atlas CLI installer.
#   curl -fsSL https://raw.githubusercontent.com/outcompute/integer-atlas-cli/main/install.sh | sh
# Env: INSTALL_DIR (default /usr/local/bin).
set -e

OWNER=outcompute
REPO=integer-atlas-cli
BIN=integer-atlas
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

os=$(uname -s)
arch=$(uname -m)
case "$os" in
  Darwin)
    case "$arch" in
      arm64) asset=integer-atlas-darwin-arm64 ;;
      x86_64) asset=integer-atlas-darwin-amd64 ;;
      *) echo "unsupported macOS arch: $arch" >&2; exit 1 ;;
    esac ;;
  Linux)
    case "$arch" in
      x86_64|amd64) asset=integer-atlas-linux-amd64 ;;
      *) echo "unsupported Linux arch: $arch (build from source: go build ./...)" >&2; exit 1 ;;
    esac ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac

url="https://github.com/$OWNER/$REPO/releases/latest/download/$asset"
echo "Downloading $asset ..."
tmp=$(mktemp)
curl -fsSL "$url" -o "$tmp"
chmod +x "$tmp"

if [ -w "$INSTALL_DIR" ]; then
  mv "$tmp" "$INSTALL_DIR/$BIN"
else
  echo "Installing to $INSTALL_DIR (needs sudo) ..."
  sudo mv "$tmp" "$INSTALL_DIR/$BIN"
fi

echo "Installed $BIN -> $INSTALL_DIR/$BIN"
echo
echo "Get started:"
echo "  $BIN packs                         # what data exists"
echo "  $BIN fetch --start 1 --end 1000000 --columns omega_big,is_prime"
echo "  $BIN sql \"SELECT count(*) FROM numbers\""
echo
echo "Optional UI (notebooks + dashboards):"
echo "  git clone https://github.com/$OWNER/integer-atlas-ui"
echo "  cd integer-atlas-ui && cp .env.example .env"
echo "  docker compose --profile build build && docker compose up -d   # http://localhost:8000"

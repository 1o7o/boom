#!/usr/bin/env bash
set -euo pipefail

# ===== 你的仓库 =====
REPO="1o7o/boom"
# =====================

BIN="boom"
BASE="https://v4.gh-proxy.org/https://github.com/${REPO}/releases/latest/download"

OS="$(uname -s)"
if [ "$OS" != "Linux" ]; then
  echo "此脚本仅支持 Linux，当前系统: $OS" >&2
  exit 1
fi

ARCH_RAW="$(uname -m)"
case "$ARCH_RAW" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "不支持的架构: $ARCH_RAW" >&2; exit 1 ;;
esac

FILE="${BIN}_linux_${ARCH}"
URL="${BASE}/${FILE}"

SUDO=""
if [ -w /usr/local/bin ] 2>/dev/null; then
  DEST="/usr/local/bin"
elif command -v sudo >/dev/null 2>&1; then
  DEST="/usr/local/bin"
  SUDO="sudo"
else
  DEST="$HOME/.local/bin"
  mkdir -p "$DEST"
fi

TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT

echo "下载 $URL"
if command -v curl >/dev/null 2>&1; then
  curl -fL --retry 3 --connect-timeout 15 -o "$TMP" "$URL"
elif command -v wget >/dev/null 2>&1; then
  wget -O "$TMP" "$URL"
else
  echo "需要 curl 或 wget" >&2
  exit 1
fi

chmod +x "$TMP"
$SUDO mv "$TMP" "$DEST/$BIN"
$SUDO chmod +x "$DEST/$BIN"

echo "✅ 已安装到 $DEST/$BIN"
if ! echo ":$PATH:" | grep -q ":$DEST:"; then
  echo "⚠️  $DEST 不在 PATH 中，请把下面一行加到 ~/.bashrc 或 ~/.zshrc："
  echo "  export PATH=\"$DEST:\$PATH\""
fi
echo "运行 $BIN 开始"

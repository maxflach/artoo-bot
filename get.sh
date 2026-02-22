#!/bin/bash
# get.sh — download and install the latest artoo-bot release
# Usage: curl -fsSL https://raw.githubusercontent.com/maxflach/artoo-bot/main/get.sh | bash
set -e

REPO="maxflach/artoo-bot"
INSTALL_DIR="/usr/local/bin"
BIN_NAME="artoo"

# ── Detect OS and arch ────────────────────────────────────────────────────────
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Darwin) OS_SLUG="darwin" ;;
  Linux)  OS_SLUG="linux"  ;;
  *)
    echo "Unsupported OS: $OS"
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64)          ARCH_SLUG="amd64" ;;
  arm64 | aarch64) ARCH_SLUG="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

ASSET="artoo-${OS_SLUG}-${ARCH_SLUG}"

# ── Find latest release ───────────────────────────────────────────────────────
echo "Fetching latest release..."
LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\(.*\)".*/\1/')

if [ -z "$LATEST" ]; then
  echo "Could not determine latest release. Check your internet connection."
  exit 1
fi

echo "Latest version: $LATEST"

DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST}/${ASSET}"

# ── Download ──────────────────────────────────────────────────────────────────
TMP="$(mktemp)"
echo "Downloading ${ASSET}..."
curl -fsSL "$DOWNLOAD_URL" -o "$TMP"
chmod +x "$TMP"

# ── Install ───────────────────────────────────────────────────────────────────
# Try /usr/local/bin first; fall back to ~/.local/bin if no permission
if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP" "${INSTALL_DIR}/${BIN_NAME}"
else
  INSTALL_DIR="$HOME/.local/bin"
  mkdir -p "$INSTALL_DIR"
  mv "$TMP" "${INSTALL_DIR}/${BIN_NAME}"
  # Warn if not in PATH
  case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *)
      echo ""
      echo "Note: $INSTALL_DIR is not in your PATH."
      echo "Add this to your shell profile:"
      echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
      ;;
  esac
fi

echo ""
echo "✓ artoo ${LATEST} installed → ${INSTALL_DIR}/${BIN_NAME}"
echo ""
echo "Run the setup wizard:"
echo "  artoo --setup"
echo ""
echo "Then install as a background service:"
echo "  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | bash"

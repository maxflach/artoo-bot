#!/bin/bash
# get.sh — download and install the latest artoo-bot release
# Usage: curl -fsSL https://raw.githubusercontent.com/maxflach/artoo-bot/main/get.sh | bash
set -e

REPO="maxflach/artoo-bot"
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
ICNS_URL="https://github.com/${REPO}/releases/download/${LATEST}/artoo.icns"

# ── Install dir ───────────────────────────────────────────────────────────────
INSTALL_DIR="/usr/local/bin"
if [ ! -w "$INSTALL_DIR" ]; then
  INSTALL_DIR="$HOME/.local/bin"
  mkdir -p "$INSTALL_DIR"
fi

# ── Download binary ───────────────────────────────────────────────────────────
TMP="$(mktemp)"
echo "Downloading ${ASSET}..."
curl -fsSL "$DOWNLOAD_URL" -o "$TMP"
chmod +x "$TMP"
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

echo "✓ artoo ${LATEST} installed → ${INSTALL_DIR}/${BIN_NAME}"

# ── macOS: create Artoo.app bundle with icon and local code signing ───────────
if [ "$OS_SLUG" = "darwin" ]; then
  echo ""
  echo "Setting up Artoo.app bundle..."

  BUNDLE_DIR="$HOME/.local/share/artoo"
  BUNDLE="$BUNDLE_DIR/Artoo.app"
  mkdir -p "$BUNDLE/Contents/MacOS" "$BUNDLE/Contents/Resources"

  # Copy binary into bundle
  cp "${INSTALL_DIR}/${BIN_NAME}" "$BUNDLE/Contents/MacOS/artoo"

  # Download icon
  ICNS_TMP="$(mktemp).icns"
  if curl -fsSL "$ICNS_URL" -o "$ICNS_TMP" 2>/dev/null; then
    cp "$ICNS_TMP" "$BUNDLE/Contents/Resources/artoo.icns"
    rm -f "$ICNS_TMP"
    ICON_KEY='<key>CFBundleIconFile</key><string>artoo</string>'
  else
    ICON_KEY=""
  fi

  cat > "$BUNDLE/Contents/Info.plist" << PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>artoo</string>
    <key>CFBundleIdentifier</key>
    <string>com.bot.artoo</string>
    <key>CFBundleName</key>
    <string>Artoo</string>
    <key>CFBundleIconFile</key>
    <string>artoo</string>
    <key>CFBundleVersion</key>
    <string>${LATEST}</string>
    <key>CFBundleShortVersionString</key>
    <string>${LATEST}</string>
    <key>LSUIElement</key>
    <true/>
</dict>
</plist>
PLIST

  # Create and trust a local code-signing certificate if not already present
  CERT_NAME="$(id -F 2>/dev/null || echo "$USER")"
  if ! security find-identity -v -p codesigning 2>/dev/null | grep -qF "$CERT_NAME"; then
    echo "Creating local code-signing certificate for '$CERT_NAME'..."
    _KEY="$(mktemp).key"
    _CSR="$(mktemp).csr"
    _CRT="$(mktemp).crt"
    _P12="$(mktemp).p12"
    _EXT="$(mktemp).ext"

    cat > "$_EXT" << 'EXTEOF'
[req]
distinguished_name = req_distinguished_name
x509_extensions = v3_req
[req_distinguished_name]
[v3_req]
keyUsage = digitalSignature
extendedKeyUsage = codeSigning
EXTEOF

    openssl genrsa -out "$_KEY" 2048 2>/dev/null
    openssl req -new -key "$_KEY" -out "$_CSR" \
      -subj "/CN=${CERT_NAME}/O=${CERT_NAME}/C=SE" 2>/dev/null
    openssl x509 -req -days 3650 -in "$_CSR" -signkey "$_KEY" -out "$_CRT" \
      -extensions v3_req -extfile "$_EXT" 2>/dev/null
    openssl pkcs12 -export -out "$_P12" -inkey "$_KEY" -in "$_CRT" \
      -passout pass:local 2>/dev/null

    security import "$_P12" \
      -k ~/Library/Keychains/login.keychain-db \
      -P local -T /usr/bin/codesign -f pkcs12 2>/dev/null
    security add-trusted-cert -d -r trustRoot \
      -k ~/Library/Keychains/login.keychain-db "$_CRT" 2>/dev/null

    rm -f "$_KEY" "$_CSR" "$_CRT" "$_P12" "$_EXT"
    echo "✓ Certificate '$CERT_NAME' created and trusted"
  fi

  # Sign the bundle
  if security find-identity -v -p codesigning 2>/dev/null | grep -qF "$CERT_NAME"; then
    codesign --force --sign "$CERT_NAME" --options runtime --timestamp=none "$BUNDLE" 2>/dev/null \
      && echo "✓ Artoo.app signed by '$CERT_NAME'"
  fi

  echo "✓ Artoo.app bundle → $BUNDLE"
  echo ""
  echo "The LaunchAgent install script will use the app bundle automatically."
fi

echo ""
echo "Run the setup wizard:"
echo "  artoo --setup"
echo ""
echo "Then install as a background service:"
echo "  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | bash"

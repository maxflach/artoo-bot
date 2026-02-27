#!/bin/bash
# release.sh — build cross-platform binaries and publish a GitHub release
# Usage: bash release.sh [tag]
#   tag defaults to the latest git tag (e.g. v0.19)
set -e

REPO="maxflach/artoo-bot"
BOT_DIR="$(cd "$(dirname "$0")" && pwd)"
TAG="${1:-$(git -C "$BOT_DIR" describe --tags --abbrev=0)}"

echo "Building release $TAG..."

# ── Build all targets ─────────────────────────────────────────────────────────
OUT="$BOT_DIR/releases"
mkdir -p "$OUT"

cd "$BOT_DIR/src"

TARGETS=(
  "darwin  arm64"
  "darwin  amd64"
  "linux   arm64"
  "linux   amd64"
)

for target in "${TARGETS[@]}"; do
  OS=$(echo "$target" | awk '{print $1}')
  ARCH=$(echo "$target" | awk '{print $2}')
  NAME="artoo-${OS}-${ARCH}"
  echo "  → $NAME"
  GOOS=$OS GOARCH=$ARCH go build -o "$OUT/$NAME" .
done

echo "All binaries built."

# ── Extract changelog section for this tag ────────────────────────────────────
NOTES=$(awk "/^## ${TAG}/,/^## v/" "$BOT_DIR/CHANGELOG.md" \
  | grep -v "^## v" | sed '/^[[:space:]]*$/d' | head -40)

if [ -z "$NOTES" ]; then
  NOTES="Release $TAG"
fi

# ── Create or update GitHub release ──────────────────────────────────────────
echo "Publishing GitHub release $TAG..."

# Delete existing release if it's a draft (allows re-running)
if gh release view "$TAG" --repo "$REPO" --json isDraft -q '.isDraft' 2>/dev/null | grep -q true; then
  gh release delete "$TAG" --repo "$REPO" --yes 2>/dev/null || true
fi

gh release create "$TAG" \
  --repo "$REPO" \
  --title "$TAG" \
  --notes "$NOTES" \
  "$OUT/artoo-darwin-arm64" \
  "$OUT/artoo-darwin-amd64" \
  "$OUT/artoo-linux-arm64" \
  "$OUT/artoo-linux-amd64" \
  "$BOT_DIR/artoo.icns"

echo ""
echo "✓ Release $TAG published"
echo "  https://github.com/${REPO}/releases/tag/${TAG}"

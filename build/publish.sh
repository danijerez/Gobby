#!/usr/bin/env bash
# Cross-compile Gobby for the three server platforms into bin/, with the platform
# in each filename (gobby-<platform>[.exe]). Pure-Go SQLite (modernc) means no CGO
# and no toolchain per target. Runs from anywhere — it cd's to the repo root first.
#   ./build/publish.sh [version]
set -euo pipefail

# Anchor to the repo root (this script lives in build/), so ./src and bin/ resolve
# no matter where it's invoked from.
cd "$(dirname "$0")/.."

VERSION="${1:-$(git describe --tags --always 2>/dev/null || echo dev)}"
OUT="bin"
LDFLAGS="-s -w -X main.version=${VERSION}"

mkdir -p "$OUT"
echo "Building Gobby ${VERSION} → ${OUT}/"

# target:  GOOS GOARCH output-name
targets=(
  "windows amd64 gobby-windows-amd64.exe"
  "linux   amd64 gobby-linux-amd64"
  "darwin  arm64 gobby-macos-arm64"   # Apple Silicon
)

for t in "${targets[@]}"; do
  set -- $t
  goos=$1 goarch=$2 name=$3
  echo "  $goos/$goarch → $OUT/$name"
  CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch \
    go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/$name" ./src
done

echo "Done:"
ls -lh "$OUT"/gobby-*

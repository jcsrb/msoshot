#!/usr/bin/env bash
# Builds the Windows exes (x64 and ARM64) from any OS. No C compiler needed.
# Matches Wails' own production build: the "production" tag, a GUI-subsystem
# exe, and an embedded manifest (per-monitor-v2 DPI awareness, run as invoker,
# common controls v6), version info and icon.
#
# The version comes from VERSION, else the latest git tag (v1.2.3), else "dev".
#
# The manifest and icon are compiled into .syso files by go-winres
# (github.com/tc-hib/go-winres), fetched by `go run` on first use.
# Set NO_RESOURCES=1 to skip that step (the app still works; Wails sets DPI
# awareness at runtime, but the exe has no icon or version info).
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p bin

VERSION="${VERSION:-$(git describe --tags --abbrev=0 2>/dev/null || echo dev)}"
VERSION="${VERSION#v}"

if [ "${NO_RESOURCES:-}" != "1" ]; then
  # Windows version resources need four numbers; "dev" becomes 0.0.0.0.
  numeric="0.0.0"
  if [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+ ]]; then numeric="${BASH_REMATCH[0]}"; fi
  go run ./internal/icon/cmd/genicon winres/icon.png
  trap 'rm -f rsrc_windows_*.syso winres/icon.png' EXIT
  go run github.com/tc-hib/go-winres@v0.3.3 make --in winres/winres.json --arch amd64,arm64 --out rsrc \
    --product-version "$numeric.0" --file-version "$numeric.0"
fi

for arch in amd64 arm64; do
  GOOS=windows GOARCH=$arch CGO_ENABLED=0 \
    go build -tags production -trimpath -buildvcs=false \
      -ldflags "-H windowsgui -s -w -X github.com/jcsrb/outlook-render/internal/api.Version=$VERSION" \
      -o "bin/outlook-render-windows-$arch.exe" .
done
echo "built version $VERSION"
ls -l bin

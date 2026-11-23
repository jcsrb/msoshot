#!/usr/bin/env bash
# Builds the Windows exes (x64 and ARM64) from any OS. No C compiler needed.
# Matches Wails' own production build: the "production" tag, a GUI-subsystem
# exe, and an embedded manifest (per-monitor-v2 DPI awareness, run as invoker,
# common controls v6), version info and icon.
#
# The manifest and icon are compiled into .syso files by go-winres
# (github.com/tc-hib/go-winres), fetched by `go run` on first use.
# Set NO_RESOURCES=1 to skip that step (the app still works; Wails sets DPI
# awareness at runtime, but the exe has no icon or version info).
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p bin

if [ "${NO_RESOURCES:-}" != "1" ]; then
  go run ./internal/icon/cmd/genicon winres/icon.png
  go run github.com/tc-hib/go-winres@v0.3.3 make --in winres/winres.json --arch amd64,arm64 --out rsrc
  trap 'rm -f rsrc_windows_*.syso winres/icon.png' EXIT
fi

for arch in amd64 arm64; do
  GOOS=windows GOARCH=$arch CGO_ENABLED=0 \
    go build -tags production -trimpath -buildvcs=false -ldflags "-H windowsgui -s -w" \
      -o "bin/outlook-render-windows-$arch.exe" .
done
ls -l bin

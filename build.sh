#!/usr/bin/env bash
# Builds the Windows exes (x64 and ARM64) from any OS. No C compiler needed.
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p bin
for arch in amd64 arm64; do
  GOOS=windows GOARCH=$arch CGO_ENABLED=0 \
    go build -trimpath -ldflags "-H windowsgui -s -w" -o "bin/outlook-render-windows-$arch.exe" .
done
ls -l bin

#!/bin/sh
# Build release archives for each supported platform.
set -eu
export LC_ALL=C
version=${1:?Usage: scripts/release.sh VERSION}
case "$version" in ''|*[!A-Za-z0-9._-]*) echo 'Invalid version.' >&2; exit 1 ;; esac
out=${2:-dist}
mkdir -p "$out"
for platform in darwin linux windows; do
  for arch in amd64 arm64; do
    work=$(mktemp -d)
    executable=willys
    if [ "$platform" = windows ]; then executable=willys.exe; fi
    CGO_ENABLED=0 GOOS="$platform" GOARCH="$arch" go build -trimpath \
      -ldflags "-s -w -X github.com/aluxf/willys-cli/internal/app.Version=$version" \
      -o "$work/$executable" ./cmd/willys
    if [ "$platform" = windows ]; then
      absolute_out=$(cd "$out" && pwd)
      (cd "$work" && zip -q "$absolute_out/willys_${platform}_${arch}.zip" "$executable")
    else
      tar -czf "$out/willys_${platform}_${arch}.tar.gz" -C "$work" "$executable"
    fi
    rm -rf "$work"
  done
done
(cd "$out" && if command -v sha256sum >/dev/null 2>&1; then
  sha256sum willys_*.tar.gz willys_*.zip > checksums.txt
else
  shasum -a 256 willys_*.tar.gz willys_*.zip > checksums.txt
fi)

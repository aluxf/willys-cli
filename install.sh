#!/bin/sh
# Install a standalone release. No language runtime is required.
set -eu
export LC_ALL=C

fail() { echo "Error: $*" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || fail 'curl is required.'
command -v tar >/dev/null 2>&1 || fail 'tar is required.'
case "$(uname -s)" in
  Darwin) platform=darwin ;;
  Linux) platform=linux ;;
  *) fail 'Use a release executable for your operating system.' ;;
esac
case "$(uname -m)" in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64) arch=amd64 ;;
  *) fail 'This processor architecture is not supported.' ;;
esac
version=${WILLYS_VERSION:-v0.2.0-beta.6}
case "$version" in ''|*[!A-Za-z0-9._-]*) fail 'Invalid WILLYS_VERSION.' ;; esac
repo=https://github.com/aluxf/willys-cli
if [ "$version" = latest ]; then
  release="$repo/releases/latest/download"
else
  release="$repo/releases/download/$version"
fi
asset="willys_${platform}_${arch}.tar.gz"
tmp=$(mktemp -d) || fail 'Cannot create a temporary directory.'
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
curl --proto '=https' --tlsv1.2 -fsSL "$release/$asset" -o "$tmp/$asset"
curl --proto '=https' --tlsv1.2 -fsSL "$release/checksums.txt" -o "$tmp/checksums.txt"
expected=$(awk -v name="$asset" '$2 == name {print $1}' "$tmp/checksums.txt")
[ "${#expected}" -eq 64 ] || fail 'The release has no valid checksum for this executable.'
case "$expected" in *[!0-9a-fA-F]*) fail 'Invalid release checksum.' ;; esac
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$asset" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')
else
  fail 'sha256sum or shasum is required.'
fi
[ "$actual" = "$expected" ] || fail 'Checksum verification failed. Nothing was installed.'
tar -xzf "$tmp/$asset" -C "$tmp" willys
[ -f "$tmp/willys" ] && [ ! -L "$tmp/willys" ] || fail 'The release executable is missing.'
chmod 755 "$tmp/willys"
"$tmp/willys" version
destination=${WILLYS_INSTALL_DIR:-"$HOME/.local/bin"}
mkdir -p "$destination"
staged=$(mktemp "$destination/.willys-install.XXXXXX") || fail 'Cannot write to the install directory.'
if ! cp "$tmp/willys" "$staged" || ! chmod 755 "$staged" || ! mv -f "$staged" "$destination/willys"; then
  rm -f "$staged"
  fail 'Installation failed.'
fi
printf 'Installed %s\n' "$destination/willys"
case ":$PATH:" in
  *":$destination:"*) echo 'Run: willys --help' ;;
  *) printf 'Add this directory to PATH: %s\n' "$destination"
     echo 'For the default location, run: export PATH="$HOME/.local/bin:$PATH"' ;;
esac

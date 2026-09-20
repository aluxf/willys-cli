#!/bin/sh
# Test the real installer with synthetic release downloads.
set -eu
export LC_ALL=C
root=$(pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
mkdir -p "$tmp/bin" "$tmp/release" "$tmp/content" "$tmp/destination"
cat > "$tmp/content/willys" <<'BIN'
#!/bin/sh
printf 'willys test\n'
BIN
chmod +x "$tmp/content/willys"
tar -czf "$tmp/release/willys_linux_amd64.tar.gz" -C "$tmp/content" willys
(cd "$tmp/release" && shasum -a 256 willys_linux_amd64.tar.gz > checksums.txt)
cat > "$tmp/bin/uname" <<'MOCK'
#!/bin/sh
case "$1" in -s) echo Linux ;; -m) echo x86_64 ;; esac
MOCK
cat > "$tmp/bin/curl" <<'MOCK'
#!/bin/sh
while [ "$#" -gt 0 ]; do
 case "$1" in
   https://*) asset=${1##*/} ;;
   -o) shift; destination=$1 ;;
 esac
 shift
done
cp "$TEST_RELEASE/$asset" "$destination"
MOCK
chmod +x "$tmp/bin/uname" "$tmp/bin/curl"
export TEST_RELEASE="$tmp/release"
export WILLYS_INSTALL_DIR="$tmp/destination"
PATH="$tmp/bin:$PATH" sh "$root/install.sh" >/dev/null
[ "$("$tmp/destination/willys" version)" = 'willys test' ]
printf 'not an archive' >> "$tmp/release/willys_linux_amd64.tar.gz"
if PATH="$tmp/bin:$PATH" sh "$root/install.sh" > "$tmp/log" 2>&1; then
 echo 'Installer accepted a corrupt archive.' >&2; exit 1
fi
[ "$("$tmp/destination/willys" version)" = 'willys test' ]
echo 'Installer passed success and checksum-failure tests.'

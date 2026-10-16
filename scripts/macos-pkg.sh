#!/bin/sh
# Builds the macOS installer package of the managed install. The package
# puts the binary at the root-owned path that the managed hook entries
# name, and nothing else: gryph install --managed makes the service
# account, the keys and the LaunchDaemon. The package is not signed. A
# Jamf policy installs it as root, and Gatekeeper does not check a package
# that no browser downloaded.
#
# Usage: scripts/macos-pkg.sh <gryph binary> <version> <output .pkg>
set -eu

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <gryph binary> <version> <output .pkg>" >&2
  exit 2
fi
binary="$1"
version="$2"
output="$3"

root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT

install -d -m 0755 "$root/opt/safedep/gryph/bin"
install -m 0755 "$binary" "$root/opt/safedep/gryph/bin/gryph"

# recommended ownership makes every payload file root:wheel on the host.
pkgbuild \
  --root "$root" \
  --identifier io.safedep.gryph \
  --version "$version" \
  --install-location / \
  --ownership recommended \
  "$output"

#!/bin/sh
# The post-install script of the Linux packages. It makes the service
# account of the decision service, so that gryph install --managed hands
# the state directory and the machine keys to it on the first run. The
# account has no home of its own and no login shell.
set -eu

account="_gryph"
home="/var/lib/safedep/gryph"

if getent passwd "$account" >/dev/null 2>&1; then
  exit 0
fi

shell="/usr/sbin/nologin"
if [ ! -x "$shell" ]; then
  shell="/sbin/nologin"
fi
if [ ! -x "$shell" ]; then
  shell="/bin/false"
fi

# Debian, Fedora and Arch Linux ship useradd. Alpine ships the BusyBox
# adduser, which needs the group first.
if command -v useradd >/dev/null 2>&1; then
  useradd --system --no-create-home --home-dir "$home" --shell "$shell" --user-group "$account"
else
  addgroup -S "$account"
  adduser -S -D -H -h "$home" -s "$shell" -G "$account" "$account"
fi

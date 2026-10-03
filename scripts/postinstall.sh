#!/bin/sh
# The post-install script of the deb and rpm packages. It makes the service
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
  shell="/bin/false"
fi

useradd --system --no-create-home --home-dir "$home" --shell "$shell" --user-group "$account"

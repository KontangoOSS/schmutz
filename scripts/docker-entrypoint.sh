#!/bin/sh
# schmutz container entrypoint
#
# Smart bootstrap flow:
#   1. If /etc/schmutz/identity.json already exists → skip enroll, go to run
#   2. Else if SCHMUTZ_CONTROLLER is set → run schmutz enroll
#   3. Else → fail with a clear message
#
# After bootstrap, this exec's into `schmutz start` (the in-process Ziti
# tunnel daemon) so the container is supervised by the Ziti SDK lifecycle.
#
# Override the default behavior by passing arguments to `docker run`:
#   docker run schmutz status                    # one-shot status
#   docker run schmutz tunnel tproxy --help      # ziti tunnel passthrough
set -e

SCHMUTZ_DIR="${SCHMUTZ_DIR:-/etc/schmutz}"
IDENTITY="${SCHMUTZ_DIR}/identity.json"

# If caller passed any args, treat this as a one-shot command rather than
# the default bootstrap-then-run flow. Useful for `docker run schmutz status`.
if [ "$#" -gt 0 ]; then
  exec /usr/local/bin/schmutz "$@"
fi

if [ ! -f "$IDENTITY" ]; then
  if [ -z "$SCHMUTZ_CONTROLLER" ]; then
    cat >&2 <<EOF
schmutz: no identity at $IDENTITY and SCHMUTZ_CONTROLLER not set.

To bootstrap a fresh container:
  docker run -e SCHMUTZ_CONTROLLER=https://ctrl-1.konoss.org \\
             -e SCHMUTZ_INSTALL_CODE=<code> \\
             -v schmutz-config:/etc/schmutz \\
             schmutz

To use an existing identity:
  docker run -v /path/to/identity.json:/etc/schmutz/identity.json \\
             -v schmutz-config:/etc/schmutz \\
             schmutz
EOF
    exit 1
  fi

  echo "schmutz: enrolling against $SCHMUTZ_CONTROLLER..."
  /usr/local/bin/schmutz enroll --controller "$SCHMUTZ_CONTROLLER"
fi

echo "schmutz: starting tunnel..."
exec /usr/local/bin/schmutz start

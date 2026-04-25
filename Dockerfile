# schmutz container image — wraps the schmutz agent + bundled ziti CLI.
#
# Built by GoReleaser at release time. The build context comes from the
# repo root via the `extra_files` directives in .goreleaser.yaml.
#
# Image layout:
#   /usr/local/bin/schmutz   — our binary (statically linked)
#   /usr/local/bin/ziti      — upstream Ziti CLI (pinned, glibc-linked)
#   /etc/schmutz/            — runtime config dir, expected to be a volume
#   /entrypoint.sh           — auto-enroll-then-run smart entrypoint
#
# Runtime contract:
#   - Mount /etc/schmutz as a persistent volume so identity survives restart
#   - Set SCHMUTZ_CONTROLLER if no identity is present (entrypoint enrolls)
#   - Set SCHMUTZ_INSTALL_CODE if your network requires it for first install
#
# Why alpine, not scratch:
#   The upstream ziti binary is glibc-linked. Pure-scratch wouldn't work
#   for `tunnel host/proxy/tproxy` subcommands. Alpine + gcompat gives us
#   ~12MB base + ld-linux compatibility for ziti.

FROM alpine:3.20

# gcompat lets glibc-linked binaries (ziti) run on a musl distro.
# ca-certificates so HTTPS to the controller works.
# tzdata so the agent reports a real timezone.
RUN apk add --no-cache ca-certificates gcompat tzdata

# GoReleaser passes the per-arch ziti path via --build-arg
ARG ZITI_BIN

# Schmutz binary lands at /schmutz/schmutz_linux_<arch>_v1/schmutz when
# goreleaser uses the `dockers` block; we copy by glob to be arch-agnostic.
COPY schmutz /usr/local/bin/schmutz
COPY ${ZITI_BIN} /usr/local/bin/ziti
COPY scripts/docker-entrypoint.sh /entrypoint.sh
COPY systemd/schmutz.service /etc/schmutz/schmutz.service

RUN chmod +x /usr/local/bin/schmutz /usr/local/bin/ziti /entrypoint.sh \
 && mkdir -p /etc/schmutz

# Volume for persistent identity. Operators should mount real storage here.
VOLUME ["/etc/schmutz"]

# No EXPOSE — the agent doesn't listen on a public port. All traffic is
# overlay (Ziti SDK dial/bind) initiated outbound.

ENTRYPOINT ["/entrypoint.sh"]
CMD []

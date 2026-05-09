# server/

Two Go binaries for the Ziti-overlay control plane.

## Binaries

### `enroll-server` (`cmd/enroll-server/`)

Public-internet enrollment endpoint. Accepts a single-use token, looks it up in
Bao, calls Ziti to create the identity + per-machine SSH service + bind/dial
policies, returns the OTT JWT. Stateless. Listens on a plain HTTP port (TLS
terminates upstream). Existed before schmutz-server.

### `schmutz-server` (`cmd/schmutz-server/`)

Overlay-only admin control plane. Exposes admin-tier operations behind RBAC
middleware: list/get/approve/deny/patch/delete identities, issue/list/get/revoke
tokens, query the audit log, healthz, whoami. Listens on a Ziti SDK-bound
service (`admin.tango` by default) so callers must already be enrolled overlay
identities. Caller identity is lifted from the SDK connection into request
context; `RequireAdmin` middleware gates the handler chain on the `#admins`
role attribute, with a separate `#admins-break-glass` tier for admin-tier
identity modifications.

Has a second startup mode (`SCHMUTZ_BOOTSTRAP=1`) that runs a separate handler
set on plain HTTP at `127.0.0.1:8766` for one-time cluster setup: bao init,
distribute keys, raft join, apply enroll policy, create the genesis
break-glass admin identity. Bootstrap mode does not require Bao or Ziti to be
up at startup.

## Running

```bash
make build              # both binaries → build/
make docker             # both images → git.kontango.io/kore/{enroll,schmutz}-server:{latest,<sha>}
make test               # go test -race ./...
```

### `enroll-server` env vars

| var | default | required |
|---|---|---|
| `BAO_ADDR` | — | yes |
| `BAO_TOKEN` | — | yes |
| `BAO_SKIP_VERIFY` | `0` | no |
| `BAO_MOUNT` | `secret` | no |
| `BAO_TOKEN_PREFIX` | `enroll-tokens` | no |
| `ZITI_API` | — | yes |
| `ZITI_USERNAME` | — | yes |
| `ZITI_PASSWORD` | — | yes |
| `LISTEN_ADDR` | `127.0.0.1:8765` | no |

### `schmutz-server` env vars

Normal mode requires the same Bao + Ziti vars as `enroll-server`, plus:

| var | default | required |
|---|---|---|
| `SCHMUTZ_LISTEN_ADDR` | `127.0.0.1:8766` | bootstrap mode only (normal mode binds via Ziti SDK) |
| `SCHMUTZ_ZITI_IDENTITY_FILE` | `/etc/schmutz/server-identity.json` | normal mode |
| `SCHMUTZ_SERVICE_NAME` | `admin.tango` | no |
| `SCHMUTZ_BOOTSTRAP` | `0` (set to `1` for bootstrap mode) | no |

Bootstrap mode skips the Bao + Ziti env-var check at startup. The
`create-break-glass` endpoint reads `BAO_ADDR`, `BAO_TOKEN`, `ZITI_API`,
`ZITI_USERNAME`, `ZITI_PASSWORD` from env at call time — set them after Bao
and Ziti come online but before invoking the endpoint.

**Footgun:** the default `SCHMUTZ_ZITI_IDENTITY_FILE` is also where
`create-break-glass` writes its output. When running the bootstrap binary,
override to a distinct path (e.g. `SCHMUTZ_ZITI_IDENTITY_FILE=/etc/schmutz/break-glass.json`)
or break-glass will overwrite the server's own identity.

## Deploy

`deploy/systemd/schmutz-server.service` — normal mode, requires
`ziti-controller.service` and `bao.service`, conflicts with the bootstrap
template.

`deploy/systemd/schmutz-server@bootstrap.service` — operator-driven bootstrap
mode, no automatic restart, conflicts with normal mode.

## E2E smoke

`tests/e2e/admin-roundtrip.sh` runs the full admin workflow against a real
overlay: whoami → issue token → list → enroll target → inspect quarantine →
approve → verify → delete → audit. Requires `curl` + `jq`, an `#admins`
identity, and `admin.tango` reachable. Set `TARGET_HOST` to ssh-enroll a
real machine; without it, only the token-issue/revoke path is exercised.

## Spec + plan

- `docs/superpowers/specs/2026-05-03-schmutz-server-design.md` — design spec
- `docs/superpowers/plans/2026-05-03-schmutz-server.md` — implementation plan

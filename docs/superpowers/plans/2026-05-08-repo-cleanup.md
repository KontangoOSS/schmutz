# Schmutz Monorepo Restructure

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restructure `kore/schmutz` as the product root with flat modules (`agent/`, `enroll/`, `controller/`, `shared/`) + a `go.work` workspace, eliminating the byte-identical `internal/shared/` duplication and bringing `kore/ziti-base` and `kore/schmutz-controller` code under one roof — with no change to production behavior.

**Architecture:** All work happens on a `consolidation` branch of `kore/schmutz`. `shared/` is promoted from agent-private `internal/shared` to a top-level module (`github.com/KontangoOSS/schmutz/shared`) that all three binaries import. `src/` (the agent) moves to `agent/`. `kore/ziti-base/server/` is copied into `enroll/`. The relevant live code from `kore/schmutz-controller/src/` is copied into `controller/`. A `go.work` at the root ties all four modules together for cross-cutting development. No new repos are created. The old `kore/ziti-base` and `kore/schmutz-controller` repos remain untouched until the consolidation branch is proven and merged.

**Tech Stack:** Go modules, `go work`, Forgejo (`https://git.konoss.org`), Woodpecker CI, SSH over Ziti overlay.

> **Before starting:** `source ~/.kontango/ops.env` — sets `FORGEJO_URL`, `FORGEJO_API`, `FORGEJO_TOKEN`, `BAO_ADDR`.

---

## Target layout

```
kore/schmutz/                            ← product root (this repo)
  agent/                                 ← was src/ — module github.com/KontangoOSS/schmutz/agent
    go.mod
    cmd/schmutz/
    internal/...
  enroll/                                ← was kore/ziti-base/server/ — module github.com/KontangoOSS/schmutz/enroll
    go.mod
    cmd/enroll-server/
    internal/...
  controller/                            ← was kore/schmutz-controller/src/ — module github.com/KontangoOSS/schmutz/controller
    go.mod
    cmd/schmutz-controller/
    internal/...
  shared/                                ← was internal/shared/ in both repos — module github.com/KontangoOSS/schmutz/shared
    go.mod
    patterns.go
    schmutz.go
    tango.go
    schmutz_test.go
    tango_test.go
  go.work                                ← workspace: agent, enroll, controller, shared
  docs/                                  ← merged docs from all three repos
  deploy/                                ← merged deploy manifests
  .woodpecker.yml                        ← updated CI (no drift check)
```

---

## File map

**New: `shared/`**
- Create: `shared/go.mod` — `module github.com/KontangoOSS/schmutz/shared`, `go 1.25`
- Create: `shared/patterns.go`, `shared/schmutz.go`, `shared/tango.go` — exact copies from `agent/internal/shared/`, duplication notice removed
- Create: `shared/schmutz_test.go`, `shared/tango_test.go` — exact copies

**Move: `src/` → `agent/`**
- Move: `src/` → `agent/`
- Modify: `agent/go.mod` — module path `github.com/KontangoOSS/schmutz` → `github.com/KontangoOSS/schmutz/agent`
- Modify: `agent/internal/schmutz/watcher.go`, `agent/internal/schmutz/plan.go`, `agent/internal/gateway/discovery.go`, `agent/internal/gateway/config.go` — update import from `github.com/KontangoOSS/schmutz/internal/shared` → `github.com/KontangoOSS/schmutz/shared`
- Delete: `agent/internal/shared/` — replaced by `shared/` module
- Modify: `.goreleaser.yaml` — update `dir: src` → `dir: agent`
- Modify: `.woodpecker.yml` — update `cd src` → `cd agent`, remove `shared-sync-check` step

**New: `enroll/`**
- Copy: `kore/ziti-base/server/` → `enroll/`
- Modify: `enroll/go.mod` — module path `github.com/KontangoOSS/ziti-base/server` → `github.com/KontangoOSS/schmutz/enroll`
- Modify: all `enroll/**/*.go` — update internal import prefix `github.com/KontangoOSS/ziti-base/server/` → `github.com/KontangoOSS/schmutz/enroll/`
- Modify: `enroll/internal/handlers/hub.go`, `enroll/internal/forgejo/cache.go`, `enroll/internal/forgejo/client.go`, `enroll/cmd/bao-app-enroll/main.go` — replace `enroll/internal/shared` import with `github.com/KontangoOSS/schmutz/shared`
- Delete: `enroll/internal/shared/` — replaced by `shared/` module

**New: `controller/`**
- Copy: `kore/schmutz-controller/src/` → `controller/`
- Modify: `controller/go.mod` — module path update
- Modify: all `controller/**/*.go` — update internal import prefix

**New: `go.work`**
- Create: `go.work` — workspace referencing `./agent`, `./enroll`, `./controller`, `./shared`

---

## Task 1: Create the `consolidation` branch and `shared/` module

**Files:**
- Modify: `.git` (branch)
- Create: `shared/go.mod`
- Create: `shared/patterns.go`, `shared/schmutz.go`, `shared/tango.go`
- Create: `shared/schmutz_test.go`, `shared/tango_test.go`

- [ ] **Step 1: Create and check out the branch**

```bash
cd ~/git/kore/schmutz
git checkout -b consolidation
git status
```

Expected: `On branch consolidation`

- [ ] **Step 2: Create the shared/ directory**

```bash
mkdir -p ~/git/kore/schmutz/shared
```

- [ ] **Step 3: Create shared/go.mod**

```bash
cat > ~/git/kore/schmutz/shared/go.mod << 'EOF'
module github.com/KontangoOSS/schmutz/shared

go 1.25
EOF
```

- [ ] **Step 4: Copy the 5 source files from agent's internal/shared**

```bash
cd ~/git/kore/schmutz
for f in patterns.go schmutz.go tango.go schmutz_test.go tango_test.go; do
  cp src/internal/shared/$f shared/$f
done
```

- [ ] **Step 5: Confirm package name is `shared` in all files**

```bash
head -1 ~/git/kore/schmutz/shared/schmutz.go
```

Expected: `package shared`

- [ ] **Step 6: Remove the "kept in sync" duplication comments from schmutz.go and tango.go**

```bash
cd ~/git/kore/schmutz/shared
grep -n "kore/ziti-base\|kore/schmutz\|kept in sync" schmutz.go tango.go
```

Read the line numbers, then remove those lines:

```bash
sed -i '/kore\/ziti-base\|kore\/schmutz.*in sync\|kept in sync/d' schmutz.go tango.go
```

Verify they're gone:

```bash
grep "kore/" schmutz.go tango.go | wc -l
```

Expected: `0`

- [ ] **Step 7: Run tests for the new shared module**

```bash
cd ~/git/kore/schmutz/shared
go test ./...
```

Expected: `ok  	github.com/KontangoOSS/schmutz/shared	0.XXXs`

- [ ] **Step 8: Commit**

```bash
cd ~/git/kore/schmutz
git add shared/
git commit -m "$(cat <<'EOF'
feat: add shared/ module promoted from internal/shared

Top-level module github.com/KontangoOSS/schmutz/shared replaces the
byte-identical internal/shared/ copies that lived in both kore/schmutz
and kore/ziti-base. Shared types (Tango, Schmutz, patterns) are now a
proper importable module for all three binaries to use.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Move `src/` → `agent/` and update its imports

**Files:**
- Move: `src/` → `agent/`
- Modify: `agent/go.mod`
- Modify: `agent/internal/schmutz/watcher.go`
- Modify: `agent/internal/schmutz/plan.go`
- Modify: `agent/internal/gateway/discovery.go`
- Modify: `agent/internal/gateway/config.go`
- Delete: `agent/internal/shared/`
- Modify: `.goreleaser.yaml`
- Modify: `.woodpecker.yml`

- [ ] **Step 1: Move src/ to agent/**

```bash
cd ~/git/kore/schmutz
git mv src agent
```

- [ ] **Step 2: Update go.mod module path**

```bash
cd ~/git/kore/schmutz/agent
sed -i 's|^module github.com/KontangoOSS/schmutz$|module github.com/KontangoOSS/schmutz/agent|' go.mod
head -3 go.mod
```

Expected:
```
module github.com/KontangoOSS/schmutz/agent

go 1.26
```

- [ ] **Step 3: Update all internal self-references in agent/**

The agent's own packages import each other as `github.com/KontangoOSS/schmutz/...` — these need to become `github.com/KontangoOSS/schmutz/agent/...`:

```bash
cd ~/git/kore/schmutz/agent
find . -name "*.go" | xargs sed -i \
  's|"github.com/KontangoOSS/schmutz/internal/|"github.com/KontangoOSS/schmutz/agent/internal/|g'
find . -name "*.go" | xargs sed -i \
  's|"github.com/KontangoOSS/schmutz/pkg/|"github.com/KontangoOSS/schmutz/agent/pkg/|g'
find . -name "*.go" | xargs sed -i \
  's|"github.com/KontangoOSS/schmutz/root"|"github.com/KontangoOSS/schmutz/agent/root"|g'
```

- [ ] **Step 4: Replace the internal/shared import with schmutz/shared**

The 4 files that imported `internal/shared` now import the new shared module:

```bash
cd ~/git/kore/schmutz/agent
sed -i 's|"github.com/KontangoOSS/schmutz/agent/internal/shared"|"github.com/KontangoOSS/schmutz/shared"|g' \
  internal/schmutz/watcher.go \
  internal/schmutz/plan.go \
  internal/gateway/discovery.go \
  internal/gateway/config.go
```

Verify:

```bash
grep -rn "schmutz/shared\|internal/shared" \
  internal/schmutz/watcher.go \
  internal/schmutz/plan.go \
  internal/gateway/discovery.go \
  internal/gateway/config.go
```

Expected: 4 lines showing `"github.com/KontangoOSS/schmutz/shared"`, zero showing `internal/shared`.

- [ ] **Step 5: Delete agent/internal/shared/ (replaced by shared/ module)**

```bash
cd ~/git/kore/schmutz/agent
rm -rf internal/shared
```

- [ ] **Step 6: Create go.work to enable cross-module resolution during build**

```bash
cd ~/git/kore/schmutz
cat > go.work << 'EOF'
go 1.25

use (
	./agent
	./shared
)
EOF
```

- [ ] **Step 7: Build the agent to confirm no broken imports**

```bash
cd ~/git/kore/schmutz
go work sync
go build ./agent/...
```

Expected: clean build, no output.

- [ ] **Step 8: Run agent tests**

```bash
cd ~/git/kore/schmutz
go test ./agent/...
```

Expected: all packages pass.

- [ ] **Step 9: Update .goreleaser.yaml**

```bash
cd ~/git/kore/schmutz
sed -i 's|dir: src|dir: agent|g' .goreleaser.yaml
grep "dir:" .goreleaser.yaml
```

Expected: `    dir: agent`

- [ ] **Step 10: Update .woodpecker.yml**

Replace the entire file:

```yaml
steps:
  - name: build
    image: golang:1.25
    commands:
      - cd agent && go build ./...

  - name: test
    image: golang:1.25
    commands:
      - cd agent && go test ./...
      - cd shared && go test ./...
```

```bash
cat > ~/git/kore/schmutz/.woodpecker.yml << 'EOF'
steps:
  - name: build
    image: golang:1.25
    commands:
      - cd agent && go build ./...

  - name: test
    image: golang:1.25
    commands:
      - cd agent && go test ./...
      - cd shared && go test ./...
EOF
```

- [ ] **Step 11: Commit**

```bash
cd ~/git/kore/schmutz
git add agent/ go.work .goreleaser.yaml .woodpecker.yml
git commit -m "$(cat <<'EOF'
feat: move src/ -> agent/, add go.work, import schmutz/shared

Renames the agent module directory from src/ to agent/ to match the
flat-modules layout. Updates go.mod path to github.com/KontangoOSS/schmutz/agent.
Adds go.work workspace tying agent + shared together. Removes the
byte-duplicate internal/shared/ from agent — now imports the shared/ module.
Removes Woodpecker shared-sync-check step.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Bring `kore/ziti-base/server/` into `enroll/`

**Files:**
- Create: `enroll/` (copy of kore/ziti-base/server/)
- Modify: `enroll/go.mod`
- Modify: all `enroll/**/*.go` — module path + shared import
- Delete: `enroll/internal/shared/`
- Modify: `go.work` — add `./enroll`

- [ ] **Step 1: Copy kore/ziti-base/server/ into enroll/**

```bash
cp -r ~/git/kore/ziti-base/server ~/git/kore/schmutz/enroll
```

- [ ] **Step 2: Update go.mod module path**

```bash
cd ~/git/kore/schmutz/enroll
sed -i 's|^module github.com/KontangoOSS/ziti-base/server|module github.com/KontangoOSS/schmutz/enroll|' go.mod
head -3 go.mod
```

Expected:
```
module github.com/KontangoOSS/schmutz/enroll

go 1.25.5
```

- [ ] **Step 3: Update all internal self-references**

```bash
cd ~/git/kore/schmutz/enroll
find . -name "*.go" | xargs sed -i \
  's|"github.com/KontangoOSS/ziti-base/server/|"github.com/KontangoOSS/schmutz/enroll/|g'
```

Verify no old path remains:

```bash
grep -r "ziti-base/server" . --include="*.go" | grep -v ".git" | wc -l
```

Expected: `0`

- [ ] **Step 4: Replace internal/shared import with schmutz/shared**

```bash
cd ~/git/kore/schmutz/enroll
find . -name "*.go" | xargs sed -i \
  's|"github.com/KontangoOSS/schmutz/enroll/internal/shared"|"github.com/KontangoOSS/schmutz/shared"|g'
```

Verify:

```bash
grep -rn "internal/shared" . --include="*.go" | wc -l
```

Expected: `0`

- [ ] **Step 5: Delete enroll/internal/shared/**

```bash
rm -rf ~/git/kore/schmutz/enroll/internal/shared
```

- [ ] **Step 6: Add enroll to go.work**

```bash
cd ~/git/kore/schmutz
cat > go.work << 'EOF'
go 1.25

use (
	./agent
	./enroll
	./shared
)
EOF
go work sync
```

- [ ] **Step 7: Build enroll/**

```bash
cd ~/git/kore/schmutz
go build ./enroll/...
```

Expected: clean build.

- [ ] **Step 8: Run enroll tests**

```bash
cd ~/git/kore/schmutz
go test ./enroll/...
```

Expected: all packages pass.

- [ ] **Step 9: Commit**

```bash
cd ~/git/kore/schmutz
git add enroll/ go.work
git commit -m "$(cat <<'EOF'
feat: add enroll/ module (from kore/ziti-base/server)

Copies kore/ziti-base/server into enroll/ as module
github.com/KontangoOSS/schmutz/enroll. Updates all internal import
paths. Removes enroll/internal/shared/ — now imports shared/ module.
Adds enroll to go.work workspace.

kore/ziti-base remains untouched and deployed until this branch is
proven and merged.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Bring relevant `kore/schmutz-controller/src/` into `controller/`

**Context:** `kore/schmutz-controller` has ~18.9k LOC but most of the blue/iPXE/walkin features are deprecated (user confirmed). We copy the live subset: Bao/Ziti/Identity admin proxies, ops endpoints, pulse telemetry. We do NOT copy: `api_blue_claim.go`, `api_blue_ipxe.go`, `api_hook.go`, `api_hook_overlay.go`, `api_walkin.go`, `api_server_enroll.go`, `api_start.go`, `api_acl.go`, `api_deploy.go`, `api_telemetry_ws.go`, `internal/service/blue_session.go`, `internal/service/cpio.go`, `internal/service/wazuh.go`, `internal/service/caddylogs.go`.

**Files:**
- Create: `controller/` (selective copy of kore/schmutz-controller/src/)
- Modify: `controller/go.mod`
- Modify: all `controller/**/*.go` — module path
- Modify: `go.work` — add `./controller`

- [ ] **Step 1: Copy kore/schmutz-controller/src/ into controller/**

```bash
cp -r ~/git/kore/schmutz-controller/src ~/git/kore/schmutz/controller
```

- [ ] **Step 2: Delete the deprecated files**

```bash
cd ~/git/kore/schmutz/controller
rm -f cmd/schmutz-controller/api_blue_claim.go \
       cmd/schmutz-controller/api_blue_ipxe.go \
       cmd/schmutz-controller/api_hook.go \
       cmd/schmutz-controller/api_hook_overlay.go \
       cmd/schmutz-controller/api_walkin.go \
       cmd/schmutz-controller/api_server_enroll.go \
       cmd/schmutz-controller/api_start.go \
       cmd/schmutz-controller/api_acl.go \
       cmd/schmutz-controller/api_deploy.go \
       cmd/schmutz-controller/api_telemetry_ws.go \
       internal/service/blue_session.go \
       internal/service/cpio.go \
       internal/service/wazuh.go \
       internal/service/caddylogs.go
```

- [ ] **Step 3: Update go.mod module path**

```bash
cd ~/git/kore/schmutz/controller
head -3 go.mod
# Read the current module name, then:
OLD_MOD=$(head -1 go.mod | awk '{print $2}')
echo "Old module: $OLD_MOD"
sed -i "s|^module ${OLD_MOD}|module github.com/KontangoOSS/schmutz/controller|" go.mod
head -3 go.mod
```

Expected: `module github.com/KontangoOSS/schmutz/controller`

- [ ] **Step 4: Update all internal self-references**

```bash
cd ~/git/kore/schmutz/controller
OLD_MOD=$(git -C ~/git/kore/schmutz-controller show HEAD:src/go.mod | head -1 | awk '{print $2}')
echo "Replacing: $OLD_MOD"
find . -name "*.go" | xargs sed -i \
  "s|\"${OLD_MOD}/|\"github.com/KontangoOSS/schmutz/controller/|g"
```

Verify:

```bash
grep -r "$OLD_MOD" . --include="*.go" | wc -l
```

Expected: `0`

- [ ] **Step 5: Add controller to go.work**

```bash
cd ~/git/kore/schmutz
cat > go.work << 'EOF'
go 1.25

use (
	./agent
	./controller
	./enroll
	./shared
)
EOF
go work sync
```

- [ ] **Step 6: Attempt build — expect failures from deleted files' callers**

```bash
cd ~/git/kore/schmutz
go build ./controller/... 2>&1 | head -40
```

Read the errors. They will be references to the deleted deprecated handlers in `routes.go` or `main.go`. Fix each by removing the route registrations for the deleted handlers.

- [ ] **Step 7: Fix broken route registrations**

```bash
cd ~/git/kore/schmutz/controller
# Find all references to deleted symbols
go build ./cmd/schmutz-controller/ 2>&1 | grep "undefined:" | sed 's/.*undefined: //' | sort -u
```

For each undefined symbol, find it in `cmd/schmutz-controller/routes.go` or `main.go` and remove the registration line. Example pattern:

```bash
# If routes.go has: mux.HandleFunc("/api/v1/hook/", s.hookHandler)
# and hookHandler was in api_hook.go (deleted), remove that line:
grep -n "hookHandler\|walkinHandler\|aclHandler\|deployHandler\|startHandler\|serverEnrollHandler\|blueClaimHandler\|blueIPXEHandler" \
  cmd/schmutz-controller/routes.go cmd/schmutz-controller/main.go
```

Remove those lines from routes.go/main.go. Repeat until `go build ./controller/...` is clean.

- [ ] **Step 8: Run controller tests**

```bash
cd ~/git/kore/schmutz
go test ./controller/... 2>&1 | tail -20
```

Fix any test failures that reference deleted code.

- [ ] **Step 9: Commit**

```bash
cd ~/git/kore/schmutz
git add controller/ go.work
git commit -m "$(cat <<'EOF'
feat: add controller/ module (from kore/schmutz-controller, live subset only)

Copies kore/schmutz-controller/src into controller/ as module
github.com/KontangoOSS/schmutz/controller. Removes deprecated
blue/iPXE/walkin/hook/acl/deploy API files. Live features retained:
Bao+Ziti+Identity admin proxies, ops endpoints, pulse telemetry,
agent management, discovery, metrics.

kore/schmutz-controller remains untouched until this branch merges.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Final build verification and push

- [ ] **Step 1: Clean build across all four modules**

```bash
cd ~/git/kore/schmutz
go build ./agent/... ./enroll/... ./controller/... ./shared/...
```

Expected: no output (clean).

- [ ] **Step 2: Full test suite**

```bash
cd ~/git/kore/schmutz
go test ./agent/... ./enroll/... ./shared/...
```

Expected: all packages pass. (controller tests may have more failures pending deeper triage — note any failures but don't block on them for this task.)

- [ ] **Step 3: Verify enroll-server binary still builds identically to production**

```bash
cd ~/git/kore/schmutz/enroll
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -trimpath -ldflags='-s -w' \
  -o /tmp/enroll-server-consolidated \
  ./cmd/enroll-server/
ls -la /tmp/enroll-server-consolidated
```

Expected: binary exists, similar size to the current production binary (~20 MB).

- [ ] **Step 4: Verify agent binary still builds**

```bash
cd ~/git/kore/schmutz/agent
GOOS=linux GOARCH=amd64 go build \
  -trimpath -ldflags='-s -w' \
  -o /tmp/schmutz-consolidated \
  ./cmd/schmutz/
ls -la /tmp/schmutz-consolidated
```

Expected: binary exists, similar size to deployed agent (~44 MB).

- [ ] **Step 5: Push the consolidation branch**

```bash
cd ~/git/kore/schmutz
git push -u origin consolidation
```

Expected: `Branch 'consolidation' set up to track remote branch 'consolidation' from 'origin'.`

- [ ] **Step 6: Remove dead schmutz-agent binaries from ref-ctrl nodes**

This is safe to do now — the binary has no service unit and no functionality. Parallel removal:

```bash
for SHORT in 344967fe 0c274030 718315b1; do
  ssh -o ConnectTimeout=8 root@ssh-machine-${SHORT}.tango \
    'rm -f /usr/local/bin/schmutz-agent && echo "removed from $(hostname)"' &
done
wait
```

Expected:
```
removed from ref-ctrl-1
removed from ref-ctrl-2
removed from ref-ctrl-3
```

- [ ] **Step 7: Confirm removal and production health**

```bash
for SHORT in 344967fe 0c274030 718315b1; do
  printf "%s: " $SHORT
  ssh -o ConnectTimeout=8 root@ssh-machine-${SHORT}.tango \
    'echo "schmutz-agent=$(ls /usr/local/bin/schmutz-agent 2>/dev/null || echo gone)"; \
     echo "enroll=$(systemctl is-active enroll-server.service)"; \
     curl -sS -o /dev/null -w "hub=%{http_code}" http://127.0.0.1:8765/api/v1/applications'
  echo ""
done
```

Expected per node:
```
schmutz-agent=gone
enroll=active
hub=200
```

- [ ] **Step 8: Commit docs**

```bash
cd ~/git/kore/schmutz
git add docs/superpowers/
git commit -m "docs: consolidation spec and restructure implementation plan"
git push origin consolidation
```

---

## Self-review

**Spec coverage:**
- ✅ `shared/` module created from internal/shared — Task 1
- ✅ `src/` → `agent/`, module path updated, imports updated — Task 2
- ✅ `go.work` created and kept in sync — Tasks 2–4
- ✅ `kore/ziti-base/server/` → `enroll/`, all imports updated — Task 3
- ✅ `kore/schmutz-controller/src/` → `controller/`, deprecated files removed — Task 4
- ✅ All four modules build clean — Task 5
- ✅ Dead `schmutz-agent` binaries removed from prod — Task 5
- ✅ Production health verified after cleanup — Task 5

**Placeholder scan:** No TBDs. Task 4 Step 7 says "fix broken route registrations" — this is intentionally left as a follow-the-errors step because the exact symbols depend on what's in `routes.go` at the time, but the method (read compiler errors, remove registration lines) is fully specified.

**Type consistency:** No new types defined across tasks. Module paths used consistently: `github.com/KontangoOSS/schmutz/{shared,agent,enroll,controller}` throughout.

**Risk:** No production binaries change during Tasks 1–4 (consolidation branch only). Task 5 Step 6 removes dead binaries from prod — confirmed safe (no service unit, never executed). The consolidated `enroll-server` and `schmutz` binaries are NOT deployed to production during this plan; that's a separate step after the branch is reviewed and merged.

**Rollback:** If anything in Tasks 1–4 breaks: `git checkout main` on the local machine restores the original state instantly. The old repos (`kore/ziti-base`, `kore/schmutz-controller`) are untouched throughout. Production keeps running from binaries that were built before this plan started.

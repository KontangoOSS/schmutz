# Schmutz code consolidation — spec

**Status:** draft (2026-05-08), awaiting review
**Author:** dillon + Claude
**Supersedes:** none
**Related:** `2026-05-06-hub-api-design.md` (defines enroll-server's API), `2026-05-06-schmutz-stack-architecture.md` (canonical naming)

---

## Problem

Schmutz exists today as three Go codebases plus a few satellite repos, with substantial naming and duplication problems:

| Repo | Module | Purpose | Production state |
|---|---|---|---|
| `kore/schmutz` | `github.com/KontangoOSS/schmutz` | The agent. `cmd/schmutz` binary. | Deployed (11 hosts on 0.4.0-dev). |
| `kore/ziti-base` | `github.com/KontangoOSS/ziti-base/server` | Hosts `cmd/enroll-server` (the May 6 hub-locked enrollment service) plus operator tooling and deploy manifests. | enroll-server deployed (3 ref-ctrl). |
| `kore/schmutz-controller` | `github.com/KontangoOSS/schmutz-controller` | Broader management plane: Bao/Ziti admin proxies, identity/OIDC subsystem, ops endpoints, pulse telemetry, plus a now-deprecated blue/iPXE/walkin layer. | NOT deployed. ~18.9k LOC. |

Concrete pain:

1. **`internal/shared/`** is byte-identical across `kore/schmutz` and `kore/ziti-base/server`. A Woodpecker job exists solely to enforce the duplication. Touching either copy without the other fails CI.
2. **Bao client, Ziti client, and enrollment logic** are reimplemented in parallel between `ziti-base` and `schmutz-controller`. Different package names (`internal/bao` vs `internal/service/bao_*`) mask the overlap. Both implementations are evolving independently.
3. **Naming** — `ziti-base` is the wrong name for what's effectively the production controller binary's home. Operator-facing nouns from the locked stack-architecture spec (`schmutz-controller`, `schmutz-agent`) don't match the repo names today.
4. **Scope creep** — `schmutz-controller` accumulated features (blue/iPXE provisioning, walk-in queue, host-tier ACL, wazuh integration) before the May 6 lock cut scope to a narrow enrollment hub. That code is parked, not deployed, but actively confusing.
5. **Dev parallelization is hard** — work on the agent, the enrollment hub, and the broader controller can't easily share a feature branch or a test-fixture across the three repos. Each PR spans repos by hand.

The goal is to reach a state where a developer can:

- Find any piece of running production logic in a clearly-named repo.
- Make a feature change that touches multiple microservices in a single staging workspace, then split the changes back into clean PRs per repo.
- Trust that there's exactly one implementation of each primitive (Bao client, Ziti client, etc.).
- Know which legacy code is dead and removable, vs. parked-for-later.

Non-goals for this spec:

- We are **not** redesigning the hub-API-LOCKED contract from `2026-05-06-hub-api-design.md`. Endpoints, payloads, and on-disk formats stay.
- We are **not** changing what's deployed in production during the consolidation. The 11 hosts on schmutz 0.4.0-dev and the 3 ref-ctrl nodes running enroll-server stay running. Any cutover happens after the new structure is proven.
- We are **not** porting deprecated features back. Code marked dead in the triage stays dead.

---

## Target architecture

Four long-term microservice repos. Each has a clear job and clear consumers.

```
kore/schmutz-agent           Single binary on every host. Owns the agent lifecycle:
                              Ziti identity → Bao token → substrate → gateway.
                              Today: kore/schmutz/src/cmd/schmutz + agent/* + most
                              of internal/* except shared.

kore/schmutz-enroll          Narrow enrollment hub. Implements the May 6 locked API:
                              /api/v1/applications, /api/v1/enrollments, /api/v1/enroll,
                              /api/v1/sync.
                              Today: kore/ziti-base/server/cmd/enroll-server +
                              the handlers, bao, ziti, forgejo, audit, identity,
                              admin packages it depends on.

kore/schmutz-controller      Broader management plane. Decision logic, approval flow,
                              dashboard backend, admin proxies (/api/ziti/*,
                              /api/bao/*, /api/identity/*, /api/pulse/*, /api/ops/*).
                              Calls schmutz-enroll over the wire for enrollment;
                              owns everything else.
                              Today: subset of kore/schmutz-controller/src/* — see
                              feature triage below.

kore/schmutz-shared          One Go module both other server-side repos import.
                              Bao + Ziti + Forgejo + audit clients. Wire-format types
                              for the hub API. Substrate + Tango shared types.
                              Today: kore/ziti-base/server/internal/{bao,ziti,
                              forgejo,audit,shared} after deduplication against the
                              schmutz-controller parallel implementations.
```

Why four and not three (with shared embedded in one):

- `schmutz-enroll` and `schmutz-controller` are *both* server-side and both need Bao/Ziti/Forgejo. If shared lives inside one, the other has to either reach into a sibling (breaks repo boundaries) or duplicate (returns to the current pain). A separate module is the only honest answer.
- `schmutz-agent` does **not** import shared today's server-side primitives — the agent has its own tiny client wrappers tailored for its needs. The `internal/shared/` package that *is* shared between agent and ziti-base is only the Tango/Schmutz wire-format types, which fit naturally in `schmutz-shared` too.

Why two server binaries (enroll + controller) and not one:

- The May 6 lock deliberately created `enroll-server` as a *minimal viable product* shipping fast. It runs in production. Replacing it with a bigger binary is a multi-week migration with real risk.
- `schmutz-controller` accumulated broader scope that the user described as "scope creep" — but the underlying intent (decision logic, approval, dashboard, admin proxies) is in scope long-term. It just got too big too fast.
- Side-by-side deployment is the safe path: enroll-server keeps owning enrollment-only on the locked API surface; schmutz-controller comes online with the broader management plane, calling enroll-server's API for enrollment operations and exposing its own admin endpoints.
- This also matches the user's "API for everything, 443 only" architecture — enroll-server and schmutz-controller are both Caddy-fronted at :443 with distinct SNIs (`enroll.kontango.net` vs `controller.kontango.net` or similar), no service-to-service trust over public network.

---

## Path: test droplet → monorepo staging → reconciliation → split

The user explicitly asked for monorepo as a staging step, with prod safety as a hard prerequisite. The plan has four phases — Phase 0 sets up a fully-isolated test environment, then the three consolidation phases happen on the consolidation branch and only get exercised against the test droplet.

### Phase 0: Test infrastructure (prerequisite, ~2-3 hours)

Provision the isolated test droplet, install Ziti+Bao+Caddy, prove end-to-end enrollment against current-production binaries. Phase 0 is fully reversible — if it fails, the droplet gets destroyed with no production impact. Detail in "Test droplet — fully isolated" below.

Phase 0 finishes when:
- Test droplet is provisioned, DNS is set, Ziti+Bao+Caddy are running.
- Current production `enroll-server` binary (built from `kore/ziti-base` master) deploys onto the test droplet and starts cleanly.
- A test LXC on hank enrolls against `enroll-test.kontango.net`, gets back a Ziti identity + Bao bundle, refreshes /run/bao-token successfully.
- Smoke tests for the test droplet's `/api/v1/applications` and `/api/v1/enroll` return expected results.

Only after Phase 0 ends successfully does Phase A begin.

### Phase A: Monorepo staging (mechanical, low risk)

Pull all three Go projects under one workspace at `kore/schmutz/`. Existing repo gets reorganized into:

```
kore/schmutz/
  agent/                      ← was kore/schmutz/src/
  enroll/                     ← was kore/ziti-base/server/
  controller/                 ← was kore/schmutz-controller/src/
  shared/                     ← new — initially populated from ziti-base/server/internal/shared
                                 plus the byte-duplicate from agent's internal/shared
  docs/                       ← merged from all three repos under single docs/ tree
  deploy/                     ← merged deploy manifests from ziti-base + schmutz-controller
  scripts/                    ← merged operator tooling
  go.work                     ← Go workspace file referencing agent, enroll, controller, shared
```

Each subdirectory keeps its own `go.mod`. The Go workspace file lets the developer build all four modules together for cross-cutting changes; Go's module resolution still treats them as independent for release purposes.

The original `kore/ziti-base` and `kore/schmutz-controller` repos get archived (read-only, not deleted) with a final commit pointing at the new home.

The agent module path stays `github.com/KontangoOSS/schmutz` for now to avoid breaking the running 0.4.0-dev binaries' upgrade path. The enroll module path renames `github.com/KontangoOSS/ziti-base/server` → `github.com/KontangoOSS/schmutz/enroll` (or stays for now; see Phase C).

Phase A is reversible. If anything breaks we can move the directories back out.

### Phase B: Reconciliation (the real work)

Eliminate parallel implementations of the same primitive. ziti-base's versions are production-tested and win; schmutz-controller's parallel implementations get rewritten to call into shared.

**Reconciliation order, by primitive:**

1. **`shared/` types** (Tango, Schmutz, patterns) — already byte-identical between agent and enroll. Move to `kore/schmutz/shared/` as a Go module. Update both agent and enroll to import. Delete the Woodpecker drift check.

2. **Bao client** — `enroll/internal/bao/` (production-tested) wins. The `controller/internal/service/bao_*.go` files (KV, auth, advanced ops) get converted into HTTP handlers that call the shared Bao client. The HTTP routes (`/api/bao/*`) stay; their implementations migrate.

3. **Ziti client** — same story. `enroll/internal/ziti/` wins. `controller/internal/service/ziti*.go` becomes HTTP handlers that call the shared client.

4. **Forgejo client** — only exists in enroll. Move to shared as-is. Controller (which doesn't currently use Forgejo) gains read access if needed.

5. **Audit, identity wrappers** — currently in enroll only. Move to shared. Both binaries gain access.

6. **Enrollment logic** — this is the trickiest. enroll has `handlers/{enroll,hub,bao_bundle}.go` (production). schmutz-controller has `internal/controller/enroll/*` (15 files, 3.7k LOC, mostly its own pipeline including walkin, SSE, certificates, install scripts). Most of the schmutz-controller enrollment code is the deprecated walkin/blue path — it goes. The few pieces that remain (e.g. `verify/fingerprint.go` looked interesting in triage) get evaluated individually.

**Triage of `kore/schmutz-controller/src/cmd/schmutz-controller/api_*.go`:**

| File | Routes | Decision | Rationale |
|---|---|---|---|
| `api_walkin.go` | `/api/v1/ws` | **drop** | Walkin queue is part of the deprecated iPXE/blue flow. |
| `api_server_enroll.go` | `/api/server/enroll` | **drop** | Superseded by `/api/v1/enroll` in enroll-server. |
| `api_start.go` | `/api/v1/start` | **drop** | Superseded by `/api/v1/enrollments` issuing OTTs. |
| `api_blue_claim.go`, `api_blue_ipxe.go` | `/api/v1/hook/*` | **drop** | iPXE/blue boot flow is deprecated. |
| `api_hook.go`, `api_hook_overlay.go` | `/api/v1/hook/*` | **drop** | Tinkerbell integration on aux ports — not 443-only. |
| `api_acl.go` | `/api/acl/*` | **drop** | Host-tier ACL model is orthogonal to ALPN-mux design. |
| `api_deploy.go` | `/api/deploy/*` | **drop** | Pipeline-side concern, not controller. |
| `api_honeypot.go` | `/api/auth/check`, `/api/honeypot/*` | **review** | Browser fingerprint flow may be needed for browzer; depends on browzer-ui design. |
| `api_telemetry_ws.go` | (9 LOC) | **drop** | Stub. |
| `api_pulse.go` | `/api/pulse/*` | **keep** | Heartbeat stream from machines. Real ops feature. |
| `api_ops.go` | `/api/machines`, `/api/approve`, `/api/deny`, `/api/discovery`, `/api/metrics`, `/api/catalog` | **keep** | The ops/dashboard API surface. Long-term controller. |
| `api_agent.go` | `/api/agent/*` | **keep** | Per-agent admin endpoints. |
| `api_bao.go`, `api_bao_advanced.go`, `api_bao_ext.go` | `/api/bao/*` | **keep** (rewrite) | Bao admin proxy — keep routes, rewrite handlers to use shared Bao client. |
| `api_ziti.go`, `api_ziti_ext.go` | `/api/ziti/*` | **keep** (rewrite) | Same as Bao — keep routes, rewrite to use shared Ziti client. |
| `api_identity.go`, `api_identity_ext.go` | `/api/identity/*` | **keep** (rewrite) | Vault-style identity subsystem. |

| Internal package | Decision |
|---|---|
| `internal/controller/agent/` | **keep** — agent admin client. |
| `internal/controller/common/` | **keep** — types live here. |
| `internal/controller/enroll/` | **drop** — enrollment moves to enroll-server. Anything unique (`verify/fingerprint.go` perhaps) gets evaluated individually before deletion. |
| `internal/controller/profiles/` | **keep** — enrollment profiles, may inform decision logic. |
| `internal/controller/verify/` | **keep with review** — connection/consensus/fingerprint verification logic; some of this may move into enroll, some stays in controller. |
| `internal/service/bao_*.go` | **drop after rewrite** — replaced by handlers using shared Bao client. |
| `internal/service/ziti*.go` | **drop after rewrite** — replaced. |
| `internal/service/blue_session.go`, `cpio.go`, `caddylogs.go`, `wazuh.go` | **drop** — deprecated. |
| `internal/service/discovery.go`, `metrics.go`, `security.go`, `acl.go` | **review** — case-by-case. |
| `internal/service/{telemetry,tcptelemetry,relay}.go` | **review** — overlap with agent's telemetry; may consolidate. |
| `internal/service/{store,store_extended,ott_store}.go` | **keep** — controller-local persistence. |
| `internal/service/{enrollment,identity*,bao_kv}.go` | **review** — overlap with enroll-server. |

By the end of Phase B:

- `agent/`, `enroll/`, `controller/` each compile and pass tests independently against `shared/`.
- Zero duplication of Bao, Ziti, Forgejo client code.
- The deprecated routes are gone from `controller/` source. Routes that survive are clearly the long-term management plane API.
- Production behavior is unchanged. enroll-server's binary is rebuilt from `enroll/` and is byte-equivalent in behavior. Agent's binary is rebuilt from `agent/` and is byte-equivalent.

### Phase C: Split (mechanical, optional)

Once Phase B is done and the staging monorepo is stable for some period (a week? a release?), the four directories can each become their own repo:

- `kore/schmutz/agent/` → `kore/schmutz-agent/`
- `kore/schmutz/enroll/` → `kore/schmutz-enroll/`
- `kore/schmutz/controller/` → `kore/schmutz-controller/` (legacy repo gets renamed to `kore/schmutz-controller-archive` first)
- `kore/schmutz/shared/` → `kore/schmutz-shared/`

`git filter-repo` or `git subtree split` preserves history. Each new repo gets its own CI, releases, versioning. The umbrella `kore/schmutz/` repo becomes either an empty meta-repo with `go.work` for cross-cutting development (recommended) or is deleted.

Phase C is optional. If the monorepo turns out to be a fine permanent home, we don't have to split.

---

## Production safety

**Production must not be touched during consolidation.** All work happens off-prod until verified, then cutover is a deliberate separate step.

### Branching

All consolidation work happens on a `consolidation` branch in `kore/schmutz`, checked out in a separate worktree at `~/git/kore/schmutz-consolidation/` so the original `~/git/kore/schmutz/` checkout stays at `main` for any prod hotfix work.

`main` / `master` on each repo stays at the current production state for the duration. The other two repos (`kore/ziti-base`, `kore/schmutz-controller`) are *imported* into the schmutz monorepo's `consolidation` branch — they don't grow their own consolidation branches. After cutover, they get a final "frozen — see kore/schmutz" commit on their main branches.

### Test droplet — fully isolated

A standalone DigitalOcean droplet that does **not** join the production ref-cluster. Real fault boundary: nothing the test droplet does can reach prod Bao or prod Ziti.

- **Provision:** 1 droplet, DO ams3 (matches ref-ctrl-1 region), 2GB basic plan. Naming: `test-ctrl-1` or similar.
- **DNS:** `ref-ctrl-test.kontango.net` — A record direct to droplet IP, NOT a CNAME via the existing wildcard. Plus `enroll-test.kontango.net` for the agent-facing endpoint.
- **Ziti:** Single-node Ziti controller + edge router on the droplet itself. Generates its own PKI trust domain (e.g. `spiffe://test-tango`). Test agents enroll into this isolated overlay only.
- **Bao:** Single-node Bao on the droplet (Raft mode but with `retry_join` disabled so it never finds prod). Initialized fresh with its own root token + unseal keys, stored in `~/.kontango/break-glass/test-droplet-bao-init.md`.
- **Caddy:** Full Caddyfile mirror of prod's structure, with all hostnames suffixed `-test`.
- **enroll-server:** Built from the consolidation branch, configured to talk to the local Bao + local Ziti.
- **schmutz-controller:** Built from the consolidation branch, deployed alongside enroll-server with its own Caddy SNI.

Setup time estimate: 2–3 hours for the first stand-up. Worth it for a real isolation guarantee.

### Test agents — disposable LXCs on hank

Two new LXCs created on hank specifically for this purpose:

- `test-agent-1` — Ubuntu 24.04, role-attribute `test-app-host`, simulates a generic worker agent.
- `test-agent-2` — Ubuntu 24.04, role-attribute `test-edge-router`, simulates a non-app role.

Both enroll against the test droplet via `enroll-test.kontango.net`. They use the test droplet's Ziti overlay only — they do NOT join prod overlay. When a test cycle is done, they're destroyed and recreated for the next cycle. CTIDs to be assigned at provision time (likely 200, 201).

### Cutover gate

Cutover from the test droplet to production happens only after:

1. Test droplet runs the consolidated `enroll-server` and `schmutz-controller` binaries successfully for at least 48 hours.
2. At least one full enrollment flow has been exercised end-to-end against the test droplet (operator issues OTT → agent enrolls → bundle issued → /run/bao-token refreshed → gateway publishes spec).
3. Controller's admin endpoints (`/api/ziti/*`, `/api/bao/*`, `/api/identity/*`, `/api/pulse/*`, `/api/ops/*`) return 200s on smoke calls.
4. No regressions in the existing 11 deployed agents when they call the test droplet.

Cutover itself is a separate execution: build production binaries from the consolidated codebase, deploy to ref-ctrl-1/2/3 one node at a time with verification between each (same pattern as the 0.4.0-dev rollout), keep the old binaries on disk as `enroll-server.bak` / `schmutz-controller.bak` for instant rollback.

## Risk and rollback

**Phase A (monorepo staging).** Reversible. Move directories back out, restore the original three module paths. The git history of each subdir is preserved by `git mv`, so this is purely a directory rearrangement. Branched work — `main` is untouched.

**Phase B (reconciliation).** Each primitive's reconciliation is its own commit on the `consolidation` branch. If a migration breaks something, revert that commit. Production binaries (enroll-server on ref-ctrl-1/2/3, schmutz on the 11 agents) are NEVER rebuilt during Phase B — only the test droplet picks up the new code. Production binaries get rebuilt only at cutover, after the cutover gate is satisfied.

**Phase C (split).** Mostly mechanical. The risk is broken import paths in any consumer outside the four repos. We grep for `github.com/KontangoOSS/{schmutz,ziti-base,schmutz-controller}` across the entire workspace before splitting and update each ref. Phase C does not happen until Phase B is in production for at least one release cycle.

---

## Triage entries marked "review"

Several entries in the feature triage are marked **review** rather than **keep** or **drop**. Each requires a focused read of the file before Phase B can begin on that piece. The set:

- `api_honeypot.go` — depends on browzer-ui design, which is independent work.
- `internal/controller/verify/{connection,consensus,fingerprint}.go` — verification primitives that may belong in enroll, controller, or shared depending on what they actually do.
- `internal/service/{discovery,metrics,security,acl}.go` — possibly overlapping with enroll-server's logic; possibly net-new ops features.
- `internal/service/{telemetry,tcptelemetry,relay}.go` — overlap with the agent's telemetry stack; need to compare implementations.
- `internal/service/{enrollment,identity*,bao_kv}.go` — direct overlap with enroll-server's primitives; need diff before deciding which wins.

These reviews don't need to happen now — but they have to happen before the corresponding piece of Phase B runs. The implementation plan for Phase B should include a "read and decide" task for each as a prerequisite.

## Concrete steps to validate this spec

Before committing the plan, verify the following claims:

- [ ] `internal/shared/` byte-identicality holds across both repos (confirmed: `diff -rq` empty).
- [ ] Production binary on ref-ctrl-1/2/3 is `enroll-server` from `kore/ziti-base` (confirmed via strings inspection).
- [ ] `schmutz-controller` binary is NOT deployed anywhere on the fleet (confirmed: probed all hosts, none have it).
- [ ] The triage table above accurately classifies every `api_*.go` file in `schmutz-controller` (Explore agent generated this; confirm with a manual read of the borderline-decision files).
- [ ] No production traffic flows to schmutz-controller endpoints (check Caddy/nginx logs across all 3 ref-ctrl).

---

## Open decisions

The following are deliberately left as open questions for review:

1. **Module path for the renamed enroll module.** Options: `github.com/KontangoOSS/schmutz/enroll` (suggests the monorepo is permanent), `github.com/KontangoOSS/schmutz-enroll` (suggests Phase C is the goal). Pick now or defer to Phase C.

2. **Honeypot endpoint fate.** Triage says "review" — depends on browzer-ui flow. Need a parallel decision before Phase B can finalize.

3. **Phase B order.** Spec lists primitives in dependency order (shared → Bao → Ziti → Forgejo → audit/identity → enrollment). Open question: do we reconcile all primitives before touching schmutz-controller's HTTP handlers, or interleave (handler-by-handler, primitive-by-primitive)? Sequential is simpler; interleaved gets working schmutz-controller features earlier.

4. **Permanent monorepo vs eventual split.** Recommendation is "split eventually," but if cross-cutting development continues to be the dominant pattern even after reconciliation, staying as a monorepo is the simpler answer.

5. **schmutz-server (admin plane binary in ziti-base).** Currently built but not deployed. Does it become part of `enroll/` (and stay built-but-unused), get folded into `controller/`, or die? Spec leaves it in `enroll/` for Phase A; the decision can come during Phase B.

---

## What this spec does not cover

- Implementation plan for any phase (that's a separate plan-of-tasks artifact).
- Migration of any deployed agent or controller binary. Production behavior is preserved throughout consolidation; cutover to new binaries is a separate exercise once the new structure is proven.
- Renames of satellite repos (`bouncer`, `browzer-ui`, `ziti-dash`, etc.). Those are independent products; this consolidation is scoped to the three core schmutz repos.
- The fate of `kore/decision-engine` — out of scope (separate microservice that may or may not become part of schmutz-controller's decision logic later).

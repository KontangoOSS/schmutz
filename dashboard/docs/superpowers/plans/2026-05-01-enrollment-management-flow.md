# Enrollment Management Flow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the approve/deny flow so it correctly syncs state across Ziti, Bao, and Postgres — and cleanly handles partial records (machines that exist in Ziti quarantine but have no Postgres enrollment record).

**Architecture:** Ziti identity is the cryptographic anchor and source of truth for access control. Bao holds machine metadata. Postgres is the audit log. The approve/deny flow must update all three in order: Ziti first (access changes), then Bao (metadata sync), then Postgres (audit record — upsert, not update, so legacy machines work too). The `machine_id` passed through the flow is always the Ziti identity `name` (machine UUID), not the Ziti internal short ID.

**Tech Stack:** Go 1.25, schmutz-controller (`~/git/kore/schmutz-controller/src/`), ziti-dash (`~/git/kore/ziti-dash/`), Ziti v2.0.0-pre11 management API, Bao KV v2, Postgres via pgx.

---

## File Map

### schmutz-controller
- **Modify:** `src/cmd/schmutz-controller/api_ops.go` — Approve: add Bao state sync after Ziti update. Deny: add Bao state sync before identity deletion.
- **No new files** — StoreService already has `PutMachine` and `GetMachine`.

### ziti-dash
- **Modify:** `internal/enrollments/routes.go` — Three fixes:
  1. `listEnrollments`: enrich each PendingMachine with Bao metadata (hostname, source_ip) by calling the controller's `/api/machines/<name>` endpoint.
  2. `approve`/`deny`: pass `Name` (machine UUID = Ziti identity name) to `UpdateEnrollmentStatus`, not the Ziti internal short ID.
  3. `approve`/`deny`: make Postgres update non-fatal — log the error but return success if Ziti/controller succeeded (Postgres is audit log, not gating).
- **Modify:** `internal/store/enrollments.go` — Change `UpdateEnrollmentStatus` to upsert (insert if no pending record exists).
- **Modify:** `internal/enrollments/plugin.go` — Add `ControllerURL` is already there; no changes needed.

---

## Task 1: Fix `UpdateEnrollmentStatus` to upsert

The current implementation fails with "no pending enrollment found" when no Postgres record exists (all 18 legacy quarantine machines). Change it to upsert so approve/deny always records the decision.

**Files:**
- Modify: `internal/store/enrollments.go:84-99`

- [ ] **Step 1: Replace `UpdateEnrollmentStatus` with upsert**

In `~/git/kore/ziti-dash/internal/store/enrollments.go`, replace the `UpdateEnrollmentStatus` function:

```go
func (s *Store) UpdateEnrollmentStatus(ctx context.Context, machineID, zitiID, hostname, status, reason, decidedBy string) error {
	now := time.Now()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO enrollment_requests
		    (machine_id, hostname, source_ip, ziti_identity_id, status,
		     decision_reason, decided_by, decided_at, raw_event)
		VALUES ($1, $2, '', $3, $4, $5, $6, $7, '{}')
		ON CONFLICT (machine_id)
		DO UPDATE SET
		    status         = EXCLUDED.status,
		    decision_reason = EXCLUDED.decision_reason,
		    decided_by     = EXCLUDED.decided_by,
		    decided_at     = EXCLUDED.decided_at,
		    ziti_identity_id = CASE
		        WHEN EXCLUDED.ziti_identity_id != '' THEN EXCLUDED.ziti_identity_id
		        ELSE enrollment_requests.ziti_identity_id
		    END
		WHERE enrollment_requests.status IN ('pending', '')
		   OR enrollment_requests.machine_id = $1`,
		machineID, hostname, zitiID, status, reason, decidedBy, now,
	)
	return err
}
```

- [ ] **Step 2: Add unique constraint migration if needed**

Check if `enrollment_requests` has a unique constraint on `machine_id`:

```bash
ssh ctrl-1 "PGPASSWORD=\$(grep POSTGRES ~/git/kore/ziti-dash/.env 2>/dev/null | cut -d= -f2) psql -U zitidash -d zitidash -c '\d enrollment_requests'" 2>/dev/null
```

If no unique constraint on `machine_id`, the upsert needs a different approach. Replace the upsert with a conditional insert + update:

```go
func (s *Store) UpdateEnrollmentStatus(ctx context.Context, machineID, zitiID, hostname, status, reason, decidedBy string) error {
	now := time.Now()
	// Try update first (existing record)
	tag, err := s.pool.Exec(ctx, `
		UPDATE enrollment_requests
		SET status=$1, decision_reason=$2, decided_by=$3, decided_at=$4,
		    ziti_identity_id=CASE WHEN $5!='' THEN $5 ELSE ziti_identity_id END
		WHERE machine_id=$6`,
		status, reason, decidedBy, now, zitiID, machineID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	// No existing record — insert audit entry for legacy/manual approvals
	_, err = s.pool.Exec(ctx, `
		INSERT INTO enrollment_requests
		    (machine_id, hostname, source_ip, ziti_identity_id, status,
		     decision_reason, decided_by, decided_at, raw_event)
		VALUES ($1, $2, 'legacy', $3, $4, $5, $6, $7, '{}')`,
		machineID, hostname, zitiID, status, reason, decidedBy, now,
	)
	return err
}
```

- [ ] **Step 3: Build ziti-dash to confirm no compile errors**

```bash
cd ~/git/kore/ziti-dash && go build ./... 2>&1
```
Expected: no output (clean build).

- [ ] **Step 4: Commit**

```bash
cd ~/git/kore/ziti-dash
git add internal/store/enrollments.go
git commit -m "fix: upsert enrollment status so legacy quarantine machines record decisions"
```

---

## Task 2: Fix approve/deny to pass machine UUID not Ziti internal ID

The `approve` and `deny` handlers in `internal/enrollments/routes.go` currently pass `req.MachineID` (Ziti internal short ID like `EngdXsl3X`) to `UpdateEnrollmentStatus`. But `enrollment_requests.machine_id` stores the machine UUID (the Ziti identity `Name` field). We need to pass `Name`, not `ID`.

The queue returns `ID` (Ziti internal ID) as `machine_id` to the frontend. We need to also return `Name` (machine UUID) and use that for the Postgres update.

**Files:**
- Modify: `internal/enrollments/routes.go`

- [ ] **Step 1: Add `MachineUUID` field to `PendingMachine`**

In `internal/enrollments/routes.go`, update the `PendingMachine` struct:

```go
type PendingMachine struct {
	ID          string   `json:"id"`
	MachineID   string   `json:"machine_id"`   // Ziti internal short ID — used by controller ops
	MachineUUID string   `json:"machine_uuid"` // Ziti identity Name (machine UUID) — used for Bao/Postgres
	Name        string   `json:"name"`
	Nickname    string   `json:"nickname"`
	Hostname    string   `json:"hostname"`
	SourceIP    string   `json:"source_ip"`
	Attributes  []string `json:"attributes"`
	Online      bool     `json:"online"`
	Status      string   `json:"status"`
}
```

- [ ] **Step 2: Populate `MachineUUID` in `listEnrollments`**

In `listEnrollments`, when building the `PendingMachine` list, set `MachineUUID` to `i.Name` (which is the machine UUID stored as the Ziti identity name):

```go
out = append(out, PendingMachine{
    ID:          i.ID,
    MachineID:   i.ID,       // controller ops use the Ziti internal ID
    MachineUUID: i.Name,     // Postgres/Bao use the machine UUID
    Name:        i.Name,
    Nickname:    nick,
    Attributes:  i.RoleAttributes,
    Online:      i.HasAPISession,
    Status:      "pending",
})
```

- [ ] **Step 3: Enrich with Bao metadata via controller**

In `listEnrollments`, after building the `out` slice, enrich each entry with hostname and source_ip from the controller's machine API. Add a helper that calls the controller:

```go
func (p *Plugin) fetchMachineMeta(ctx context.Context, machineUUID string) (hostname, sourceIP string) {
	url := fmt.Sprintf("%s/api/machines/%s", p.ControllerURL, machineUUID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", ""
	}
	resp, err := controllerClient.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	var data struct {
		Hostname string `json:"hostname"`
		SourceIP string `json:"source_ip"`
	}
	json.NewDecoder(resp.Body).Decode(&data)
	return data.Hostname, data.SourceIP
}
```

Then in the loop:

```go
hostname, sourceIP := p.fetchMachineMeta(r.Context(), i.Name)
out = append(out, PendingMachine{
    ID:          i.ID,
    MachineID:   i.ID,
    MachineUUID: i.Name,
    Name:        i.Name,
    Nickname:    nick,
    Hostname:    hostname,
    SourceIP:    sourceIP,
    Attributes:  i.RoleAttributes,
    Online:      i.HasAPISession,
    Status:      "pending",
})
```

- [ ] **Step 4: Fix `approve` to use `MachineUUID` for Postgres update and make it non-fatal**

The frontend sends `machine_id` (Ziti internal ID). The controller resolves it. But for Postgres we need the UUID. Since we don't have the UUID in the request body, we need to look it up from Ziti after the controller call succeeds.

Replace the `approve` handler:

```go
func (p *Plugin) approve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MachineID string `json:"machine_id"` // Ziti internal ID
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MachineID == "" {
		bff.WriteError(w, 400, "machine_id required", err)
		return
	}
	// Call controller — this updates Ziti (remove quarantine, add lan) and Bao
	if err := p.callController(r.Context(), "approve", req.MachineID, ""); err != nil {
		bff.WriteError(w, 502, "controller approve failed", err)
		return
	}
	// Look up machine UUID (Ziti identity Name) for Postgres audit record
	machineUUID, hostname := p.resolveIdentityName(r.Context(), req.MachineID)
	// Postgres update is best-effort audit log — don't fail the request if it errors
	if machineUUID != "" {
		if err := p.Store.UpdateEnrollmentStatus(r.Context(), machineUUID, req.MachineID, hostname, "approved", "reviewer approved", "reviewer"); err != nil {
			log.Printf("enrollments: postgres audit update failed (non-fatal): %v", err)
		}
	}
	bff.WriteJSON(w, map[string]string{"machine_id": req.MachineID, "status": "approved"})
}
```

- [ ] **Step 5: Add `resolveIdentityName` helper**

```go
func (p *Plugin) resolveIdentityName(ctx context.Context, zitiID string) (name, hostname string) {
	tok, err := p.zitiPasswordAuth()
	if err != nil {
		return "", ""
	}
	raw, err := p.Ziti.Get(tok, "/edge/management/v1/identities/"+zitiID)
	if err != nil {
		return "", ""
	}
	var result struct {
		Data struct {
			Name    string                 `json:"name"`
			AppData map[string]interface{} `json:"appData"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", ""
	}
	h, _ := result.Data.AppData["hostname"].(string)
	return result.Data.Name, h
}
```

- [ ] **Step 6: Fix `deny` handler the same way**

Replace the `deny` handler:

```go
func (p *Plugin) deny(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MachineID string `json:"machine_id"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MachineID == "" {
		bff.WriteError(w, 400, "machine_id required", err)
		return
	}
	if req.Reason == "" {
		req.Reason = "denied by reviewer"
	}
	// Resolve name before deletion (identity gone after deny)
	machineUUID, hostname := p.resolveIdentityName(r.Context(), req.MachineID)
	if err := p.callController(r.Context(), "deny", req.MachineID, req.Reason); err != nil {
		bff.WriteError(w, 502, "controller deny failed", err)
		return
	}
	// Postgres audit — non-fatal
	if machineUUID != "" {
		if err := p.Store.UpdateEnrollmentStatus(r.Context(), machineUUID, req.MachineID, hostname, "denied", req.Reason, "reviewer"); err != nil {
			log.Printf("enrollments: postgres audit update failed (non-fatal): %v", err)
		}
	}
	bff.WriteJSON(w, map[string]string{"machine_id": req.MachineID, "status": "denied"})
}
```

- [ ] **Step 7: Add `log` import to routes.go**

Add `"log"` to the imports in `internal/enrollments/routes.go`.

- [ ] **Step 8: Build and confirm clean**

```bash
cd ~/git/kore/ziti-dash && go build ./... 2>&1
```
Expected: no output.

- [ ] **Step 9: Commit**

```bash
cd ~/git/kore/ziti-dash
git add internal/enrollments/routes.go internal/enrollments/plugin.go
git commit -m "fix: approve/deny use machine UUID for postgres, enrich queue with hostname/ip, non-fatal audit"
```

---

## Task 3: Fix schmutz-controller Approve/Deny to sync Bao state

After Approve, the Bao machine record still shows `state: pending`. After Deny, the record stays in Bao forever. Fix both.

**Files:**
- Modify: `src/cmd/schmutz-controller/api_ops.go`

- [ ] **Step 1: Update `Approve` to sync Bao state**

In `api_ops.go`, after the successful Ziti attribute update in `Approve`, add:

```go
// Sync Bao machine record state to approved
if a.store != nil {
    existing, _ := a.store.GetMachine(req.MachineID)
    if existing == nil {
        existing = map[string]interface{}{}
    }
    existing["state"] = "approved"
    existing["approved_at"] = time.Now().Unix()
    existing["ziti_id"] = zitiID
    a.store.PutMachine(req.MachineID, existing)
}
jsonOrErr(w, map[string]string{"status": "approved", "machine_id": req.MachineID, "ziti_id": zitiID}, nil)
```

The full updated `Approve` function (replace from `jsonOrErr` onward):

```go
func (a *API) Approve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		MachineID string `json:"machine_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.MachineID == "" {
		http.Error(w, "machine_id required", http.StatusBadRequest)
		return
	}
	token, err := a.ziti.Authenticate()
	if err != nil {
		errJSON(w, "ziti auth failed", http.StatusBadGateway)
		return
	}
	zitiID := req.MachineID
	if id, _, _, err := a.ziti.GetIdentityByName(token, req.MachineID); err == nil && id != "" {
		zitiID = id
	}
	if err := a.ziti.UpdateIdentityRemoveAttr(token, zitiID, "quarantine"); err != nil {
		errJSON(w, fmt.Sprintf("remove quarantine: %v", err), http.StatusInternalServerError)
		return
	}
	if err := a.ziti.UpdateIdentityAddAttr(token, zitiID, "lan"); err != nil {
		errJSON(w, fmt.Sprintf("add lan: %v", err), http.StatusInternalServerError)
		return
	}
	// Sync Bao machine record state
	if a.store != nil {
		existing, _ := a.store.GetMachine(req.MachineID)
		if existing == nil {
			existing = map[string]interface{}{}
		}
		existing["state"] = "approved"
		existing["approved_at"] = time.Now().Unix()
		existing["ziti_id"] = zitiID
		a.store.PutMachine(req.MachineID, existing)
	}
	jsonOrErr(w, map[string]string{"status": "approved", "machine_id": req.MachineID, "ziti_id": zitiID}, nil)
}
```

- [ ] **Step 2: Update `Deny` to sync Bao state before deletion**

```go
func (a *API) Deny(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		MachineID string `json:"machine_id"`
		Reason    string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.MachineID == "" {
		http.Error(w, "machine_id required", http.StatusBadRequest)
		return
	}
	token, err := a.ziti.Authenticate()
	if err != nil {
		errJSON(w, "ziti auth failed", http.StatusBadGateway)
		return
	}
	zitiID := req.MachineID
	if id, _, _, err := a.ziti.GetIdentityByName(token, req.MachineID); err == nil && id != "" {
		zitiID = id
	}
	// Sync Bao state before deleting identity
	if a.store != nil {
		existing, _ := a.store.GetMachine(req.MachineID)
		if existing == nil {
			existing = map[string]interface{}{}
		}
		existing["state"] = "denied"
		existing["denied_at"] = time.Now().Unix()
		existing["denied_reason"] = req.Reason
		a.store.PutMachine(req.MachineID, existing)
	}
	if err := a.ziti.DeleteIdentity(token, zitiID); err != nil {
		errJSON(w, fmt.Sprintf("delete identity: %v", err), http.StatusInternalServerError)
		return
	}
	jsonOrErr(w, map[string]string{"status": "denied", "machine_id": req.MachineID, "ziti_id": zitiID}, nil)
}
```

- [ ] **Step 3: Add `time` import to api_ops.go if not already present**

```bash
grep '"time"' ~/git/kore/schmutz-controller/src/cmd/schmutz-controller/api_ops.go
```
If missing, add `"time"` to the imports block.

- [ ] **Step 4: Build schmutz-controller**

```bash
cd ~/git/kore/schmutz-controller/src && go build ./... 2>&1
```
Expected: no output.

- [ ] **Step 5: Cross-compile and deploy to all 3 controllers**

```bash
cd ~/git/kore/schmutz-controller/src && \
  GOOS=linux GOARCH=amd64 go build -o /tmp/sc-enrollment-fix ./cmd/schmutz-controller/

for ctrl in ctrl-1 ctrl-2 ctrl-3; do
  ssh $ctrl "systemctl stop schmutz-controller"
  scp /tmp/sc-enrollment-fix $ctrl:/opt/kontango/bin/schmutz-controller
  ssh $ctrl "systemctl start schmutz-controller && echo $ctrl ok"
done
```
Expected: `ctrl-1 ok`, `ctrl-2 ok`, `ctrl-3 ok`

- [ ] **Step 6: Commit**

```bash
cd ~/git/kore/schmutz-controller/src
git add cmd/schmutz-controller/api_ops.go
git commit -m "fix: approve/deny sync Bao machine state (approved/denied + timestamp)"
```

---

## Task 4: Rebuild and restart ziti-dash

- [ ] **Step 1: Build ziti-dash with all fixes**

```bash
cd ~/git/kore/ziti-dash && go build -o build/ziti-dash ./cmd/ziti-dash/ 2>&1
```
Expected: no output.

- [ ] **Step 2: Restart**

```bash
sudo systemctl restart ziti-dash
```

- [ ] **Step 3: Verify queue loads with enriched data**

```bash
curl -s http://localhost:9090/api/enrollments | python3 -c "
import json,sys
d=json.load(sys.stdin)
print(f'{len(d)} pending machines')
for m in d[:3]:
    print(f'  {m[\"nickname\"] or m[\"name\"][:16]:16s}  host={m.get(\"hostname\",\"?\"):20s}  ip={m.get(\"source_ip\",\"?\")}')
"
```
Expected: 18 machines, some with hostnames populated.

---

## Task 5: Clean up the 18 pending machines

Now that approve/deny fully syncs all three stores, process the backlog.

- [ ] **Step 1: List all pending machines with their metadata**

```bash
curl -s http://localhost:9090/api/enrollments | python3 -c "
import json,sys
d=json.load(sys.stdin)
print(f'{'Nickname':16s}  {'Hostname':20s}  {'UUID':36s}  ID')
for m in sorted(d, key=lambda x: x.get('nickname','')):
    print(f'{m.get(\"nickname\",\"-\"):16s}  {m.get(\"hostname\",\"-\"):20s}  {m.get(\"machine_uuid\",m[\"name\"]):36s}  {m[\"id\"]}')
"
```

- [ ] **Step 2: Approve known machines via ziti-dash**

For each machine you recognize (your internal LXC/VMs — grafana, ticketarr, inventree, viseron-cam, etc.), approve via the API using the Ziti internal ID:

```bash
# Example — replace ID with actual Ziti internal ID from the list above
curl -s -X POST http://localhost:9090/api/ops/approve \
  -H "Content-Type: application/json" \
  -d '{"machine_id":"<ZITI_INTERNAL_ID>"}' | python3 -m json.tool
```

- [ ] **Step 3: Deny unknown machines**

For machines you don't recognize (test machines, duplicate hank attempts, `x` hostnames), deny them:

```bash
curl -s -X POST http://localhost:9090/api/ops/deny \
  -H "Content-Type: application/json" \
  -d '{"machine_id":"<ZITI_INTERNAL_ID>","reason":"unrecognized machine, cleanup"}' | python3 -m json.tool
```

- [ ] **Step 4: Verify queue is empty**

```bash
curl -s http://localhost:9090/api/enrollments | python3 -c "
import json,sys; d=json.load(sys.stdin); print(f'{len(d)} pending remaining')
"
```
Expected: `0 pending remaining`

- [ ] **Step 5: Verify Bao state updated for approved machines**

```bash
# Pick one approved machine UUID from step 1
BAO_ADDR=https://bao.tango:8200 BAO_SKIP_VERIFY=1 \
  bao kv get -mount=secret identities/machines/<UUID> 2>/dev/null | grep state
```
Expected: `state    approved`

---

## Self-Review

**Spec coverage:**
- ✅ `UpdateEnrollmentStatus` upsert — Task 1
- ✅ Pass machine UUID not Ziti internal ID to Postgres — Task 2
- ✅ Enrich queue with hostname/IP from Bao — Task 2 Step 3
- ✅ Non-fatal Postgres update in approve/deny — Task 2 Steps 4+6
- ✅ Bao state sync in schmutz-controller Approve — Task 3 Step 1
- ✅ Bao state sync in schmutz-controller Deny — Task 3 Step 2
- ✅ Clean up 18 legacy machines — Task 5

**No placeholders** — all code is complete and concrete.

**Type consistency:**
- `UpdateEnrollmentStatus` signature changes in Task 1 must match calls in Task 2 — both use `(ctx, machineUUID, zitiID, hostname, status, reason, decidedBy)`. ✅
- `PendingMachine.MachineUUID` added in Task 2 Step 1, used in Step 4. ✅
- `resolveIdentityName` defined in Task 2 Step 5, called in Steps 4 and 6. ✅

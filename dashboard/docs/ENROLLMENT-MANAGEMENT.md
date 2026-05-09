# Enrollment Management

## Overview

The Enrollments page in ziti-dash is the operator's view of machines waiting
for approval. It queries Ziti directly on every load — there is no separate
database or cache. The source of truth is the Ziti identity's role attributes.

## Stages Displayed

### booting (orange)
- Ziti role: `blue-demo`
- Source: Hook OS machines booted via iPXE, no OS installed yet
- Machine is dialing `claim.demo` to show QR code for claiming
- Available action: **Deny** only — can't approve before OS exists
- Transitions to **pending** automatically when OS installs and schmutz enrolls

### pending (yellow)
- Ziti role: `quarantine`
- Source: schmutz SSE enrollment or to-go OTT enrollment
- Machine is connected, services not yet bound
- Available actions: **Approve** (with app + env tags) or **Deny**

## Approve Action

Clicking Approve sends `POST /api/ops/approve` to the local ziti-dash BFF,
which forwards to the schmutz-controller at `CONTROLLER_URL`.

Request body:
```json
{ "machine_id": "<ziti-id>", "app": "ticketarr", "env": "prod" }
```

The controller then:
1. Removes `quarantine` from Ziti identity roles
2. Adds `lan` → schmutz binds services within seconds
3. Adds `app=<x>` and `env=<y>` as Ziti role attributes
4. Writes machine record to Bao at `secret/identities/machines/<slug>`
5. Writes SSH host keys to `secret/identities/machines/<slug>/ssh_host_keys`

## Auth

The enrollment list uses password auth against the Ziti management API
(configured via `ZITI_CTRL_ADDR`, `ZITI_ADMIN_USER`, `ZITI_ADMIN_PASS` env vars).
This is required because the management API in Ziti pre11 does not support
cert-based auth for non-admin identities.

## Nav Badge

The badge on the Enrollments nav item counts `pending + booting` — both
require operator attention, just different actions.

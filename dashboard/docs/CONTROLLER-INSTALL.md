# Schmutz Controller — Installation & Seed Data

The schmutz-controller is the complete fabric control plane. One binary,
one node, everything needed to run the mesh.

---

## Components

```
Schmutz Controller Node
├── Caddy            TLS termination, cert management (LE via Cloudflare DNS-01)
│   └── storage:     Bao KV v1 (cert replication across nodes)
│
├── Ziti Controller  Mesh runtime (identities, services, policies, routing)
│   └── storage:     BoltDB (Raft replicated)
│
├── Ziti Router      Edge router (client connections, traffic routing)
│
├── Ziti Tunnel      Local tunnel (DNS interception, service hosting)
│
├── OpenBao          Secrets, configuration, PKI
│   └── storage:     BoltDB (Raft replicated)
│
├── PostgreSQL       Application state (enrollments, machines, sessions, audit)
│
└── Schmutz API      Fabric middleware (Go binary)
    ├── Ziti client   → controller API
    ├── Bao client    → secrets/config API
    ├── DB client     → PostgreSQL
    └── HTTP API      → consumed by ziti-dash GUI
```

---

## Storage Separation

| Store | Contents | Replication |
|-------|----------|-------------|
| Ziti BoltDB | Mesh runtime: identities, services, policies, terminators, configs | Ziti Raft |
| Bao BoltDB | Secrets, cert storage, config catalog, AppRoles, PKI | Bao Raft |
| PostgreSQL | Enrollments, machine history, sessions, audit trail | Standard PG replication |

These are always separate. If the node splits in the future (Ziti on one box,
Bao on another), the fabric middleware just updates its client addresses.

---

## Installation Steps

### Phase 1: Base OS

```
1. Ubuntu 24.04 LTS (minimal)
2. UFW: allow 443 only (everything else through Caddy/Ziti)
3. /etc/hosts: controller peer entries
```

### Phase 2: Install Binaries

```
4. Ziti binary         → /opt/zstack/bin/ziti
5. OpenBao binary      → /opt/zstack/bin/bao
6. Caddy (custom build) → /opt/zstack/bin/caddy
   Plugins: cloudflare, vault-storage, ziti-caddy, layer4
7. PostgreSQL          → apt install postgresql
8. Schmutz binary      → /opt/schmutz-controller/build/binary/schmutz-controller
```

### Phase 3: Initialize Infrastructure (first node only)

```
9.  Ziti PKI: create root CA with trust domain spiffe://kontango.io
10. Ziti controller: init database, create admin user
11. Ziti router: create and enroll (edge, public-edge, region-<region>)
12. Ziti tunnel: create identity and enroll (infra-hosts, admin-users, tunnel)
13. Bao: init and unseal, create root token
14. Bao: create caddy/ KV v1 mount for cert storage
15. Bao: create secret/ KV v2 mount for secrets
16. Caddy: configure with Bao storage, layer4 ALPN mux, Cloudflare DNS
17. Caddy: create identity and enroll (do-caddy, web-clients)
18. PostgreSQL: create schmutz database
```

### Phase 4: Seed Data — Profiles & Policies

This is the critical step. The following are created ONCE on the first
controller and replicated to all joining nodes via Ziti Raft.

#### 4a. Create fabric-profiles identity

```bash
ziti edge create identity fabric-profiles -t Service
```

This identity stores all profile and role description data in its appData.

#### 4b. Seed Profiles

```
Profile: join
  identity_role:           quarantine
  edge_router_roles:       [#public-edge]
  service_bind_roles:      [#quarantine-services]
  service_dial_roles:      []
  default_hosting_cost:    0
  default_hosting_prec:    default
  auth_policy:             Default
  terminator_strategy:     smartrouting
  promotes_to:             standard
  heartbeat_enabled:       true
  heartbeat_fallback:      join-api

Profile: standard
  identity_role:           tango-standard
  edge_router_roles:       [#public-edge, #lan]
  service_bind_roles:      [#tango-services, #ssh-services]
  service_dial_roles:      []
  default_hosting_cost:    0
  default_hosting_prec:    default
  auth_policy:             Default
  terminator_strategy:     smartrouting
  promotes_to:             (none)
  heartbeat_enabled:       true

Profile: infra
  identity_role:           infra-hosts
  edge_router_roles:       [#all]
  service_bind_roles:      [#infra-services]
  service_dial_roles:      [#infra-services, #tango-services]
  default_hosting_cost:    0
  default_hosting_prec:    default
  auth_policy:             Default
  terminator_strategy:     smartrouting

Profile: workstation
  identity_role:           workstations
  edge_router_roles:       [#all]
  service_bind_roles:      []
  service_dial_roles:      [#all]
  default_hosting_cost:    0
  default_hosting_prec:    default
  auth_policy:             Default
  terminator_strategy:     smartrouting
```

#### 4c. Seed Service Policies (Bind — who hosts what)

```
quarantine-ssh-bind:     #quarantine       → #quarantine-services
tango-bind:              #tango-standard   → #tango-services
ssh-bind:                #tango-standard   → #ssh-services
infra-services-bind:     #infra-hosts      → #infra-services
web-services-bind:       #web-hosts        → #web-services
home-bind-services:      #home-router      → #home-services
k8s-bind:                #k8s-hosts        → #k8s-services
```

#### 4d. Seed Service Policies (Dial — who can reach what)

```
quarantine-ssh-dial:     #admin-users              → #quarantine-services
tango-dial:              #admin-users, #web-clients → #tango-services
ssh-services-dial:       #ssh-clients              → #ssh-services
infra-services-dial:     #admin-users              → #infra-services
web-services-dial:       #web-clients              → #web-services
k8s-dial:                #admin-users, #web-clients → #k8s-services
admin-dial-home:         #admin-users              → #home-services
```

#### 4e. Seed Edge Router Policies (who reaches which routers)

```
quarantine-erp:          #quarantine      → #public-edge
tango-standard-erp:      #tango-standard  → #public-edge, #lan
infra-all-routers:       #infra-hosts     → #all
admin-all-routers:       #admin-users     → #all
workstation-all-routers: #workstations    → #all
caddy-all-routers:       #do-caddy        → #all
k8s-all-routers:         #k8s-hosts       → #all
```

#### 4f. Seed Service Edge Router Policy

```
all-services-all-routers: #all → #all
```

#### 4g. Seed Role Descriptions

Identity roles (13):
```
quarantine          Freshly enrolled, awaiting promotion — limited access
tunnel              Has Ziti tunnel running — base role for all enrolled nodes
tango-standard      Promoted standard mesh participant
infra-hosts         Infrastructure controllers and tunnels
admin-users         Admin-level dial access to all services
ssh-hosts           Hosts SSH service for remote access
ssh-clients         Can dial SSH services
web-hosts           Hosts web application service
web-clients         Can dial web services
workstations        Developer workstation — dial-only, no hosting
do-caddy            Caddy reverse proxy on DO nodes
k8s-hosts           Kubernetes cluster node
home-router         LAN router identity
```

Service roles (12):
```
quarantine-services   Services available to quarantined nodes — admin SSH inbound only
tango-services        Standard app services hosted by enrolled tango nodes
app-services          Application HTTP services (web apps, APIs)
ssh-services          SSH access services for remote management
web-services          Web application services (Ticketarr, MkDocs)
home-services         LAN home network services
infra-services        Infrastructure management (Bao, controller, join API)
k8s-services          Kubernetes cluster services (Grafana, Loki)
monitoring-services   Monitoring and observability
mgmt-services         Ziti management API access
hypervisors           Proxmox VE hypervisor access
join-services         Join and enrollment services
```

Router roles (3):
```
edge                Edge router — terminates client connections
public-edge         Internet-facing edge router (DO nodes)
lan                 LAN-only router (not publicly reachable)
```

### Phase 5: Seed Bao

```
19. Bao auth: create AppRole mount
20. Bao AppRoles: schmutz-enroll, admin, and per-service roles
21. Bao policies: schmutz-machine, service-default, admin, per-app policies
22. Bao secrets: seed secret/networking/openziti/app (admin password)
23. Bao secrets: seed secret/infra/cloudflare (CF API token)
24. Bao identities: create secret/identities/ structure
    - machines/     (per-machine enrollment data)
    - devices/      (hardware fingerprints)
    - known/        (named infrastructure hosts)
    - profiles/     (identity profiles and templates)
    - acl/          (per-identity Bao access rules)
```

### Phase 6: Join Additional Nodes

```
25. Copy root CA from init node
26. Run install on joining node (no --init)
27. Join Ziti Raft:  ziti agent cluster add tls:ctrl-2.tango:1280
28. Join Bao Raft:   bao operator raft join http://<init-ip>:8200
29. Unseal Bao on joining node
30. All seed data replicates automatically via Raft
```

### Phase 7: Start Schmutz Controller

```
31. Configure schmutz: ZITI_CTRL_ADDR, BAO_ADDR, DATABASE_URL
32. Start schmutz-controller service
33. Verify: join.kontango.net serves enrollment page
34. Test: enroll a machine, verify it lands in quarantine profile
```

---

## Cert Architecture

```
Internal (.tango, self-signed Ziti PKI):
  ctrl-1.tango, ctrl-2.tango, ctrl-3.tango
  SANs: as many as needed per node
  Trusted by: enrolled devices (have Ziti root CA)

External (*.kontango.io, Let's Encrypt via Caddy):
  *.kontango.io, *.konoss.org, *.kontango.net,
  *.kontango.org, *.kontango.us, *.aftrserv.com
  Trusted by: everyone (publicly trusted CA)

Caddy bridges the two:
  :443 → LE cert → layer4 ALPN mux
    → ziti-edge ALPN  → router (self-signed, internal)
    → ziti-ctrl ALPN  → controller (self-signed, internal)
    → HTTPS           → transport ziti → services (LE cert, public)
```

---

## Adding a Service (post-install)

```bash
# 3 commands, no cert config, no Caddy change
ziti edge create config <name>-host host.v1 \
  '{"protocol":"tcp","address":"127.0.0.1","port":<port>}'

ziti edge create config <name>-intercept intercept.v1 \
  '{"protocols":["tcp"],"addresses":["<name>.kontango.io"],"portRanges":[{"low":443,"high":443}]}'

ziti edge create service <name> \
  --configs <name>-host,<name>-intercept \
  -a "tango-services,web-services"
```

Existing policies handle the rest. The LE wildcard cert covers the domain.

---

## Bootstrap Script

The seed data from Phase 4 is automated:

```bash
export ZITI_CTRL_URL=https://ctrl-1.tango:1280
export ZITI_ADMIN_PASS=<password>
bash scripts/bootstrap-profiles.sh
```

This creates all profiles, policies, ERPs, and the fabric-profiles identity
in a single run. Idempotent — safe to re-run.

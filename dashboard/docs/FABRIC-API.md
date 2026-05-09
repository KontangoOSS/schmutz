# Fabric API Reference

Base URL: `https://fabric.kontango.io/api/v1`

---

## Health & Discovery

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Service health check |
| GET | `/version` | Fabric version + Ziti controller version |
| GET | `/plugins` | List loaded BFF plugins |
| GET | `/capabilities` | All available operations grouped by domain |

---

## Mesh — Network Topology

Everything about the overlay network: routers, regions, routing.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/mesh/status` | Network overview (services, identities, routers online, terminators) |
| GET | `/mesh/routers` | List all edge routers with status, cost, region |
| GET | `/mesh/routers/{name}` | Get router details |
| PATCH | `/mesh/routers/{name}` | Update router (cost, disabled, noTraversal, roles) |
| GET | `/mesh/regions` | List all regions |
| GET | `/mesh/regions/{name}` | Get region details (router, status) |
| GET | `/mesh/terminators` | List all terminators with service/router mapping |

---

## Identity — Who Is On The Network

Machines, users, service accounts — anything with a Ziti identity.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/identity/list` | List all identities with status, type, profile, roles |
| GET | `/identity/{name}` | Get identity details |
| POST | `/identity/create` | Create identity (assigns profile, provisions Bao path) |
| DELETE | `/identity/{name}` | Delete identity (cleanup Bao path, DNS, policies) |
| POST | `/identity/{name}/promote` | Promote identity to next profile (e.g. join → standard) |
| POST | `/identity/{name}/demote` | Demote identity back to join/quarantine |
| PATCH | `/identity/{name}/profile` | Change identity's profile |
| PATCH | `/identity/{name}/roles` | Override identity role attributes |
| GET | `/identity/{name}/services` | List services this identity can reach |
| GET | `/identity/{name}/policy-check` | Check effective policies for this identity |

---

## Profiles — Identity Templates

Profiles define what an identity can do. Stored in Bao at `secret/mesh/profiles/`.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/profiles` | List all profiles |
| GET | `/profiles/{name}` | Get profile definition |
| POST | `/profiles` | Create new profile |
| PUT | `/profiles/{name}` | Update profile (propagates to all identities with this profile) |
| DELETE | `/profiles/{name}` | Delete profile (fails if identities still assigned) |
| GET | `/profiles/{name}/members` | List identities currently assigned this profile |

---

## Services — What's Available On The Network

Named network services that identities bind (host) or dial (connect to).

| Method | Path | Description |
|--------|------|-------------|
| GET | `/services` | List all services with roles, configs, status |
| GET | `/services/{name}` | Get service details including configs and terminators |
| POST | `/services` | Create service (host config + intercept config + policies) |
| PUT | `/services/{name}` | Update service |
| DELETE | `/services/{name}` | Delete service (and associated configs, policies) |
| GET | `/services/{name}/terminators` | List active terminators for this service |
| GET | `/services/{name}/identities` | List identities that can bind/dial this service |

---

## Policies — Access Control Rules

Who can reach what, through which routers.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/policies/service` | List all service policies (bind/dial) |
| GET | `/policies/service/{name}` | Get service policy details |
| POST | `/policies/service` | Create service policy |
| PUT | `/policies/service/{name}` | Update service policy |
| DELETE | `/policies/service/{name}` | Delete service policy |
| GET | `/policies/edge-router` | List all edge router policies |
| POST | `/policies/edge-router` | Create edge router policy |
| PUT | `/policies/edge-router/{name}` | Update edge router policy |
| DELETE | `/policies/edge-router/{name}` | Delete edge router policy |
| GET | `/policies/service-edge-router` | List all service-edge-router policies |
| POST | `/policies/service-edge-router` | Create service-edge-router policy |
| DELETE | `/policies/service-edge-router/{name}` | Delete service-edge-router policy |

---

## Roles — The Tag System

Atomic labels applied to identities, services, and routers.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/roles/identity` | List all identity roles with descriptions and member counts |
| GET | `/roles/identity/{name}` | Get identity role details |
| POST | `/roles/identity` | Create identity role definition |
| DELETE | `/roles/identity/{name}` | Delete identity role |
| GET | `/roles/service` | List all service roles |
| GET | `/roles/service/{name}` | Get service role details |
| POST | `/roles/service` | Create service role definition |
| DELETE | `/roles/service/{name}` | Delete service role |
| GET | `/roles/router` | List all router roles |
| POST | `/roles/router` | Create router role definition |
| DELETE | `/roles/router/{name}` | Delete router role |

---

## Secrets — Credential Management

Interface to Bao for service credentials and mesh configuration.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/secrets/list/{path}` | List keys under a Bao path |
| GET | `/secrets/get/{path}` | Get secret metadata (keys only, never values) |
| POST | `/secrets/create/{path}` | Create a new secret path |
| DELETE | `/secrets/delete/{path}` | Delete a secret path |
| POST | `/secrets/validate/{path}` | Validate a secret path exists and has expected keys |

---

## Auth — Authentication Configuration

OIDC providers, auth policies, certificate authorities.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/auth/policies` | List auth policies |
| GET | `/auth/policies/{name}` | Get auth policy details |
| POST | `/auth/policies` | Create auth policy |
| PUT | `/auth/policies/{name}` | Update auth policy |
| DELETE | `/auth/policies/{name}` | Delete auth policy |
| GET | `/auth/jwt-signers` | List external JWT signers (OIDC providers) |
| POST | `/auth/jwt-signers` | Create JWT signer |
| DELETE | `/auth/jwt-signers/{name}` | Delete JWT signer |

---

## Routing — Traffic Engineering

Cost, precedence, terminator strategy configuration.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/routing/strategies` | List available terminator strategies |
| GET | `/routing/costs` | Get default costs per profile/region |
| PUT | `/routing/costs` | Update default costs |
| GET | `/routing/precedences` | Get default precedence rules |
| PUT | `/routing/precedences` | Update default precedences |

---

## Catalog — Bao Mesh Configuration

Raw access to the mesh catalog in Bao. The source of truth.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/catalog/tree` | Full catalog tree structure |
| GET | `/catalog/{path}` | Read any catalog entry |
| PUT | `/catalog/{path}` | Write any catalog entry |
| DELETE | `/catalog/{path}` | Delete any catalog entry |

---

## Workflows — Orchestrated Operations

Recipes that combine multiple operations into a single action.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/workflows` | List saved workflows |
| GET | `/workflows/{name}` | Get workflow definition |
| POST | `/workflows` | Save a new workflow |
| PUT | `/workflows/{name}` | Update a workflow |
| DELETE | `/workflows/{name}` | Delete a workflow |
| POST | `/workflows/{name}/validate` | Validate a workflow against the registry |
| POST | `/workflows/{name}/compile/shell` | Compile to bash script |
| POST | `/workflows/{name}/compile/woodpecker` | Compile to Woodpecker YAML |
| POST | `/workflows/{name}/execute` | Execute a workflow (future) |

---

## Registry — Plugin Catalog

The plugin index. Read-only, loaded from `plugin.json` files.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/registry/plugins` | List all plugins with action counts |
| GET | `/registry/plugins/{name}` | Get plugin details (actions, settings schema) |
| GET | `/registry/actions` | Flat list of all actions across all plugins |
| GET | `/registry/actions/{plugin}/{action}` | Get action details (inputs, outputs) |

---

## Summary

| Domain | Endpoints | Backed by |
|--------|-----------|-----------|
| Health | 4 | Fabric internals |
| Mesh | 7 | Ziti management API |
| Identity | 10 | Ziti API + Bao profiles |
| Profiles | 6 | Bao `secret/mesh/profiles/` |
| Services | 7 | Ziti API |
| Policies | 12 | Ziti API |
| Roles | 10 | Bao `secret/mesh/roles/` |
| Secrets | 5 | Bao API |
| Auth | 7 | Ziti API + Bao |
| Routing | 5 | Bao `secret/mesh/routing/` |
| Catalog | 4 | Bao `secret/mesh/` |
| Workflows | 8 | Bao + plugin registry + compiler |
| Registry | 4 | Plugin `plugin.json` files |
| **Total** | **89** | |

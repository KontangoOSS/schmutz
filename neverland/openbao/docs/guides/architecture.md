# Architecture

## System Overview

```mermaid
graph TB
    subgraph Internet
        User[User / Browser]
        CI[Woodpecker CI]
    end

    subgraph Ziti Overlay
        Caddy[Caddy Reverse Proxy]
    end

    subgraph LXC Container
        App[APPNAME]
        DB[(PostgreSQL)]
        S3[MinIO / S3]
    end

    User -->|HTTPS| Caddy
    Caddy -->|Ziti Transport| App
    App --> DB
    App --> S3
    CI -->|Deploy| App
```

## Request Flow

```mermaid
sequenceDiagram
    participant B as Browser
    participant C as Caddy
    participant A as API Server
    participant M as Middleware
    participant S as Service Layer
    participant R as Repository
    participant D as Database

    B->>C: HTTPS Request
    C->>A: Ziti Transport
    A->>M: Rate Limit + Auth
    M->>S: Business Logic
    S->>R: Data Access
    R->>D: SQL Query
    D-->>R: Rows
    R-->>S: Domain Objects
    S-->>M: Response
    M-->>A: JSON
    A-->>C: HTTP Response
    C-->>B: HTTPS Response
```

## Component Breakdown

### API Server

The HTTP layer. Handles routing, request parsing, and response formatting.

| Responsibility | Not Responsible For |
|----------------|---------------------|
| Route registration | Business rules |
| Request validation | Data persistence |
| JSON serialization | External API calls |
| Error formatting | Authorization decisions |

### Service Layer

Where business logic lives. Services depend on repositories, never on HTTP concepts.

```
┌─────────────────────────────────────────┐
│              Handler (HTTP)              │
├─────────────────────────────────────────┤
│             Service (Logic)             │
├─────────────────────────────────────────┤
│           Repository (Data)             │
├─────────────────────────────────────────┤
│            Database (SQL)               │
└─────────────────────────────────────────┘
```

!!! tip "Rule of thumb"
    If a function needs `*http.Request` or `http.ResponseWriter`, it belongs in the handler layer.
    If it needs `*sql.DB`, it belongs in the repository layer.
    Everything else is a service.

### Repository Layer

Thin data access layer. Translates between SQL rows and Go structs.

- One repository per aggregate root
- No business logic — only CRUD + query building
- Uses `sqlx` for scanning and named parameters

### Database

PostgreSQL with schema managed via migration files.

```
migrations/
├── 001_initial.sql
├── 002_add_items.sql
└── 003_add_metadata.sql
```

## Deployment

```mermaid
graph LR
    subgraph Developer
        Code[git push]
    end

    subgraph Woodpecker CI
        Build[Build Image]
        Push[Push to Registry]
        Deploy[SSH + Docker Deploy]
    end

    subgraph Proxmox
        LXC[LXC Container]
        Docker[Docker Compose]
    end

    Code --> Build --> Push --> Deploy --> LXC
    LXC --> Docker
```

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `DB_HOST` | yes | PostgreSQL host |
| `DB_PASSWORD` | yes | Database password |
| `JWT_SECRET` | yes | 32+ character secret for token signing |
| `S3_ENDPOINT` | no | MinIO/S3 endpoint for file storage |
| `S3_ACCESS_KEY` | no | S3 access key |
| `S3_SECRET_KEY` | no | S3 secret key |
| `PORT` | no | HTTP port (default: `8080`) |

!!! danger "Secrets"
    Never commit secrets to git. Use Phase or Vaultwarden for secret management.
    The CI pipeline fetches secrets at build time via the Phase plugin.

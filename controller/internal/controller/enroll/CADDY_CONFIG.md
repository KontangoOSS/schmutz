# Caddy Configuration for Consolidated Progress Streaming

## Architecture

Caddy acts as the front-end reverse proxy and traffic router:

```
                    ┌─────────────────────────────────────┐
                    │       Device (Enrolled)              │
                    │   - Posts to /api/enroll/stream      │
                    │   - Dials progress-service via Ziti  │
                    └──────────────┬──────────────────────┘
                                   │
                        ┌──────────┴──────────┐
                        │                     │
                   HTTPS:443            Ziti Tunnel
                        │                     │
                    ┌───▼───────────────────────────┐
                    │         Caddy (443)           │
                    │   - SNI routing               │
                    │   - TLS termination           │
                    │   - Traffic split             │
                    └───┬──────────────┬────────────┘
                        │              │
                 HTTP:3080        Ziti Services
               (schmutz-controller)  (progress-service)
                        │              │
                    ┌───▴──────────────▴────────┐
                    │  schmutz-controller       │
                    │  ┌──────────────────────┐ │
                    │  │ /api/enroll/stream   │ │
                    │  │ - Public SSE         │ │
                    │  │ - Publishes to NATS  │ │
                    │  │   (progress-public)  │ │
                    │  └──────────────────────┘ │
                    │                           │
                    │  Progress Service         │
                    │  ┌──────────────────────┐ │
                    │  │ :9091 (localhost)    │ │
                    │  │ - Ziti authenticated │ │
                    │  │ - Publishes to NATS  │ │
                    │  │   (progress-ziti)    │ │
                    │  └──────────────────────┘ │
                    │                           │
                    │  NATS JetStream           │
                    │  ┌──────────────────────┐ │
                    │  │ Stream: progress-pub │ │
                    │  │ Stream: progress-ziti│ │
                    │  │ Subject: progress.>  │ │
                    │  └──────────────────────┘ │
                    └───────────────────────────┘
```

## Caddy Configuration Example

```caddy
# Main public endpoint on 443
kontango.io {
  # TLS setup
  tls /etc/kontango/certs/kontango.io.pem /etc/kontango/certs/kontango.io.key
  
  # Layer 4 SNI routing for internal APIs
  route /api/enroll/* {
    # Public enrollment stream (unauthenticated)
    reverse_proxy 127.0.0.1:3080
  }
  
  route /api/health* {
    # Health checks (public)
    reverse_proxy 127.0.0.1:3080
  }
  
  route /api/ziti/* {
    # Ziti admin API (requires authentication)
    reverse_proxy 127.0.0.1:3080
    # Could add auth middleware here
  }
}

# Ziti services (accessed via tunnel, not external)
# These services are internal to the Ziti overlay
#
# progress-service
#   - Intercept: progress-service (no port, Ziti routing)
#   - Host: 127.0.0.1:9091
#   - Authentication: Ziti identity required
#   - Authorization: #enrolled attribute
#
# ssh-{nickname}
#   - Intercept: ssh-{nickname}.tango:22
#   - Host: 127.0.0.1:2222
#   - Authorization: #stage-3 (or #admin for SSH)
#
# api-{nickname}
#   - Intercept: {nickname}.tango:8080
#   - Host: 127.0.0.1:8080
#   - Authorization: #stage-2 and above
```

## Traffic Flow Examples

### 1. Initial Enrollment (Public SSE Stream)

```
Device → Caddy (HTTPS:443) → schmutz-controller (/api/enroll/stream)
  ├─ POST comprehensive fingerprint
  ├─ Receive verification events (verify, decision)
  ├─ Receive identity + enrollment tag
  └─ Controller publishes to NATS (progress-public stream)
       └─ Available for UI subscription in real-time
```

**Classification**: `progress-public`
**Source**: Unauthenticated HTTP
**NATS Stream**: `progress-public`
**Subjects**: `progress.public.{step}`

### 2. Post-Enrollment Progress (Ziti-Authenticated)

```
Device → Ziti Tunnel → progress-service (localhost:9091)
  ├─ Ziti SDK authenticates device identity
  ├─ Send newline-delimited JSON (NDJSON)
  └─ Controller publishes to NATS (progress-ziti stream)
       └─ Available for monitoring/dashboard in real-time
```

**Classification**: `progress-ziti`
**Source**: Ziti-authenticated overlay
**NATS Stream**: `progress-ziti`
**Subjects**: `progress.ziti.{deviceID}`

## NATS Stream Configuration

```nats
# Progress messages from public enrollment (pre-authentication)
stream progress-public {
  subjects: progress.public.>
  retention: limits
  max_age: 24h
  max_msgs: 1000000
}

# Progress messages from Ziti (post-authentication)
stream progress-ziti {
  subjects: progress.ziti.>
  retention: limits
  max_age: 24h
  max_msgs: 1000000
}

# Combined progress subscription for UI
consumer progress-ui {
  flow_control: true
  idle_heartbeat: 5s
  deliver_policy: new
}
```

## Real-Time UI Subscription Pattern

The enrollment UI can subscribe to real-time progress via NATS WebSocket:

```javascript
// Subscribe to all progress for a specific device
// Gets messages from BOTH public and ziti streams
const sub = nats.subscribe("progress.*.device-id-001");

for await (const msg of sub) {
  const update = JSON.parse(msg.data);
  console.log(`${update.step}: ${update.status}`);
}
```

## Key Advantages

✅ **Consolidated source** — all progress events go through NATS  
✅ **Real-time delivery** — NATS subscribers get instant updates  
✅ **Classification** — public vs ziti messages kept separate for audit  
✅ **No HTTP overhead** — Ziti progress doesn't hit HTTP endpoint  
✅ **Caddy traffic split** — easy to add authentication layers  
✅ **Scalable** — multiple UIs can subscribe independently  
✅ **Audit trail** — all events recorded in NATS for compliance  

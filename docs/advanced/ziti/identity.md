# Ziti Identity and Permissions

[← Advanced Reference](../README.md)

---

Schmutz is a Ziti SDK application. Each edge node has a Ziti identity
file -- a JSON document containing the node's x509 certificate, private
key, and controller endpoint(s). The identity is the only secret on the
machine.

---

## Identity Loading

```mermaid
flowchart TD
    subgraph provision["Node Provisioning"]
        P1["Controller creates identity\nwith #edge attribute"]
        P2["Identity enrolled\n(x509 cert + key)"]
        P3["JSON file written to disk"]
    end

    subgraph startup["Schmutz Startup"]
        S1["Read config.yaml\n(identity path)"]
        S2["ziti.NewContextFromFile(path)"]
        S3["SDK loads cert + key"]
        S4["SDK connects to controller\n(mTLS)"]
        S5["Controller authenticates identity\nvia certificate chain"]
        S6["SDK receives session token\nand service list"]
    end

    provision --> startup

    style provision fill:#f0f4ff,stroke:#4a86c8
    style startup fill:#e6f9e6,stroke:#38a169
```

```go
zitiCtx, err := ziti.NewContextFromFile(cfg.Identity)
if err != nil {
    logger.Error("load ziti identity",
        "identity", cfg.Identity,
        "error", err,
    )
    os.Exit(1)
}
defer zitiCtx.Close()
```

The SDK handles all certificate validation, session management, and
reconnection internally. Schmutz calls one function and gets back a
context that can dial services.

---

## Identity File Contents

```json
{
    "ztAPI": "https://ctrl.example.io:1280/edge/client/v1",
    "ztAPIs": [
        "https://ctrl-1.example.io:1280/edge/client/v1",
        "https://ctrl-2.example.io:1280/edge/client/v1",
        "https://ctrl-3.example.io:1280/edge/client/v1"
    ],
    "id": {
        "cert": "pem-encoded x509 certificate",
        "key": "pem-encoded private key",
        "ca": "pem-encoded CA bundle"
    }
}
```

The `ztAPIs` list enables controller failover. If one controller is
unreachable, the SDK tries the next.

---

## Dial-Only Permissions Model

Ziti identities can have two types of permissions:

- **Bind** -- host a service (accept incoming connections)
- **Dial** -- connect to a service (make outgoing connections)

Edge node identities have **dial-only** permissions. They can connect to
public-facing services but cannot host anything on the overlay.

```mermaid
flowchart LR
    subgraph policy["Ziti Service Policies"]
        subgraph dial_pol["Dial Policies"]
            DP["#edge identities\nCAN dial:\npublic-app\npublic-auth\nhoneypot\ndefault-ingress"]
        end
        subgraph bind_pol["Bind Policies"]
            BP["#edge identities\nCAN bind:\n(nothing)"]
        end
    end

    subgraph effect["Practical Effect"]
        E1["Can relay traffic\nTO services"]
        E2["Cannot create\nfake services"]
        E3["Cannot intercept\nreal services"]
        E4["Cannot access\ncontroller API"]
    end

    dial_pol --> E1
    bind_pol --> E2
    bind_pol --> E3
    bind_pol --> E4

    style dial_pol fill:#e6f9e6,stroke:#38a169
    style bind_pol fill:#ffe6e6,stroke:#e53e3e
    style effect fill:#f0f4ff,stroke:#4a86c8
```

---

## Why No Bind?

If an edge node could bind services, a compromised node could:

1. Register as a terminator for `auth-provider`
2. Intercept authentication traffic
3. Steal credentials or session tokens

With dial-only permissions, a compromised edge node can only do what a
random internet client already can: connect to public services. The blast
radius is zero.

---

## Attribute-Based Policy Enforcement

Before a dial succeeds, the Ziti controller checks two policy layers:

```mermaid
flowchart TD
    D["Dial('public-app')"] --> SP{Service Policy\ncheck}
    SP -->|"Identity has\n#edge attribute\nService allows\n#edge to dial"| EP{Edge Router\nPolicy check}
    SP -->|"No matching\npolicy"| FAIL1["Dial fails:\naccess denied"]

    EP -->|"Identity can\nreach a router\nthat hosts the\nservice"| OK["Circuit\nestablished"]
    EP -->|"No reachable\nrouter"| FAIL2["Dial fails:\nno route"]

    style D fill:#f0f4ff,stroke:#4a86c8
    style SP fill:#fff8e6,stroke:#f58220
    style EP fill:#fff8e6,stroke:#f58220
    style OK fill:#e6f9e6,stroke:#38a169
    style FAIL1 fill:#ffe6e6,stroke:#e53e3e
    style FAIL2 fill:#ffe6e6,stroke:#e53e3e
```

1. **Service Policy**: Does this identity (with `#edge` attribute) have
   dial access to `public-app`?
2. **Edge Router Policy**: Can this identity reach an edge router that is
   connected to a terminator for `public-app`?

Both must pass. The controller administrator can revoke an edge node's
access to specific services without touching the edge node itself.

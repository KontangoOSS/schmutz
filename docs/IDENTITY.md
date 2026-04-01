# Edge Node Identity Model

[← Back to README](../README.md)

---

## Principles

Edge nodes are **ephemeral, anonymous, and isolated**. They are disposable
infrastructure — the networking equivalent of a paper towel.

```mermaid
flowchart LR
    E["Ephemeral\nborn at install,\ndies with the VM"] --- A["Anonymous\nall share one\nattribute"]
    A --- I["Isolated\ncan't see\neach other"]
    I --- P["Public\nrequires a\npublic IP"]
    P --- S["Sealed\nhardware fingerprint\nencrypted at install"]

    style E fill:#f0f4ff,stroke:#4a86c8
    style A fill:#fff3e6,stroke:#f58220
    style I fill:#f5f0ff,stroke:#7c3aed
    style P fill:#e6f9e6,stroke:#38a169
    style S fill:#fefcbf,stroke:#d69e2e
```

1. **Ephemeral** — identity created at install time, dies with the node
2. **Anonymous** — all edge nodes share one attribute, identical permissions
3. **Isolated** — nodes cannot reach each other, no peer awareness
4. **Public** — edge nodes require a public IP (installer refuses private addresses)
5. **Sealed** — the node encrypts a hardware fingerprint at install time; only
   the controller can verify the claim

---

## What an Edge Node Can Do

```mermaid
flowchart LR
    E["🛡️ Edge Node"] -->|"dial ✅"| App["public-app"]
    E -->|"dial ✅"| HP["honeypot"]
    E -->|"dial ✅"| Auth["auth-provider"]
    E -->|"dial 🚫"| Vault["secrets-vault"]
    E -->|"dial 🚫"| Git["git-forge"]
    E -->|"bind 🚫"| Any["anything"]

    style App fill:#e6f9e6,stroke:#38a169
    style HP fill:#e6f9e6,stroke:#38a169
    style Auth fill:#e6f9e6,stroke:#38a169
    style Vault fill:#ffe6e6,stroke:#e53e3e
    style Git fill:#ffe6e6,stroke:#e53e3e
    style Any fill:#ffe6e6,stroke:#e53e3e
```

Edge nodes can only **dial** public-facing services. They cannot bind services,
access management APIs, or reach internal infrastructure.

If an edge node is compromised, the attacker can only do what a random
internet client could do: talk to public services.

---

## Why Dial-Only?

A Ziti identity can **bind** (host a service) or **dial** (connect to a
service). Edge nodes only dial. They never host anything.

```mermaid
flowchart TD
    subgraph bind["Bind (host)"]
        B1["Register as service terminator"]
        B2["Accept incoming Ziti circuits"]
        B3["Serve requests"]
    end

    subgraph dial["Dial (connect)"]
        D1["Request connection to a service"]
        D2["Fabric routes to terminator"]
        D3["Relay bytes"]
    end

    Edge["🛡️ Edge Node"] -->|"✅ allowed"| dial
    Edge -->|"🚫 blocked"| bind

    style bind fill:#ffe6e6,stroke:#e53e3e
    style dial fill:#e6f9e6,stroke:#38a169
```

This means:
- An attacker on an edge node **can't create fake services**
- An attacker **can't intercept traffic** meant for real services
- An attacker **can't register** as a service terminator
- The blast radius of a compromised edge node is: **one node's Ziti identity**

---

## Geo-Routing

Edge nodes don't need to know where services run. The Ziti fabric handles it:

```mermaid
flowchart TD
    subgraph regionA["Region A"]
        EA["edge-1"] --> RA["router-a"]
        TA["tunneler-a"] --- RA
    end

    subgraph regionC["Region C"]
        EC["edge-3"] --> RC["router-c"]
        TC["tunneler-c"] --- RC
    end

    RA <-->|"fabric link"| RC

    EA -.->|"dials public-default\n→ picks tunneler-a\n(lowest cost)"| TA
    EC -.->|"dials public-default\n→ picks tunneler-c\n(lowest cost)"| TC

    style regionA fill:#f0f4ff,stroke:#4a86c8
    style regionC fill:#fff3e6,stroke:#f58220
```

If a region's tunneler is down, the fabric routes to the next best option.
No edge node configuration changes needed.

---

## Identity Lifecycle

```mermaid
flowchart TD
    A["🖥️ VM spins up"] --> B["📦 Installer runs"]
    B --> C["🪪 Ziti identity created\n(#edge attribute)"]
    C --> D["✅ Controller verifies\nAdds to dial policy"]
    D --> E["🛡️ Node starts\nProcesses traffic"]
    E --> F{"HP drops\nto zero?"}
    F -->|"No"| E
    F -->|"Yes"| G["🔴 Still running\nDropping everything"]
    G --> H["🗑️ Operator destroys VM\nRemoves DNS A record"]
    H --> I["🧹 Controller cleans up\nOrphaned identity removed"]

    style A fill:#f0f4ff,stroke:#4a86c8
    style E fill:#e6f9e6,stroke:#38a169
    style G fill:#fed7d7,stroke:#e53e3e
    style H fill:#f5f5f5,stroke:#999
```

No SSH access to edge nodes after install. No configuration changes.
No updates. No maintenance. If something goes wrong, destroy and replace.

---

## The Exception

The Ziti controller Raft cluster is the **only** component where controllers
communicate directly with each other. Controllers are not edge nodes —
they are the trust anchor. They run in a separate security domain with
different identities, different permissions, and different operational
procedures.

```mermaid
flowchart LR
    subgraph controllers["🎛️ Controllers (trust anchor)"]
        C1["node-1"] <--> C2["node-2"]
        C2 <--> C3["node-3"]
        C1 <--> C3
    end

    subgraph edges["🛡️ Edge Nodes (isolated)"]
        E1["edge-1"]
        E2["edge-2"]
        E3["edge-3"]
    end

    E1 -.->|"dial only"| controllers
    E2 -.->|"dial only"| controllers
    E3 -.->|"dial only"| controllers

    E1 ~~~ E2
    E2 ~~~ E3

    style controllers fill:#f0f4ff,stroke:#4a86c8
    style edges fill:#fff3e6,stroke:#f58220
```

Controllers talk to each other. Edge nodes never talk to other edge nodes.
Period.

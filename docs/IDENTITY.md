# Identity Model

[← Back to README](../README.md)

---

## What Gets Created

On successful enrollment, the controller creates a Ziti identity via the management API:

```json
{
  "name": "eph-{hostname}-{random8}",
  "type": "Default",
  "isAdmin": false,
  "roleAttributes": ["{tier}"],
  "enrollment": { "ott": true },
  "tags": {
    "enrolledFrom": "{client_ip}",
    "enrolledAt": "{timestamp}",
    "hostname": "{hostname}",
    "os": "{os}",
    "arch": "{arch}",
    "platform": "{platform}",
    "tier": "{tier}"
  }
}
```

The identity name is ephemeral-style (`eph-` prefix) with a random suffix to avoid collisions across re-enrollments.

## OTT JWT

After creating the identity, the controller fetches the enrollment JWT from:

```
GET /edge/management/v1/identities/{id}/enrollments
```

This JWT is returned to the agent in the `/enroll` response. The agent passes it to the Ziti SDK's enrollment flow, which exchanges it for a signed certificate. The JWT is single-use and expires.

## Re-enrollment

The fingerprint is stored in Bao (`enrollment/kv/data/approved/{fingerprint}`) along with the Ziti identity ID, identity name, hostname, and tier.

On a subsequent `/enroll` from a known fingerprint, the controller skips the telemetry window and immediately creates a new identity (same tier as last time). The old identity is not deleted — it may still be active if the device is running.

## Role Attributes and Service Access

Role attributes on the identity (`sandbox` or `quarantine`) are matched against Ziti AppWAN bind/dial policies to control which services the device can reach. Adding a device to a different tier means updating its role attribute in Ziti, not re-enrolling.

## Banned Devices

Banned fingerprints are stored in Bao (`enrollment/kv/data/banned/{fingerprint}`). A banned device gets `{"status":"busy"}` from `/enroll` — the same response as any other rejection, giving no signal about why.

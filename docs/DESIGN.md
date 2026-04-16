# Design

[← Back to README](../README.md)

---

## Goals

- Enroll real machines into the Kontango overlay with minimal friction
- Give the controller enough signal to distinguish real devices from bots/scanners
- Keep the agent small and dependency-light — it runs as part of an installer
- Be safe to run on untrusted machines: no secrets on disk until enrollment completes

## Two-Pass Design

The core insight is that a bot hitting `/enroll` once can fake system info, but it's much harder to sustain 60 seconds of plausible real-time telemetry. The two-pass flow uses the telemetry window as a lightweight proof-of-work:

1. First POST opens a window and issues a stream token
2. Agent streams real metrics for ~60 seconds
3. Second POST triggers the decision engine with the collected data

The `stream_token` is a short-lived random credential that authenticates stream submissions to the window. It's never stored on disk — if the agent crashes during streaming, it restarts from the first POST and gets a new token.

## Why HTTPS Not Ziti

During enrollment the device has no Ziti identity. Stream traffic goes over plain HTTPS to `/stream` on the controller. This is acceptable because:

- The stream token prevents unauthorized writes to the window
- The metrics themselves are not sensitive
- The window evicts after 90 seconds regardless

Once enrolled, a device has a Ziti identity and future communication uses the overlay.

## Future: Ziti-Bootstrap Path

A second enrollment type is planned but not yet implemented. The controller would issue a throwaway Ziti identity with `dial-only` permission to a single service (`enrollment-stream.tango`). The agent would enroll this identity and stream metrics over Ziti. On approval, the throwaway identity is deleted and the real identity is issued. This removes the HTTPS dependency for stream traffic entirely.

## Decision Engine

The controller runs a set of checkers against the collected data:

| Checker | What it looks for |
|---------|-------------------|
| Trusted | Client IP in known trusted list |
| Duplicate | Fingerprint already enrolled |
| Sysinfo | Plausible hostname, OS, arch |
| Stream | Sufficient metrics received |

Stream scoring is tiered: 0 metrics → 0 pts, 1-4 → 10, 5-19 → 30, 20-59 → 40, 60+ → 50 (max). At 5-second intervals over 60 seconds, an honest device sends ~12 metrics, landing in the 10-19 → 30pt range. With the window extended or agent started early, 60+ is achievable.

## Tiers

Tier is assigned at enrollment and maps to Ziti role attributes:

- `sandbox` — trusted client IP; more permissive service access
- `quarantine` — unknown device that passed scoring; minimal service access

Tier is stored in Bao and used on re-enrollment to restore the same identity class.

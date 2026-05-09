# Origin

[← Back to README](../README.md)

---

Schmutz started as an L4 edge classifier — a TCP proxy that fingerprinted connections using JA4/JA3 TLS heuristics and decided whether to pass them through, honeypot them, or drop them. That code no longer exists in this repo.

The current schmutz is a device enrollment agent. The name stuck.

The enrollment approach — stream telemetry first, decide second — came from the same problem the original schmutz was solving: how do you tell a real machine from a bot without requiring any prior relationship? The original answer was TLS fingerprints. The current answer is: make the device prove it can sustain 60 seconds of real system metrics.

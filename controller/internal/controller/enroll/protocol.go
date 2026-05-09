package enroll

// Protocol defines the WebSocket message types for the enrollment conversation.
// Same protocol for web (BrowZer) and CLI. Client responds to probes.

// --- Client → Controller ---

// Hello is the first message. Declares intent.
//
//   {"type": "hello", "method": "new"}     — new device, no credentials
//   {"type": "hello", "method": "scan"}    — returning device, figure out who I am
//   {"type": "hello", "method": "oidc", "token": "eyJ..."}  — person with OIDC token
//   {"type": "hello", "method": "approle", "role_id": "...", "secret_id": "..."}  — trusted device

// ProbeResponse is a response to a controller probe.
// The client fills in whatever it can. Missing fields are fine.
//
// OS probe response:
//   {"type": "os", "hostname": "web-1", "os": "linux", "os_version": "Ubuntu 24.04 LTS",
//    "arch": "amd64", "kernel": "6.14.0-37-generic"}
//
// Hardware probe response:
//   {"type": "hardware", "cpu_info": "i7-1360P", "cpu_cores": 16,
//    "memory_mb": 32768, "machine_id": "7c8b694855b9...",
//    "serial": "PF4...", "hardware_hash": "26be41b89ce2bcda",
//    "disk_serials": ["S3YKNX0R504537"], "timezone": "America/Denver"}
//
// Network probe response:
//   {"type": "network", "macs": ["fc:5c:ee:c2:fb:90", ...],
//    "interfaces": [{"name": "eth0", "mac": "fc:5c:ee:c2:fb:90",
//      "ips": ["10.11.30.50"], "up": true}],
//    "dns_servers": ["10.11.30.20"], "gateway": "10.11.30.1"}
//
// System probe response:
//   {"type": "system", "uptime_secs": 86400, "boot_id": "a1b2c3...",
//    "locale": "en_US.UTF-8", "ssh_host_keys": ["ssh-ed25519 AAAA..."],
//    "open_ports": [22, 8080], "package_count": 342}

// --- Controller → Client ---

// Probe requests specific information.
//   {"type": "probe", "probe": "os"}
//   {"type": "probe", "probe": "hardware"}
//   {"type": "probe", "probe": "network"}
//   {"type": "probe", "probe": "system"}

// Result is the final verdict.
//   {"type": "identity", "status": "approved", "id": "...", "nickname": "...",
//    "identity": <signed JSON>, "config": {...}}
//   {"type": "identity", "status": "quarantine", "id": "...", "nickname": "...",
//    "identity": <signed JSON>, "config": {...}}
//   {"type": "status", "status": "rejected", "reason": "banned"}
//   {"type": "error", "reason": "enrollment failed"}

// Probes is the ordered list of probes the controller sends.
// Each probe is independent — the pipeline processes results as they arrive.
var Probes = []string{"os", "hardware", "network", "system"}

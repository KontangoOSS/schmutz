package store

const schema = `
CREATE TABLE IF NOT EXISTS enrollment_requests (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_id       text NOT NULL,
    hostname         text NOT NULL,
    source_ip        text NOT NULL,
    ziti_identity_id text,
    status           text NOT NULL CHECK (status IN (
                         'pending','auto_approved','approved','denied','rejected'
                     )),
    decision_reason  text,
    match_score      int,
    match_confidence text,
    decided_at       timestamptz,
    decided_by       text,
    raw_event        jsonb NOT NULL,
    requested_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_er_machine_id    ON enrollment_requests (machine_id);
CREATE INDEX IF NOT EXISTS idx_er_status        ON enrollment_requests (status);
CREATE INDEX IF NOT EXISTS idx_er_requested_at  ON enrollment_requests (requested_at DESC);

CREATE TABLE IF NOT EXISTS machine_fingerprints (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_id     text UNIQUE NOT NULL,
    hostname       text NOT NULL,
    source_ip      text NOT NULL,
    hardware_hash  text NOT NULL,
    os             text,
    os_version     text,
    arch           text,
    kernel         text,
    cpu_model      text,
    cpu_cores      int,
    memory_mb      int,
    serial_number  text,
    mac_addrs      text[],
    disk_serials   text[],
    ssh_host_keys  text[],
    system_hash    text,
    network_hash   text,
    full_hash      text,
    boot_id        text,
    timezone       text,
    locale         text,
    gateway        text,
    dns_servers    text[],
    open_ports     int[],
    package_count  int,
    uptime_secs    int,
    ja4            text,
    user_agent     text,
    extra          jsonb,
    first_seen     timestamptz NOT NULL DEFAULT now(),
    last_seen      timestamptz NOT NULL DEFAULT now(),
    seen_count     int NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_mf_hardware_hash ON machine_fingerprints (hardware_hash);
CREATE INDEX IF NOT EXISTS idx_mf_full_hash     ON machine_fingerprints (full_hash);

CREATE TABLE IF NOT EXISTS telemetry_snapshots (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_id  text NOT NULL,
    slug        text NOT NULL,
    payload     jsonb NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ts_machine_slug_time
    ON telemetry_snapshots (machine_id, slug, recorded_at DESC);
`

-- COPY OF migrations/0001_beacon_log.sql for go:embed in tests. Keep in sync.
-- Run as: psql -h postgres-db.tango -U neverland -d kontango_boot -f migrations/0001_beacon_log.sql
CREATE TABLE IF NOT EXISTS beacon_log (
    id           BIGSERIAL    PRIMARY KEY,
    session_id   UUID         NOT NULL,
    level        TEXT         NOT NULL CHECK (level IN ('ipxe', 'hook')),
    mac          TEXT         NOT NULL,
    ip           TEXT,
    payload      JSONB        NOT NULL,
    received_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    claimed_at   TIMESTAMPTZ,
    claimed_by   TEXT
);

CREATE INDEX IF NOT EXISTS beacon_log_session_idx ON beacon_log (session_id);
CREATE INDEX IF NOT EXISTS beacon_log_mac_idx     ON beacon_log (mac);
CREATE INDEX IF NOT EXISTS beacon_log_received_idx ON beacon_log (received_at DESC);

package store

import (
	"context"
	"encoding/json"
	"time"
)

type TelemetrySnapshot struct {
	ID         string          `json:"id"`
	MachineID  string          `json:"machine_id"`
	Slug       string          `json:"slug"`
	Payload    json.RawMessage `json:"payload"`
	RecordedAt time.Time       `json:"recorded_at"`
}

func (s *Store) InsertSnapshot(ctx context.Context, machineID, slug string, payload json.RawMessage) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO telemetry_snapshots (machine_id, slug, payload) VALUES ($1,$2,$3)`,
		machineID, slug, payload,
	)
	return err
}

func (s *Store) PruneSnapshots(ctx context.Context, keep int) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM telemetry_snapshots
		WHERE id NOT IN (
		    SELECT id FROM (
		        SELECT id, ROW_NUMBER() OVER (
		            PARTITION BY machine_id, slug ORDER BY recorded_at DESC
		        ) AS rn FROM telemetry_snapshots
		    ) ranked WHERE rn <= $1
		)`, keep,
	)
	return err
}

func (s *Store) ListSnapshots(ctx context.Context, machineID, slug string, limit int) ([]TelemetrySnapshot, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, machine_id, slug, payload, recorded_at
		FROM telemetry_snapshots
		WHERE machine_id=$1 AND slug=$2
		ORDER BY recorded_at DESC LIMIT $3`,
		machineID, slug, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snaps []TelemetrySnapshot
	for rows.Next() {
		var sn TelemetrySnapshot
		if err := rows.Scan(&sn.ID, &sn.MachineID, &sn.Slug, &sn.Payload, &sn.RecordedAt); err != nil {
			return nil, err
		}
		snaps = append(snaps, sn)
	}
	return snaps, rows.Err()
}

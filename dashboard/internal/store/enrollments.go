package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type EnrollmentRecord struct {
	ID              string          `json:"id"`
	MachineID       string          `json:"machine_id"`
	Hostname        string          `json:"hostname"`
	SourceIP        string          `json:"source_ip"`
	ZitiIdentityID  string          `json:"ziti_identity_id,omitempty"`
	Status          string          `json:"status"`
	DecisionReason  string          `json:"decision_reason,omitempty"`
	MatchScore      *int            `json:"match_score,omitempty"`
	MatchConfidence string          `json:"match_confidence,omitempty"`
	DecidedAt       *time.Time      `json:"decided_at,omitempty"`
	DecidedBy       string          `json:"decided_by,omitempty"`
	RawEvent        json.RawMessage `json:"raw_event"`
	RequestedAt     time.Time       `json:"requested_at"`
}

func (s *Store) InsertEnrollment(ctx context.Context, r EnrollmentRecord) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO enrollment_requests
		    (machine_id, hostname, source_ip, ziti_identity_id, status,
		     decision_reason, match_score, match_confidence, decided_at,
		     decided_by, raw_event)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		r.MachineID, r.Hostname, r.SourceIP, nilIfEmpty(r.ZitiIdentityID),
		r.Status, nilIfEmpty(r.DecisionReason), r.MatchScore,
		nilIfEmpty(r.MatchConfidence), r.DecidedAt, nilIfEmpty(r.DecidedBy),
		r.RawEvent,
	)
	return err
}

func (s *Store) ListEnrollments(ctx context.Context, status string) ([]EnrollmentRecord, error) {
	var query string
	var args []any

	if status == "" {
		query = `SELECT id, machine_id, hostname, source_ip,
		                COALESCE(ziti_identity_id,''), status,
		                COALESCE(decision_reason,''), match_score,
		                COALESCE(match_confidence,''), decided_at,
		                COALESCE(decided_by,''), raw_event, requested_at
		         FROM enrollment_requests ORDER BY requested_at DESC LIMIT 500`
	} else {
		query = `SELECT id, machine_id, hostname, source_ip,
		                COALESCE(ziti_identity_id,''), status,
		                COALESCE(decision_reason,''), match_score,
		                COALESCE(match_confidence,''), decided_at,
		                COALESCE(decided_by,''), raw_event, requested_at
		         FROM enrollment_requests WHERE status=$1 ORDER BY requested_at DESC LIMIT 500`
		args = []any{status}
	}

	pgRows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer pgRows.Close()

	var rows []EnrollmentRecord
	for pgRows.Next() {
		var r EnrollmentRecord
		if err := pgRows.Scan(
			&r.ID, &r.MachineID, &r.Hostname, &r.SourceIP,
			&r.ZitiIdentityID, &r.Status, &r.DecisionReason,
			&r.MatchScore, &r.MatchConfidence, &r.DecidedAt,
			&r.DecidedBy, &r.RawEvent, &r.RequestedAt,
		); err != nil {
			return nil, err
		}
		rows = append(rows, r)
	}
	return rows, pgRows.Err()
}

func (s *Store) UpdateEnrollmentStatus(ctx context.Context, machineID, status, reason, decidedBy string) error {
	now := time.Now()
	tag, err := s.pool.Exec(ctx, `
		UPDATE enrollment_requests
		SET status=$1, decision_reason=$2, decided_by=$3, decided_at=$4
		WHERE machine_id=$5 AND status='pending'`,
		status, reason, decidedBy, now, machineID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no pending enrollment found for machine_id %q", machineID)
	}
	return nil
}

func (s *Store) HasDeniedRecord(ctx context.Context, hardwareHash string) (bool, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM enrollment_requests er
		JOIN machine_fingerprints mf ON mf.machine_id = er.machine_id
		WHERE mf.hardware_hash = $1 AND er.status = 'denied'`,
		hardwareHash,
	).Scan(&count)
	return count > 0, err
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

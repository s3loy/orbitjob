package postgres

import (
	"database/sql"
	"encoding/json"
	"fmt"

	domainworker "orbitjob/internal/core/domain/worker"
)

type WorkerRepository struct {
	db *sql.DB
}

func NewWorkerRepository(db *sql.DB) *WorkerRepository {
	return &WorkerRepository{db: db}
}

func scanWorkerSnapshot(scanner rowScanner) (domainworker.Snapshot, error) {
	var out domainworker.Snapshot
	var labelsBytes []byte

	err := scanner.Scan(
		&out.TenantID,
		&out.WorkerID,
		&out.Status,
		&out.LastHeartbeatAt,
		&out.LeaseExpiresAt,
		&out.Capacity,
		&labelsBytes,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return domainworker.Snapshot{}, fmt.Errorf("scan worker snapshot: %w", err)
	}

	if len(labelsBytes) == 0 {
		out.Labels = map[string]any{}
		return out, nil
	}
	if err := json.Unmarshal(labelsBytes, &out.Labels); err != nil {
		return domainworker.Snapshot{}, fmt.Errorf("decode worker labels: %w", err)
	}
	if out.Labels == nil {
		out.Labels = map[string]any{}
	}

	return out, nil
}

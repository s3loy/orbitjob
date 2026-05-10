package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type JobRepository struct {
	db *sql.DB
}

func NewJobRepository(db *sql.DB) *JobRepository {
	return &JobRepository{db: db}
}

// GetQuota returns the tenant's quotas map.
func (r *JobRepository) GetQuota(ctx context.Context, tenantID string) (map[string]any, error) {
	var raw []byte
	err := r.db.QueryRowContext(ctx, `
		SELECT quotas FROM tenants WHERE id = $1
	`, tenantID).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant quota: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var quotas map[string]any
	if err := json.Unmarshal(raw, &quotas); err != nil {
		return nil, fmt.Errorf("unmarshal tenant quotas: %w", err)
	}
	return quotas, nil
}

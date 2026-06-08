package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/domain/resource"
)

// SLIRepository provides write-side access to slis table.
type SLIRepository struct {
	db *sql.DB
}

// NewSLIRepository creates a new SLI repository.
func NewSLIRepository(db *sql.DB) *SLIRepository {
	return &SLIRepository{db: db}
}

// Create inserts a new SLI and returns its snapshot.
func (r *SLIRepository) Create(ctx context.Context, tenantID string, spec sli.CreateSpec) (sli.Snapshot, error) {
	var snap sli.Snapshot

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return snap, fmt.Errorf("begin create tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return snap, fmt.Errorf("set tenant context: %w", err)
	}

	sourceConfigBytes, err := json.Marshal(spec.SourceConfig)
	if err != nil {
		return snap, fmt.Errorf("marshal source_config: %w", err)
	}

	goodEventBytes, err := json.Marshal(spec.GoodEventCriteria)
	if err != nil {
		return snap, fmt.Errorf("marshal good_event_criteria: %w", err)
	}

	var sourceConfigRaw, goodEventRaw []byte
	err = tx.QueryRowContext(ctx, `
		INSERT INTO slis (tenant_id, name, description, sli_type, source_type, source_config, aggregation, good_event_criteria)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8::jsonb)
		RETURNING id, tenant_id, name, description, sli_type, source_type, source_config, aggregation, good_event_criteria, version, created_at, updated_at
	`, tenantID, spec.Name, spec.Description, spec.SLIType, spec.SourceType, sourceConfigBytes, spec.Aggregation, goodEventBytes).Scan(
		&snap.ID, &snap.TenantID, &snap.Name, &snap.Description, &snap.SLIType, &snap.SourceType,
		&sourceConfigRaw, &snap.Aggregation, &goodEventRaw, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
	)
	if err != nil {
		return snap, fmt.Errorf("insert sli: %w", err)
	}

	if len(sourceConfigRaw) > 0 {
		if err := json.Unmarshal(sourceConfigRaw, &snap.SourceConfig); err != nil {
			return snap, fmt.Errorf("unmarshal source_config: %w", err)
		}
	}
	if len(goodEventRaw) > 0 {
		if err := json.Unmarshal(goodEventRaw, &snap.GoodEventCriteria); err != nil {
			return snap, fmt.Errorf("unmarshal good_event_criteria: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return snap, fmt.Errorf("commit create sli: %w", err)
	}

	return snap, nil
}

// Delete soft-deletes an SLI.
func (r *SLIRepository) Delete(ctx context.Context, tenantID string, id int64, version int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return fmt.Errorf("set tenant context: %w", err)
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE slis SET deleted_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND version = $3 AND deleted_at IS NULL
	`, tenantID, id, version)
	if err != nil {
		return fmt.Errorf("delete sli: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return &resource.NotFoundError{Resource: "sli", ID: id}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit delete sli: %w", err)
	}

	return nil
}

// FindByCheckID finds all SLIs that source from the given check ID.
func (r *SLIRepository) FindByCheckID(ctx context.Context, tenantID string, checkID int64) ([]sli.Snapshot, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin find tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return nil, fmt.Errorf("set tenant context: %w", err)
	}

	// Query SLIs where source_config contains the check_id.
	rows, err := tx.QueryContext(ctx, `
		SELECT id, tenant_id, name, description, sli_type, source_type, source_config, aggregation, good_event_criteria, version, created_at, updated_at
		FROM slis
		WHERE tenant_id = $1 AND deleted_at IS NULL
		  AND source_type = 'check_run'
		  AND source_config @> jsonb_build_object('check_id', $2::int)
	`, tenantID, checkID)
	if err != nil {
		return nil, fmt.Errorf("query slis by check_id: %w", err)
	}
	defer func() { _ = rows.Close() }()

	slis, err := r.scanSLIs(rows)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit find sli: %w", err)
	}

	return slis, nil
}

func (r *SLIRepository) scanSLIs(rows *sql.Rows) ([]sli.Snapshot, error) {
	var slis []sli.Snapshot
	for rows.Next() {
		var snap sli.Snapshot
		var sourceConfigRaw, goodEventRaw []byte
		err := rows.Scan(
			&snap.ID, &snap.TenantID, &snap.Name, &snap.Description, &snap.SLIType, &snap.SourceType,
			&sourceConfigRaw, &snap.Aggregation, &goodEventRaw, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan sli: %w", err)
		}
		if len(sourceConfigRaw) > 0 {
			if err := json.Unmarshal(sourceConfigRaw, &snap.SourceConfig); err != nil {
				return nil, fmt.Errorf("unmarshal source_config: %w", err)
			}
		}
		if len(goodEventRaw) > 0 {
			if err := json.Unmarshal(goodEventRaw, &snap.GoodEventCriteria); err != nil {
				return nil, fmt.Errorf("unmarshal good_event_criteria: %w", err)
			}
		}
		slis = append(slis, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate slis: %w", err)
	}
	return slis, nil
}

package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"orbitjob/internal/core/domain/function"
)

// AttemptsByRunID distinguishes unavailable/ambiguous history (known=false)
// from a uniquely linked ledger run with no attempts (known=true, empty slice).
// The UUID digest rule matches functionRunReadColumns and the log locator.
func (r *FunctionRunRepository) AttemptsByRunID(ctx context.Context, tenantID string, functionID int64, runID string) ([]function.Attempt, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, fmt.Errorf("begin function attempts: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return nil, false, err
	}
	rows, err := tx.QueryContext(ctx, `
 WITH matched AS (
  SELECT r.id FROM function_runs fr
  JOIN job_run_control_plane r ON r.tenant_id=fr.tenant_id AND r.source_uid='function-' || fr.function_id::text
  JOIN job_definition_revisions d ON d.id=r.revision_id AND d.tenant_id=r.tenant_id AND d.source_mode='function' AND d.source_uid=r.source_uid
  WHERE fr.tenant_id=$1 AND fr.function_id=$2 AND fr.run_id=$3::uuid
   AND replace(fr.run_id::text,'-','')=substr(CASE WHEN length(rtrim(r.occurrence_key))=64 THEN rtrim(r.occurrence_key)
    ELSE encode(digest(rtrim(r.occurrence_key),'sha256'),'hex') END,1,32)
 ), unique_run AS (SELECT min(id) AS id FROM matched HAVING count(*)=1)
 SELECT a.attempt_number,a.phase,a.started_at,a.completed_at
 FROM unique_run r LEFT JOIN job_run_attempts_control_plane a ON a.run_id=r.id AND a.tenant_id=$1
 ORDER BY a.attempt_number`, tenantID, functionID, runID)
	if err != nil {
		return nil, false, fmt.Errorf("query function attempts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	attempts := []function.Attempt{}
	known := false
	for rows.Next() {
		known = true
		var number sql.NullInt64
		var phase sql.NullString
		var started, completed sql.NullTime
		if err := rows.Scan(&number, &phase, &started, &completed); err != nil {
			return nil, false, err
		}
		if !number.Valid {
			continue
		}
		item := function.Attempt{AttemptNumber: int(number.Int64), Phase: phase.String}
		if started.Valid {
			value := started.Time
			item.StartedAt = &value
		}
		if completed.Valid {
			value := completed.Time
			item.CompletedAt = &value
		}
		attempts = append(attempts, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	if !known {
		return nil, false, nil
	}
	return attempts, true, nil
}

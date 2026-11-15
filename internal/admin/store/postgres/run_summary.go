package postgres

import (
	"context"
	"fmt"

	runquery "orbitjob/internal/admin/app/run/query"
	"orbitjob/internal/domain/queryfilter"
)

// Summary groups the same ledger predicates as List in a single snapshot.
func (r *RunRepository) Summary(ctx context.Context, in runquery.ListInput) (runquery.Summary, error) {
	tx, err := WithTenant(ctx, r.db, in.TenantID)
	if err != nil {
		return runquery.Summary{}, fmt.Errorf("begin run summary: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT phase, count(*) FROM job_run_control_plane r `+runFilterSQL+` GROUP BY phase`, in.TenantID, in.Phase, in.SourceUID, in.OccurrenceKey, queryfilter.MicrosecondBound(in.CreatedFrom), queryfilter.MicrosecondBound(in.CreatedBefore))
	if err != nil {
		return runquery.Summary{}, fmt.Errorf("query run summary: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := runquery.Summary{Phases: make(map[string]int64)}
	for rows.Next() {
		var phase string
		var count int64
		if err := rows.Scan(&phase, &count); err != nil {
			return runquery.Summary{}, fmt.Errorf("scan run summary: %w", err)
		}
		out.Phases[phase] = count
		out.Total += count
	}
	if err := rows.Err(); err != nil {
		return runquery.Summary{}, fmt.Errorf("iterate run summary: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return runquery.Summary{}, fmt.Errorf("commit run summary: %w", err)
	}
	return out, nil
}

// runFilterSQL is shared by list and summary; pagination never changes the
// identity, tenant or time predicates counted by the aggregate.
const runFilterSQL = `WHERE r.tenant_id=$1 AND ($2='' OR r.phase=$2)
 AND ($3='' OR r.source_uid=$3) AND ($4='' OR r.occurrence_key=$4)
 AND (NULLIF($5,'')::timestamptz IS NULL OR r.created_at >= NULLIF($5,'')::timestamptz)
 AND (NULLIF($6,'')::timestamptz IS NULL OR r.created_at < NULLIF($6,'')::timestamptz)`

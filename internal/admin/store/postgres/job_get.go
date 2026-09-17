package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	query "orbitjob/internal/admin/app/job/query"
	"orbitjob/internal/domain/resource"
)

// Get reads the active revision of one job definition.
//
// A job is a ScheduledJob Custom Resource; the API serves the revision the
// operator projected from it. Only the active revision is served, because that
// is the definition new runs use — an inactive revision is history, not the job.
func (r *JobRepository) Get(ctx context.Context, in query.GetInput) (query.GetItem, error) {
	tx, err := WithTenant(ctx, r.db, in.TenantID)
	if err != nil {
		return query.GetItem{}, fmt.Errorf("begin job get tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(ctx, `
		SELECT id, tenant_id, source_mode, source_uid, source_namespace, source_name,
		       generation, rtrim(spec_hash), normalized_spec::text, actor, created_at
		FROM job_definition_revisions
		WHERE tenant_id = $1 AND id = $2 AND is_active
	`, in.TenantID, in.ID)

	item, err := scanDefinitionDetail(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return query.GetItem{}, &resource.NotFoundError{Resource: "job", ID: in.ID}
		}
		return query.GetItem{}, fmt.Errorf("query job definition: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return query.GetItem{}, fmt.Errorf("commit job get tx: %w", err)
	}
	return item, nil
}

func scanDefinitionDetail(scanner rowScanner) (query.GetItem, error) {
	var (
		out query.GetItem
		raw []byte
	)
	if err := scanner.Scan(
		&out.ID,
		&out.TenantID,
		&out.SourceMode,
		&out.SourceUID,
		&out.Namespace,
		&out.Name,
		&out.Generation,
		&out.SpecHash,
		&raw,
		&out.Actor,
		&out.CreatedAt,
	); err != nil {
		return query.GetItem{}, err
	}

	spec, err := decodeScheduledJobSpec(raw)
	if err != nil {
		return query.GetItem{}, err
	}
	out.Schedule = spec.Schedule
	out.Suspend = spec.Suspend
	out.ConcurrencyPolicy = string(spec.ConcurrencyPolicy)
	out.MisfirePolicy = string(spec.MisfirePolicy)
	out.TimeoutSeconds = spec.TimeoutSeconds
	out.RetryMaxAttempts = spec.RetryPolicy.MaxAttempts
	out.JobTemplate = query.JobTemplate{
		Image:        spec.JobTemplate.Image,
		Command:      spec.JobTemplate.Command,
		Args:         spec.JobTemplate.Args,
		BackoffLimit: spec.JobTemplate.BackoffLimit,
	}
	out.ScheduleSummary = query.BuildScheduleSummary(out.Schedule, out.Suspend)
	return out, nil
}

// decodeScheduledJobSpec decodes a revision's normalized spec. The projection
// stores the full ScheduledJob spec, so a row that does not parse is a
// write-side defect rather than a missing definition; return the error instead
// of an empty job that would read as "exists, does nothing".
func decodeScheduledJobSpec(raw []byte) (v1alpha1.ScheduledJobSpec, error) {
	var spec v1alpha1.ScheduledJobSpec
	if len(raw) == 0 {
		return spec, nil
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		return v1alpha1.ScheduledJobSpec{}, fmt.Errorf("decode normalized spec: %w", err)
	}
	return spec, nil
}

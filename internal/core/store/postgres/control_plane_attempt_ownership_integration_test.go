//go:build integration

package postgres

import (
	"errors"
	"strings"
	"testing"

	"orbitjob/internal/core/domain/jobrun"
)

func TestUpdateAttemptPhaseRequiresPersistedJobUID(t *testing.T) {
	db := OpenStoreDB(t)
	ctx := ctxStore(t)
	const tenantID = "01J00000000000000000000001"

	if _, err := db.ExecContext(ctx, `
		INSERT INTO tenants (id, slug, name)
		VALUES ($1::char(26), 'attempt-owner', 'Attempt Owner');
		WITH revision AS (
		  INSERT INTO job_definition_revisions
		    (tenant_id, source_mode, source_uid, source_namespace, source_name,
		     generation, spec_hash, normalized_spec, actor)
		  VALUES ($1::char(26), 'kubernetes', 'scheduled-uid', 'tasks', 'nightly',
		          1, repeat('a', 64), '{}', 'operator')
		  RETURNING id
		), run AS (
		  INSERT INTO job_run_control_plane
		    (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor,
		     phase, attempt, max_attempts)
		  SELECT $1::char(26), 'scheduled-uid', id, repeat('b', 64), 'Schedule',
		         'scheduler', 'CreatingAttempt', 1, 1
		  FROM revision
		  RETURNING id
		)
		INSERT INTO job_run_attempts_control_plane
		  (tenant_id, run_id, attempt_number, phase, kubernetes_job_name,
		   kubernetes_job_uid)
		SELECT $1::char(26), id, 1, 'CreatingAttempt', 'oj-nightly-1', 'uid-owner'
		FROM run
	`, tenantID); err != nil {
		t.Fatalf("seed attempt ownership fixture: %v", err)
	}

	repo := NewControlPlaneRepository(db)
	var runID int64
	if err := db.QueryRowContext(ctx, `
		SELECT run_id FROM job_run_attempts_control_plane
		WHERE tenant_id=$1::char(26) AND kubernetes_job_name='oj-nightly-1'
	`, tenantID).Scan(&runID); err != nil {
		t.Fatalf("read seeded run id: %v", err)
	}
	conflictingAttempt := jobrun.Attempt{
		Number: 1, KubernetesJobName: "oj-nightly-1", KubernetesJobUID: "uid-replacement",
		Phase: jobrun.CreatingAttempt,
	}
	if err := repo.CreateAttemptForTenant(ctx, tenantID, runID, conflictingAttempt, jobrun.Running); !errors.Is(err, ErrAttemptConflict) {
		t.Fatalf("replacement UID create returned %v, want ErrAttemptConflict", err)
	}

	if _, _, err := repo.UpdateAttemptPhase(ctx, tenantID, "oj-nightly-1", "Running", "uid-replacement", "rv-2"); !errors.Is(err, ErrAttemptOwnership) {
		t.Fatalf("replacement UID returned %v, want ErrAttemptOwnership", err)
	}

	var phase, uid, resourceVersion string
	if err := db.QueryRowContext(ctx, `
		SELECT phase, kubernetes_job_uid, COALESCE(observed_resource_version, '')
		FROM job_run_attempts_control_plane
		WHERE tenant_id=$1::char(26) AND kubernetes_job_name='oj-nightly-1'
	`, tenantID).Scan(&phase, &uid, &resourceVersion); err != nil {
		t.Fatalf("read refused attempt update: %v", err)
	}
	if phase != "CreatingAttempt" || uid != "uid-owner" || resourceVersion != "" {
		t.Fatalf("replacement Job changed attempt: phase=%q uid=%q resourceVersion=%q", phase, uid, resourceVersion)
	}

	if _, _, err := repo.UpdateAttemptPhase(ctx, tenantID, "oj-nightly-1", "Running", "uid-owner", "rv-3"); err != nil {
		t.Fatalf("owner UID rejected: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT phase, kubernetes_job_uid, observed_resource_version
		FROM job_run_attempts_control_plane
		WHERE tenant_id=$1::char(26) AND kubernetes_job_name='oj-nightly-1'
	`, tenantID).Scan(&phase, &uid, &resourceVersion); err != nil {
		t.Fatalf("read accepted attempt update: %v", err)
	}
	if phase != "Running" || uid != "uid-owner" || resourceVersion != "rv-3" {
		t.Fatalf("owner Job update = phase %q uid %q rv %q", phase, uid, resourceVersion)
	}

	if _, _, err := repo.UpdateAttemptPhase(ctx, tenantID, "oj-nightly-1", string(jobrun.Canceled), "", ""); err != nil {
		t.Fatalf("trusted cancellation without observed UID rejected: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT phase FROM job_run_attempts_control_plane WHERE tenant_id=$1::char(26)`, tenantID).Scan(&phase); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(phase, string(jobrun.Canceled)) {
		t.Fatalf("cancellation phase = %q, want %q", phase, jobrun.Canceled)
	}
}

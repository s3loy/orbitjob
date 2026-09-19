//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	pq "github.com/lib/pq"
)

// The workflow run ledger's family contract, migration 0005.
// workflow_run_control_plane is history: the actor column is the ledger's
// answer to "who triggered this", the occurrence key deduplicates firings, and
// job_run_control_plane.workflow_run_id is the only thing that makes an
// ordinary run a workflow step. The step foreign key is RESTRICT on purpose:
// the retainer's atomic prune deletes steps first and the workflow row only
// after its last step is gone, and any other order fails loudly here instead of
// orphaning steps. These tests pin that behavior on real rows.

func seedWorkflowRevision(t *testing.T, ctx context.Context, owner *sql.DB, tenantID, sourceUID string) int64 {
	t.Helper()
	var id int64
	if err := owner.QueryRowContext(ctx, `
		INSERT INTO job_definition_revisions
		  (tenant_id, source_mode, source_uid, source_namespace, source_name,
		   generation, spec_hash, normalized_spec, actor)
		VALUES ($1::char(26), 'k8s', $2, 'ns', 'name', 1, repeat('a', 64), '{}', 'operator')
		RETURNING id
	`, tenantID, sourceUID).Scan(&id); err != nil {
		t.Fatalf("seed revision for %s: %v", tenantID, err)
	}
	return id
}

func seedWorkflowRun(t *testing.T, ctx context.Context, owner *sql.DB, tenantID string, revisionID int64, sourceUID, occurrenceKey, actor, phase string) int64 {
	t.Helper()
	var id int64
	if err := owner.QueryRowContext(ctx, `
		INSERT INTO workflow_run_control_plane
		  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
		VALUES ($1::char(26), $2, $3, $4, 'Manual', $5, $6)
		RETURNING id
	`, tenantID, sourceUID, revisionID, occurrenceKey, actor, phase).Scan(&id); err != nil {
		t.Fatalf("seed workflow run: %v", err)
	}
	return id
}

// seedWorkflowStep stamps an ordinary run as a workflow step. The step's
// source_uid is the referenced definition's, not the workflow's, so the two
// never collide on the run ledger's uniqueness pair.
func seedWorkflowStep(t *testing.T, ctx context.Context, owner *sql.DB, tenantID string, revisionID, workflowRunID int64, sourceUID, occurrenceKey string) int64 {
	t.Helper()
	var id int64
	if err := owner.QueryRowContext(ctx, `
		INSERT INTO job_run_control_plane
		  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase, workflow_run_id)
		VALUES ($1::char(26), $2, $3, $4, 'Workflow', 'workflow-walker', 'Pending', $5)
		RETURNING id
	`, tenantID, sourceUID, revisionID, occurrenceKey, workflowRunID).Scan(&id); err != nil {
		t.Fatalf("seed workflow step: %v", err)
	}
	return id
}

func pqCode(t *testing.T, err error) string {
	t.Helper()
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		t.Fatalf("error is not a PostgreSQL error: %v", err)
	}
	return string(pqErr.Code)
}

// TestWorkflowRunActorIsRequiredAndNonEmpty: "who triggered this" is the
// ledger's product claim, so the column cannot be omitted or blanked.
func TestWorkflowRunActorIsRequiredAndNonEmpty(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	db := ownerDB(t)
	seedTenantIDs(t, ctx, db, "alpha")
	revisionID := seedWorkflowRevision(t, ctx, db, "alpha", "wf-actor")

	// Positive control: a run with a real actor is accepted.
	seedWorkflowRun(t, ctx, db, "alpha", revisionID,
		"wf-actor", strings.Repeat("a", 64), "workflow-walker", "Pending")

	// An omitted actor is a NOT NULL violation: the column has no DEFAULT, so
	// the row cannot even be formed without naming who triggered it.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO workflow_run_control_plane
		  (tenant_id, source_uid, revision_id, occurrence_key, trigger, phase)
		VALUES ('alpha', 'wf-actor', $1, $2, 'Manual', 'Pending')
	`, revisionID, strings.Repeat("w", 64)); err != nil {
		if got := pqCode(t, err); got != "23502" {
			t.Errorf("omitted actor failed with %s, want not_null_violation 23502", got)
		}
	} else {
		t.Error("workflow_run_control_plane accepted a row with the actor omitted")
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO workflow_run_control_plane
		  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
		VALUES ('alpha', 'wf-actor', $1, $2, 'Manual', '', 'Pending')
	`, revisionID, strings.Repeat("z", 64)); err != nil {
		if got := pqCode(t, err); got != "23514" {
			t.Errorf("empty actor failed with %s, want check_violation 23514 (chk_workflow_run_actor_non_empty)", got)
		}
	} else {
		t.Error("workflow_run_control_plane accepted an empty actor")
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO workflow_run_control_plane
		  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
		VALUES ('alpha', 'wf-actor', $1, $2, 'Manual', '   ', 'Pending')
	`, revisionID, strings.Repeat("y", 64)); err != nil {
		if got := pqCode(t, err); got != "23514" {
			t.Errorf("whitespace actor failed with %s, want check_violation 23514 (chk_workflow_run_actor_non_empty)", got)
		}
	} else {
		t.Error("workflow_run_control_plane accepted a whitespace-only actor")
	}
}

// TestWorkflowRunOccurrenceKeyDedups: one workflow run per schedule occurrence
// or manual trigger. The pair (source_uid, occurrence_key) is the deduplication
// key; a replayed firing must be refused as a conflict, while a new occurrence
// of the same workflow and the same occurrence key of a different workflow both
// pass.
func TestWorkflowRunOccurrenceKeyDedups(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	db := ownerDB(t)
	seedTenantIDs(t, ctx, db, "alpha")
	revisionID := seedWorkflowRevision(t, ctx, db, "alpha", "wf-dedup")

	key := strings.Repeat("d", 64)
	seedWorkflowRun(t, ctx, db, "alpha", revisionID,
		"wf-dedup", key, "workflow-walker", "Pending")

	_, err := db.ExecContext(ctx, `
		INSERT INTO workflow_run_control_plane
		  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
		VALUES ('alpha', 'wf-dedup', $1, $2, 'Manual', 'workflow-walker', 'Pending')
	`, revisionID, key)
	if err == nil {
		t.Fatal("the same occurrence fired twice and both rows were stored")
	}
	if got := pqCode(t, err); got != "23505" {
		t.Errorf("replayed occurrence failed with %s, want unique_violation 23505", got)
	}

	// A genuinely new occurrence of the same workflow is a new row.
	seedWorkflowRun(t, ctx, db, "alpha", revisionID,
		"wf-dedup", strings.Repeat("e", 64), "workflow-walker", "Pending")

	// The same key material under a different workflow is a different run.
	seedWorkflowRun(t, ctx, db, "alpha", revisionID,
		"wf-other", key, "workflow-walker", "Pending")

	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM workflow_run_control_plane`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("workflow runs stored = %d, want 3", n)
	}
}

// TestWorkflowRunDeleteWaitsForItsSteps is the atomicity substrate the
// retainer's prune relies on: a workflow row that still has steps cannot be
// deleted (RESTRICT), the same delete succeeds once the steps are gone. Steps
// first, then the row, in one pass -- an out-of-order delete fails here.
func TestWorkflowRunDeleteWaitsForItsSteps(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	db := ownerDB(t)
	seedTenantIDs(t, ctx, db, "alpha")
	revisionID := seedWorkflowRevision(t, ctx, db, "alpha", "wf-prune")
	workflowID := seedWorkflowRun(t, ctx, db, "alpha", revisionID,
		"wf-prune", strings.Repeat("p", 64), "workflow-walker", "Succeeded")
	stepID := seedWorkflowStep(t, ctx, db, "alpha", revisionID, workflowID,
		"wf-prune-step-def", strings.Repeat("s", 64))

	_, err := db.ExecContext(ctx,
		`DELETE FROM workflow_run_control_plane WHERE id = $1`, workflowID)
	if err == nil {
		t.Fatal("a workflow row with steps was deleted; the step foreign key no longer RESTRICTs")
	}
	if got := pqCode(t, err); got != "23503" {
		t.Errorf("workflow delete with steps failed with %s, want foreign_key_violation 23503", got)
	}
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM workflow_run_control_plane WHERE id = $1`, workflowID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("workflow rows after the failed delete = %d, want 1", n)
	}

	if _, err := db.ExecContext(ctx,
		`DELETE FROM job_run_control_plane WHERE id = $1`, stepID); err != nil {
		t.Fatalf("delete the step: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`DELETE FROM workflow_run_control_plane WHERE id = $1`, workflowID); err != nil {
		t.Fatalf("workflow row still undeletable after its last step was purged: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM workflow_run_control_plane WHERE id = $1`, workflowID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("workflow rows after the prune = %d, want 0", n)
	}
}

// TestWorkflowRunIdIsNullableAndIndexed pins the step-grouping column's shape:
// a plain run carries no workflow_run_id (the retainer's per-definition sweep
// reads exactly this NULL side), and the partial index behind the walker's
// step lookups exists and is usable.
func TestWorkflowRunIdIsNullableAndIndexed(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	db := ownerDB(t)
	seedTenantIDs(t, ctx, db, "alpha")
	revisionID := seedWorkflowRevision(t, ctx, db, "alpha", "wf-plain")

	if _, err := db.ExecContext(ctx, `
		INSERT INTO job_run_control_plane
		  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
		VALUES ('alpha', 'wf-plain', $1, $2, 'manual', 'scheduler@cluster', 'Pending')
	`, revisionID, strings.Repeat("n", 64)); err != nil {
		t.Fatalf("a plain run without workflow_run_id was rejected: %v", err)
	}

	var steps int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM job_run_control_plane WHERE workflow_run_id IS NULL`).Scan(&steps); err != nil {
		t.Fatal(err)
	}
	if steps != 1 {
		t.Errorf("runs with a NULL workflow_run_id = %d, want 1; the column is not nullable", steps)
	}

	var columnList string
	if err := db.QueryRowContext(ctx, `
		SELECT coalesce(string_agg(a.attname, ',' ORDER BY a.attnum), '')
		FROM pg_index i
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_class tc ON tc.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = tc.relnamespace
		JOIN pg_attribute a ON a.attrelid = tc.oid AND a.attnum = ANY (i.indkey)
		WHERE n.nspname = 'public'
		  AND ic.relname = 'idx_job_run_workflow'
		  AND tc.relname = 'job_run_control_plane'
	`).Scan(&columnList); err == sql.ErrNoRows {
		t.Fatal("idx_job_run_workflow does not exist on job_run_control_plane")
	} else if err != nil {
		t.Fatalf("read index columns: %v", err)
	}
	columns := strings.Split(columnList, ",")
	if columnList == "" || len(columns) != 1 || columns[0] != "workflow_run_id" {
		t.Errorf("idx_job_run_workflow indexes %q, want workflow_run_id", columnList)
	}

	var valid bool
	if err := db.QueryRowContext(ctx, `
		SELECT i.indisvalid AND i.indisready
		FROM pg_index i
		JOIN pg_class ic ON ic.oid = i.indexrelid
		WHERE ic.relname = 'idx_job_run_workflow'
	`).Scan(&valid); err != nil {
		t.Fatalf("read index validity: %v", err)
	}
	if !valid {
		t.Error("idx_job_run_workflow exists but is not valid and ready")
	}
}

// TestWorkflowStepsAreTenantVisible: both ends of the step join carry RLS, so a
// tenant walking its workflow sees its own steps and nothing of anyone else's,
// and with no tenant context neither side of the join is visible.
func TestWorkflowStepsAreTenantVisible(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	owner := ownerDB(t)
	seedTenantIDs(t, ctx, owner, "alpha", "beta")
	alphaRev := seedWorkflowRevision(t, ctx, owner, "alpha", "wf-vis-a")
	betaRev := seedWorkflowRevision(t, ctx, owner, "beta", "wf-vis-b")
	alphaWf := seedWorkflowRun(t, ctx, owner, "alpha", alphaRev,
		"wf-vis-a", strings.Repeat("a", 64), "workflow-walker", "Running")
	betaWf := seedWorkflowRun(t, ctx, owner, "beta", betaRev,
		"wf-vis-b", strings.Repeat("b", 64), "workflow-walker", "Running")
	seedWorkflowStep(t, ctx, owner, "alpha", alphaRev, alphaWf,
		"wf-vis-a-def", strings.Repeat("x", 64))
	seedWorkflowStep(t, ctx, owner, "beta", betaRev, betaWf,
		"wf-vis-b-def", strings.Repeat("x", 64))

	withRoleConn(t, "orbitjob_admin", func(ctx context.Context, conn *sql.Conn) {
		if _, err := conn.ExecContext(ctx, `SET app.tenant_id = 'alpha'`); err != nil {
			t.Fatalf("set tenant context: %v", err)
		}
		if n := countAs(t, ctx, conn, `SELECT count(*) FROM workflow_run_control_plane`); n != 1 {
			t.Fatalf("alpha sees %d workflow row(s), want 1", n)
		}
		if n := countAs(t, ctx, conn, `SELECT count(*) FROM job_run_control_plane`); n != 1 {
			t.Fatalf("alpha sees %d run row(s), want 1", n)
		}
		if n := countAs(t, ctx, conn,
			`SELECT count(*) FROM workflow_run_control_plane WHERE tenant_id = 'beta'`); n != 0 {
			t.Errorf("alpha reads %d of beta's workflow rows, want 0", n)
		}
		if n := countAs(t, ctx, conn,
			`SELECT count(*) FROM job_run_control_plane WHERE tenant_id = 'beta'`); n != 0 {
			t.Errorf("alpha reads %d of beta's steps, want 0", n)
		}
		if n := countAs(t, ctx, conn, `
			SELECT count(*)
			FROM job_run_control_plane r
			JOIN workflow_run_control_plane w ON w.id = r.workflow_run_id
		`); n != 1 {
			t.Errorf("the tenant-scoped step join returns %d row(s), want alpha's single step", n)
		}

		if _, err := conn.ExecContext(ctx, `RESET app.tenant_id`); err != nil {
			t.Fatalf("reset tenant context: %v", err)
		}
		if n := countAs(t, ctx, conn, `SELECT count(*) FROM workflow_run_control_plane`); n != 0 {
			t.Errorf("no tenant context sees %d workflow row(s), want 0; RLS did not fail closed", n)
		}
		if n := countAs(t, ctx, conn, `
			SELECT count(*)
			FROM job_run_control_plane r
			JOIN workflow_run_control_plane w ON w.id = r.workflow_run_id
		`); n != 0 {
			t.Errorf("no tenant context sees %d step(s) through the join, want 0", n)
		}
	})
}

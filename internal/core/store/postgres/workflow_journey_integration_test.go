//go:build integration

package postgres

import (
	"errors"
	"testing"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
	"orbitjob/internal/domain/resource"
)

func TestWorkflowHistoricalRevisionAndFailureDecisionsStayTenantScoped(t *testing.T) {
	db := OpenStoreDB(t)
	ctx := ctxStore(t)
	const tenant = "01J00000000000000000000001"
	const foreign = "01J00000000000000000000002"
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants(id,slug,name)VALUES($1,'journey-home','Home'),($2,'journey-foreign','Foreign')`, tenant, foreign); err != nil {
		t.Fatal(err)
	}
	var rev int64
	if err := db.QueryRowContext(ctx, `INSERT INTO job_definition_revisions(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,is_active,actor)VALUES($1,'workflow','workflow-old','own','pipeline',1,'hash','{"tasks":[{"name":"one","jobRef":{"name":"gone-job"}}]}',false,'test')RETURNING id`, tenant).Scan(&rev); err != nil {
		t.Fatal(err)
	}
	reader := NewWorkflowDefinitionRepository(openRoleDB(t, db, "orbitjob_admin"))
	old, err := reader.WorkflowRevision(ctx, tenant, rev)
	if err != nil || old.SourceUID != "workflow-old" || len(old.Spec.Tasks) != 1 {
		t.Fatalf("inactive history %+v %v", old, err)
	}
	for _, call := range []func() error{
		func() error { _, err := reader.ActiveWorkflowDefinition(ctx, tenant, rev); return err },
		func() error { _, err := reader.WorkflowRevision(ctx, foreign, rev); return err },
	} {
		var missing *resource.NotFoundError
		if err := call(); !errors.As(err, &missing) {
			t.Fatalf("invalid visibility: %v", err)
		}
	}
	runtimeDB := openRoleDB(t, db, "orbitjob_runtime")
	ledger := NewControlPlaneRepository(runtimeDB)
	// Historical payloads stay usable while a newer revision is the source's
	// active admission policy. New work cannot bypass a fully inactive source.
	if _, err := ledger.ApplyRevisionForTenant(ctx, tenant, revision.Revision{
		Identity:   revision.Identity{SourceMode: workflow.SourceModeWorkflow, SourceUID: "workflow-old", Namespace: "own", Name: "pipeline"},
		Generation: 2, SpecHash: "active-hash", NormalizedSpec: `{"concurrencyPolicy":"Allow","tasks":[{"name":"one","jobRef":{"name":"gone-job"}}]}`, Actor: "test",
	}); err != nil {
		t.Fatal(err)
	}
	runtime := NewWorkflowRunRepository(runtimeDB)
	run, _, err := runtime.CreateRunForTenant(ctx, tenant, workflow.Run{SourceUID: "workflow-old", RevisionID: rev, OccurrenceKey: "missing-reference", Trigger: jobrun.Manual, Actor: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.DeactivateSource(ctx, tenant, workflow.SourceModeWorkflow, "workflow-old"); err != nil {
		t.Fatal(err)
	}
	decisions := map[string]workflow.TaskDecision{"one": {Skipped: true, Reason: workflow.ReasonMissingJob}}
	phase, done := workflow.DerivePhase([]string{"one"}, nil, decisions)
	if !done || phase != workflow.PhaseFailed {
		t.Fatal("missing reference was not failure")
	}
	if _, err := runtime.UpdatePhase(ctx, tenant, run.ID, phase, decisions); err != nil {
		t.Fatal(err)
	}
	stored, found, err := runtime.RunByID(ctx, tenant, run.ID)
	if err != nil || !found || stored.Phase != workflow.PhaseFailed || stored.TaskDecisions["one"].Reason != workflow.ReasonMissingJob {
		t.Fatalf("failure not durable: %+v %v", stored, err)
	}
	if _, found, err := runtime.RunByID(ctx, foreign, run.ID); err != nil || found {
		t.Fatalf("foreign workflow outcome found=%v err=%v", found, err)
	}
}

//go:build integration

package postgres

import (
	"testing"

	"orbitjob/internal/platform/scan"
)

func TestWorkflowPruneDrainsStepRowsBeforeWritingAudit(t *testing.T) {
	db := OpenStoreDB(t)
	ctx := ctxStore(t)
	tenantID := scan.GenerateID()
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants(id,slug,name) VALUES($1::char(26),$1::text,$1::text)`, tenantID); err != nil {
		t.Fatal(err)
	}
	var workflowID int64
	if err := db.QueryRowContext(ctx, `
WITH revision AS (
 INSERT INTO job_definition_revisions(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,actor)
 VALUES($1,'workflow','prune-source','ns','prune',1,repeat('a',64),'{}','operator') RETURNING id
)
INSERT INTO workflow_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase)
SELECT $1,'prune-source',id,repeat('b',64),'Manual','operator','Succeeded' FROM revision RETURNING id`, tenantID).Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase,workflow_run_id)
SELECT $1,'step-'||n,w.revision_id,repeat(n::text,64),'Workflow','operator','Succeeded',$2
FROM workflow_run_control_plane w CROSS JOIN generate_series(1,2) n WHERE w.id=$2`, tenantID, workflowID); err != nil {
		t.Fatal(err)
	}
	if deleted, steps, err := NewWorkflowRunRepository(db).PruneRun(ctx, tenantID, workflowID); err != nil || deleted || steps != 0 {
		t.Fatalf("unprojected workflow pruned: deleted=%v steps=%d error=%v", deleted, steps, err)
	}
	ledger := NewControlPlaneRepository(db)
	pending, err := ledger.TerminalOutcomesPending(ctx, tenantID, 10)
	if err != nil || len(pending) != 2 {
		t.Fatalf("step outcomes=%v error=%v", pending, err)
	}
	for _, step := range pending {
		if err := ledger.MarkOutcomeRecorded(ctx, tenantID, step.RunID); err != nil {
			t.Fatal(err)
		}
	}
	if deleted, _, err := NewWorkflowRunRepository(db).PruneRun(ctx, tenantID, workflowID); err != nil || deleted {
		t.Fatalf("new workflow lost archive registration grace: %v %v", deleted, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE workflow_run_control_plane SET updated_at=now()-interval '16 days' WHERE id=$1`, workflowID); err != nil {
		t.Fatal(err)
	}
	deleted, steps, err := NewWorkflowRunRepository(db).PruneRun(ctx, tenantID, workflowID)
	if err != nil || !deleted || steps != 2 {
		t.Fatalf("prune: deleted=%v steps=%d error=%v", deleted, steps, err)
	}
	var audits int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE tenant_id=$1`, tenantID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 3 {
		t.Fatalf("prune wrote %d audit rows, want two steps and one workflow", audits)
	}
}

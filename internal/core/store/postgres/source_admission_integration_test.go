//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/workflow"
	store "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/postgrestest"
	"orbitjob/internal/platform/scan"
)

func seedAdmissionWorkflow(t *testing.T) workflowFenceFixture {
	t.Helper()
	f := workflowFenceFixture{owner: postgrestest.Open(t), tenant: scan.GenerateID(), source: scan.GenerateID()}
	if _, err := f.owner.ExecContext(t.Context(), `INSERT INTO tenants(id,slug,name) VALUES($1,$2,'source admission')`, f.tenant, "admission-"+f.tenant); err != nil {
		t.Fatal(err)
	}
	parentSource := scan.GenerateID()
	var parentRevision int64
	for _, item := range []struct {
		mode, source, name string
		id                 *int64
	}{
		{"workflow", parentSource, "workflow", &parentRevision},
		{"kubernetes", f.source, "task", &f.revision},
	} {
		if err := f.owner.QueryRowContext(t.Context(), `INSERT INTO job_definition_revisions
 (tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,actor,is_active)
 VALUES($1,$2,$3,'admission-ns',$4,1,repeat('a',64),'{"concurrencyPolicy":"Allow","jobTemplate":{"image":"busybox"}}','review',true) RETURNING id`,
			f.tenant, item.mode, item.source, item.name).Scan(item.id); err != nil {
			t.Fatal(err)
		}
	}
	f.application, _ = admissionConnection(t, f)
	var err error
	f.parent, _, err = store.NewWorkflowRunRepository(f.application).CreateRunForTenant(t.Context(), f.tenant, workflow.Run{
		SourceUID: parentSource, RevisionID: parentRevision, OccurrenceKey: strings.Repeat("b", 64), Trigger: jobrun.Manual, Actor: "review", Phase: workflow.PhasePending,
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Use two physical sessions with a hostile server default. The repository must
// explicitly select READ COMMITTED rather than inherit Repeatable Read.
func admissionConnection(t *testing.T, f workflowFenceFixture) (*sql.DB, int) {
	t.Helper()
	var path string
	if err := f.owner.QueryRowContext(t.Context(), "SHOW search_path").Scan(&path); err != nil {
		t.Fatal(err)
	}
	dsn, err := url.Parse(postgrestest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	query := dsn.Query()
	query.Set("search_path", path)
	query.Set("options", strings.TrimSpace(query.Get("options")+" -c role=orbitjob_runtime"))
	query.Set("default_transaction_isolation", "repeatable read")
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("postgres", dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	var pid int
	var isolation, role string
	if err := db.QueryRowContext(t.Context(), "SELECT pg_backend_pid(), current_setting('default_transaction_isolation'), current_user").Scan(&pid, &isolation, &role); err != nil {
		t.Fatal(err)
	}
	if isolation != "repeatable read" || role != "orbitjob_runtime" {
		t.Fatalf("session isolation=%q role=%q", isolation, role)
	}
	return db, pid
}

type admissionResult struct {
	id      int64
	created bool
	err     error
}

func raceSourceAdmission(t *testing.T, f workflowFenceFixture, mode, source string, calls [2]func(*sql.DB) admissionResult) []admissionResult {
	t.Helper()
	first, pid1 := admissionConnection(t, f)
	second, pid2 := admissionConnection(t, f)
	if pid1 == pid2 {
		t.Fatal("concurrency test reused one physical database session")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	conn, err := f.owner.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	key := f.tenant + "/" + mode + "/" + source
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock(hashtextextended($1,0))", key); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock(hashtextextended($1,0))", key)
	}()
	results := make(chan admissionResult, 2)
	go func() { results <- calls[0](first) }()
	go func() { results <- calls[1](second) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting int
		if err := f.owner.QueryRowContext(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted AND pid=ANY($1)`, pq.Array([]int{pid1, pid2})).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("both independent contenders did not reach the source lock")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_unlock(hashtextextended($1,0))", key); err != nil {
		t.Fatal(err)
	}
	out := make([]admissionResult, 0, 2)
	for range 2 {
		select {
		case result := <-results:
			out = append(out, result)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	return out
}

func requireOneAdmitted(t *testing.T, results []admissionResult) {
	t.Helper()
	created, blocked := 0, 0
	for _, result := range results {
		switch {
		case result.err == nil && result.created && result.id > 0:
			created++
		case errors.Is(result.err, jobrun.ErrAdmissionBlocked) && !result.created && result.id == 0:
			blocked++
		default:
			t.Fatalf("unexpected admission result: %+v", result)
		}
	}
	if created != 1 || blocked != 1 {
		t.Fatalf("created=%d blocked=%d", created, blocked)
	}
}

func TestSourceAdmissionWorkflowRootsUseOneSlotAcrossConnections(t *testing.T) {
	f := seedAdmissionWorkflow(t)
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE workflow_run_control_plane SET phase='Canceled' WHERE id=$1`, f.parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_definition_revisions SET normalized_spec='{"concurrencyPolicy":"Forbid"}' WHERE id=$1`, f.parent.RevisionID); err != nil {
		t.Fatal(err)
	}
	calls := [2]func(*sql.DB) admissionResult{}
	for i := range calls {
		calls[i] = func(db *sql.DB) admissionResult {
			run := f.parent
			run.ID = 0
			run.OccurrenceKey = fmt.Sprintf("%064x", i+1)
			run.Phase = workflow.PhasePending
			row, created, err := store.NewWorkflowRunRepository(db).CreateRunForTenant(t.Context(), f.tenant, run)
			return admissionResult{row.ID, created, err}
		}
	}
	results := raceSourceAdmission(t, f, "workflow", f.parent.SourceUID, calls)
	requireOneAdmitted(t, results)
	var open, audits int
	if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM workflow_run_control_plane WHERE source_uid=$1 AND phase='Pending'`, f.parent.SourceUID).Scan(&open); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_events WHERE event_type='workflow_run.created' AND resource_id=ANY($1)`, pq.Array([]string{fmt.Sprint(results[0].id), fmt.Sprint(results[1].id)})).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if open != 1 || audits != 1 {
		t.Fatalf("open=%d creation audits=%d", open, audits)
	}
}

func TestSourceAdmissionWorkflowStepAndManualShareTaskSlot(t *testing.T) {
	f := seedAdmissionWorkflow(t)
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_definition_revisions SET normalized_spec='{"concurrencyPolicy":"Forbid"}' WHERE id=$1`, f.revision); err != nil {
		t.Fatal(err)
	}
	calls := [2]func(*sql.DB) admissionResult{
		func(db *sql.DB) admissionResult {
			row, created, err := store.NewWorkflowRunRepository(db).CreateStepForTenant(t.Context(), f.tenant, f.parent.ID, workflow.StepRun{SourceUID: f.source, RevisionID: f.revision, OccurrenceKey: strings.Repeat("c", 64), Trigger: jobrun.Workflow}, 1, "review")
			return admissionResult{row.ID, created, err}
		},
		func(db *sql.DB) admissionResult {
			row, created, err := store.NewControlPlaneRepository(db).CreateOccurrenceForTenant(t.Context(), f.tenant, jobrun.JobRun{SourceUID: f.source, RevisionID: fmt.Sprint(f.revision), OccurrenceKey: strings.Repeat("d", 64), Trigger: jobrun.Manual}, 1, "review")
			return admissionResult{row.ID, created, err}
		},
	}
	requireOneAdmitted(t, raceSourceAdmission(t, f, "kubernetes", f.source, calls))
}

func TestSourceAdmissionCanceledUnadmittedWorkflowPersistsWhileInactive(t *testing.T) {
	f := seedAdmissionWorkflow(t)
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_definition_revisions SET normalized_spec='{"concurrencyPolicy":"Forbid"}' WHERE id=$1`, f.parent.RevisionID); err != nil {
		t.Fatal(err)
	}
	repo := store.NewWorkflowRunRepository(f.application)
	run := f.parent
	run.ID = 0
	run.OccurrenceKey = strings.Repeat("d", 64)
	if _, created, err := repo.CreateRunForTenant(t.Context(), f.tenant, run); !errors.Is(err, jobrun.ErrAdmissionBlocked) || created {
		t.Fatalf("busy workflow admitted: created=%t err=%v", created, err)
	}
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_definition_revisions SET is_active=false WHERE id=$1`, run.RevisionID); err != nil {
		t.Fatal(err)
	}
	row, created, err := repo.CancelUnadmittedRunForTenant(t.Context(), f.tenant, run)
	if err != nil || !created || row.Phase != workflow.PhaseCanceled {
		t.Fatalf("cancel row=%+v created=%t err=%v", row, created, err)
	}
	for _, cancelRequest := range []bool{false, true} {
		var replay workflow.Run
		if cancelRequest {
			replay, created, err = repo.CancelUnadmittedRunForTenant(t.Context(), f.tenant, run)
		} else {
			replay, created, err = repo.CreateRunForTenant(t.Context(), f.tenant, run)
		}
		if err != nil || created || replay.ID != row.ID || replay.Phase != workflow.PhaseCanceled {
			t.Fatalf("canceled replay=%+v created=%t err=%v", replay, created, err)
		}
	}
	var audits, steps int
	if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_events WHERE event_type='workflow_run.created' AND resource_id=$1`, fmt.Sprint(row.ID)).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM job_run_control_plane WHERE workflow_run_id=$1`, row.ID).Scan(&steps); err != nil {
		t.Fatal(err)
	}
	if audits != 1 || steps != 0 {
		t.Fatalf("cancel audits=%d steps=%d", audits, steps)
	}
}

func TestSourceAdmissionStepReplayPrecedesParentAndActivePolicy(t *testing.T) {
	f := seedAdmissionWorkflow(t)
	step := f.step(t)
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE workflow_run_control_plane SET phase='ExecutionUnknown' WHERE id=$1`, f.parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_definition_revisions SET is_active=false WHERE id=$1`, f.revision); err != nil {
		t.Fatal(err)
	}
	repo := store.NewWorkflowRunRepository(f.application)
	request := workflow.StepRun{SourceUID: f.source, RevisionID: f.revision, OccurrenceKey: step.OccurrenceKey, Trigger: jobrun.Workflow}
	row, created, err := repo.CreateStepForTenant(t.Context(), f.tenant, f.parent.ID, request, 1, "review")
	if err != nil || created || row.ID != step.ID {
		t.Fatalf("existing step lost during isolation: row=%+v created=%t err=%v", row, created, err)
	}
	request.OccurrenceKey = strings.Repeat("e", 64)
	if _, created, err := repo.CreateStepForTenant(t.Context(), f.tenant, f.parent.ID, request, 1, "review"); created || !errors.Is(err, store.ErrWorkflowBlocked) {
		t.Fatalf("unknown parent created new step: created=%t err=%v", created, err)
	}
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE workflow_run_control_plane SET phase='Canceled' WHERE id=$1`, f.parent.ID); err != nil {
		t.Fatal(err)
	}
	request.OccurrenceKey = step.OccurrenceKey
	if row, created, err := repo.CreateStepForTenant(t.Context(), f.tenant, f.parent.ID, request, 1, "review"); err != nil || created || row.ID != step.ID {
		t.Fatalf("stopped existing replay row=%+v created=%t err=%v", row, created, err)
	}
}

func TestSourceAdmissionWorkflowRollupGuardsUnknownAndOpenChildren(t *testing.T) {
	f := seedAdmissionWorkflow(t)
	step := f.step(t)
	repo := store.NewWorkflowRunRepository(f.application)
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET phase='ExecutionUnknown' WHERE id=$1`, step.ID); err != nil {
		t.Fatal(err)
	}
	if changed, err := repo.UpdatePhase(t.Context(), f.tenant, f.parent.ID, workflow.PhaseExecutionUnknown, nil); err != nil || !changed {
		t.Fatalf("unknown rollup changed=%t err=%v", changed, err)
	}
	for _, phase := range []workflow.Phase{workflow.PhaseRunning, workflow.PhaseSucceeded, workflow.PhaseFailed, workflow.PhaseCanceled} {
		if changed, err := repo.UpdatePhase(t.Context(), f.tenant, f.parent.ID, phase, nil); err != nil || changed {
			t.Fatalf("unknown child released parent: phase=%s changed=%t err=%v", phase, changed, err)
		}
	}
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET phase='Running' WHERE id=$1`, step.ID); err != nil {
		t.Fatal(err)
	}
	if changed, err := repo.UpdatePhase(t.Context(), f.tenant, f.parent.ID, workflow.PhaseRunning, nil); err != nil || !changed {
		t.Fatalf("recovered parent changed=%t err=%v", changed, err)
	}
	if changed, err := repo.UpdatePhase(t.Context(), f.tenant, f.parent.ID, workflow.PhaseSucceeded, nil); err != nil || changed {
		t.Fatalf("open child terminalized parent changed=%t err=%v", changed, err)
	}
	if changed, err := repo.UpdatePhase(t.Context(), f.tenant, f.parent.ID, workflow.PhaseCancelRequested, nil); err != nil || !changed {
		t.Fatalf("cancel changed=%t err=%v", changed, err)
	}
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET phase='Succeeded' WHERE id=$1`, step.ID); err != nil {
		t.Fatal(err)
	}
	if changed, err := repo.UpdatePhase(t.Context(), f.tenant, f.parent.ID, workflow.PhaseSucceeded, nil); err != nil || changed {
		t.Fatalf("ordinary terminal overwrote committed cancel changed=%t err=%v", changed, err)
	}
	if changed, err := repo.UpdatePhase(t.Context(), f.tenant, f.parent.ID, workflow.PhaseCanceled, nil); err != nil || !changed {
		t.Fatalf("confirmed canceled parent changed=%t err=%v", changed, err)
	}
}

func TestSourceAdmissionWorkflowRollupSeesChildAfterParentLockWait(t *testing.T) {
	f := seedAdmissionWorkflow(t)
	step := f.step(t)
	db, pid := admissionConnection(t, f)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tx, err := f.owner.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_run_control_plane SET phase='ExecutionUnknown' WHERE id=$1`, f.parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE job_run_control_plane SET phase='ExecutionUnknown' WHERE id=$1`, step.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan admissionResult, 1)
	go func() {
		changed, err := store.NewWorkflowRunRepository(db).UpdatePhase(ctx, f.tenant, f.parent.ID, workflow.PhaseRunning, nil)
		done <- admissionResult{created: changed, err: err}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting bool
		if err := f.owner.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("rollup did not wait for the held parent row")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.err != nil || result.created {
			t.Fatalf("stale rollup erased child unknown: %+v", result)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	row, found, err := store.NewWorkflowRunRepository(db).RunByID(ctx, f.tenant, f.parent.ID)
	if err != nil || !found || row.Phase != workflow.PhaseExecutionUnknown {
		t.Fatalf("isolated parent row=%+v found=%t err=%v", row, found, err)
	}
}

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/core/domain/workflow"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/platform/scan"
)

func standaloneControlRun(t *testing.T, f workflowSnapshotFixture, key int) jobrun.StoredRun {
	t.Helper()
	run, created, err := f.control.CreateOccurrenceForTenant(t.Context(), f.tenant, jobrun.JobRun{SourceUID: f.extract, RevisionID: strconv.FormatInt(f.extractRevision, 10), OccurrenceKey: fmt.Sprintf("%064x", key), Trigger: jobrun.Manual}, 1, "test")
	if err != nil || !created {
		t.Fatalf("run=%+v created=%t err=%v", run, created, err)
	}
	return run
}

func requireAdmissionReason(t *testing.T, err error, want string) {
	t.Helper()
	var blocked *jobrun.AdmissionBlockedError
	if !errors.As(err, &blocked) || blocked.Reason != want {
		t.Fatalf("error=%v want admission reason %q", err, want)
	}
}

func TestSourceControlsSeparateScheduleExecutionAndCAS(t *testing.T) {
	f := newWorkflowSnapshotFixture(t)
	controls := NewSourceControlRepository(openRoleDB(t, f.owner, "orbitjob_admin"))
	state, found, err := controls.GetForTenant(t.Context(), f.tenant, "kubernetes", f.extract)
	if err != nil || found || state.Version != 0 {
		t.Fatalf("default=%+v found=%t err=%v", state, found, err)
	}
	state, err = controls.UpdateForTenant(t.Context(), f.tenant, "kubernetes", f.extract, 0, true, false, "test")
	if err != nil || state.Version != 1 || !state.SchedulePaused || state.ExecutionPaused {
		t.Fatalf("schedule control=%+v err=%v", state, err)
	}
	planned := time.Now().UTC().Add(time.Minute)
	_, _, err = f.control.CreateOccurrenceForTenant(t.Context(), f.tenant, jobrun.JobRun{SourceUID: f.extract, RevisionID: strconv.FormatInt(f.extractRevision, 10), OccurrenceKey: fmt.Sprintf("%064x", 1), Trigger: jobrun.Schedule, ScheduledFor: planned}, 1, "test")
	requireAdmissionReason(t, err, "schedule_paused")
	manual := standaloneControlRun(t, f, 2)
	state, err = controls.UpdateForTenant(t.Context(), f.tenant, "kubernetes", f.extract, state.Version, true, true, "test")
	if err != nil || state.Version != 2 {
		t.Fatalf("execution control=%+v err=%v", state, err)
	}
	_, err = controls.UpdateForTenant(t.Context(), f.tenant, "kubernetes", f.extract, 1, false, false, "test")
	var conflict *resource.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("stale version accepted: %v", err)
	}
	_, _, err = f.control.CreateOccurrenceForTenant(t.Context(), f.tenant, jobrun.JobRun{SourceUID: f.extract, RevisionID: strconv.FormatInt(f.extractRevision, 10), OccurrenceKey: fmt.Sprintf("%064x", 3), Trigger: jobrun.Manual}, 1, "test")
	requireAdmissionReason(t, err, "execution_paused")
	called := false
	err = f.control.CreateAttemptWithEnsure(t.Context(), f.tenant, manual.ID, jobrun.Attempt{Number: 1, KubernetesJobName: "paused-extract", OwnerRunUID: scan.GenerateID()}, func(context.Context) (jobrun.Attempt, error) { called = true; return jobrun.Attempt{}, nil })
	requireAdmissionReason(t, err, "execution_paused")
	if called {
		t.Fatal("paused Pending dispatched")
	}
	var attempts int
	if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM job_run_attempts_control_plane WHERE run_id=$1`, manual.ID).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("Pending advanced despite pause: attempts=%d err=%v", attempts, err)
	}
	// An already accepted occurrence remains an idempotent receipt during pause.
	prior, fresh, err := f.control.CreateOccurrenceForTenant(t.Context(), f.tenant, jobrun.JobRun{SourceUID: f.extract, RevisionID: strconv.FormatInt(f.extractRevision, 10), OccurrenceKey: fmt.Sprintf("%064x", 2), Trigger: jobrun.Manual}, 1, "test")
	if err != nil || fresh || prior.ID != manual.ID {
		t.Fatalf("paused replay=%+v fresh=%t err=%v", prior, fresh, err)
	}
	if _, found, err := controls.GetForTenant(t.Context(), scan.GenerateID(), "kubernetes", f.extract); err != nil || found {
		t.Fatalf("foreign controls found=%t err=%v", found, err)
	}
}

func TestPreparedPauseAndTenantSuspensionNeverEraseMayHaveStarted(t *testing.T) {
	f := newWorkflowSnapshotFixture(t)
	controls := NewSourceControlRepository(openRoleDB(t, f.owner, "orbitjob_admin"))
	run := standaloneControlRun(t, f, 1)
	intent := jobrun.Attempt{Number: 1, KubernetesJobName: "original-extract", OwnerRunUID: scan.GenerateID(), Phase: jobrun.CreatingAttempt}
	if err := f.control.createAttemptForTenant(t.Context(), f.tenant, run.ID, intent, jobrun.CreatingAttempt, true); err != nil {
		t.Fatal(err)
	}
	state, err := controls.UpdateForTenant(t.Context(), f.tenant, "kubernetes", f.extract, 0, false, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	called := 0
	send := func(context.Context) (jobrun.Attempt, error) {
		called++
		return jobrun.Attempt{}, errors.New("creation acknowledgement lost")
	}
	err = f.control.CreateAttemptWithEnsure(t.Context(), f.tenant, run.ID, intent, send)
	requireAdmissionReason(t, err, "execution_paused")
	current, found, err := f.control.CurrentExecution(t.Context(), f.tenant, run.ID)
	if err != nil || !found || current.DispatchStage != jobrun.DispatchPrepared || called != 0 {
		t.Fatalf("Prepared not frozen: %+v called=%d err=%v", current, called, err)
	}
	if _, err := controls.UpdateForTenant(t.Context(), f.tenant, "kubernetes", f.extract, state.Version, false, false, "test"); err != nil {
		t.Fatal(err)
	}
	if err := f.control.CreateAttemptWithEnsure(t.Context(), f.tenant, run.ID, intent, send); err == nil || called != 1 {
		t.Fatalf("send called=%d err=%v", called, err)
	}
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE tenants SET status='suspended' WHERE id=$1`, f.tenant); err != nil {
		t.Fatal(err)
	}
	state, _, err = controls.GetForTenant(t.Context(), f.tenant, "kubernetes", f.extract)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controls.UpdateForTenant(t.Context(), f.tenant, "kubernetes", f.extract, state.Version, false, true, "test"); err != nil {
		t.Fatal(err)
	}
	err = f.control.CreateAttemptWithEnsure(t.Context(), f.tenant, run.ID, intent, send)
	if !errors.Is(err, jobrun.ErrDispatchMayHaveStarted) || called != 1 {
		t.Fatalf("pause or tenant suspension rewrote uncertain dispatch: calls=%d err=%v", called, err)
	}
	current, found, err = f.control.CurrentExecution(t.Context(), f.tenant, run.ID)
	if err != nil || !found || current.DispatchStage != jobrun.DispatchMayHaveStarted {
		t.Fatalf("May obligation lost: %+v err=%v", current, err)
	}
}

func TestDeletedSnapshotStepsRemainSubjectToExplicitExecutionPause(t *testing.T) {
	f := newWorkflowSnapshotFixture(t)
	parent, _, err := f.store.CreateRunForTenant(t.Context(), f.tenant, f.run(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.control.DeactivateSource(t.Context(), f.tenant, "kubernetes", f.load); err != nil {
		t.Fatal(err)
	}
	controls := NewSourceControlRepository(openRoleDB(t, f.owner, "orbitjob_admin"))
	state, found, err := controls.GetForTenant(t.Context(), f.tenant, "kubernetes", f.load)
	if err != nil || !found || !state.Tombstoned {
		t.Fatalf("deleted=%+v found=%t err=%v", state, found, err)
	}
	state, err = controls.UpdateForTenant(t.Context(), f.tenant, "kubernetes", f.load, state.Version, false, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	step := workflow.StepRun{SourceUID: f.load, RevisionID: f.loadRevision, OccurrenceKey: workflow.StepOccurrenceKey(f.source, parent.OccurrenceKey, "load"), Trigger: jobrun.Workflow}
	_, _, err = f.store.CreateStepForTenant(t.Context(), f.tenant, parent.ID, step, 1, "test")
	requireAdmissionReason(t, err, "execution_paused")
	if _, err := controls.UpdateForTenant(t.Context(), f.tenant, "kubernetes", f.load, state.Version, false, false, "test"); err != nil {
		t.Fatal(err)
	}
	parentState, err := controls.UpdateForTenant(t.Context(), f.tenant, "workflow", f.source, 0, false, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = f.store.CreateStepForTenant(t.Context(), f.tenant, parent.ID, step, 1, "test")
	requireAdmissionReason(t, err, "execution_paused")
	if _, err := controls.UpdateForTenant(t.Context(), f.tenant, "workflow", f.source, parentState.Version, false, false, "test"); err != nil {
		t.Fatal(err)
	}
	if _, fresh, err := f.store.CreateStepForTenant(t.Context(), f.tenant, parent.ID, step, 1, "test"); err != nil || !fresh {
		t.Fatalf("resume lost accepted deleted source: fresh=%t err=%v", fresh, err)
	}
	// A stale source CR cannot reactivate the deleted UID after resume.
	rev, err := f.control.RevisionByID(t.Context(), f.tenant, f.loadRevision)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.control.ApplyRevisionForTenant(t.Context(), f.tenant, rev)
	requireAdmissionReason(t, err, "source_deleted")
}

func TestSuspendedTenantBlocksNewAdmissionAndPreparedMarker(t *testing.T) {
	f := newWorkflowSnapshotFixture(t)
	run := standaloneControlRun(t, f, 1)
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE tenants SET status='suspended' WHERE id=$1`, f.tenant); err != nil {
		t.Fatal(err)
	}
	_, _, err := f.store.CreateRunForTenant(t.Context(), f.tenant, f.run(2))
	requireAdmissionReason(t, err, "tenant_inactive")
	called := false
	err = f.control.CreateAttemptWithEnsure(t.Context(), f.tenant, run.ID, jobrun.Attempt{Number: 1, KubernetesJobName: "inactive-extract", OwnerRunUID: scan.GenerateID()}, func(context.Context) (jobrun.Attempt, error) { called = true; return jobrun.Attempt{}, nil })
	requireAdmissionReason(t, err, "tenant_inactive")
	if called {
		t.Fatal("suspended tenant dispatched")
	}
	var canAdmin, canRuntime bool
	if err := f.owner.QueryRowContext(t.Context(), `SELECT has_function_privilege('orbitjob_admin','execution_tenant_active()','EXECUTE'),has_function_privilege('orbitjob_runtime','execution_tenant_active()','EXECUTE')`).Scan(&canAdmin, &canRuntime); err != nil || canAdmin || !canRuntime {
		t.Fatalf("capability grants admin=%t runtime=%t err=%v", canAdmin, canRuntime, err)
	}
}

func TestScheduledAdmissionHonorsFirstActivationAndReplay(t *testing.T) {
	f := newWorkflowSnapshotFixture(t)
	var activated time.Time
	if err := f.owner.QueryRowContext(t.Context(), `SELECT activated_at FROM job_definition_revisions WHERE id=$1`, f.extractRevision).Scan(&activated); err != nil {
		t.Fatal(err)
	}
	rev, err := f.control.RevisionByID(t.Context(), f.tenant, f.extractRevision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.control.ApplyRevisionForTenant(t.Context(), f.tenant, rev); err != nil {
		t.Fatal(err)
	}
	var replayActivation time.Time
	if err := f.owner.QueryRowContext(t.Context(), `SELECT activated_at FROM job_definition_revisions WHERE id=$1`, f.extractRevision).Scan(&replayActivation); err != nil || !activated.Equal(replayActivation) {
		t.Fatalf("activation changed %s -> %s err=%v", activated, replayActivation, err)
	}
	run := jobrun.JobRun{SourceUID: f.extract, RevisionID: strconv.FormatInt(f.extractRevision, 10), OccurrenceKey: fmt.Sprintf("%064x", 1), Trigger: jobrun.Schedule, ScheduledFor: activated.Add(-time.Minute)}
	_, _, err = f.control.CreateOccurrenceForTenant(t.Context(), f.tenant, run, 1, "test")
	requireAdmissionReason(t, err, "before_revision_activation")
	run.ScheduledFor = activated.Add(time.Minute)
	accepted, created, err := f.control.CreateOccurrenceForTenant(t.Context(), f.tenant, run, 1, "test")
	if err != nil || !created {
		t.Fatalf("eligible occurrence created=%t err=%v", created, err)
	}
	if err := f.control.DeactivateSource(t.Context(), f.tenant, "kubernetes", f.extract); err != nil {
		t.Fatal(err)
	}
	replayed, fresh, err := f.control.CreateOccurrenceForTenant(t.Context(), f.tenant, run, 1, "test")
	if err != nil || fresh || replayed.ID != accepted.ID {
		t.Fatalf("old receipt gated by new controls: %+v fresh=%t err=%v", replayed, fresh, err)
	}
}

func waitControlSessionLock(t *testing.T, owner *sql.DB, pid int, done <-chan error) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		select {
		case err := <-done:
			t.Fatalf("session %d completed before required lock wait: %v", pid, err)
		default:
		}
		var waiting bool
		if err := owner.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("session %d never waited on lock", pid)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTenantSuspensionAndSourcePauseSerializeBeforeDispatch(t *testing.T) {
	f := newWorkflowSnapshotFixture(t)
	run := standaloneControlRun(t, f, 1)
	f.control.db.SetMaxOpenConns(1)
	var dispatchPID int
	if err := f.control.db.QueryRowContext(t.Context(), `SELECT pg_backend_pid()`).Scan(&dispatchPID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tenantTx, err := f.owner.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tenantTx.Rollback()
	if _, err := tenantTx.ExecContext(ctx, `UPDATE tenants SET status='suspended' WHERE id=$1`, f.tenant); err != nil {
		t.Fatal(err)
	}
	dispatched := make(chan error, 1)
	go func() {
		dispatched <- f.control.CreateAttemptWithEnsure(ctx, f.tenant, run.ID, jobrun.Attempt{Number: 1, KubernetesJobName: "tenant-suspension-race", OwnerRunUID: scan.GenerateID()}, func(context.Context) (jobrun.Attempt, error) {
			return jobrun.Attempt{}, errors.New("unexpected external dispatch")
		})
	}()
	waitControlSessionLock(t, f.owner, dispatchPID, dispatched)
	admin := openRoleDB(t, f.owner, "orbitjob_admin")
	admin.SetMaxOpenConns(1)
	var pausePID int
	if err := admin.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pausePID); err != nil {
		t.Fatal(err)
	}
	paused := make(chan error, 1)
	go func() {
		_, err := NewSourceControlRepository(admin).UpdateForTenant(ctx, f.tenant, "kubernetes", f.extract, 0, false, true, "test")
		paused <- err
	}()
	waitControlSessionLock(t, f.owner, pausePID, paused)
	if err := tenantTx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-dispatched:
		requireAdmissionReason(t, err, "tenant_inactive")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-paused:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var attempts int
	if err := f.owner.QueryRowContext(ctx, `SELECT count(*) FROM job_run_attempts_control_plane WHERE run_id=$1`, run.ID).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("suspension race reserved work: attempts=%d err=%v", attempts, err)
	}
}

func TestSourcePauseDoesNotReverseSLIConfigurationFreezeLocks(t *testing.T) {
	f := newWorkflowSnapshotFixture(t)
	run := standaloneControlRun(t, f, 1)
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET phase='Succeeded' WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	barrier, err := f.owner.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback()
	if _, err := barrier.ExecContext(ctx, `SELECT orbitjob_lock_sli_source($1,$2)`, f.tenant, f.extract); err != nil {
		t.Fatal(err)
	}
	admin := openRoleDB(t, f.owner, "orbitjob_admin")
	reader := openRoleDB(t, f.owner, "orbitjob_runtime")
	admin.SetMaxOpenConns(1)
	reader.SetMaxOpenConns(1)
	var createPID, freezePID int
	if err := admin.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&createPID); err != nil {
		t.Fatal(err)
	}
	if err := reader.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&freezePID); err != nil {
		t.Fatal(err)
	}
	created, frozen := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := NewSLIRepository(admin).Create(monitoringActorContext(ctx), f.tenant, sli.CreateSpec{Name: "extract-completion", SLIType: "availability", SourceType: "job_run", SourceConfig: map[string]any{"source_uid": f.extract}, Aggregation: "ratio", GoodEventCriteria: map[string]any{"status": "success"}})
		created <- err
	}()
	go func() {
		plan, err := NewSLIRepository(reader).EventConfigurationForOutcome(ctx, f.tenant, run.ID)
		if err == nil && len(plan.Configurations) != 0 {
			err = fmt.Errorf("SLI created after terminal event was applied retroactively")
		}
		frozen <- err
	}()
	waitControlSessionLock(t, f.owner, createPID, created)
	waitControlSessionLock(t, f.owner, freezePID, frozen)
	// These controls use the execution source lock, never the SLI event lock.
	pauseCtx, cancelPause := context.WithTimeout(ctx, time.Second)
	defer cancelPause()
	if _, err := NewSourceControlRepository(openRoleDB(t, f.owner, "orbitjob_admin")).UpdateForTenant(pauseCtx, f.tenant, "kubernetes", f.extract, 0, false, true, "test"); err != nil {
		t.Fatalf("pause acquired an SLI lock in reverse order: %v", err)
	}
	if err := barrier.Commit(); err != nil {
		t.Fatal(err)
	}
	for name, ch := range map[string]<-chan error{"create": created, "freeze": frozen} {
		select {
		case err := <-ch:
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

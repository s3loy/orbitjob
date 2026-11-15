//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/workflow"
	store "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/operator"
	"orbitjob/internal/platform/postgrestest"
	"orbitjob/internal/platform/scan"
)

type workflowFenceFixture struct {
	owner, application *sql.DB
	tenant, source     string
	revision           int64
	parent             workflow.Run
}

func seedWorkflowFence(t *testing.T) workflowFenceFixture {
	t.Helper()
	owner := postgrestest.Open(t)
	f := workflowFenceFixture{owner: owner, tenant: scan.GenerateID(), source: scan.GenerateID()}
	if _, err := owner.ExecContext(t.Context(), `INSERT INTO tenants(id,slug,name) VALUES($1,$2,'workflow fence')`, f.tenant, "fence-"+f.tenant); err != nil {
		t.Fatal(err)
	}
	var parentRevision int64
	parentSource := scan.GenerateID()
	for _, item := range []struct {
		mode, source, name string
		id                 *int64
	}{
		{"workflow", parentSource, "workflow", &parentRevision}, {"kubernetes", f.source, "task", &f.revision},
	} {
		spec := `{"jobTemplate":{"image":"busybox"},"timeoutSeconds":30}`
		if err := owner.QueryRowContext(t.Context(), `INSERT INTO job_definition_revisions(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,actor,is_active) VALUES($1,$2,$3,'fence-ns',$4,1,repeat('a',64),$5,'review',true) RETURNING id`, f.tenant, item.mode, item.source, item.name, spec).Scan(item.id); err != nil {
			t.Fatal(err)
		}
	}
	var searchPath string
	if err := owner.QueryRowContext(t.Context(), "SHOW search_path").Scan(&searchPath); err != nil {
		t.Fatal(err)
	}
	dsn, err := url.Parse(postgrestest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	query := dsn.Query()
	query.Set("search_path", searchPath)
	// Retain the harness login credentials, but execute repository SQL with
	// the deployed operator role. No cluster-wide role passwords are changed.
	query.Set("options", strings.TrimSpace(query.Get("options")+" -c role=orbitjob_runtime"))
	dsn.RawQuery = query.Encode()
	f.application, err = sql.Open("postgres", dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.application.Close() })
	var role string
	if err := f.application.QueryRowContext(t.Context(), "SELECT current_user").Scan(&role); err != nil || role != "orbitjob_runtime" {
		t.Fatalf("application role=%s err=%v", role, err)
	}
	f.parent, _, err = store.NewWorkflowRunRepository(f.application).CreateRunForTenant(t.Context(), f.tenant, workflow.Run{
		SourceUID: parentSource, RevisionID: parentRevision, OccurrenceKey: strings.Repeat("b", 64), Trigger: jobrun.Manual, Actor: "review", Phase: workflow.PhasePending,
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f workflowFenceFixture) step(t *testing.T) workflow.StepRun {
	t.Helper()
	step, _, err := store.NewWorkflowRunRepository(f.application).CreateStepForTenant(t.Context(), f.tenant, f.parent.ID, workflow.StepRun{
		SourceUID: f.source, RevisionID: f.revision, OccurrenceKey: strings.Repeat("c", 64), Trigger: jobrun.Workflow, Phase: jobrun.Pending,
	}, 1, "review")
	if err != nil {
		t.Fatal(err)
	}
	return step
}

func TestWorkflowStopFencesStepAndStalePhaseUnderApplicationRole(t *testing.T) {
	for _, phase := range []workflow.Phase{workflow.PhaseCancelRequested, workflow.PhaseCancelUnknown, workflow.PhaseCanceled, workflow.PhaseSucceeded, workflow.PhaseFailed} {
		t.Run(string(phase), func(t *testing.T) {
			f := seedWorkflowFence(t)
			repo := store.NewWorkflowRunRepository(f.application)
			if _, err := repo.UpdatePhase(t.Context(), f.tenant, f.parent.ID, phase, nil); err != nil {
				t.Fatal(err)
			}
			_, created, err := repo.CreateStepForTenant(t.Context(), f.tenant, f.parent.ID, workflow.StepRun{SourceUID: f.source, RevisionID: f.revision, OccurrenceKey: strings.Repeat("c", 64), Trigger: jobrun.Workflow}, 1, "review")
			if !errors.Is(err, store.ErrRunChanged) || created {
				t.Fatalf("stopped parent accepted step: created=%t err=%v", created, err)
			}
			_, _ = repo.UpdatePhase(t.Context(), f.tenant, f.parent.ID, workflow.PhaseRunning, nil)
			row, found, err := repo.RunByOccurrence(t.Context(), f.tenant, f.parent.SourceUID, f.parent.OccurrenceKey)
			if err != nil || !found || row.Phase != phase {
				t.Fatalf("stale walker erased stop: row=%+v found=%t err=%v", row, found, err)
			}
		})
	}
}

func TestWorkflowCancelWinningParentLockStartsNoAttempt(t *testing.T) {
	f := seedWorkflowFence(t)
	step := f.step(t)
	tx, err := f.owner.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(t.Context(), `UPDATE workflow_run_control_plane SET phase='CancelRequested' WHERE id=$1`, f.parent.ID); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	ensured := false
	go func() {
		finished <- store.NewControlPlaneRepository(f.application).CreateAttemptWithEnsure(t.Context(), f.tenant, step.ID, jobrun.Attempt{Number: 1, KubernetesJobName: "never-created"}, func(context.Context) (jobrun.Attempt, error) {
			ensured = true
			return jobrun.Attempt{}, errors.New("must not execute")
		})
	}()
	select {
	case err := <-finished:
		t.Fatalf("attempt bypassed locked parent: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; !errors.Is(err, store.ErrRunChanged) || ensured {
		t.Fatalf("cancel did not fence reservation: ensured=%t err=%v", ensured, err)
	}
	var attempts int
	if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM job_run_attempts_control_plane WHERE run_id=$1`, step.ID).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("refused reservation left intent: attempts=%d err=%v", attempts, err)
	}
}

func TestWorkflowCancelWaitsForInFlightEnsureParentLock(t *testing.T) {
	f := seedWorkflowFence(t)
	step := f.step(t)
	entered, release := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- store.NewControlPlaneRepository(f.application).CreateAttemptWithEnsure(t.Context(), f.tenant, step.ID, jobrun.Attempt{Number: 1, KubernetesJobName: "created-before-cancel"}, func(ctx context.Context) (jobrun.Attempt, error) {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return jobrun.Attempt{}, ctx.Err()
			}
			return jobrun.Attempt{Number: 1, KubernetesJobName: "created-before-cancel", KubernetesJobUID: "known-uid", Phase: jobrun.Running}, nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Ensure did not start")
	}
	canceled := make(chan error, 1)
	go func() {
		_, err := store.NewWorkflowRunRepository(f.application).UpdatePhase(t.Context(), f.tenant, f.parent.ID, workflow.PhaseCancelRequested, nil)
		canceled <- err
	}()
	select {
	case err := <-canceled:
		t.Fatalf("cancel crossed in-flight Ensure: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if err := <-canceled; err != nil {
		t.Fatal(err)
	}
}

type fenceJobs struct{ ensured int }

func (j *fenceJobs) Ensure(_ context.Context, desired *batchv1.Job) (*batchv1.Job, error) {
	j.ensured++
	out := desired.DeepCopy()
	out.UID = "workload-uid"
	return out, nil
}
func (*fenceJobs) Get(context.Context, string, string) (*batchv1.Job, bool, error) {
	return nil, false, nil
}
func (*fenceJobs) Delete(context.Context, string, string, ...string) error { return nil }
func (*fenceJobs) Stopped(context.Context, string, string) (bool, error)   { return true, nil }

func TestLatePublishedWorkflowStepCannotExecuteAfterParentCancel(t *testing.T) {
	f := seedWorkflowFence(t)
	step := f.step(t) // row committed; publication is delayed until after cancel
	if _, err := store.NewWorkflowRunRepository(f.application).UpdatePhase(t.Context(), f.tenant, f.parent.ID, workflow.PhaseCancelRequested, nil); err != nil {
		t.Fatal(err)
	}
	source := &unstructured.Unstructured{Object: map[string]any{"apiVersion": v1alpha1.GroupVersion.String(), "kind": "ScheduledJob", "metadata": map[string]any{"name": "task", "namespace": "fence-ns", "uid": f.source}}}
	run := v1alpha1.JobRun{TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "JobRun"}, ObjectMeta: metav1.ObjectMeta{Name: v1alpha1.RunObjectName("task", step.OccurrenceKey), Namespace: "fence-ns", UID: types.UID("run-uid")}, Spec: v1alpha1.JobRunSpec{ScheduledJobRef: v1alpha1.ObjectReference{Name: "task", UID: f.source}, DefinitionRevision: f.revision, OccurrenceKey: step.OccurrenceKey, Trigger: v1alpha1.Workflow, Actor: "review", TimeoutSeconds: 30}}
	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&run)
	if err != nil {
		t.Fatal(err)
	}
	obj := &unstructured.Unstructured{Object: object}
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), source, obj)
	jobs := &fenceJobs{}
	ledger := store.NewControlPlaneRepository(f.application)
	rt := operator.Runtime{Dynamic: client, Jobs: jobs, Revisions: ledger, Runs: ledger, Tenants: operator.DefaultTenantResolver{Tenant: f.tenant}}
	immediate, delayed := 0, 0
	rt.Enqueue = func(string, string, string) { immediate++ }
	rt.EnqueueAfter = func(_ string, _ string, _ string, after time.Duration) {
		if after > 0 {
			delayed++
		}
	}
	if err := rt.ReconcileJobRun(t.Context(), *obj); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM job_run_attempts_control_plane WHERE run_id=$1`, step.ID).Scan(&count); err != nil || count != 0 || jobs.ensured != 0 {
		t.Fatalf("late publication executed: attempts=%d Ensure=%d err=%v", count, jobs.ensured, err)
	}
	// Parent intent alone enters the existing cancel path, without a walker
	// flag or immediate requeue, and must not invent an uncertain attempt.
	if immediate != 0 || delayed != 1 {
		t.Fatalf("parent stop retry hot loop: immediate=%d delayed=%d", immediate, delayed)
	}
	var phase string
	if err := f.owner.QueryRowContext(t.Context(), `SELECT phase FROM job_run_control_plane WHERE id=$1`, step.ID).Scan(&phase); err != nil || phase != "Canceled" {
		t.Fatalf("never-started step phase=%s err=%v", phase, err)
	}
}

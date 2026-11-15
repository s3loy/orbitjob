//go:build integration

package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/projection"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/workflow"
	"orbitjob/internal/platform/scan"
)

type workflowSnapshotFixture struct {
	owner                                   *sql.DB
	tenant, source, extract, load           string
	revision, extractRevision, loadRevision int64
	control                                 *ControlPlaneRepository
	store                                   *WorkflowRunRepository
}

func newWorkflowSnapshotFixture(t *testing.T) workflowSnapshotFixture {
	t.Helper()
	f := workflowSnapshotFixture{owner: OpenStoreDB(t), tenant: scan.GenerateID(), source: scan.GenerateID(), extract: scan.GenerateID(), load: scan.GenerateID()}
	if _, err := f.owner.ExecContext(t.Context(), `INSERT INTO tenants(id,slug,name)VALUES($1::char(26),$1::text,'Revenue close')`, f.tenant); err != nil {
		t.Fatal(err)
	}
	runtimeDB := openRoleDB(t, f.owner, "orbitjob_runtime")
	f.control, f.store = NewControlPlaneRepository(runtimeDB), NewWorkflowRunRepository(runtimeDB)
	f.extractRevision = f.projectJob(t, f.extract, "extract", 1, 1, true)
	f.loadRevision = f.projectJob(t, f.load, "load", 1, 1, true)
	var err error
	f.revision, err = (projection.Service{Revisions: f.control}).ApplyWorkflowJob(t.Context(), v1alpha1.WorkflowJob{
		ObjectMeta: metav1.ObjectMeta{Namespace: "finance", Name: "monthly-close", UID: types.UID(f.source), Generation: 1},
		Spec: v1alpha1.WorkflowJobSpec{ExecutionPolicyVersion: 1, Tasks: []v1alpha1.WorkflowTaskSpec{
			{Name: "extract", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "extract"}},
			{Name: "load", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "load"}, DependsOn: []string{"extract"}},
		}},
	}, f.tenant, "test")
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f workflowSnapshotFixture) projectJob(t *testing.T, uid, name string, generation int64, policy int32, digest bool) int64 {
	t.Helper()
	image := "registry.example.com/finance/" + name + ":reviewed"
	if digest {
		image = "registry.example.com/finance/" + name + "@sha256:" + strings.Repeat("a", 64)
	}
	id, err := (projection.Service{Revisions: f.control}).ApplyScheduledJob(t.Context(), v1alpha1.ScheduledJob{
		ObjectMeta: metav1.ObjectMeta{Namespace: "finance", Name: name, UID: types.UID(uid), Generation: generation},
		Spec:       v1alpha1.ScheduledJobSpec{ExecutionPolicyVersion: policy, JobTemplate: v1alpha1.JobTemplateSpec{Image: image}},
	}, f.tenant, "test")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f workflowSnapshotFixture) run(key int) workflow.Run {
	return workflow.Run{SourceUID: f.source, RevisionID: f.revision, OccurrenceKey: fmt.Sprintf("%064x", key), Trigger: jobrun.Manual, Actor: "test"}
}

func TestWorkflowSnapshotRetainsWholeGraphAcrossEditsAndDeletion(t *testing.T) {
	f := newWorkflowSnapshotFixture(t)
	run, created, err := f.store.CreateRunForTenant(t.Context(), f.tenant, f.run(1))
	if err != nil || !created || run.SnapshotVersion != 1 || len(run.TaskSnapshots) != 2 {
		t.Fatalf("run=%+v created=%t err=%v", run, created, err)
	}
	if got := run.TaskSnapshots["load"]; got.SourceUID != f.load || got.RevisionID != f.loadRevision || !strings.Contains(got.Image, "@sha256:") {
		t.Fatalf("incomplete pinned reference: %+v", got)
	}
	// No step yet exists for load. Its snapshot alone must retain history.
	if _, err := f.owner.ExecContext(t.Context(), `DELETE FROM job_definition_revisions WHERE id=$1`, f.loadRevision); err == nil {
		t.Fatal("unstarted task's history was deleted")
	}
	newRevision := f.projectJob(t, f.load, "load", 2, 1, true)
	key := workflow.StepOccurrenceKey(f.source, run.OccurrenceKey, "load")
	if _, _, err := f.store.CreateStepForTenant(t.Context(), f.tenant, run.ID, workflow.StepRun{SourceUID: f.load, RevisionID: newRevision, OccurrenceKey: key, Trigger: jobrun.Workflow}, 1, "test"); err == nil {
		t.Fatal("step bypassed accepted revision")
	}
	if err := f.control.DeactivateSource(t.Context(), f.tenant, "kubernetes", f.load); err != nil {
		t.Fatal(err)
	}
	f.projectJob(t, scan.GenerateID(), "load", 1, 1, true)
	step, fresh, err := f.store.CreateStepForTenant(t.Context(), f.tenant, run.ID, workflow.StepRun{SourceUID: f.load, RevisionID: f.loadRevision, OccurrenceKey: key, Trigger: jobrun.Workflow}, 1, "test")
	if err != nil || !fresh || step.SourceUID != f.load || step.RevisionID != f.loadRevision {
		t.Fatalf("original step=%+v fresh=%t err=%v", step, fresh, err)
	}
	if accepted, err := f.control.SnapshotWorkflowStep(t.Context(), f.tenant, step.ID, step.RevisionID); err != nil || !accepted {
		t.Fatalf("accepted identity=%t err=%v", accepted, err)
	}
	if accepted, err := f.control.SnapshotWorkflowStep(t.Context(), scan.GenerateID(), step.ID, step.RevisionID); err != nil || accepted {
		t.Fatalf("foreign accepted identity=%t err=%v", accepted, err)
	}
	// A replay returns the first immutable graph even after all active pointers changed.
	replayed, fresh, err := f.store.CreateRunForTenant(t.Context(), f.tenant, f.run(1))
	if err != nil || fresh || replayed.TaskSnapshots["load"].RevisionID != f.loadRevision {
		t.Fatalf("replayed=%+v fresh=%t err=%v", replayed, fresh, err)
	}
	var snapshots int
	if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM workflow_task_snapshots WHERE workflow_run_id=$1`, run.ID).Scan(&snapshots); err != nil || snapshots != 2 {
		t.Fatalf("snapshot count=%d err=%v", snapshots, err)
	}
}

func TestWorkflowSnapshotRejectsUnavailableReferencesBeforeParentInsert(t *testing.T) {
	for _, mode := range []string{"missing", "legacy", "mutable_image", "ambiguous_name"} {
		t.Run(mode, func(t *testing.T) {
			f := newWorkflowSnapshotFixture(t)
			switch mode {
			case "missing":
				if err := f.control.DeactivateSource(t.Context(), f.tenant, "kubernetes", f.load); err != nil {
					t.Fatal(err)
				}
			case "legacy":
				f.projectJob(t, f.load, "load", 2, 0, true)
			case "mutable_image":
				f.projectJob(t, f.load, "load", 2, 1, false)
			case "ambiguous_name":
				f.projectJob(t, scan.GenerateID(), "load", 1, 1, true)
			}
			_, created, err := f.store.CreateRunForTenant(t.Context(), f.tenant, f.run(1))
			if created || !errors.Is(err, jobrun.ErrAdmissionBlocked) {
				t.Fatalf("created=%t err=%v", created, err)
			}
			var count int
			if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM workflow_run_control_plane WHERE source_uid=$1`, f.source).Scan(&count); err != nil || count != 0 {
				t.Fatalf("partial parent count=%d err=%v", count, err)
			}
		})
	}
}

func TestWorkflowSnapshotFailureRollsBackEntireGraph(t *testing.T) {
	f := newWorkflowSnapshotFixture(t)
	if _, err := f.owner.ExecContext(t.Context(), `ALTER TABLE workflow_task_snapshots ADD CONSTRAINT test_reject_second_task CHECK(task_name <> 'load')`); err != nil {
		t.Fatal(err)
	}
	if _, created, err := f.store.CreateRunForTenant(t.Context(), f.tenant, f.run(1)); err == nil || created {
		t.Fatalf("created=%t err=%v", created, err)
	}
	for _, table := range []string{"workflow_run_control_plane", "workflow_task_snapshots"} {
		var count int
		if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table+` WHERE tenant_id=$1`, f.tenant).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
}

func TestWorkflowSnapshotDatabaseRequiresCompleteImmutableIdentity(t *testing.T) {
	f := newWorkflowSnapshotFixture(t)
	tx, err := f.owner.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(t.Context(), `INSERT INTO workflow_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase,snapshot_version)VALUES($1,$2,$3,$4,'Manual','test','Pending',1)`, f.tenant, f.source, f.revision, f.run(1).OccurrenceKey); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err == nil {
		t.Fatal("incomplete snapshot committed")
	}
	run, _, err := f.store.CreateRunForTenant(t.Context(), f.tenant, f.run(2))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE workflow_run_control_plane SET snapshot_version=0 WHERE id=$1`, run.ID); err == nil {
		t.Fatal("snapshot downgrade succeeded")
	}
}

func TestWorkflowSnapshotDoesNotInventHistoryForLegacyReplay(t *testing.T) {
	f := newWorkflowSnapshotFixture(t)
	var err error
	f.revision, err = (projection.Service{Revisions: f.control}).ApplyWorkflowJob(t.Context(), v1alpha1.WorkflowJob{
		ObjectMeta: metav1.ObjectMeta{Namespace: "finance", Name: "monthly-close", UID: types.UID(f.source), Generation: 2},
		Spec:       v1alpha1.WorkflowJobSpec{Tasks: []v1alpha1.WorkflowTaskSpec{{Name: "unavailable", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "unavailable"}}}},
	}, f.tenant, "test")
	if err != nil {
		t.Fatal(err)
	}
	run, created, err := f.store.CreateRunForTenant(t.Context(), f.tenant, f.run(1))
	if err != nil || !created || run.SnapshotVersion != 0 || len(run.TaskSnapshots) != 0 {
		t.Fatalf("legacy run=%+v fresh=%t err=%v", run, created, err)
	}
	if err := f.control.DeactivateSource(t.Context(), f.tenant, "workflow", f.source); err != nil {
		t.Fatal(err)
	}
	replayed, fresh, err := f.store.CreateRunForTenant(t.Context(), f.tenant, f.run(1))
	if err != nil || fresh || replayed.SnapshotVersion != 0 || len(replayed.TaskSnapshots) != 0 {
		t.Fatalf("legacy replay=%+v fresh=%t err=%v", replayed, fresh, err)
	}
}

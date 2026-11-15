package operator

import (
	"context"
	"errors"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
)

type scheduleRunStore struct {
	fakeWorkflowRuns
	rows     map[string]workflow.Run
	conflict bool
}

func (f *scheduleRunStore) CreateRunForTenant(_ context.Context, _ string, in workflow.Run) (workflow.Run, bool, error) {
	if row, ok := f.rows[in.OccurrenceKey]; ok {
		return row, false, nil
	}
	if f.conflict {
		in.Phase = workflow.PhaseRejected
		in.Reason = "concurrency_forbid"
	}
	if f.rows == nil {
		f.rows = map[string]workflow.Run{}
	}
	in.ID = int64(len(f.rows) + 1)
	f.rows[in.OccurrenceKey] = in
	return in, true, nil
}
func (f *scheduleRunStore) BindResource(_ context.Context, _ string, id int64, uid string) error {
	for key, row := range f.rows {
		if row.ID == id {
			row.ResourceUID = uid
			f.rows[key] = row
			return nil
		}
	}
	return nil
}

func (f *scheduleRunStore) OpenRuns(context.Context, string) ([]workflow.Run, error) {
	rows := []workflow.Run{}
	for _, row := range f.rows {
		if !workflow.Terminal(row.Phase) {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

type schedulePublisher struct {
	objects []v1alpha1.WorkflowRun
	err     error
}

func (f *schedulePublisher) PublishWorkflow(_ context.Context, obj v1alpha1.WorkflowRun) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.objects = append(f.objects, obj)
	return "scheduled-resource-uid", nil
}

func schedulerFixture(t *testing.T) (WorkflowScheduler, *fakeWorkflowRevisions, *scheduleRunStore, *schedulePublisher) {
	t.Helper()
	now := time.Date(2026, 10, 24, 1, 0, 12, 0, time.UTC)
	spec := twoTaskSpec()
	spec.Schedule = "0 9 * * *"
	spec.TimeZone = "Asia/Shanghai"
	spec.ConcurrencyPolicy = v1alpha1.Forbid
	rev := workflowRevision(t, 5, workflowSpecJSON(t, spec))
	rev.CreatedAt = now.Add(-time.Hour)
	revs := &fakeWorkflowRevisions{byID: map[int64]revision.Revision{5: rev}, workflowActive: []revision.Revision{rev}}
	store := &scheduleRunStore{}
	publisher := &schedulePublisher{}
	return WorkflowScheduler{Revisions: revs, Workflows: store, Publisher: publisher, Tenants: []string{"tenant-a"}, Now: func() time.Time { return now }}, revs, store, publisher
}

func TestWorkflowSchedulePinsTimeAndRepairsAfterRestart(t *testing.T) {
	scheduler, revs, store, publisher := schedulerFixture(t)
	publisher.err = errors.New("apiserver unavailable")
	if created, err := scheduler.Tick(context.Background()); err == nil || created != 1 {
		t.Fatalf("created=%d err=%v", created, err)
	}
	if len(store.rows) != 1 {
		t.Fatalf("row was not committed before publication: %v", store.rows)
	}
	// A new leader must repair the old revision even after it was edited into a
	// manual-only definition. Repair must keep planned time and cancel intent.
	var row workflow.Run
	for _, stored := range store.rows {
		row = stored
	}
	row.Phase = workflow.PhaseCancelRequested
	store.rows[row.OccurrenceKey] = row
	updated := twoTaskSpec()
	updated.Schedule = ""
	rev := workflowRevision(t, 6, workflowSpecJSON(t, updated))
	revs.workflowActive = []revision.Revision{rev}
	revs.byID[6] = rev
	publisher.err = nil
	scheduler.Now = func() time.Time { return time.Date(2026, 10, 25, 1, 0, 0, 0, time.UTC) }
	if created, err := scheduler.Tick(context.Background()); err != nil || created != 0 {
		t.Fatalf("repair=%d %v", created, err)
	}
	if len(publisher.objects) != 1 {
		t.Fatalf("published=%d", len(publisher.objects))
	}
	obj := publisher.objects[0]
	if obj.Spec.DefinitionRevision != 5 || obj.Spec.Trigger != v1alpha1.Schedule || !obj.Spec.CancelRequested {
		t.Fatalf("repaired identity=%+v", obj.Spec)
	}
	if obj.Annotations["orbitjob.io/scheduled-at"] != "2026-10-24T01:00:00Z" {
		t.Fatalf("planned=%v", obj.Annotations)
	}
	if obj.Name != v1alpha1.WorkflowRunObjectName("nightly-pipeline", row.OccurrenceKey) {
		t.Fatalf("unstable name=%s", obj.Name)
	}
}

func TestWorkflowScheduleNoBackfillAndNoPreActivation(t *testing.T) {
	for _, name := range []string{"manual", "paused", "missed", "revision_after_due", "conflict"} {
		t.Run(name, func(t *testing.T) {
			scheduler, revs, store, publisher := schedulerFixture(t)
			spec := twoTaskSpec()
			spec.Schedule = "0 9 * * *"
			spec.TimeZone = "Asia/Shanghai"
			switch name {
			case "manual":
				spec.Schedule = ""
			case "paused":
				spec.Suspend = true
			case "missed":
				scheduler.Now = func() time.Time { return time.Date(2026, 10, 24, 1, 1, 0, 0, time.UTC) }
			case "revision_after_due":
				revs.workflowActive[0].CreatedAt = time.Date(2026, 10, 24, 1, 0, 1, 0, time.UTC)
			case "conflict":
				store.conflict = true
			}
			revs.workflowActive[0].NormalizedSpec = workflowSpecJSON(t, spec)
			if n, err := scheduler.Tick(context.Background()); err != nil || n != 0 || len(publisher.objects) != 0 {
				t.Fatalf("created=%d objects=%v err=%v", n, publisher.objects, err)
			}
		})
	}
}

func TestWorkflowScheduleDuplicateTicksAndLeaderLoss(t *testing.T) {
	scheduler, _, store, _ := schedulerFixture(t)
	for range 2 {
		if _, err := scheduler.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.rows) != 1 {
		t.Fatalf("duplicate occurrences=%d", len(store.rows))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if n, err := scheduler.Tick(ctx); !errors.Is(err, context.Canceled) || n != 0 {
		t.Fatalf("lost leader created=%d err=%v", n, err)
	}
}

func TestScheduledWorkflowResourceRequiresMatchingRowAndCanCancel(t *testing.T) {
	revs := &fakeWorkflowRevisions{byID: map[int64]revision.Revision{5: workflowRevision(t, 5, workflowSpecJSON(t, twoTaskSpec()))}}
	obj := workflowRunObject(t, func(run *v1alpha1.WorkflowRun) {
		run.Spec.Trigger = v1alpha1.Schedule
		run.Spec.Actor = ActorWorkflowScheduler
		run.Spec.CancelRequested = true
	})
	runs := &fakeWorkflowRuns{}
	runtime := newWorkflowRuntime(t, revs, runs, &obj)
	if err := runtime.ReconcileWorkflowRun(context.Background(), obj); err == nil || len(runs.created) != 0 {
		t.Fatalf("orphan schedule invented work: %v", err)
	}
	runs.stored = workflow.Run{ID: 9, SourceUID: "wf-uid-1", RevisionID: 5, OccurrenceKey: "wocc-1", Trigger: jobrun.Schedule, Actor: ActorWorkflowScheduler, Phase: workflow.PhasePending}
	runs.found = true
	if err := runtime.ReconcileWorkflowRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if len(runs.phases) != 1 || runs.phases[0].phase != workflow.PhaseCancelRequested {
		t.Fatalf("cancel=%v", runs.phases)
	}
}

func TestWorkflowSchedulePublisherAdoptsIdentityWithoutClearingCancel(t *testing.T) {
	obj := v1alpha1.WorkflowRun{TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "WorkflowRun"}, ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: "finance"}, Spec: v1alpha1.WorkflowRunSpec{WorkflowRef: v1alpha1.ObjectReference{Name: "pipeline", UID: "uid-1"}, DefinitionRevision: 5, Trigger: v1alpha1.Schedule, Actor: ActorWorkflowScheduler, OccurrenceKey: "key-1"}}
	client := fakeDynamic(t)
	client.(*dynamicfake.FakeDynamicClient).PrependReactor("create", "workflowruns", func(action clienttesting.Action) (bool, runtime.Object, error) {
		obj := action.(clienttesting.CreateAction).GetObject().(metav1.Object)
		obj.SetUID("scheduled-resource-uid")
		obj.SetResourceVersion("1")
		return false, nil, nil
	})
	publisher := RunPublisher{Client: client}
	if _, err := publisher.PublishWorkflow(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	_, err := client.Resource(workflowRunGVR).Namespace("finance").Patch(context.Background(), obj.Name, "application/merge-patch+json", []byte(`{"spec":{"cancelRequested":true}}`), metav1.PatchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.PublishWorkflow(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	stored, err := client.Resource(workflowRunGVR).Namespace("finance").Get(context.Background(), obj.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var decoded v1alpha1.WorkflowRun
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(stored.Object, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Spec.CancelRequested {
		t.Fatal("repair cleared cancellation")
	}
	obj.UID = "replacement-resource-uid"
	if _, err := publisher.PublishWorkflow(context.Background(), obj); err == nil {
		t.Fatal("accepted replacement UID")
	}
	obj.UID = ""
	obj.Spec.DefinitionRevision = 6
	if _, err := publisher.PublishWorkflow(context.Background(), obj); err == nil {
		t.Fatal("adopted wrong revision")
	}
	client.(*dynamicfake.FakeDynamicClient).PrependReactor("create", "workflowruns", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "workflowruns"}, obj.Name, errors.New("denied"))
	})
	if _, err := publisher.PublishWorkflow(context.Background(), obj); err == nil {
		t.Fatal("publication permission error lost")
	}
}

func TestWorkflowScheduleOneBrokenRepairDoesNotBlockOtherWorkflows(t *testing.T) {
	scheduler, revs, store, publisher := schedulerFixture(t)
	old := workflowRevision(t, 4, workflowSpecJSON(t, twoTaskSpec()))
	old.Identity.SourceUID = "old-workflow"
	revs.byID[4] = old
	store.rows = map[string]workflow.Run{"old-occurrence": {ID: 99, SourceUID: "old-workflow", RevisionID: 4, OccurrenceKey: "old-occurrence", Trigger: jobrun.Schedule, Actor: ActorWorkflowScheduler, Phase: workflow.PhasePending}}
	publisher.err = errors.New("apiserver unavailable")
	n, err := scheduler.Tick(context.Background())
	if err == nil || n != 1 || len(store.rows) != 2 {
		t.Fatalf("one broken repair blocked a separate due workflow: created=%d rows=%d err=%v", n, len(store.rows), err)
	}
}

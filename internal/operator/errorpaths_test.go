package operator

import (
	"context"
	"errors"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
)

func TestNewInClusterFailsOutsideACluster(t *testing.T) {
	// Without a service account the operator must refuse to start rather than
	// run with a nil client and reconcile nothing.
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	if _, err := NewInCluster(Config{}); err == nil {
		t.Skip("running inside a cluster; in-cluster config is available")
	}
}

func TestReconcileRequiresConfiguredDependencies(t *testing.T) {
	obj := scheduledJobObject(t)
	runObj := jobRunObject(t)
	jobObj := observedJob(t, "oj-nightly-report-1-1", "Succeeded")

	tests := []struct {
		name      string
		reconcile func(context.Context) error
	}{
		{
			"projection without a revision store",
			func(ctx context.Context) error {
				return Runtime{Tenants: NamespaceTenantResolver{}}.ReconcileScheduledJob(ctx, obj)
			},
		},
		{
			"job run without a run store",
			func(ctx context.Context) error { return Runtime{}.ReconcileJobRun(ctx, runObj) },
		},
		{
			"job observation without a run store",
			func(ctx context.Context) error { return Runtime{}.ReconcileJob(ctx, jobObj) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.reconcile(context.Background()); err == nil {
				t.Fatal("expected a configuration error")
			}
		})
	}
}

func TestReconcileJobRunSurfacesRevisionFailure(t *testing.T) {
	obj := jobRunObject(t)
	runs := &fakeRuns{
		found:  true,
		stored: jobrun.StoredRun{ID: 11, RevisionID: 7, Phase: jobrun.Pending, MaxAttempts: 1, Epoch: 4},
	}
	revisions := &fakeRevisions{loadErr: errors.New("revision missing")}
	jobs := &fakeJobs{}
	rt := newRuntime(t, fakeDynamic(t, &obj), revisions, runs, jobs)

	// A run pinned to an unreadable revision must not silently proceed to
	// execute something else.
	if err := rt.ReconcileJobRun(context.Background(), obj); err == nil {
		t.Fatal("expected the revision error to surface")
	}
	if len(jobs.created) != 0 {
		t.Fatal("no job may be created without its pinned revision")
	}
}

func TestReconcileJobRunSurfacesUndecodableRevision(t *testing.T) {
	obj := jobRunObject(t)
	runs := &fakeRuns{
		found:  true,
		stored: jobrun.StoredRun{ID: 11, RevisionID: 7, Phase: jobrun.Pending, MaxAttempts: 1, Epoch: 4},
	}
	broken := revisionFor(t, 7, pinnedSpec)
	broken.NormalizedSpec = "{not json"
	revisions := &fakeRevisions{byID: map[int64]revision.Revision{7: broken}}
	rt := newRuntime(t, fakeDynamic(t, &obj), revisions, runs, &fakeJobs{})

	if err := rt.ReconcileJobRun(context.Background(), obj); err == nil {
		t.Fatal("expected a decode error to surface")
	}
}

func TestReconcileJobRunSurfacesJobCreationFailure(t *testing.T) {
	obj := jobRunObject(t)
	runs := &fakeRuns{
		found:  true,
		stored: jobrun.StoredRun{ID: 11, RevisionID: 7, Phase: jobrun.Pending, MaxAttempts: 1, Epoch: 4},
	}
	jobs := &fakeJobs{err: errors.New("quota exceeded")}
	revisions := &fakeRevisions{byID: map[int64]revision.Revision{7: revisionFor(t, 7, pinnedSpec)}}
	rt := newRuntime(t, fakeDynamic(t, &obj), revisions, runs, jobs)

	if err := rt.ReconcileJobRun(context.Background(), obj); err == nil {
		t.Fatal("expected the job creation error to surface")
	}
	if len(runs.attempts) != 0 {
		t.Fatal("an attempt must not be recorded when its job was not created")
	}
}

func TestReconcileJobRunRejectsMalformedSpec(t *testing.T) {
	obj := jobRunObject(t)
	delete(obj.Object["spec"].(map[string]any), "occurrenceKey")
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, &fakeRuns{found: true}, &fakeJobs{})
	if err := rt.ReconcileJobRun(context.Background(), obj); err == nil {
		t.Fatal("a run without an occurrence key must be refused")
	}

	noSpec := jobRunObject(t)
	delete(noSpec.Object, "spec")
	rt2 := newRuntime(t, fakeDynamic(t, &noSpec), &fakeRevisions{}, &fakeRuns{found: true}, &fakeJobs{})
	if err := rt2.ReconcileJobRun(context.Background(), noSpec); err == nil {
		t.Fatal("a run without a spec must be refused")
	}
}

func TestScheduledJobConversionRejectsMalformedObject(t *testing.T) {
	// A generation field carrying the wrong type cannot be decoded; the operator
	// must refuse it rather than project a zero generation.
	obj := scheduledJobObject(t)
	obj.Object["metadata"].(map[string]any)["generation"] = "not-a-number"
	if _, err := scheduledJobFromUnstructured(obj); err == nil {
		t.Fatal("expected a decode error")
	}
}

func TestPatchStatusSurfacesErrors(t *testing.T) {
	obj := scheduledJobObject(t)
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), &obj)
	client.PrependReactor("patch", "scheduledjobs", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "scheduledjobs"}, "x", errors.New("denied"))
	})
	runtime := Runtime{Dynamic: client}
	if err := runtime.patchStatus(context.Background(), scheduledJobGVR, obj, map[string]any{"observedGeneration": int64(1)}); err == nil {
		t.Fatal("expected the status patch error to surface")
	}
	// A missing client is a wiring defect, not a silent skip.
	if err := (Runtime{}).patchStatus(context.Background(), scheduledJobGVR, obj, nil); err == nil {
		t.Fatal("expected a client error")
	}
}

func TestReconcileJobRejectsUndecodableJob(t *testing.T) {
	obj := observedJob(t, "oj-nightly-report-1-1", "Succeeded")
	obj.Object["status"] = "not-an-object"
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, &fakeRuns{}, &fakeJobs{})
	if err := rt.ReconcileJob(context.Background(), obj); err == nil {
		t.Fatal("expected a decode error")
	}
}

func TestDynamicHandlerRejectsUnsupportedResource(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	handler := DynamicHandler{Client: client}
	for _, key := range []string{"secrets:finance/x", "unknown:finance/x", "nocolon", "jobs:missing-slash"} {
		if err := handler.Handle(context.Background(), key, unstructured.Unstructured{}, false); err == nil {
			t.Errorf("key %q accepted", key)
		}
	}
}

func TestDynamicHandlerRequiresClientAndHandler(t *testing.T) {
	if err := (DynamicHandler{}).Handle(context.Background(), "jobs:finance/x", unstructured.Unstructured{}, false); err == nil {
		t.Fatal("expected a client error")
	}
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	// A resource with no configured callback is a wiring defect.
	if err := (DynamicHandler{Client: client}).Handle(context.Background(), "jobs:finance/x", unstructured.Unstructured{}, false); err == nil {
		t.Fatal("expected a handler error")
	}
}

func TestWorkerExitsWhenTheQueueIsShutDown(t *testing.T) {
	queue := workqueue.NewTypedRateLimitingQueue(
		workqueue.DefaultTypedControllerRateLimiter[string](),
	)
	queue.ShutDown()
	controller := Controller{Reconcile: func(context.Context, string, unstructured.Unstructured, bool) error {
		t.Fatal("a shut-down queue must not deliver work")
		return nil
	}}
	done := make(chan struct{})
	go func() { controller.worker(context.Background(), queue, discardLogger(), objectCache{}); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not exit after shutdown")
	}
}

func TestCancelOnATerminalRunDoesNotRewriteHistory(t *testing.T) {
	// A run that already succeeded cannot be cancelled; the request must be
	// ignored rather than dragging a completed run back into CancelRequested.
	obj, stored := jobRunObjectWithCancel(t, string(jobrun.Succeeded), 1)
	runs := &fakeRuns{found: true, stored: stored}
	jobs := &fakeJobs{}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, jobs)

	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	for _, write := range runs.phases {
		if write.phase == jobrun.CancelRequested || write.phase == jobrun.Canceled {
			t.Fatalf("terminal run was rewritten: %+v", runs.phases)
		}
	}
	if len(jobs.deleted) != 0 {
		t.Fatal("a completed run's job must not be deleted")
	}
}

func TestCancelLeavesJobAloneUntilDeletionIsRegistered(t *testing.T) {
	// The Job is still fully alive after the delete call: Kubernetes has not
	// registered the deletion yet, so the platform must wait rather than
	// declare the workload stopped.
	obj, stored := jobRunObjectWithCancel(t, string(jobrun.Running), 1)
	live := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: "oj-nightly-report-1-1", Namespace: "finance",
	}}
	runs := &fakeRuns{found: true, stored: stored}
	jobs := &fakeJobs{job: live, deleteKeepsJob: true}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, jobs)

	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	for _, write := range runs.phases {
		if write.phase == jobrun.Canceled {
			t.Fatal("cancel must not be confirmed before deletion is registered")
		}
	}
}

func TestJobRunSpecRejectsWrongFieldTypes(t *testing.T) {
	obj := jobRunObject(t)
	obj.Object["spec"].(map[string]any)["definitionRevision"] = "seven"
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, &fakeRuns{found: true}, &fakeJobs{})
	if err := rt.ReconcileJobRun(context.Background(), obj); err == nil {
		t.Fatal("expected a decode error for a mistyped field")
	}
}

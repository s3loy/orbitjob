package operator

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	"orbitjob/internal/core/domain/workflow"
)

func TestWorkflowScheduleForbidSkipCannotStartLaterInSameMinute(t *testing.T) {
	scheduler, _, store, publisher := schedulerFixture(t)
	store.conflict = true
	if n, err := scheduler.Tick(context.Background()); err != nil || n != 0 {
		t.Fatalf("skip=%d %v", n, err)
	}
	if len(store.rows) != 1 {
		t.Fatal("overlap did not record a durable occurrence decision")
	}
	store.conflict = false
	if n, err := scheduler.Tick(context.Background()); err != nil || n != 0 || len(publisher.objects) != 0 {
		t.Fatalf("same-minute retry queued a skipped occurrence: n=%d published=%d err=%v", n, len(publisher.objects), err)
	}
}

func TestWorkflowPublicationFailureBlocksStepsUntilBoundResourceRepair(t *testing.T) {
	scheduler, revs, store, publisher := schedulerFixture(t)
	jobs := walkerRevisions(t)
	revs.active = jobs.active
	for id, rev := range jobs.byID {
		if id != 5 {
			revs.byID[id] = rev
		}
	}
	publisher.err = errors.New("apiserver unavailable")
	if _, err := scheduler.Tick(context.Background()); err == nil {
		t.Fatal("expected failed initial publication")
	}
	dyn := fakeDynamic(t)
	dyn.(*dynamicfake.FakeDynamicClient).PrependReactor("create", "workflowruns", func(action clienttesting.Action) (bool, runtime.Object, error) {
		obj := action.(clienttesting.CreateAction).GetObject().(metav1.Object)
		obj.SetUID("repaired-resource-uid")
		obj.SetResourceVersion("1")
		return false, nil, nil
	})
	rt := Runtime{Dynamic: dyn, Revisions: revs, Workflows: store}
	stepPublisher := &fakeWorkflowPublisher{}
	walker := WorkflowWalker{Workflows: store, Revisions: revs, Publisher: stepPublisher, Tenants: []string{"tenant-a"}, ObserveResource: rt.ObserveWorkflowResource}
	if n, err := walker.Tick(context.Background()); err != nil || n != 0 || len(store.createdStep) != 0 {
		t.Fatalf("unpublished workflow executed steps: n=%d err=%v", n, err)
	}
	scheduler.Publisher = RunPublisher{Client: dyn}
	if _, err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	if n, err := walker.Tick(context.Background()); err != nil || n != 1 || len(store.createdStep) != 1 {
		t.Fatalf("bound repaired workflow did not advance: n=%d err=%v", n, err)
	}
}

func TestWorkflowWalkerReadsLiveCancelBeforeStartingSteps(t *testing.T) {
	runs := &fakeWorkflowRuns{stored: walkerRun()}
	walker, _ := newWalker(t, runs, walkerRevisions(t), &fakeWorkflowPublisher{})
	walker.ObserveResource = func(context.Context, string, workflow.Run) (bool, error) { return true, nil }
	if n, err := walker.Tick(context.Background()); err != nil || n != 0 || len(runs.createdStep) != 0 {
		t.Fatalf("cancel intent lost: n=%d err=%v", n, err)
	}
	if len(runs.phases) == 0 || runs.phases[0].phase != workflow.PhaseCancelRequested {
		t.Fatalf("cancel not recorded before advance: %v", runs.phases)
	}
}

func TestWorkflowWalkerRefusesMissingAndReplacementBoundResources(t *testing.T) {
	for _, name := range []string{"missing", "replacement"} {
		t.Run(name, func(t *testing.T) {
			runs := &fakeWorkflowRuns{stored: walkerRun()}
			revs := walkerRevisions(t)
			dyn := fakeDynamic(t)
			if name == "replacement" {
				obj := workflowRunObject(t, nil)
				obj.SetName("nightly-pipeline-wocc-1")
				obj.SetUID("replacement")
				if err := dyn.(*dynamicfake.FakeDynamicClient).Tracker().Add(&obj); err != nil {
					t.Fatal(err)
				}
			}
			walker, _ := newWalker(t, runs, revs, &fakeWorkflowPublisher{})
			walker.ObserveResource = (Runtime{Dynamic: dyn, Revisions: revs}).ObserveWorkflowResource
			if n, err := walker.Tick(context.Background()); err != nil || n != 0 || len(runs.createdStep) != 0 {
				t.Fatalf("unsafe resource executed steps: n=%d err=%v", n, err)
			}
		})
	}
}

package operator

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/cache"
	"orbitjob/internal/core/app/execution"
)

func managedJobEvent() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetNamespace("finance")
	obj.SetName("attempt-1")
	obj.SetLabels(map[string]string{execution.LabelRunName: "run-1", execution.LabelRunUID: "run-uid"})
	return obj
}

func TestJobDeletionWakesRunAfterObjectLeavesCache(t *testing.T) {
	q := queueWith("unrelated:finance/x")
	defer q.ShutDown()
	key, _ := q.Get()
	q.Done(key)
	enqueue(q, jobGVR, cache.DeletedFinalStateUnknown{Key: "finance/attempt-1", Obj: managedJobEvent()})
	got := map[string]bool{}
	for q.Len() > 0 {
		key, _ := q.Get()
		got[key] = true
		q.Done(key)
	}
	if !got["jobs:finance/attempt-1"] || !got["jobruns:finance/run-1"] {
		t.Fatalf("deletion lost the parent link: %v", got)
	}
}

func TestJobObservationWakesRunAfterCommit(t *testing.T) {
	obj := managedJobEvent()
	informer := cache.NewSharedIndexInformer(&cache.ListWatch{}, &unstructured.Unstructured{}, 0, nil)
	if err := informer.GetStore().Add(obj); err != nil {
		t.Fatal(err)
	}
	watched := objectCache{byResource: map[string]cache.SharedIndexInformer{"jobs": informer}}
	q := queueWith("jobs:finance/attempt-1")
	defer q.ShutDown()
	key, _ := q.Get()
	committed := false
	controller := Controller{Reconcile: func(_ context.Context, _ string, _ unstructured.Unstructured, cached bool) error {
		if !cached || q.Len() != 0 {
			t.Fatal("test must begin before the parent can run")
		}
		committed = true
		return nil
	}}
	controller.process(context.Background(), q, discardLogger(), watched, key)
	if !committed || q.Len() != 1 {
		t.Fatalf("parent not woken after commit: %d", q.Len())
	}
	next, _ := q.Get()
	q.Done(next)
	if next != "jobruns:finance/run-1" {
		t.Fatalf("key = %s", next)
	}
}

func TestCancellationRechecksWithoutMoreInformerEvents(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"namespace": "finance", "name": "run-1"},
		"spec":     map[string]any{"cancelRequested": true},
		"status":   map[string]any{"phase": "CancelRequested"},
	}}
	informer := cache.NewSharedIndexInformer(&cache.ListWatch{}, &unstructured.Unstructured{}, 0, nil)
	if err := informer.GetStore().Add(obj); err != nil {
		t.Fatal(err)
	}
	watched := objectCache{byResource: map[string]cache.SharedIndexInformer{"jobruns": informer}}
	q := queueWith("jobruns:finance/run-1")
	defer q.ShutDown()
	key, _ := q.Get()
	controller := Controller{Reconcile: func(context.Context, string, unstructured.Unstructured, bool) error { return nil }}
	controller.process(context.Background(), q, discardLogger(), watched, key)
	next := make(chan string, 1)
	go func() {
		item, stopped := q.Get()
		if !stopped {
			q.Done(item)
			next <- item
		}
	}()
	select {
	case item := <-next:
		if item != key {
			t.Fatalf("key = %s", item)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("stuck cancellation waits for informer resync instead of checking its timeout")
	}
}

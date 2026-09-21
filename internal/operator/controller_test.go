package operator

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func queueWith(key string) workqueue.TypedRateLimitingInterface[string] {
	queue := workqueue.NewTypedRateLimitingQueue(
		workqueue.DefaultTypedControllerRateLimiter[string](),
	)
	queue.Add(key)
	return queue
}

// newTestInformer builds a SharedIndexInformer whose store holds obj, without
// running the informer: objectCache reads only the store.
func newTestInformer(t *testing.T, obj any) cache.SharedIndexInformer {
	t.Helper()
	informer := cache.NewSharedIndexInformer(&cache.ListWatch{}, &unstructured.Unstructured{}, 0, cache.Indexers{})
	if err := informer.GetStore().Add(obj); err != nil {
		t.Fatal(err)
	}
	return informer
}

func TestReconcileOneForgetsOnSuccess(t *testing.T) {
	queue := queueWith("jobruns:finance/x")
	controller := Controller{Reconcile: func(context.Context, string, unstructured.Unstructured, bool) error { return nil }}
	controller.reconcileOne(context.Background(), queue, discardLogger(), objectCache{}, "jobruns:finance/x")

	if queue.NumRequeues("jobruns:finance/x") != 0 {
		t.Fatal("a successful reconcile must clear its backoff history")
	}
}

func TestReconcileOneRequeuesOnFailure(t *testing.T) {
	queue := queueWith("jobruns:finance/x")
	controller := Controller{Reconcile: func(context.Context, string, unstructured.Unstructured, bool) error { return errors.New("conflict") }}
	controller.reconcileOne(context.Background(), queue, discardLogger(), objectCache{}, "jobruns:finance/x")

	// The item must come back rather than being dropped: a transient database
	// conflict would otherwise silently stop reconciliation for that run.
	if queue.Len() == 0 {
		t.Fatal("a failed reconcile must be requeued")
	}
	if queue.NumRequeues("jobruns:finance/x") == 0 {
		t.Fatal("the retry must be rate limited, not immediate")
	}
}

func TestReconcileOneDoesNotRequeueDuringShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	queue := queueWith("jobruns:finance/x")
	controller := Controller{Reconcile: func(context.Context, string, unstructured.Unstructured, bool) error { return errors.New("api down") }}
	controller.reconcileOne(ctx, queue, discardLogger(), objectCache{}, "jobruns:finance/x")

	// Work is retried on the next start; counting a requeue here would spin
	// against a cancelled context until the process exits.
	if queue.NumRequeues("jobruns:finance/x") != 0 {
		t.Fatal("shutdown must not rate-limit-requeue work")
	}
}

// TestReconcileOneServesTheCachedObject pins the informer fast path: a key the
// store still holds must hand its object to the handler with cached set, so
// reconciles cost no apiserver read. Under a burst that read is what makes the
// queue drain slower than it fills.
func TestReconcileOneServesTheCachedObject(t *testing.T) {
	queue := queueWith("jobruns:finance/x")
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "workloads.orbitjob.io/v1alpha1",
		"kind":       "JobRun",
		"metadata":   map[string]any{"name": "x", "namespace": "finance"},
	}}
	watched := objectCache{byResource: map[string]cache.SharedIndexInformer{
		"jobruns": newTestInformer(t, obj),
	}}
	type received struct {
		obj    unstructured.Unstructured
		cached bool
	}
	got := make(chan received, 1)
	controller := Controller{Reconcile: func(_ context.Context, _ string, o unstructured.Unstructured, cached bool) error {
		got <- received{o, cached}
		return nil
	}}
	controller.reconcileOne(context.Background(), queue, discardLogger(), watched, "jobruns:finance/x")

	select {
	case r := <-got:
		if !r.cached {
			t.Fatal("a stored object must reach the handler as cached")
		}
		if r.obj.GetName() != "x" || r.obj.GetNamespace() != "finance" {
			t.Fatalf("object = %s/%s", r.obj.GetNamespace(), r.obj.GetName())
		}
	default:
		t.Fatal("the handler was never called")
	}
}

// TestReconcileOneMarksUnknownKeysUncached pins the fallback: a key the store
// does not hold (deleted while queued) must reach the handler with cached
// false, so the handler knows to read the API server instead of trusting a
// zero object.
func TestReconcileOneMarksUnknownKeysUncached(t *testing.T) {
	queue := queueWith("jobs:finance/gone")
	cached := make(chan bool, 1)
	controller := Controller{Reconcile: func(_ context.Context, _ string, _ unstructured.Unstructured, c bool) error {
		cached <- c
		return nil
	}}
	controller.reconcileOne(context.Background(), queue, discardLogger(), objectCache{}, "jobs:finance/gone")

	select {
	case c := <-cached:
		if c {
			t.Fatal("an absent store entry must not be reported as cached")
		}
	default:
		t.Fatal("the handler was never called")
	}
}

// TestReconcileOneRequeuesExpiredBudget pins the difference between a stopped
// operator and a wedged one: a pass that outlives its budget is a failure to
// retry, not a shutdown, so the item must come back.
func TestReconcileOneRequeuesExpiredBudget(t *testing.T) {
	queue := queueWith("jobruns:finance/x")
	controller := Controller{
		Config: Config{Budget: 20 * time.Millisecond},
		Reconcile: func(ctx context.Context, _ string, _ unstructured.Unstructured, _ bool) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	controller.reconcileOne(context.Background(), queue, discardLogger(), objectCache{}, "jobruns:finance/x")

	if queue.Len() == 0 {
		t.Fatal("an expired-budget reconcile must be requeued")
	}
}

// TestProcessRequeuesAfterPanic pins the bookkeeping of the recovery path: a
// panicking reconcile must be counted as a reconcile error and requeued
// through the rate limiter, exactly like an error return. The requeue is only
// visible once Done has released the key -- a key held by a dead worker never
// re-enters the queue -- so the poll below pins both halves.
func TestProcessRequeuesAfterPanic(t *testing.T) {
	queue := queueWith("jobruns:finance/x")
	controller := Controller{Reconcile: func(context.Context, string, unstructured.Unstructured, bool) error {
		panic("reconcile exploded")
	}}
	controller.process(context.Background(), queue, discardLogger(), objectCache{}, "jobruns:finance/x")

	if got := queue.NumRequeues("jobruns:finance/x"); got != 1 {
		t.Fatalf("requeues = %d, want 1 (the retry must be rate limited, not immediate)", got)
	}
	deadline := time.Now().Add(2 * time.Second)
	for queue.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if queue.Len() == 0 {
		t.Fatal("a panicking reconcile must be requeued for retry")
	}
}

// TestWorkerSurvivesPanickingReconciles pins the failure mode the recovery
// wrapper exists for: before it, the first panic unwound through the worker
// loop, the goroutine died, Done was never called, and the operator stopped
// reconciling while its liveness stayed green. Three panics in a row must all
// come back through the queue and be served again.
func TestWorkerSurvivesPanickingReconciles(t *testing.T) {
	queue := queueWith("jobruns:finance/x")
	calls := make(chan struct{}, 8)
	controller := Controller{Reconcile: func(context.Context, string, unstructured.Unstructured, bool) error {
		calls <- struct{}{}
		panic("reconcile exploded")
	}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		controller.worker(context.Background(), queue, discardLogger(), objectCache{})
	}()
	for i := 0; i < 3; i++ {
		select {
		case <-calls:
		case <-time.After(10 * time.Second):
			t.Fatalf("the worker stopped serving keys after %d panic(s)", i)
		}
	}
	queue.ShutDown()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker did not return on shutdown")
	}
}

func TestEnqueueNamespacesKeysByResource(t *testing.T) {
	queue := workqueue.NewTypedRateLimitingQueue(
		workqueue.DefaultTypedControllerRateLimiter[string](),
	)
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "workloads.orbitjob.io/v1alpha1",
		"kind":       "JobRun",
		"metadata":   map[string]any{"name": "run-1", "namespace": "finance"},
	}}
	enqueue(queue, jobRunGVR, obj)

	// The resource prefix is what keeps a ScheduledJob and a JobRun sharing a
	// namespace/name from being reconciled as each other.
	item, _ := queue.Get()
	if item != "jobruns:finance/run-1" {
		t.Fatalf("key = %q", item)
	}
}

func TestRunListsOnlyConfiguredNamespaces(t *testing.T) {
	listKinds := map[schema.GroupVersionResource]string{
		scheduledJobGVR: "ScheduledJobList",
		jobRunGVR:       "JobRunList",
		workflowJobGVR:  "WorkflowJobList",
		workflowRunGVR:  "WorkflowRunList",
		jobGVR:          "JobList",
	}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- (Controller{
			Config:    Config{Namespaces: []string{"team-a", "team-b"}},
			Dynamic:   client,
			Reconcile: func(context.Context, string, unstructured.Unstructured, bool) error { return nil },
			Log:       discardLogger(),
		}).Run(ctx)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		counts := map[string]int{}
		for _, action := range client.Actions() {
			if action.GetVerb() == "list" {
				counts[action.GetNamespace()]++
			}
		}
		if counts["team-a"] == len(watchedResources) && counts["team-b"] == len(watchedResources) {
			if counts[""] != 0 || len(counts) != 2 {
				cancel()
				t.Fatalf("list namespaces = %v, want only team-a and team-b", counts)
			}
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("timed out waiting for namespaced informer lists: %v", counts)
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not stop after cancellation")
	}
}

func TestEnqueueUsesTombstoneMetadata(t *testing.T) {
	queue := workqueue.NewTypedRateLimitingQueue(
		workqueue.DefaultTypedControllerRateLimiter[string](),
	)
	inner := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata":   map[string]any{"name": "oj-run-1-1", "namespace": "finance"},
	}}
	// A delete event arrives wrapped in a tombstone by the informer machinery.
	enqueue(queue, jobGVR, cache.DeletedFinalStateUnknown{Key: "finance/oj-run-1-1", Obj: inner})

	if queue.Len() != 1 {
		t.Fatal("a tombstone must still produce a queue key")
	}
	item, _ := queue.Get()
	if item != "jobs:finance/oj-run-1-1" {
		t.Fatalf("key = %q", item)
	}
	// A tombstone wrapping something unaddressable must be dropped silently
	// rather than panicking the event handler.
	enqueue(queue, jobGVR, cache.DeletedFinalStateUnknown{Key: "x", Obj: "not-an-object"})
	if queue.Len() != 0 {
		t.Fatal("unaddressable tombstones must be ignored")
	}
}

func TestEnqueueIgnoresUnaddressableObjects(t *testing.T) {
	queue := workqueue.NewTypedRateLimitingQueue(
		workqueue.DefaultTypedControllerRateLimiter[string](),
	)
	enqueue(queue, jobGVR, "not-an-object")
	enqueue(queue, jobGVR, struct{}{})
	if queue.Len() != 0 {
		t.Fatal("objects without metadata must not become queue keys")
	}
}

// TestEnqueueKeysInformerDeliveredShapes pins the shape a live informer
// delivers: a probe against the kind apiserver (client-go v0.32, batch/v1
// jobs) showed LIST and watch both hand handlers *unstructured.Unstructured.
// This is a hardening pin, not a regression pin: *unstructured.Unstructured
// satisfies metav1.Object, so the old direct assertion never dropped these
// events either. meta.Accessor is kept because it reaches the metadata of
// every shape uniformly instead of only direct metav1.Object matches. The
// regression that actually left events unreconciled was client-go rate-limit
// starvation on the apiserver client, fixed at the factory level
// (applyClientLimits), not in event delivery.
func TestEnqueueKeysInformerDeliveredShapes(t *testing.T) {
	plain := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "workloads.orbitjob.io/v1alpha1",
		"kind":       "ScheduledJob",
		"metadata":   map[string]any{"name": "nightly", "namespace": "orbitjob-tasks-load"},
	}}

	queue := workqueue.NewTypedRateLimitingQueue(
		workqueue.DefaultTypedControllerRateLimiter[string](),
	)
	enqueue(queue, scheduledJobGVR, plain)
	if queue.Len() != 1 {
		t.Fatal("list-delivered object produced no queue key")
	}
	item, _ := queue.Get()
	if item != "scheduledjobs:orbitjob-tasks-load/nightly" {
		t.Fatalf("key = %q", item)
	}
}

// TestEnqueueKeysObjectsWithoutNamespaceOrName pins the one shape enqueue
// does not guard: metadata with an empty namespace (a cluster-scoped
// delivery) or an empty name still yields a key -- "jobruns:/name" or
// "jobruns:/". Such a key reconciles through the uncached fallback, finds
// nothing, and drains through the normal retry path, so no guard is needed;
// pinning that here makes adding one a deliberate change rather than a silent
// behavior shift.
func TestEnqueueKeysObjectsWithoutNamespaceOrName(t *testing.T) {
	queue := workqueue.NewTypedRateLimitingQueue(
		workqueue.DefaultTypedControllerRateLimiter[string](),
	)
	enqueue(queue, jobRunGVR, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "workloads.orbitjob.io/v1alpha1",
		"kind":       "JobRun",
		"metadata":   map[string]any{"name": "run-1"},
	}})
	enqueue(queue, jobRunGVR, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "workloads.orbitjob.io/v1alpha1",
		"kind":       "JobRun",
		"metadata":   map[string]any{"namespace": "finance"},
	}})
	if queue.Len() != 2 {
		t.Fatalf("queue holds %d keys, want 2", queue.Len())
	}
	item, _ := queue.Get()
	if item != "jobruns:/run-1" {
		t.Fatalf("namespaceless key = %q", item)
	}
	item, _ = queue.Get()
	if item != "jobruns:finance/" {
		t.Fatalf("nameless key = %q", item)
	}
}

func TestRunRequiresClientsAndHandler(t *testing.T) {
	if err := (Controller{}).Run(context.Background()); err == nil {
		t.Fatal("a controller without a dynamic client must refuse to start")
	}
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	if err := (Controller{Dynamic: client}).Run(context.Background()); err == nil {
		t.Fatal("a controller without a reconcile handler must refuse to start")
	}
}

func TestRunReturnsPromptlyWhenContextIsAlreadyCancelled(t *testing.T) {
	// A cancelled context means there is nothing to watch: Run must return the
	// context error rather than waiting for informers that will never sync.
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	controller := Controller{
		Dynamic:   client,
		Config:    Config{Resync: time.Hour, Workers: 1},
		Reconcile: func(context.Context, string, unstructured.Unstructured, bool) error { return nil },
		Log:       discardLogger(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() { done <- controller.Run(ctx) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return for a cancelled context")
	}
}

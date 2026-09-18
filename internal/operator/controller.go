package operator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"orbitjob/internal/platform/metrics"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicinformer "k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

var (
	scheduledJobGVR = schema.GroupVersionResource{Group: "workloads.orbitjob.io", Version: "v1alpha1", Resource: "scheduledjobs"}
	jobRunGVR       = schema.GroupVersionResource{Group: "workloads.orbitjob.io", Version: "v1alpha1", Resource: "jobruns"}
	jobGVR          = schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}
	workflowRunGVR  = schema.GroupVersionResource{Group: "workloads.orbitjob.io", Version: "v1alpha1", Resource: "workflowruns"}
	workflowJobGVR  = schema.GroupVersionResource{Group: "workloads.orbitjob.io", Version: "v1alpha1", Resource: "workflowjobs"}
)

// watchedResources are the objects the control plane reconciles. The key prefix
// keeps a ScheduledJob and a JobRun of the same namespace/name apart.
var watchedResources = []schema.GroupVersionResource{scheduledJobGVR, jobRunGVR, jobGVR, workflowRunGVR, workflowJobGVR}

// reconcileBudget bounds one pass. Every read and write it makes honours this
// deadline, so a wedged connection or a stuck database turns into a failed
// reconcile that is retried, not a worker that hangs forever and starves the
// queue behind it in silence.
const reconcileBudget = 60 * time.Second

type Config struct {
	// Resync re-delivers every watched object periodically. It is the safety net
	// for events dropped while the operator was down.
	Resync time.Duration
	// Workers bounds concurrent reconciles. Each reconcile may touch PostgreSQL
	// and the Kubernetes API, so this is a load knob, not a throughput knob.
	Workers int
	// Budget bounds one reconcile pass; zero means the default (reconcileBudget).
	// A pass that outlives it fails and is retried, which is what keeps a wedged
	// read from pinning a worker forever.
	Budget time.Duration
}

// ReconcileHandler processes one resource key of the form "<resource>:<ns>/<name>".
// obj is the state the informer last observed for the key, and cached reports
// whether it could be served from the informer's store. When cached is false
// (the object was deleted while queued, or the handler was invoked outside the
// watch path) the handler reads the API server itself.
type ReconcileHandler func(context.Context, string, unstructured.Unstructured, bool) error

// Controller watches the control plane resources and drives reconciliation.
type Controller struct {
	Kubernetes kubernetes.Interface
	Dynamic    dynamic.Interface
	Config     Config
	// Reconcile is required. A nil handler is a wiring defect, and silently
	// starting an operator that reconciles nothing would look healthy while
	// doing no work.
	Reconcile ReconcileHandler
	Log       *slog.Logger
}

// objectCache serves the last state the informers observed, keyed by the
// resource prefix of the reconcile key. Reads hit the local store, so the
// reconcile hot path pays no apiserver round trip per event -- under a burst
// that round trip is what leaves the queue draining slower than it fills,
// starving the keys enqueued last. A miss (the object was deleted while
// queued, or the resource is not watched) sends the handler to the API server.
type objectCache struct {
	byResource map[string]cache.SharedIndexInformer
}

// get returns the stored object for the key and whether one was found. A store
// error is reported as not-found rather than failed: the handler's fallback
// read decides authoritatively, and a transient index error must not fail a
// reconcile that would have succeeded.
func (o objectCache) get(resource, namespace, name string) (unstructured.Unstructured, bool) {
	informer, ok := o.byResource[resource]
	if !ok {
		return unstructured.Unstructured{}, false
	}
	obj, exists, err := informer.GetStore().GetByKey(namespace + "/" + name)
	if err != nil || !exists {
		return unstructured.Unstructured{}, false
	}
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return unstructured.Unstructured{}, false
	}
	return *u, true
}

// Run watches until ctx is cancelled. It returns only after in-flight reconciles
// finish, so a graceful shutdown does not abandon a half-written attempt.
func (c Controller) Run(ctx context.Context) error {
	if c.Dynamic == nil {
		return fmt.Errorf("dynamic client is required")
	}
	if c.Reconcile == nil {
		return fmt.Errorf("reconcile handler is required")
	}
	log := c.Log
	if log == nil {
		log = slog.Default()
	}

	factory := dynamicinformer.NewDynamicSharedInformerFactory(c.Dynamic, c.Config.Resync)
	// Per-item exponential backoff: a reconcile that fails for a persistent
	// reason backs off instead of spinning.
	queue := workqueue.NewTypedRateLimitingQueue(
		workqueue.DefaultTypedControllerRateLimiter[string](),
	)

	byResource := make(map[string]cache.SharedIndexInformer, len(watchedResources))
	informers := make([]cache.SharedIndexInformer, 0, len(watchedResources))
	for _, gvr := range watchedResources {
		gvr := gvr
		informer := factory.ForResource(gvr).Informer()
		informers = append(informers, informer)
		if _, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    func(obj any) { enqueue(queue, gvr, obj) },
			UpdateFunc: func(_, obj any) { enqueue(queue, gvr, obj) },
			DeleteFunc: func(obj any) { enqueue(queue, gvr, obj) },
		}); err != nil {
			return fmt.Errorf("watch %s: %w", gvr.Resource, err)
		}
		byResource[gvr.Resource] = informer
	}
	watched := objectCache{byResource: byResource}

	factory.Start(ctx.Done())

	synced := make([]cache.InformerSynced, 0, len(informers))
	for _, informer := range informers {
		synced = append(synced, informer.HasSynced)
	}
	if !cache.WaitForCacheSync(ctx.Done(), synced...) {
		return ctx.Err()
	}
	log.Info("operator cache synced", "resources", len(watchedResources))

	workers := c.Config.Workers
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.worker(ctx, queue, log, watched)
		}()
	}

	<-ctx.Done()
	queue.ShutDown()
	wg.Wait()
	return nil
}

func (c Controller) worker(ctx context.Context, queue workqueue.TypedRateLimitingInterface[string], log *slog.Logger, watched objectCache) {
	for {
		key, shutdown := queue.Get()
		if shutdown {
			return
		}
		c.reconcileOne(ctx, queue, log, watched, key)
		queue.Done(key)
	}
}

func (c Controller) reconcileOne(ctx context.Context, queue workqueue.TypedRateLimitingInterface[string], log *slog.Logger, watched objectCache, key string) {
	// The key is "<resource>:<namespace>/<name>" (see enqueue below). Only the
	// resource half is a label: it is a bounded set of five, where the name
	// would grow without bound.
	resource, nsname, _ := strings.Cut(key, ":")
	namespace, name, _ := strings.Cut(nsname, "/")
	obj, cached := watched.get(resource, namespace, name)

	// shutdown is read off the parent context: the budget below gives the child
	// a deadline of its own, and a deadline that expired mid-reconcile must
	// requeue the work instead of being mistaken for a stop.
	shutdown := ctx.Err() != nil
	budget := c.Config.Budget
	if budget <= 0 {
		budget = reconcileBudget
	}
	reconcileCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	started := time.Now()
	err := c.Reconcile(reconcileCtx, key, obj, cached)
	metrics.OperatorReconcileDuration.WithLabelValues(resource).Observe(time.Since(started).Seconds())

	switch {
	case err == nil:
		// Drop the backoff history so the next failure starts from the shortest
		// delay.
		queue.Forget(key)
	case shutdown:
		// Shutting down: do not requeue, the work is retried on next start. Not
		// counted as a reconcile error -- the pass did not fail, it was stopped.
		queue.Forget(key)
	default:
		queue.AddRateLimited(key)
		metrics.OperatorReconcileErrorsTotal.WithLabelValues(resource).Inc()
		log.Error("reconcile failed", "key", key, "error", err)
	}
}

func enqueue(queue workqueue.TypedRateLimitingInterface[string], gvr schema.GroupVersionResource, obj any) {
	// meta.Accessor, not a direct metav1.Object assertion, is the canonical
	// event-handler idiom: it reaches the metadata of every shape the informer
	// machinery hands over, and a failed assertion here would drop the event
	// silently -- an operator that reconciles nothing but logs no errors.
	// DeletedFinalStateUnknown is not itself a metav1.Object (and meta.Accessor
	// cannot know client-go's tombstone type), so its inner object is unwrapped
	// explicitly.
	object, err := meta.Accessor(obj)
	if err != nil {
		if tombstone, isTombstone := obj.(cache.DeletedFinalStateUnknown); isTombstone {
			object, err = meta.Accessor(tombstone.Obj)
		}
		if err != nil {
			return
		}
	}
	queue.Add(gvr.Resource + ":" + object.GetNamespace() + "/" + object.GetName())
}

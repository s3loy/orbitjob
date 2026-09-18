package operator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"orbitjob/internal/platform/metrics"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

type Config struct {
	// Resync re-delivers every watched object periodically. It is the safety net
	// for events dropped while the operator was down.
	Resync time.Duration
	// Workers bounds concurrent reconciles. Each reconcile may touch PostgreSQL
	// and the Kubernetes API, so this is a load knob, not a throughput knob.
	Workers int
}

// ReconcileHandler processes one resource key of the form "<resource>:<ns>/<name>".
type ReconcileHandler func(context.Context, string) error

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

	for _, gvr := range watchedResources {
		gvr := gvr
		handler := cache.ResourceEventHandlerFuncs{
			AddFunc:    func(obj any) { enqueue(queue, gvr, obj) },
			UpdateFunc: func(_, obj any) { enqueue(queue, gvr, obj) },
			DeleteFunc: func(obj any) { enqueue(queue, gvr, obj) },
		}
		if _, err := factory.ForResource(gvr).Informer().AddEventHandler(handler); err != nil {
			return fmt.Errorf("watch %s: %w", gvr.Resource, err)
		}
	}

	factory.Start(ctx.Done())

	synced := make([]cache.InformerSynced, 0, len(watchedResources))
	for _, gvr := range watchedResources {
		synced = append(synced, factory.ForResource(gvr).Informer().HasSynced)
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
			c.worker(ctx, queue, log)
		}()
	}

	<-ctx.Done()
	queue.ShutDown()
	wg.Wait()
	return nil
}

func (c Controller) worker(ctx context.Context, queue workqueue.TypedRateLimitingInterface[string], log *slog.Logger) {
	for {
		key, shutdown := queue.Get()
		if shutdown {
			return
		}
		c.reconcileOne(ctx, queue, log, key)
		queue.Done(key)
	}
}

func (c Controller) reconcileOne(ctx context.Context, queue workqueue.TypedRateLimitingInterface[string], log *slog.Logger, key string) {
	// The key is "<resource>:<namespace>/<name>" (see enqueue below). Only the
	// resource half is a label: it is a bounded set of three, where the name
	// would grow without bound.
	resource, _, _ := strings.Cut(key, ":")

	started := time.Now()
	err := c.Reconcile(ctx, key)
	metrics.OperatorReconcileDuration.WithLabelValues(resource).Observe(time.Since(started).Seconds())

	switch {
	case err == nil:
		// Drop the backoff history so the next failure starts from the shortest
		// delay.
		queue.Forget(key)
	case ctx.Err() != nil:
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
	meta, ok := obj.(metav1.Object)
	if !ok {
		// Deleted-object tombstones still carry metadata; anything else cannot
		// be addressed and is dropped rather than enqueued as a bad key.
		tombstone, isTombstone := obj.(cache.DeletedFinalStateUnknown)
		if !isTombstone {
			return
		}
		meta, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			return
		}
	}
	queue.Add(gvr.Resource + ":" + meta.GetNamespace() + "/" + meta.GetName())
}

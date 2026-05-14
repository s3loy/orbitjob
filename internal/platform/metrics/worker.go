package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// WorkerCapacity tracks the current adaptive capacity of a worker.
	WorkerCapacity = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "orbitjob_worker_capacity",
		Help: "Current adaptive capacity of the worker, labeled by worker_id and tenant_id.",
	}, []string{"worker_id", "tenant_id"})

	// WorkerLeaseDuration tracks the current dynamic lease duration in seconds.
	WorkerLeaseDuration = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "orbitjob_worker_lease_duration_seconds",
		Help: "Current dynamic lease duration in seconds, labeled by worker_id and tenant_id.",
	}, []string{"worker_id", "tenant_id"})

	// WorkerProbeRTTSeconds tracks DB probe round-trip time.
	WorkerProbeRTTSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "orbitjob_worker_probe_rtt_seconds",
		Help:    "DB probe RTT in seconds, labeled by worker_id and tenant_id.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
	}, []string{"worker_id", "tenant_id"})

	// WorkerQueueDepth tracks the number of dispatched instances waiting.
	WorkerQueueDepth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "orbitjob_worker_queue_depth",
		Help: "Number of dispatched instances waiting, labeled by worker_id and tenant_id.",
	}, []string{"worker_id", "tenant_id"})

	// WorkerIdleTicksTotal counts idle ticks where no work was found.
	WorkerIdleTicksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_worker_idle_ticks_total",
		Help: "Total idle ticks (no work dispatched), labeled by worker_id and tenant_id.",
	}, []string{"worker_id", "tenant_id"})
)

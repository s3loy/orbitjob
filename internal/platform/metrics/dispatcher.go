package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// DispatcherTickDuration tracks dispatcher tick durations by tenant.
	DispatcherTickDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "orbitjob_dispatcher_tick_duration_seconds",
		Help:    "Dispatcher tick duration in seconds, labeled by tenant_id.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 10},
	}, []string{"tenant_id"})

	// DispatcherDispatchTotal counts dispatch decisions by tenant and action.
	DispatcherDispatchTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_dispatcher_dispatch_total",
		Help: "Total dispatch decisions, labeled by tenant_id and action (dispatch|skip|replace).",
	}, []string{"tenant_id", "action"})

	// DispatcherOrphanRecoveryTotal counts recovered orphans by tenant and type.
	DispatcherOrphanRecoveryTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_dispatcher_orphan_recovery_total",
		Help: "Total orphan recoveries, labeled by tenant_id and type (dispatched|running|worker).",
	}, []string{"tenant_id", "type"})

	// DispatcherQueueDepth exposes the number of pending/retry_wait instances per tenant.
	DispatcherQueueDepth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "orbitjob_dispatcher_queue_depth",
		Help: "Number of pending + retry_wait instances, labeled by tenant_id.",
	}, []string{"tenant_id"})

	// DispatcherBatchClaimTotal counts batch claim attempts.
	DispatcherBatchClaimTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_dispatcher_batch_claim_total",
		Help: "Total batch claim attempts.",
	}, []string{"tenant_id"})

	// DispatcherBatchClaimEmptyTotal counts empty batch claims.
	DispatcherBatchClaimEmptyTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_dispatcher_batch_claim_empty_total",
		Help: "Total empty batch claims (no candidates).",
	}, []string{"tenant_id"})

	// DispatcherEventWakeTotal counts wakes from NOTIFY events.
	DispatcherEventWakeTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_dispatcher_event_wake_total",
		Help: "Total wakes from NOTIFY events.",
	}, []string{"tenant_id"})

	// DispatcherIdleTicksTotal counts idle ticks.
	DispatcherIdleTicksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_dispatcher_idle_ticks_total",
		Help: "Total idle ticks (no work dispatched).",
	}, []string{"tenant_id"})

	// DispatcherLongIntervalTotal counts switches to long interval mode.
	DispatcherLongIntervalTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_dispatcher_long_interval_total",
		Help: "Total switches to long interval mode.",
	}, []string{"tenant_id"})
)

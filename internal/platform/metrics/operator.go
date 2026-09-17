package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Operator metrics cover the component that turns declared revisions into
// Kubernetes Jobs. The reconciled resource is the only label any of them
// carries, and it is a bounded set of three (scheduledjobs|jobruns|jobs): a
// tenant id or a run name would make the series grow without bound, and the
// ledger of who ran what is the audit trail, not a metric.
var (
	// OperatorRunsCreatedTotal counts runs the operator materialized from a bare
	// JobRun Custom Resource. Scheduled runs are created by the scheduler, not
	// here, so this is the manual-trigger path only.
	OperatorRunsCreatedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orbitjob_operator_runs_created_total",
		Help: "Total job runs materialized by the operator from a manual trigger.",
	})

	// OperatorRunsFailedTotal counts runs the operator drove to the terminal
	// Failed phase because their attempt budget was spent.
	OperatorRunsFailedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orbitjob_operator_runs_failed_total",
		Help: "Total runs driven to Failed by the operator after exhausting their attempts.",
	})

	// OperatorReconcileErrorsTotal counts reconcile passes that returned an
	// error. It is the top-level signal that the operator is not converging.
	OperatorReconcileErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_operator_reconcile_errors_total",
		Help: "Total failed reconcile passes, labeled by watched resource (scheduledjobs|jobruns|jobs).",
	}, []string{"resource"})

	// OperatorJobCreationFailuresTotal counts failures to create or adopt the
	// Kubernetes Job for a run attempt. A reconcile error covers these too; this
	// separates them so a burst is visible without reading logs.
	OperatorJobCreationFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orbitjob_operator_job_creation_failures_total",
		Help: "Total failures to ensure a Kubernetes Job for a run attempt.",
	})

	// OperatorReconcileDuration tracks how long a reconcile pass takes. A pass
	// may touch PostgreSQL and the Kubernetes API, so a rising tail is the
	// earliest sign of backing pressure.
	OperatorReconcileDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "orbitjob_operator_reconcile_duration_seconds",
		Help:    "Reconcile pass duration in seconds, labeled by watched resource (scheduledjobs|jobruns|jobs).",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10},
	}, []string{"resource"})
)

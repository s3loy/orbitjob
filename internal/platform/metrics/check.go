package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// CheckRunsCreatedTotal counts the number of check runs created by the scheduler.
	CheckRunsCreatedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_check_runs_created_total",
		Help: "Total number of check runs created by the check scheduler.",
	}, []string{"tenant_id"})

	// CheckRunsCompletedTotal counts completed check runs by tenant, check type, and severity.
	CheckRunsCompletedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_check_runs_completed_total",
		Help: "Total number of check runs completed by the check worker.",
	}, []string{"tenant_id", "check_type", "severity"})

	// CheckRunDurationSeconds records check run execution duration.
	CheckRunDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "orbitjob_check_run_duration_seconds",
		Help:    "Duration of check run execution in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"tenant_id", "check_type"})
)

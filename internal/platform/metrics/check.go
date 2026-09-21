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

	// CheckRunsCompletedTotal counts check runs that reached a terminal
	// outcome. outcome is success or failed, the exit-code-only evaluation v1
	// ships; canceled runs are a human decision and are deliberately absent.
	// The producer is the operator's terminal-phase bookkeeping.
	CheckRunsCompletedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_check_runs_completed_total",
		Help: "Total number of check runs that reached a terminal outcome.",
	}, []string{"tenant_id", "check_type", "outcome"})

	// CheckRunDurationSeconds observes the wall-clock span of a check run's
	// newest attempt, from the operator's terminal-phase bookkeeping. Labels
	// deliberately carry no severity: v1 evaluation is exit-code-only.
	CheckRunDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "orbitjob_check_run_duration_seconds",
		Help:    "Duration of check run executions in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"tenant_id", "check_type"})
)

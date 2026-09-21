package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// FunctionInvocationsTotal counts function invocations the invoke use
	// case accepted, labeled by tenant_id and outcome: created for an
	// invocation whose JobRun the API server accepted as a new object,
	// deduplicated for a replay that adopted the run a previous attempt
	// already published. Invocations refused before the publish (unknown or
	// paused definition, unmaterialized revision) are not counted: the series
	// counts invocations that exist, not requests that failed.
	FunctionInvocationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_function_invocations_total",
		Help: "Total function invocations the invoke use case accepted, labeled by tenant_id and outcome (created or deduplicated).",
	}, []string{"tenant_id", "outcome"})

	// FunctionDurationSeconds observes the invoke use case's own span: from
	// the invoke call to the JobRun Custom Resource being accepted by the API
	// server, labeled by tenant_id. It is the function twin of the manual
	// trigger's latency histogram and measures the API half only — the
	// invocation's execution duration is a terminal outcome of the run, not a
	// property of the request that created it.
	FunctionDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "orbitjob_function_duration_seconds",
		Help:    "Duration of function invocations in seconds, from invoke request to the JobRun being accepted, labeled by tenant_id.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
	}, []string{"tenant_id"})
)

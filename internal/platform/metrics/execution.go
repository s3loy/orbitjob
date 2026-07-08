package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ExecutionsTotal counts handler executions by result.
	ExecutionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_executions_total",
		Help: "Total number of handler executions, labeled by handler_type and result.",
	}, []string{"handler_type", "result"})

	// ExecutionsActive tracks the number of in-flight task executions.
	ExecutionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "orbitjob_executions_active",
		Help: "Number of task executions currently in progress.",
	})

	// HandlerExecutionDuration tracks how long a handler takes to execute.
	HandlerExecutionDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "orbitjob_handler_execution_duration_seconds",
		Help:    "Handler execution duration by handler type and result code.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30},
	}, []string{"handler_type", "result_code"})

	// LeaseExtensionFailuresTotal counts failed lease extension attempts.
	LeaseExtensionFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orbitjob_lease_extension_failures_total",
		Help: "Total number of failed lease extension attempts.",
	})

	// WorkersActive tracks the number of workers currently online.
	WorkersActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "orbitjob_workers_active",
		Help: "Number of workers currently online.",
	})
)

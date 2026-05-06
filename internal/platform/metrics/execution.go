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

	// LeaseExtensionFailuresTotal counts failed lease extension attempts.
	LeaseExtensionFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orbitjob_lease_extension_failures_total",
		Help: "Total number of failed lease extension attempts.",
	})
)

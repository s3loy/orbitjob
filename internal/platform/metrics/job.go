package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// JobsTotal counts the total number of jobs created.
	JobsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_jobs_total",
		Help: "Total number of jobs created",
	}, []string{"tenant_id", "trigger_type"})

	// TriggerLatency tracks the delay from manual trigger request to instance row creation.
	TriggerLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "orbitjob_trigger_latency_seconds",
		Help:    "Latency from manual trigger request to instance creation.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
	}, []string{"tenant_id"})
)

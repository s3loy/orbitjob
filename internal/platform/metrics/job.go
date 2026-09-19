package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// TriggerLatency tracks the delay from a manual trigger request to the
	// JobRun Custom Resource being accepted by the API server.
	//
	// It measures the API half only. The ledger row is written later by the
	// operator, and putting that latency in the same histogram would mix two
	// different waits — the operator's reconcile duration is its own metric.
	//
	// The tenant label is bounded by the number of tenants, which the platform
	// already bounds; it is not a per-run or per-object label.
	TriggerLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "orbitjob_trigger_latency_seconds",
		Help:    "Latency from a manual trigger request to the JobRun being accepted.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
	}, []string{"tenant_id"})
)

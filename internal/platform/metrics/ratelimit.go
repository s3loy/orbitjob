package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// RateLimitHits counts rate-limited requests by tenant and endpoint group.
	RateLimitHits = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_ratelimit_hits_total",
		Help: "Total rate-limited requests, labeled by tenant_id and endpoint_group.",
	}, []string{"tenant_id", "endpoint_group"})

	// RateLimitPassed counts allowed requests by tenant and endpoint group.
	RateLimitPassed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_ratelimit_passed_total",
		Help: "Total allowed requests, labeled by tenant_id and endpoint_group.",
	}, []string{"tenant_id", "endpoint_group"})
)

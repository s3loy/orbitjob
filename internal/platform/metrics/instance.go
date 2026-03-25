package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// InstancesTotal counts job instance state transitions.
	InstancesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_instances_total",
		Help: "Total job instance state transitions",
	}, []string{"tenant_id", "status"})
)

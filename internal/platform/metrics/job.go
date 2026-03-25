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
)

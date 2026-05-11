package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// SchedulerTickDuration tracks scheduler tick durations.
	SchedulerTickDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "orbitjob_scheduler_tick_duration_seconds",
		Help:    "Scheduler tick duration in seconds.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 10},
	})

	// SchedulerInstancesCreated counts instances created by the scheduler per tick.
	SchedulerInstancesCreated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orbitjob_scheduler_instances_created_total",
		Help: "Total instances created by the scheduler.",
	})

	// SchedulerCronErrors counts cron evaluation errors.
	SchedulerCronErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orbitjob_scheduler_cron_errors_total",
		Help: "Total cron evaluation errors.",
	})

	// ScheduleLag tracks the end-to-end delay from cron due time to instance creation.
	ScheduleLag = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "orbitjob_schedule_lag_seconds",
		Help:    "End-to-end delay from cron due time to instance execution start.",
		Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60, 300},
	})
)

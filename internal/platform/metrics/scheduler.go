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

	// SchedulerLimit tracks the current batch limit computed by the adaptive controller.
	SchedulerLimit = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "orbitjob_scheduler_limit",
		Help: "Current batch limit computed by the adaptive controller.",
	})

	// SchedulerPhase tracks the current operational phase (0=Discovery, 1=Steady, 2=Protect, 3=HalfOpen).
	SchedulerPhase = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "orbitjob_scheduler_phase",
		Help: "Current operational phase: 0=Discovery, 1=Steady, 2=Protect, 3=HalfOpen.",
	})

	// SchedulerProbeRTT tracks SELECT 1 probe round-trip time in seconds.
	SchedulerProbeRTT = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "orbitjob_scheduler_probe_rtt_seconds",
		Help:    "Probe RTT in seconds (SELECT 1).",
		Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
	})

	// SchedulerDBPressure tracks the Vegas dbPressure estimate (unitless, relative).
	SchedulerDBPressure = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "orbitjob_scheduler_db_pressure",
		Help: "Vegas estimated database pressure (unitless, relative).",
	})

	// SchedulerBreakerTransitions counts circuit breaker state transitions.
	SchedulerBreakerTransitions = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orbitjob_scheduler_breaker_transitions_total",
		Help: "Total circuit breaker state transitions.",
	})

	// SchedulerQueueDepth tracks the total number of active instances (backpressure signal).
	SchedulerQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "orbitjob_scheduler_queue_depth",
		Help: "Total active instances (pending + retry_wait + dispatched + running).",
	})

	// SchedulerIdleTicksTotal counts scheduler ticks that found no due jobs.
	SchedulerIdleTicksTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orbitjob_scheduler_idle_ticks_total",
		Help: "Total scheduler ticks with no due jobs found.",
	})

	// SchedulerIntervalMode indicates whether the scheduler is in short (0) or long (1) tick interval.
	SchedulerIntervalMode = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "orbitjob_scheduler_interval_mode",
		Help: "Current tick interval mode: 0=short, 1=long.",
	})

	// SchedulerDiscoveryIterations counts how many batch iterations the discovery phase ran.
	SchedulerDiscoveryIterations = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orbitjob_scheduler_discovery_iterations_total",
		Help: "Total discovery phase batch iterations.",
	})

	// SchedulerDiscoveryDuration tracks how long each discovery phase takes.
	SchedulerDiscoveryDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "orbitjob_scheduler_discovery_duration_seconds",
		Help:    "Discovery phase duration in seconds.",
		Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60},
	})
)

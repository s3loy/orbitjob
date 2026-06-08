package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// SLIEvaluationsTotal counts the number of SLI evaluations performed.
	SLIEvaluationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_sli_evaluations_total",
		Help: "Total number of SLI evaluations performed.",
	}, []string{"tenant_id", "sli_type", "result"})

	// SLOSnapshotIncrementsTotal counts pre-aggregated snapshot updates.
	SLOSnapshotIncrementsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_slo_snapshot_increments_total",
		Help: "Total number of SLI snapshot increment operations.",
	}, []string{"tenant_id", "sli_id"})

	// SLOBudgetBurnRate tracks the current burn rate for SLO budgets.
	SLOBudgetBurnRate = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "orbitjob_slo_budget_burn_rate",
		Help: "Current burn rate for SLO budgets.",
	}, []string{"tenant_id", "slo_id"})

	// SLOBudgetStatus tracks budget status (0=healthy, 1=at_risk, 2=exhausted).
	SLOBudgetStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "orbitjob_slo_budget_status",
		Help: "Current budget status: 0=healthy, 1=at_risk, 2=exhausted.",
	}, []string{"tenant_id", "slo_id"})

	// SLOBudgetAlertsTotal counts burn rate alerts triggered.
	SLOBudgetAlertsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_slo_budget_alerts_total",
		Help: "Total number of budget burn rate alerts triggered.",
	}, []string{"tenant_id", "alert_type"})

	// SLOEvaluationDuration records the duration of SLO evaluation cycles.
	SLOEvaluationDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "orbitjob_slo_evaluation_duration_seconds",
		Help:    "Duration of SLO evaluation cycles in seconds.",
		Buckets: prometheus.DefBuckets,
	})
)

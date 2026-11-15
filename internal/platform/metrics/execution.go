package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	ExecutionIsolatedRuns = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "orbitjob_execution_isolated_runs",
		Help: "Unresolved ledger runs in configured tenant scopes, labeled by phase (ExecutionUnknown|CancelUnknown). Each replica observes the same scopes; use max rather than sum across replicas.",
	}, []string{"phase"})
	ExecutionIsolationOldest = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "orbitjob_execution_isolation_oldest_timestamp_seconds",
		Help: "Oldest unresolved ledger obligation timestamp in configured tenant scopes, labeled by phase (ExecutionUnknown|CancelUnknown); zero means none in the last complete observation.",
	}, []string{"phase"})
	ExecutionIsolationObserved = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "orbitjob_execution_isolation_observed_timestamp_seconds",
		Help: "Time of the last complete execution isolation observation across configured tenant scopes; zero means no complete observation.",
	})
	ExecutionIsolationCollectionSuccess = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "orbitjob_execution_isolation_collection_success",
		Help: "Whether the latest execution isolation observation completed for every configured tenant scope (1 or 0). Failed observations preserve the previous snapshot.",
	})
)

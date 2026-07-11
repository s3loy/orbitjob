package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// APIKeysTotal counts the total number of API keys created.
	APIKeysTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_api_keys_total",
		Help: "Total number of API keys created",
	}, []string{"tenant_id"})

	// APIKeysRevokedTotal counts the total number of API keys revoked.
	APIKeysRevokedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_api_keys_revoked_total",
		Help: "Total number of API keys revoked",
	}, []string{"tenant_id"})
)

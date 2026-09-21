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

	// PoliciesTotal counts tenant-authored authorization policies created.
	// A tenant that can create policies can widen its own access up to the
	// ceiling its boundary allows, so the rate is worth watching.
	PoliciesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_policies_total",
		Help: "Total number of authorization policies created",
	}, []string{"tenant_id"})

	// RunCancelRequestsTotal counts cancel requests the admin API patched onto
	// a JobRun Custom Resource. It carries no labels on purpose: a tenant or
	// run-id label would grow the series without bound, and who canceled what
	// is the ledger's and the audit trail's question, not a metric's.
	RunCancelRequestsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orbitjob_run_cancel_requests_total",
		Help: "Total run cancel requests patched onto JobRun custom resources.",
	})

	// HTTPRequestsTotal counts every HTTP request the admin API serves. The
	// path label is the gin route template (for example /api/v1/jobs/:id), not
	// the raw request path, so ids cannot grow the series without bound; the
	// status label is the response status class the client received, which is
	// what makes a 5xx error rate alertable.
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_http_requests_total",
		Help: "Total HTTP requests served by the admin API, counted by request method, route path template, and response status code",
	}, []string{"method", "path", "status"})
)

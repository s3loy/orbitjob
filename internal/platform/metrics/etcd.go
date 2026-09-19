//go:build etcd

// Every symbol here is referenced only from code behind //go:build etcd
// (internal/platform/election). Without the tag on this file the metrics are
// registered in every shipped binary and never incremented, because the only
// writers are compiled out. The tag keeps the series out of the default build
// and alive for `make bench-etcd`.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// EtcdCampaignDuration tracks leader election campaign latency.
	EtcdCampaignDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "orbitjob_etcd_campaign_duration_seconds",
		Help:    "Leader election campaign latency in seconds.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 10},
	})

	// EtcdLockAcquireDuration tracks distributed lock acquire latency.
	EtcdLockAcquireDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "orbitjob_etcd_lock_acquire_duration_seconds",
		Help:    "Distributed lock acquire latency in seconds.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
	}, []string{"lock_name"})

	// EtcdSessionExpiresTotal counts etcd session expiry events.
	EtcdSessionExpiresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_etcd_session_expires_total",
		Help: "Total etcd session expiry events.",
	}, []string{"component"})

	// EtcdOperationErrorsTotal counts etcd operation errors.
	EtcdOperationErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orbitjob_etcd_operation_errors_total",
		Help: "Total etcd operation errors.",
	}, []string{"operation"})
)

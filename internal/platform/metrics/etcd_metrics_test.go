//go:build etcd

package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// Like the metrics they cover, these tests only compile behind the etcd tag
// (make bench-etcd); the default build must not register these series.

func TestEtcdCountersCountByLabel(t *testing.T) {
	const (
		component = "scheduler"
		operation = "campaign"
	)

	EtcdSessionExpiresTotal.WithLabelValues(component).Inc()
	EtcdOperationErrorsTotal.WithLabelValues(operation).Add(2)

	if got := testutil.ToFloat64(EtcdSessionExpiresTotal.WithLabelValues(component)); got != 1 {
		t.Fatalf("session expires = %v, want 1", got)
	}
	if got := testutil.ToFloat64(EtcdOperationErrorsTotal.WithLabelValues(operation)); got != 2 {
		t.Fatalf("operation errors = %v, want 2", got)
	}
}

func TestEtcdHistogramsRecordObservations(t *testing.T) {
	EtcdCampaignDuration.Observe(0.1)
	EtcdLockAcquireDuration.WithLabelValues("leader-election").Observe(0.01)

	if n := testutil.CollectAndCount(EtcdCampaignDuration); n != 1 {
		t.Fatalf("campaign duration series count = %d, want 1", n)
	}
	if n := testutil.CollectAndCount(EtcdLockAcquireDuration); n != 1 {
		t.Fatalf("lock acquire duration series count = %d, want 1", n)
	}
}

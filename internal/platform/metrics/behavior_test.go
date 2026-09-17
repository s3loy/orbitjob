package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// The metrics are registered globally by promauto, so every test uses label
// values unique to itself and reads back only its own series. That keeps the
// assertions exact regardless of test order.

// ---------------------------------------------------------------------------
// admin.go
// ---------------------------------------------------------------------------

func TestAdminCountersCountByTenant(t *testing.T) {
	const tenant = "metrics-admin-fixture"

	APIKeysTotal.WithLabelValues(tenant).Add(2)
	APIKeysRevokedTotal.WithLabelValues(tenant).Inc()
	PoliciesTotal.WithLabelValues(tenant).Add(3)

	if got := testutil.ToFloat64(APIKeysTotal.WithLabelValues(tenant)); got != 2 {
		t.Fatalf("api keys created = %v, want 2", got)
	}
	if got := testutil.ToFloat64(APIKeysRevokedTotal.WithLabelValues(tenant)); got != 1 {
		t.Fatalf("api keys revoked = %v, want 1", got)
	}
	if got := testutil.ToFloat64(PoliciesTotal.WithLabelValues(tenant)); got != 3 {
		t.Fatalf("policies created = %v, want 3", got)
	}
}

func TestRunCancelRequestsCounterCountsAsOneUnlabeledSeries(t *testing.T) {
	before := testutil.ToFloat64(RunCancelRequestsTotal)
	RunCancelRequestsTotal.Inc()
	RunCancelRequestsTotal.Add(2)

	if got := testutil.ToFloat64(RunCancelRequestsTotal); got != before+3 {
		t.Fatalf("cancel requests = %v, want %v", got, before+3)
	}
	// No labels on purpose: tenant or run-id labels would grow the series
	// without bound, so the counter must stay a single series.
	if n := testutil.CollectAndCount(RunCancelRequestsTotal); n != 1 {
		t.Fatalf("series count = %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// check.go
// ---------------------------------------------------------------------------

func TestCheckRunCreatedCounterCountsByTenant(t *testing.T) {
	const tenant = "metrics-check-fixture"

	CheckRunsCreatedTotal.WithLabelValues(tenant).Add(2)

	if got := testutil.ToFloat64(CheckRunsCreatedTotal.WithLabelValues(tenant)); got != 2 {
		t.Fatalf("check runs created = %v, want 2", got)
	}
}

// ---------------------------------------------------------------------------
// job.go
// ---------------------------------------------------------------------------

func TestTriggerLatencyHistogramFillsCustomBuckets(t *testing.T) {
	TriggerLatency.WithLabelValues("metrics-trigger-hist").Observe(0.05)

	expected := `
# HELP orbitjob_trigger_latency_seconds Latency from a manual trigger request to the JobRun being accepted.
# TYPE orbitjob_trigger_latency_seconds histogram
orbitjob_trigger_latency_seconds_bucket{tenant_id="metrics-trigger-hist",le="0.001"} 0
orbitjob_trigger_latency_seconds_bucket{tenant_id="metrics-trigger-hist",le="0.005"} 0
orbitjob_trigger_latency_seconds_bucket{tenant_id="metrics-trigger-hist",le="0.01"} 0
orbitjob_trigger_latency_seconds_bucket{tenant_id="metrics-trigger-hist",le="0.05"} 1
orbitjob_trigger_latency_seconds_bucket{tenant_id="metrics-trigger-hist",le="0.1"} 1
orbitjob_trigger_latency_seconds_bucket{tenant_id="metrics-trigger-hist",le="0.5"} 1
orbitjob_trigger_latency_seconds_bucket{tenant_id="metrics-trigger-hist",le="1"} 1
orbitjob_trigger_latency_seconds_bucket{tenant_id="metrics-trigger-hist",le="5"} 1
orbitjob_trigger_latency_seconds_bucket{tenant_id="metrics-trigger-hist",le="+Inf"} 1
orbitjob_trigger_latency_seconds_sum{tenant_id="metrics-trigger-hist"} 0.05
orbitjob_trigger_latency_seconds_count{tenant_id="metrics-trigger-hist"} 1
`
	if err := testutil.CollectAndCompare(TriggerLatency, strings.NewReader(expected)); err != nil {
		t.Fatalf("unexpected histogram output: %v", err)
	}
}

// ---------------------------------------------------------------------------
// operator.go
// ---------------------------------------------------------------------------

func TestOperatorCountersCount(t *testing.T) {
	beforeCreated := testutil.ToFloat64(OperatorRunsCreatedTotal)
	beforeFailed := testutil.ToFloat64(OperatorRunsFailedTotal)
	beforeCreationFailures := testutil.ToFloat64(OperatorJobCreationFailuresTotal)
	beforeReconcileErrors := testutil.ToFloat64(OperatorReconcileErrorsTotal.WithLabelValues("scheduledjobs"))

	OperatorRunsCreatedTotal.Inc()
	OperatorRunsFailedTotal.Inc()
	OperatorJobCreationFailuresTotal.Add(2)
	OperatorReconcileErrorsTotal.WithLabelValues("scheduledjobs").Add(3)

	if got := testutil.ToFloat64(OperatorRunsCreatedTotal); got != beforeCreated+1 {
		t.Fatalf("operator runs created = %v, want %v", got, beforeCreated+1)
	}
	if got := testutil.ToFloat64(OperatorRunsFailedTotal); got != beforeFailed+1 {
		t.Fatalf("operator runs failed = %v, want %v", got, beforeFailed+1)
	}
	if got := testutil.ToFloat64(OperatorJobCreationFailuresTotal); got != beforeCreationFailures+2 {
		t.Fatalf("job creation failures = %v, want %v", got, beforeCreationFailures+2)
	}
	if got := testutil.ToFloat64(OperatorReconcileErrorsTotal.WithLabelValues("scheduledjobs")); got != beforeReconcileErrors+3 {
		t.Fatalf("reconcile errors = %v, want %v", got, beforeReconcileErrors+3)
	}
}

func TestOperatorReconcileDurationRecordsPerResource(t *testing.T) {
	OperatorReconcileDuration.WithLabelValues("jobruns").Observe(0.01)

	if n := testutil.CollectAndCount(OperatorReconcileDuration); n != 1 {
		t.Fatalf("reconcile duration series count = %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// ratelimit.go
// ---------------------------------------------------------------------------

func TestRateLimitCountersCountByTenantAndEndpoint(t *testing.T) {
	const (
		tenant   = "metrics-ratelimit-fixture"
		endpoint = "trigger"
	)

	RateLimitHits.WithLabelValues(tenant, endpoint).Add(2)
	RateLimitPassed.WithLabelValues(tenant, endpoint).Add(5)

	if got := testutil.ToFloat64(RateLimitHits.WithLabelValues(tenant, endpoint)); got != 2 {
		t.Fatalf("rate limit hits = %v, want 2", got)
	}
	if got := testutil.ToFloat64(RateLimitPassed.WithLabelValues(tenant, endpoint)); got != 5 {
		t.Fatalf("rate limit passed = %v, want 5", got)
	}
}

// ---------------------------------------------------------------------------
// slo.go
// ---------------------------------------------------------------------------

func TestSLOCountersAndGaugesRecord(t *testing.T) {
	const (
		tenant = "metrics-slo-fixture"
		slo    = "slo-1"
	)

	SLIEvaluationsTotal.WithLabelValues(tenant, "latency", "success").Inc()
	SLOSnapshotIncrementsTotal.WithLabelValues(tenant, "sli-1").Add(2)
	SLOBudgetBurnRate.WithLabelValues(tenant, slo).Set(1.25)
	SLOBudgetStatus.WithLabelValues(tenant, slo).Set(2)
	SLOBudgetAlertsTotal.WithLabelValues(tenant, "burn_rate").Inc()

	if got := testutil.ToFloat64(SLIEvaluationsTotal.WithLabelValues(tenant, "latency", "success")); got != 1 {
		t.Fatalf("sli evaluations = %v, want 1", got)
	}
	if got := testutil.ToFloat64(SLOSnapshotIncrementsTotal.WithLabelValues(tenant, "sli-1")); got != 2 {
		t.Fatalf("snapshot increments = %v, want 2", got)
	}
	if got := testutil.ToFloat64(SLOBudgetBurnRate.WithLabelValues(tenant, slo)); got != 1.25 {
		t.Fatalf("burn rate = %v, want 1.25", got)
	}
	// The status gauge encodes healthy/at-risk/exhausted as 0/1/2.
	if got := testutil.ToFloat64(SLOBudgetStatus.WithLabelValues(tenant, slo)); got != 2 {
		t.Fatalf("budget status = %v, want 2 (exhausted)", got)
	}
	if got := testutil.ToFloat64(SLOBudgetAlertsTotal.WithLabelValues(tenant, "burn_rate")); got != 1 {
		t.Fatalf("budget alerts = %v, want 1", got)
	}
}

func TestSLOEvaluationDurationRecordsOneSeries(t *testing.T) {
	SLOEvaluationDuration.Observe(0.25)

	if n := testutil.CollectAndCount(SLOEvaluationDuration); n != 1 {
		t.Fatalf("evaluation duration series count = %d, want 1", n)
	}
}

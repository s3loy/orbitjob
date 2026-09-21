package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// Behavior tests for the function invoke metrics. The vectors are registered
// globally by promauto, so each test reads only the series whose tenant label
// is unique to it.

func TestFunctionInvocationCounterCountsByOutcome(t *testing.T) {
	const tenant = "metrics-fn-fixture"

	FunctionInvocationsTotal.WithLabelValues(tenant, "created").Add(2)
	FunctionInvocationsTotal.WithLabelValues(tenant, "deduplicated").Inc()

	if got := testutil.ToFloat64(FunctionInvocationsTotal.WithLabelValues(tenant, "created")); got != 2 {
		t.Fatalf("created invocations = %v, want 2", got)
	}
	if got := testutil.ToFloat64(FunctionInvocationsTotal.WithLabelValues(tenant, "deduplicated")); got != 1 {
		t.Fatalf("deduplicated invocations = %v, want 1", got)
	}
	// Two outcomes for the tenant, not a fresh series per call shape beyond
	// the documented pair.
	if n := testutil.CollectAndCount(FunctionInvocationsTotal); n != 2 {
		t.Fatalf("series count = %d, want exactly the created/deduplicated pair", n)
	}
}

func TestFunctionDurationHistogramFillsTriggerLatencyBuckets(t *testing.T) {
	FunctionDurationSeconds.WithLabelValues("metrics-fn-dur").Observe(0.05)

	expected := `
# HELP orbitjob_function_duration_seconds Duration of function invocations in seconds, from invoke request to the JobRun being accepted, labeled by tenant_id.
# TYPE orbitjob_function_duration_seconds histogram
orbitjob_function_duration_seconds_bucket{tenant_id="metrics-fn-dur",le="0.001"} 0
orbitjob_function_duration_seconds_bucket{tenant_id="metrics-fn-dur",le="0.005"} 0
orbitjob_function_duration_seconds_bucket{tenant_id="metrics-fn-dur",le="0.01"} 0
orbitjob_function_duration_seconds_bucket{tenant_id="metrics-fn-dur",le="0.05"} 1
orbitjob_function_duration_seconds_bucket{tenant_id="metrics-fn-dur",le="0.1"} 1
orbitjob_function_duration_seconds_bucket{tenant_id="metrics-fn-dur",le="0.5"} 1
orbitjob_function_duration_seconds_bucket{tenant_id="metrics-fn-dur",le="1"} 1
orbitjob_function_duration_seconds_bucket{tenant_id="metrics-fn-dur",le="5"} 1
orbitjob_function_duration_seconds_bucket{tenant_id="metrics-fn-dur",le="+Inf"} 1
orbitjob_function_duration_seconds_sum{tenant_id="metrics-fn-dur"} 0.05
orbitjob_function_duration_seconds_count{tenant_id="metrics-fn-dur"} 1
`
	if err := testutil.CollectAndCompare(FunctionDurationSeconds, strings.NewReader(expected)); err != nil {
		t.Fatalf("histogram shape drifted: %v", err)
	}
}

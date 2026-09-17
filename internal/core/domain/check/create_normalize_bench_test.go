package check

import (
	"testing"
	"time"
)

// benchSpec keeps benchmark results alive so the compiler cannot drop the calls.
var benchSpec CreateSpec

// NormalizeCreate is the admin boundary's whole input contract: one call
// validates and defaults every field of a check before the store ever sees
// it. Among the pure functions in this package it is the expensive one — cron
// parsing and timezone loading are real work — and every create pays it, so
// the benchmark tracks the accepting path for cron and interval schedules as
// well as the refusing path for a malformed cron expression. The timezone
// load is part of the measured cost: NormalizeCreate accepts any IANA name,
// and repeated creates with a named zone exercise that load.
func BenchmarkNormalizeCreate(b *testing.B) {
	description := "primary endpoint reachability with status assertion"
	labels := map[string]any{"team": "payments", "tier": "critical", "region": "eu-central"}
	rules := []AssertionRule{
		{Metric: "status", Operator: "==", Threshold: 200, Severity: "critical"},
		{Metric: "latency_ms", Operator: "<=", Threshold: 1000, Severity: "warning"},
	}
	now := time.Date(2026, 9, 18, 9, 30, 0, 0, time.UTC)

	b.Run("valid/cron-schedule", func(b *testing.B) {
		cron := "*/5 6-20 * * MON-FRI"
		in := CreateInput{
			Name:           "payments-api-health",
			Description:    &description,
			TenantID:       validTenantID,
			CheckType:      CheckTypeHTTPHealth,
			CheckConfig:    map[string]any{"url": "https://api.example.com/v1/health", "expected_status": 200},
			AssertionRules: rules,
			ScheduleType:   ScheduleTypeCron,
			CronExpr:       &cron,
			Timezone:       "Europe/Berlin",
			TimeoutSec:     10,
			RetryLimit:     2,
			Priority:       5,
			Labels:         labels,
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchSpec, _ = NormalizeCreate(now, in)
		}
	})

	b.Run("valid/interval-schedule", func(b *testing.B) {
		interval := 60
		in := CreateInput{
			Name:         "payments-api-health",
			TenantID:     validTenantID,
			CheckType:    CheckTypeHTTPHealth,
			CheckConfig:  map[string]any{"url": "https://api.example.com/v1/health"},
			ScheduleType: ScheduleTypeInterval,
			IntervalSec:  &interval,
			TimeoutSec:   10,
			RetryLimit:   2,
			Priority:     5,
			Labels:       labels,
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchSpec, _ = NormalizeCreate(now, in)
		}
	})

	b.Run("invalid/malformed-cron", func(b *testing.B) {
		cron := "*/5 6-20 * * FOOBAR"
		in := CreateInput{
			Name:         "payments-api-health",
			TenantID:     validTenantID,
			CheckType:    CheckTypeHTTPHealth,
			CheckConfig:  map[string]any{"url": "https://api.example.com/v1/health"},
			ScheduleType: ScheduleTypeCron,
			CronExpr:     &cron,
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchSpec, _ = NormalizeCreate(now, in)
		}
	})
}

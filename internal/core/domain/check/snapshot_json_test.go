package check

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"
)

// snapshotJSONKeys marshals v and returns the set of top-level JSON keys.
func snapshotJSONKeys(t *testing.T, snap Snapshot) map[string]struct{} {
	t.Helper()
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	keys := make(map[string]struct{}, len(decoded))
	for k := range decoded {
		keys[k] = struct{}{}
	}
	return keys
}

// TestSnapshotJSONKeySetFull locks the exact JSON shape of a fully populated
// snapshot. GetCheck marshals this struct straight into the HTTP response, so
// these names are the published API contract, not an implementation detail.
func TestSnapshotJSONKeySetFull(t *testing.T) {
	interval := 300
	cron := "*/5 * * * *"
	desc := "probe"
	next := time.Date(2026, 1, 4, 9, 30, 0, 0, time.UTC)
	snap := Snapshot{
		ID:             7,
		Name:           "api-health",
		Description:    &desc,
		TenantID:       "acme",
		Status:         StatusActive,
		CheckType:      CheckTypeHTTPHealth,
		CheckConfig:    map[string]any{"url": "http://example.com"},
		AssertionRules: []AssertionRule{{Metric: "status_code", Operator: "==", Threshold: 200, Severity: "warning"}},
		ScheduleType:   ScheduleTypeCron,
		CronExpr:       &cron,
		IntervalSec:    &interval,
		Timezone:       DefaultTimezone,
		TimeoutSec:     30,
		RetryLimit:     2,
		Priority:       5,
		Labels:         map[string]any{"team": "payments"},
		NextRunAt:      &next,
		Version:        3,
		CreatedAt:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	want := map[string]struct{}{
		"id": {}, "name": {}, "description": {}, "tenant_id": {}, "status": {},
		"check_type": {}, "check_config": {}, "assertion_rules": {},
		"schedule_type": {}, "cron_expr": {}, "interval_sec": {}, "timezone": {},
		"timeout_sec": {}, "retry_limit": {}, "priority": {}, "labels": {},
		"next_run_at": {}, "version": {}, "created_at": {}, "updated_at": {},
	}
	got := snapshotJSONKeys(t, snap)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("json keys = %v, want %v", sortedKeys(got), sortedKeys(want))
	}
}

// TestSnapshotJSONKeySetOmitsEmptyOptionals pins the omitempty behavior on the
// optional fields: a cron check carries no interval_sec and a check without a
// schedule or description must not publish those keys at all.
func TestSnapshotJSONKeySetOmitsEmptyOptionals(t *testing.T) {
	snap := Snapshot{
		ID:        7,
		Name:      "api-health",
		TenantID:  "acme",
		Status:    StatusPaused,
		CheckType: CheckTypeHTTPHealth,
		Timezone:  DefaultTimezone,
		Version:   1,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	for _, key := range []string{"description", "cron_expr", "interval_sec", "next_run_at"} {
		if _, present := snapshotJSONKeys(t, snap)[key]; present {
			t.Fatalf("key %q must be omitted when the field is unset", key)
		}
	}
}

// TestSnapshotJSONRoundTrip proves the tag set is lossless in both directions:
// a consumer decoding the response must recover every field exactly.
func TestSnapshotJSONRoundTrip(t *testing.T) {
	desc := "probe"
	next := time.Date(2026, 1, 4, 9, 30, 0, 0, time.UTC)
	snap := Snapshot{
		ID:             7,
		Name:           "api-health",
		Description:    &desc,
		TenantID:       "acme",
		Status:         StatusActive,
		CheckType:      CheckTypeHTTPHealth,
		CheckConfig:    map[string]any{"url": "http://example.com"},
		AssertionRules: []AssertionRule{{Metric: "latency_ms", Operator: "<", Threshold: 800, Severity: "critical"}},
		ScheduleType:   ScheduleTypeCron,
		CronExpr:       strPtr("*/5 * * * *"),
		Timezone:       DefaultTimezone,
		TimeoutSec:     30,
		RetryLimit:     2,
		Priority:       5,
		Labels:         map[string]any{"team": "payments"},
		NextRunAt:      &next,
		Version:        3,
		CreatedAt:      time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC),
	}

	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Snapshot
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, snap) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, snap)
	}
}

func strPtr(s string) *string { return &s }

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

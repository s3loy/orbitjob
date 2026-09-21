package checkrun

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestStatusAndSeverityLiterals pins the persisted vocabulary. These strings go
// into the check_runs columns and are compared verbatim by the SLO evaluator
// (a run counts as good only when Status == "success", and severity gates
// latency/quality events), so renaming a constant's value silently orphans
// every historical row and flips SLO accounting.
func TestStatusAndSeverityLiterals(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"status pending", StatusPending, "pending"},
		{"status running", StatusRunning, "running"},
		{"status success", StatusSuccess, "success"},
		{"status failed", StatusFailed, "failed"},
		{"severity ok", SeverityOK, "ok"},
		{"severity warning", SeverityWarning, "warning"},
		{"severity critical", SeverityCritical, "critical"},
		{"severity unknown", SeverityUnknown, "unknown"},
	}
	seen := map[string]string{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("constant = %q, want %q", tt.got, tt.want)
			}
			if other, dup := seen[tt.got]; dup {
				t.Fatalf("constant value %q is shared with %s; the vocabularies must stay disjoint", tt.got, other)
			}
			seen[tt.got] = tt.name
		})
	}
}

func checkrunJSONKeys(t *testing.T, snap Snapshot) map[string]struct{} {
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

// TestSnapshotJSONKeySetFull locks the exact key set with every optional field
// populated, so a renamed tag or a dropped field fails here instead of at the
// consumer.
func TestSnapshotJSONKeySetFull(t *testing.T) {
	severity := SeverityCritical
	started := time.Date(2026, 1, 4, 0, 0, 5, 0, time.UTC)
	finished := started.Add(2 * time.Second)
	duration := 2000
	snap := Snapshot{
		ID:               42,
		RunID:            "run-abc",
		TenantID:         "acme",
		CheckID:          7,
		Status:           StatusFailed,
		Severity:         &severity,
		Output:           map[string]any{"status_code": 503},
		EvaluationResult: map[string]any{"passed": false},
		ScheduledAt:      time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC),
		StartedAt:        &started,
		FinishedAt:       &finished,
		DurationMs:       &duration,
		Version:          1,
		CreatedAt:        time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC),
	}

	want := map[string]struct{}{
		"id": {}, "run_id": {}, "tenant_id": {}, "check_id": {}, "status": {},
		"severity": {}, "output": {}, "evaluation_result": {}, "scheduled_at": {},
		"started_at": {}, "finished_at": {}, "duration_ms": {}, "version": {}, "created_at": {},
	}
	got := checkrunJSONKeys(t, snap)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("json keys = %v, want %v", sortedKeyList(got), sortedKeyList(want))
	}
}

// TestSnapshotJSONKeySetOmitsNilPointers pins the omitempty semantics: a run
// that never started publishes no started_at, finished_at, duration_ms or
// severity, while output and evaluation_result are always present.
func TestSnapshotJSONKeySetOmitsNilPointers(t *testing.T) {
	snap := Snapshot{
		ID:          42,
		RunID:       "run-abc",
		TenantID:    "acme",
		CheckID:     7,
		Status:      StatusPending,
		ScheduledAt: time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC),
	}
	keys := checkrunJSONKeys(t, snap)
	for _, key := range []string{"severity", "started_at", "finished_at", "duration_ms"} {
		if _, present := keys[key]; present {
			t.Fatalf("key %q must be omitted when the field is nil", key)
		}
	}
	for _, key := range []string{"output", "evaluation_result"} {
		if _, present := keys[key]; !present {
			t.Fatalf("key %q must always be present", key)
		}
	}
}

// A zero duration measured from real pointers is a fact about the run and must
// still be published; only an absent measurement is omitted.
func TestSnapshotJSONZeroDurationIsPublished(t *testing.T) {
	duration := 0
	snap := Snapshot{
		RunID:       "run-abc",
		ScheduledAt: time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC),
		DurationMs:  &duration,
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `"duration_ms":0`; !strings.Contains(string(raw), want) {
		t.Fatalf("body %s must contain %s", raw, want)
	}
}

// TestSnapshotJSONRoundTrip proves the tags round-trip every field losslessly.
func TestSnapshotJSONRoundTrip(t *testing.T) {
	severity := SeverityOK
	started := time.Date(2026, 1, 4, 0, 0, 5, 0, time.UTC)
	finished := started.Add(time.Second)
	duration := 1000
	// Map values use float64/bool because that is what encoding/json decodes
	// back into any: the round trip below must be lossless against the wire
	// representation, not the in-memory one.
	snap := Snapshot{
		ID:               42,
		RunID:            "run-abc",
		TenantID:         "acme",
		CheckID:          7,
		Status:           StatusSuccess,
		Severity:         &severity,
		Output:           map[string]any{"status_code": float64(200)},
		EvaluationResult: map[string]any{"passed": true},
		ScheduledAt:      time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC),
		StartedAt:        &started,
		FinishedAt:       &finished,
		DurationMs:       &duration,
		Version:          2,
		CreatedAt:        time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC),
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

func sortedKeyList(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

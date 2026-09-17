package job

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestLifecycleConstantsAreDistinct pins the job vocabulary. The status and
// action strings are written to storage and compared when pause/resume is
// dispatched, so two constants collapsing into one value would make distinct
// states indistinguishable.
func TestLifecycleConstantsAreDistinct(t *testing.T) {
	groups := []struct {
		name   string
		values []string
	}{
		{"statuses", []string{StatusActive, StatusPaused}},
		{"actions", []string{ActionPause, ActionResume}},
		{"trigger types", []string{TriggerTypeCron, TriggerTypeManual}},
		{"handler types", []string{HandlerTypeExec, HandlerTypeHTTP, HandlerTypeWebhook, HandlerTypePGNotify, HandlerTypeContainer}},
		{"concurrency policies", []string{ConcurrencyAllow, ConcurrencyForbid, ConcurrencyReplace}},
		{"misfire policies", []string{MisfireSkip, MisfireFireNow, MisfireCatchUp}},
	}
	for _, g := range groups {
		t.Run(g.name, func(t *testing.T) {
			seen := map[string]bool{}
			for _, v := range g.values {
				if v == "" {
					t.Errorf("empty constant in %s", g.name)
				}
				if seen[v] {
					t.Errorf("constant value %q appears twice in %s", v, g.name)
				}
				seen[v] = true
			}
		})
	}
	for literal, got := range map[string]string{
		"active": StatusActive, "paused": StatusPaused,
		"pause": ActionPause, "resume": ActionResume,
	} {
		if got != literal {
			t.Errorf("constant = %q, want persisted literal %q", got, literal)
		}
	}
}

func jobJSONKeys(t *testing.T, snap Snapshot) map[string]struct{} {
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

// TestSnapshotJSONKeySet locks the persisted job state's JSON shape. Unlike the
// check snapshot there is no omitempty: next_run_at must publish as null when
// unset (a manual job has no next run, and consumers read the key rather than
// probing for it).
func TestSnapshotJSONKeySet(t *testing.T) {
	next := time.Date(2026, 1, 4, 9, 30, 0, 0, time.UTC)
	snap := Snapshot{
		ID:        7,
		Name:      "nightly-report",
		TenantID:  "acme",
		Status:    StatusActive,
		Version:   3,
		NextRunAt: &next,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}
	want := map[string]struct{}{
		"id": {}, "name": {}, "tenant_id": {}, "status": {}, "version": {},
		"next_run_at": {}, "created_at": {}, "updated_at": {},
	}
	got := jobJSONKeys(t, snap)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("json keys = %v, want %v", sortedKeys(got), sortedKeys(want))
	}

	snap.NextRunAt = nil
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"next_run_at":null`) {
		t.Fatalf("unset next_run_at must serialize as null, got %s", raw)
	}
}

// TestSnapshotJSONRoundTrip proves the tags are lossless in both directions.
func TestSnapshotJSONRoundTrip(t *testing.T) {
	snap := Snapshot{
		ID:        7,
		Name:      "nightly-report",
		TenantID:  "acme",
		Status:    StatusPaused,
		Version:   3,
		CreatedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC),
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

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

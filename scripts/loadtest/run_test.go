package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBurstScheduleHasExactCount(t *testing.T) {
	cfg, err := LoadConfig("../../test/load/config/standard.yaml")
	if err != nil {
		t.Fatal(err)
	}
	schedule := BuildPhaseSchedule(cfg, nil)
	if count := CountBurst(schedule.Events); count != 500 {
		t.Fatalf("burst count = %d, want 500", count)
	}
}

func TestBurstEventsFitSubmitWindow(t *testing.T) {
	cfg, err := LoadConfig("../../test/load/config/standard.yaml")
	if err != nil {
		t.Fatal(err)
	}
	schedule := BuildPhaseSchedule(cfg, nil)
	var first, last time.Duration
	for _, event := range schedule.Events {
		if event.Phase != "peak" || event.IdempotencyKey[:11] != "v020-burst-" {
			continue
		}
		if first == 0 || event.At < first {
			first = event.At
		}
		if event.At > last {
			last = event.At
		}
	}
	span := last - first
	if span > cfg.Burst.SubmitWithin {
		t.Fatalf("burst span = %s, want <= %s", span, cfg.Burst.SubmitWithin)
	}
}

func TestPhasesRunInOrder(t *testing.T) {
	cfg, err := LoadConfig("../../test/load/config/standard.yaml")
	if err != nil {
		t.Fatal(err)
	}
	schedule := BuildPhaseSchedule(cfg, nil)
	var prev time.Duration
	for _, event := range schedule.Events {
		if event.At < prev {
			t.Fatalf("event at %s precedes previous at %s", event.At, prev)
		}
		prev = event.At
	}
}

func TestBuildPhaseScheduleCyclesDefinitions(t *testing.T) {
	cfg := Config{
		Phases: []Phase{
			{Name: "test", Offset: 0, Duration: time.Minute, RatePerMinute: 5, MaxActive: 10},
		},
	}
	cases := []CreatedDefinition{
		{CaseID: "a", JobID: 1, Tenant: "t1"},
		{CaseID: "b", JobID: 2, Tenant: "t2"},
	}
	schedule := BuildPhaseSchedule(cfg, cases)
	if len(schedule.Events) != 5 {
		t.Fatalf("events = %d, want 5", len(schedule.Events))
	}
	want := []int64{1, 2, 1, 2, 1}
	seen := map[string]bool{}
	for i, ev := range schedule.Events {
		if ev.JobID != want[i] {
			t.Fatalf("event %d jobID = %d, want %d", i, ev.JobID, want[i])
		}
		if seen[ev.IdempotencyKey] {
			t.Fatalf("duplicate idempotency key %s", ev.IdempotencyKey)
		}
		seen[ev.IdempotencyKey] = true
	}
}

// waitForStats polls until want in-flight triggers settle or the deadline passes.
func waitForStats(t *testing.T, engine *RunEngine, settled int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		triggered, accepted, rejected, _ := engine.Stats()
		if accepted+rejected >= settled && triggered >= settled {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRunEngineCountsSkippedWhenAtCapacity(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"run_id":"r","job_id":1,"tenant_id":"t1","status":"pending","created":true}`)
	}))
	defer srv.Close()

	schedule := PhaseSchedule{Events: []TriggerEvent{
		{At: 0, JobID: 1, Tenant: "t1", Phase: "p", IdempotencyKey: "k1"},
		{At: 0, JobID: 2, Tenant: "t1", Phase: "p", IdempotencyKey: "k2"},
	}}
	engine := NewRunEngine(map[string]*APIClient{"t1": NewAPIClient(srv.URL, "k")}, schedule, map[string]int{"p": 1})
	start := time.Now()
	if err := engine.Run(context.Background(), func() time.Duration { return time.Since(start) }); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitForStats(t, engine, 1)

	triggered, accepted, rejected, skipped := engine.Stats()
	if triggered != 1 || skipped != 1 {
		t.Fatalf("triggered=%d skipped=%d, want 1/1 (scheduled = triggered + skipped)", triggered, skipped)
	}
	if accepted != 1 || rejected != 0 {
		t.Fatalf("accepted=%d rejected=%d, want 1/0", accepted, rejected)
	}
}

func TestRunEngineClassifiesRejections(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	schedule := PhaseSchedule{Events: []TriggerEvent{
		{At: 0, JobID: 1, Tenant: "t1", Phase: "p", IdempotencyKey: "k1"},
		{At: 0, JobID: 1, Tenant: "t1", Phase: "p", IdempotencyKey: "k2"},
		{At: 0, JobID: 0, Tenant: "t1", Phase: "p", IdempotencyKey: "k3"}, // placeholder: no client call
	}}
	engine := NewRunEngine(map[string]*APIClient{"t1": NewAPIClient(srv.URL, "k")}, schedule, map[string]int{})
	start := time.Now()
	if err := engine.Run(context.Background(), func() time.Duration { return time.Since(start) }); err != nil {
		t.Fatal(err)
	}
	waitForStats(t, engine, 3)

	_, accepted, rejected, skipped := engine.Stats()
	if accepted != 0 || rejected != 3 || skipped != 0 {
		t.Fatalf("accepted=%d rejected=%d skipped=%d, want 0/3/0", accepted, rejected, skipped)
	}
	breakdown := engine.Rejections()
	if breakdown.RateLimited != 2 {
		t.Fatalf("rate limited = %d, want 2", breakdown.RateLimited)
	}
	if breakdown.Other != 1 {
		t.Fatalf("other = %d, want 1 (placeholder job)", breakdown.Other)
	}
}

// Burst events are generated after phase events but scheduled inside the run
// window; the merged schedule must be time-ordered or the burst fires in a
// lump at the end of the run.
func TestBuildPhaseScheduleKeepsBurstInTimeOrder(t *testing.T) {
	cfg := Config{
		Phases: []Phase{
			{Name: "steady", Offset: 0, Duration: 10 * time.Minute, RatePerMinute: 2, MaxActive: 5},
			{Name: "peak", Offset: 10 * time.Minute, Duration: 10 * time.Minute, RatePerMinute: 2, MaxActive: 5},
		},
		Burst: BurstConfig{Phase: "peak", Offset: time.Minute, Count: 3, SubmitWithin: time.Minute},
	}
	cases := []CreatedDefinition{{CaseID: "a", JobID: 1, Tenant: "t1"}}

	schedule := BuildPhaseSchedule(cfg, cases)

	var prev time.Duration
	burstSeen := 0
	for i, ev := range schedule.Events {
		if ev.At < prev {
			t.Fatalf("event %d at %s precedes previous at %s", i, ev.At, prev)
		}
		prev = ev.At
		if strings.HasPrefix(ev.IdempotencyKey, "v020-burst-") {
			burstSeen++
			want := 10*time.Minute + time.Minute
			if ev.At < want || ev.At >= want+time.Minute {
				t.Fatalf("burst event at %s, want within [%s, %s)", ev.At, want, want+time.Minute)
			}
		}
	}
	if burstSeen != 3 {
		t.Fatalf("burst events = %d, want 3", burstSeen)
	}
}

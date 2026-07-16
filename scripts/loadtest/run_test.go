package main

import (
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

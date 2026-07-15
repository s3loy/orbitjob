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

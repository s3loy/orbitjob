package schedule

import (
	"context"
	"testing"
	"time"

	domain "orbitjob/internal/core/domain"
)

func TestPhaseString(t *testing.T) {
	tests := []struct {
		p    Phase
		want string
	}{
		{PhaseDiscovery, "discovery"},
		{PhaseSteady, "steady"},
		{PhaseProtect, "protect"},
		{PhaseHalfOpen, "halfopen"},
		{Phase(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.p.String(); got != tt.want {
			t.Errorf("Phase(%d).String() = %q, want %q", tt.p, got, tt.want)
		}
	}
}

// discovery stubs

type discStub struct {
	probes  []time.Duration
	batches []struct {
		handled int
		class   domain.ErrorClass
	}
	pIdx int
	bIdx int
}

func (s *discStub) probe(ctx context.Context) (time.Duration, error) {
	if s.pIdx >= len(s.probes) {
		return 0, nil
	}
	d := s.probes[s.pIdx]
	s.pIdx++
	return d, nil
}

func (s *discStub) runBatch(ctx context.Context, limit int) (int, domain.ErrorClass) {
	if s.bIdx >= len(s.batches) {
		return limit, domain.ClassNone
	}
	r := s.batches[s.bIdx]
	s.bIdx++
	return r.handled, r.class
}

func TestDiscovery_FindsCapacityNoCongestion(t *testing.T) {
	stub := &discStub{
		probes: make([]time.Duration, 20),
	}
	state := &ControllerState{MaxBatchSize: 500}
	d := NewDiscovery(stub.probe, stub.runBatch, state)

	ph := d.Run(context.Background())
	if ph != PhaseSteady {
		t.Fatalf("expected Steady, got %v", ph)
	}
	if state.Limit < 100 {
		t.Fatalf("expected Limit >= 100 after discovery, got %d", state.Limit)
	}
	if state.Limit > 500 {
		t.Fatalf("expected Limit <= 500, got %d", state.Limit)
	}
}

func TestDiscovery_StopsOnCongestion(t *testing.T) {
	stub := &discStub{
		probes: []time.Duration{
			1 * time.Millisecond,
			1 * time.Millisecond,
			1 * time.Millisecond,
			1 * time.Millisecond,
			1 * time.Millisecond,
			5 * time.Millisecond,
		},
		batches: []struct {
			handled int
			class   domain.ErrorClass
		}{
			{handled: 1, class: domain.ClassNone},
			{handled: 4, class: domain.ClassNone},
			{handled: 10, class: domain.ClassNone},
		},
	}
	state := &ControllerState{MaxBatchSize: 500}
	d := NewDiscovery(stub.probe, stub.runBatch, state)

	ph := d.Run(context.Background())
	if ph != PhaseSteady {
		t.Fatalf("expected Steady, got %v", ph)
	}
	if state.Limit != 8 {
		t.Fatalf("expected Limit=8 after congestion at 16, got %d", state.Limit)
	}
}

func TestDiscovery_BackoffWorthyExit(t *testing.T) {
	stub := &discStub{
		probes: []time.Duration{1 * time.Millisecond, 1 * time.Millisecond},
		batches: []struct {
			handled int
			class   domain.ErrorClass
		}{
			{handled: 0, class: domain.BackoffWorthy},
		},
	}
	state := &ControllerState{MaxBatchSize: 500}
	d := NewDiscovery(stub.probe, stub.runBatch, state)

	ph := d.Run(context.Background())
	if ph != PhaseSteady {
		t.Fatalf("expected Steady after BackoffWorthy, got %v", ph)
	}
	// limit was 1, halved to 0 — caller clamps downstream
}

func TestDiscovery_FatalImmediateProtect(t *testing.T) {
	stub := &discStub{
		probes: []time.Duration{1 * time.Millisecond, 1 * time.Millisecond},
		batches: []struct {
			handled int
			class   domain.ErrorClass
		}{
			{handled: 0, class: domain.FatalWorthy},
		},
	}
	state := &ControllerState{MaxBatchSize: 500}
	d := NewDiscovery(stub.probe, stub.runBatch, state)

	ph := d.Run(context.Background())
	if ph != PhaseProtect {
		t.Fatalf("expected Protect on Fatal, got %v", ph)
	}
}

func TestSteady_Accelerate(t *testing.T) {
	state := &ControllerState{
		Limit:        20,
		LongtermRtt:  2 * time.Millisecond,
		MaxBatchSize: 500,
	}
	newLimit, _ := UpdateSteady(state, 2*time.Millisecond, domain.ClassNone, 500)
	if newLimit != 23 {
		t.Fatalf("expected Limit=23 on accelerate, got %d", newLimit)
	}
}

func TestSteady_Decelerate(t *testing.T) {
	state := &ControllerState{
		Limit:        100,
		LongtermRtt:  2 * time.Millisecond,
		MaxBatchSize: 500,
	}
	newLimit, _ := UpdateSteady(state, 5*time.Millisecond, domain.ClassNone, 500)
	if newLimit != 90 {
		t.Fatalf("expected Limit=90 on decelerate, got %d", newLimit)
	}
}

func TestSteady_Stable(t *testing.T) {
	state := &ControllerState{
		Limit:        50,
		LongtermRtt:  3 * time.Millisecond,
		MaxBatchSize: 500,
	}
	newLimit, _ := UpdateSteady(state, 4*time.Millisecond, domain.ClassNone, 500)
	if newLimit != 47 {
		t.Fatalf("expected Limit=47 on moderate deceleration, got %d", newLimit)
	}
}

func TestSteady_AlphaCap(t *testing.T) {
	state := &ControllerState{
		Limit:        500,
		LongtermRtt:  2 * time.Millisecond,
		MaxBatchSize: 500,
	}
	newLimit, _ := UpdateSteady(state, 2*time.Millisecond, domain.ClassNone, 500)
	if newLimit != 500 {
		t.Fatalf("expected Limit=500 (capped), got %d", newLimit)
	}
}

func TestSteady_BackoffWorthyTriggersDecelerate(t *testing.T) {
	state := &ControllerState{
		Limit:        50,
		LongtermRtt:  2 * time.Millisecond,
		MaxBatchSize: 500,
	}
	newLimit, _ := UpdateSteady(state, 2*time.Millisecond, domain.BackoffWorthy, 500)
	if newLimit < 49 || newLimit > 50 {
		t.Fatalf("expected Limit=49-50 with BackoffWorthy (gradient~1.0), got %d", newLimit)
	}
}

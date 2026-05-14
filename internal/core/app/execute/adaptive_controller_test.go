package execute

import (
	"testing"
	"time"
)

func TestAdaptiveCapacity_InitialState(t *testing.T) {
	ac := NewAdaptiveCapacity(10, 3)
	if ac.Current() != 1 {
		t.Fatalf("expected Current()=1, got %d", ac.Current())
	}
	if ac.Max() != 10 {
		t.Fatalf("expected Max()=10, got %d", ac.Max())
	}
}

func TestAdaptiveCapacity_MaxCapacityClamp(t *testing.T) {
	ac := NewAdaptiveCapacity(0, 0)
	if ac.Max() != 1 {
		t.Fatalf("expected max clamped to 1, got %d", ac.Max())
	}
	if ac.Current() != 1 {
		t.Fatalf("expected Current()=1 after zero max, got %d", ac.Current())
	}
}

func TestAdaptiveCapacity_RttThresholdDefault(t *testing.T) {
	ac := NewAdaptiveCapacity(10, 0)
	// threshold <= 0 should default to 3
	// Verify by triggering an update with probeRtt that would exceed default threshold
	ac.Update(100*time.Millisecond, 0, 1, "w1", "t1")
	ac.Update(100*time.Millisecond, 0, 1, "w1", "t1")
	// longtermRtt ~= 100ms, threshold = 300ms
	// probeRtt = 500ms > 300ms → target halved
	cap := ac.Update(500*time.Millisecond, 10, 1, "w1", "t1")
	if cap >= 10 {
		t.Fatalf("expected capacity reduced under RTT pressure, got %d", cap)
	}
}

func TestAdaptiveCapacity_Update_FirstCall(t *testing.T) {
	ac := NewAdaptiveCapacity(10, 3)
	cap := ac.Update(50*time.Millisecond, 5, 1, "w1", "t1")
	if cap < 1 {
		t.Fatalf("expected capacity >= 1, got %d", cap)
	}
	if cap > 10 {
		t.Fatalf("expected capacity <= max, got %d", cap)
	}
}

func TestAdaptiveCapacity_Update_QueueDepthZero(t *testing.T) {
	ac := NewAdaptiveCapacity(10, 3)
	ac.Update(50*time.Millisecond, 0, 1, "w1", "t1")
	cap := ac.Update(50*time.Millisecond, 0, 1, "w1", "t1")
	// queueDepth=0 → target = max(0/1, 1) = 1
	if cap != 1 {
		t.Fatalf("expected capacity=1 when queueDepth=0, got %d", cap)
	}
}

func TestAdaptiveCapacity_Update_ActiveWorkersZero(t *testing.T) {
	ac := NewAdaptiveCapacity(10, 3)
	ac.Update(50*time.Millisecond, 100, 0, "w1", "t1")
	// activeWorkers=0 is clamped to 1, so target = ceil(100/1) = 100, clamped to max=10
	cap := ac.Current()
	if cap < 1 || cap > 10 {
		t.Fatalf("expected capacity in [1,10], got %d", cap)
	}
}

func TestAdaptiveCapacity_Update_TargetExceedsMax(t *testing.T) {
	ac := NewAdaptiveCapacity(5, 3)
	ac.Update(50*time.Millisecond, 100, 1, "w1", "t1")
	// target = ceil(100/1) = 100, clamped to max=5
	if ac.Current() > 5 {
		t.Fatalf("expected capacity <= 5, got %d", ac.Current())
	}
}

func TestAdaptiveCapacity_Update_RTTPressure(t *testing.T) {
	ac := NewAdaptiveCapacity(10, 3)
	// Establish baseline RTT of ~100ms
	ac.Update(100*time.Millisecond, 10, 1, "w1", "t1")
	ac.Update(100*time.Millisecond, 10, 1, "w1", "t1")
	// Now probeRtt = 500ms > 3 * 100ms = 300ms threshold
	cap := ac.Update(500*time.Millisecond, 10, 1, "w1", "t1")
	// Should have been halved from target
	if cap >= 10 {
		t.Fatalf("expected capacity reduced under RTT pressure, got %d", cap)
	}
}

func TestAdaptiveCapacity_Update_NoRttPressure(t *testing.T) {
	ac := NewAdaptiveCapacity(10, 3)
	// Establish baseline
	ac.Update(100*time.Millisecond, 10, 1, "w1", "t1")
	ac.Update(100*time.Millisecond, 10, 1, "w1", "t1")
	// Normal RTT, high queue depth → should increase capacity
	cap := ac.Update(100*time.Millisecond, 50, 1, "w1", "t1")
	if cap < 2 {
		t.Fatalf("expected capacity increased with queue depth, got %d", cap)
	}
}

func TestAdaptiveCapacity_Update_EMASmoothing(t *testing.T) {
	ac := NewAdaptiveCapacity(10, 3)
	// First call establishes baseline
	ac.Update(50*time.Millisecond, 10, 1, "w1", "t1")
	c1 := ac.Current()
	// Same conditions → capacity should not jump immediately due to EMA
	ac.Update(50*time.Millisecond, 10, 1, "w1", "t1")
	c2 := ac.Current()
	if c2 < c1 {
		t.Fatalf("expected capacity not to decrease with same conditions, got %d < %d", c2, c1)
	}
}

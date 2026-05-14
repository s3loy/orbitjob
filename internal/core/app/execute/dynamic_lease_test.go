package execute

import (
	"testing"
	"time"
)

func TestDynamicLease_Defaults(t *testing.T) {
	dl := NewDynamicLease(0, 0, 0)
	if dl.Current() != 10*time.Second {
		t.Fatalf("expected default min=10s, got %s", dl.Current())
	}
}

func TestDynamicLease_Current_BeforeRecord(t *testing.T) {
	dl := NewDynamicLease(5*time.Second, 5*time.Minute, 0.1)
	if got := dl.Current(); got != 5*time.Second {
		t.Fatalf("expected min duration before any record, got %s", got)
	}
}

func TestDynamicLease_RecordExecution_First(t *testing.T) {
	dl := NewDynamicLease(10*time.Second, 5*time.Minute, 0.1)
	dl.RecordExecution(30 * time.Second)
	// First record sets EMA directly → lease = 3 * 30s = 90s
	got := dl.Current()
	if got != 90*time.Second {
		t.Fatalf("expected lease=90s after first 30s record, got %s", got)
	}
}

func TestDynamicLease_RecordExecution_EMA(t *testing.T) {
	dl := NewDynamicLease(10*time.Second, 5*time.Minute, 0.5)
	dl.RecordExecution(10 * time.Second) // EMA = 10s, lease = 30s
	dl.RecordExecution(20 * time.Second) // EMA = 10*0.5 + 20*0.5 = 15s, lease = 45s
	got := dl.Current()
	if got != 45*time.Second {
		t.Fatalf("expected lease=45s after EMA convergence, got %s", got)
	}
}

func TestDynamicLease_RecordExecution_ZeroOrNegative(t *testing.T) {
	dl := NewDynamicLease(10*time.Second, 5*time.Minute, 0.1)
	dl.RecordExecution(0)
	dl.RecordExecution(-1 * time.Second)
	if got := dl.Current(); got != 10*time.Second {
		t.Fatalf("expected min duration after zero/negative records, got %s", got)
	}
}

func TestDynamicLease_Current_ClampedToMin(t *testing.T) {
	dl := NewDynamicLease(10*time.Second, 5*time.Minute, 0.1)
	dl.RecordExecution(1 * time.Second) // lease = 3s, clamped to 10s
	if got := dl.Current(); got != 10*time.Second {
		t.Fatalf("expected min lease 10s, got %s", got)
	}
}

func TestDynamicLease_Current_ClampedToMax(t *testing.T) {
	dl := NewDynamicLease(10*time.Second, 5*time.Minute, 0.1)
	dl.RecordExecution(10 * time.Minute) // lease = 30m, clamped to 5m
	if got := dl.Current(); got != 5*time.Minute {
		t.Fatalf("expected max lease 5m, got %s", got)
	}
}

func TestDynamicLease_MaxLessThanMin(t *testing.T) {
	dl := NewDynamicLease(10*time.Minute, 5*time.Second, 0.1)
	// max < min → max defaults to 5m
	if got := dl.Current(); got != 10*time.Minute {
		t.Fatalf("expected min=10m when max < min, got %s", got)
	}
}

func TestDynamicLease_Update(t *testing.T) {
	dl := NewDynamicLease(10*time.Second, 5*time.Minute, 0.1)
	lease := dl.Update(30*time.Second, "w1", "t1")
	if lease != 90*time.Second {
		t.Fatalf("expected lease=90s, got %s", lease)
	}
	// Verify EMA was recorded
	if dl.Current() != 90*time.Second {
		t.Fatalf("expected Current()=90s after Update, got %s", dl.Current())
	}
}

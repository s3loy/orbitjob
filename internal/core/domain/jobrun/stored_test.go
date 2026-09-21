package jobrun

import "testing"

// TestCanStartAttemptRefusesARunWithAnAttemptInFlight pins the invariant the
// JobRun reconciler depends on: a run whose latest attempt has started and has
// not been observed terminal must not be handed a second attempt. The stored run
// advances to Running or CreatingAttempt when the attempt starts, so those
// phases mean an attempt exists and another would execute the same occurrence
// concurrently.
func TestCanStartAttemptRefusesARunWithAnAttemptInFlight(t *testing.T) {
	tests := []struct {
		name  string
		run   StoredRun
		start bool
	}{
		{"pending with no attempt starts the first one", StoredRun{Phase: Pending, Attempt: 0, MaxAttempts: 3}, true},
		{"retry wait starts the next attempt", StoredRun{Phase: RetryWaiting, Attempt: 1, MaxAttempts: 3}, true},
		{"running attempt is not startable", StoredRun{Phase: Running, Attempt: 1, MaxAttempts: 3}, false},
		{"attempt being created is not startable", StoredRun{Phase: CreatingAttempt, Attempt: 1, MaxAttempts: 3}, false},
		{"exhausted attempts are not startable", StoredRun{Phase: RetryWaiting, Attempt: 3, MaxAttempts: 3}, false},
		{"terminal run is not startable", StoredRun{Phase: Succeeded, Attempt: 1, MaxAttempts: 3}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.run.CanStartAttempt(); got != tt.start {
				t.Fatalf("CanStartAttempt() = %v, want %v (phase %s, attempt %d of %d)",
					got, tt.start, tt.run.Phase, tt.run.Attempt, tt.run.MaxAttempts)
			}
		})
	}
}

func TestAttemptInFlightRequiresAStartedAttempt(t *testing.T) {
	if (StoredRun{Phase: Running, Attempt: 0}).AttemptInFlight() {
		t.Error("a run with no attempt cannot have one in flight")
	}
	if (StoredRun{Phase: Pending, Attempt: 1}).AttemptInFlight() {
		t.Error("Pending does not mean an attempt is in flight")
	}
	for _, phase := range []Phase{Running, CreatingAttempt} {
		if !(StoredRun{Phase: phase, Attempt: 1}).AttemptInFlight() {
			t.Errorf("%s with a started attempt must report an attempt in flight", phase)
		}
	}
}

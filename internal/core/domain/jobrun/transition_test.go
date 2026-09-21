package jobrun

import (
	"errors"
	"testing"
	"time"
)

var testNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func pendingRun() JobRun { return JobRun{ID: "run-1", Phase: Pending, Attempt: 0} }

func TestStartAttemptFromPendingAndRetryWaiting(t *testing.T) {
	for _, phase := range []Phase{Pending, RetryWaiting} {
		t.Run(string(phase), func(t *testing.T) {
			run := JobRun{ID: "run-1", Phase: phase}
			attempt := Attempt{JobRunID: "run-1", Number: 2}

			updated, started, err := StartAttempt(run, attempt, testNow)
			if err != nil {
				t.Fatal(err)
			}
			if updated.Phase != CreatingAttempt || updated.Attempt != 2 {
				t.Fatalf("run = %+v", updated)
			}
			if started.Phase != CreatingAttempt {
				t.Fatalf("attempt = %+v", started)
			}
			// The caller's values must not be mutated: reconcile may retry.
			if run.Attempt != 0 || attempt.Phase != "" {
				t.Fatal("StartAttempt mutated its inputs")
			}
		})
	}
}

func TestStartAttemptRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		run     JobRun
		attempt Attempt
		now     time.Time
	}{
		{"zero attempt number", pendingRun(), Attempt{JobRunID: "run-1", Number: 0}, testNow},
		{"negative attempt number", pendingRun(), Attempt{JobRunID: "run-1", Number: -1}, testNow},
		{"attempt for another run", pendingRun(), Attempt{JobRunID: "run-2", Number: 1}, testNow},
		{"run already running", JobRun{ID: "run-1", Phase: Running}, Attempt{JobRunID: "run-1", Number: 1}, testNow},
		{"run already terminal", JobRun{ID: "run-1", Phase: Succeeded}, Attempt{JobRunID: "run-1", Number: 1}, testNow},
		{"run being cancelled", JobRun{ID: "run-1", Phase: CancelRequested}, Attempt{JobRunID: "run-1", Number: 1}, testNow},
		// A zero clock means the caller forgot to pass time; accepting it would
		// stamp attempts with year one.
		{"zero clock", pendingRun(), Attempt{JobRunID: "run-1", Number: 1}, time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := StartAttempt(tt.run, tt.attempt, tt.now); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("error = %v, want ErrInvalidTransition", err)
			}
		})
	}
}

func TestRequestCancel(t *testing.T) {
	for _, phase := range []Phase{Pending, CreatingAttempt, Running, RetryWaiting} {
		t.Run(string(phase), func(t *testing.T) {
			updated, err := RequestCancel(JobRun{ID: "run-1", Phase: phase}, testNow)
			if err != nil {
				t.Fatal(err)
			}
			if updated.Phase != CancelRequested {
				t.Fatalf("phase = %s", updated.Phase)
			}
		})
	}
}

func TestRequestCancelRejects(t *testing.T) {
	if _, err := RequestCancel(pendingRun(), time.Time{}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("zero clock: %v", err)
	}
	// Already requested: repeating the request is not a state change and the
	// caller must not be able to loop on it.
	if _, err := RequestCancel(JobRun{Phase: CancelRequested}, testNow); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("already requested: %v", err)
	}
	if _, err := RequestCancel(JobRun{Phase: Succeeded}, testNow); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal run: %v", err)
	}
}

func TestConfirmCanceled(t *testing.T) {
	updated, err := ConfirmCanceled(JobRun{ID: "run-1", Phase: CancelRequested}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != Canceled {
		t.Fatalf("phase = %s", updated.Phase)
	}

	if _, err := ConfirmCanceled(JobRun{Phase: CancelRequested}, time.Time{}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("zero clock: %v", err)
	}
	// Confirming without a request would record a stop the platform never issued.
	if _, err := ConfirmCanceled(JobRun{Phase: Running}, testNow); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("running without request: %v", err)
	}
}

func TestStoredRunNextAttempt(t *testing.T) {
	if got := (StoredRun{Attempt: 0}).NextAttempt(); got != 1 {
		t.Fatalf("first attempt = %d", got)
	}
	if got := (StoredRun{Attempt: 4}).NextAttempt(); got != 5 {
		t.Fatalf("next attempt = %d", got)
	}
}

func TestStoredRunCanStartAttempt(t *testing.T) {
	tests := []struct {
		name  string
		state StoredRun
		want  bool
	}{
		{"first attempt", StoredRun{Phase: Pending, Attempt: 0, MaxAttempts: 1}, true},
		{"retry within budget", StoredRun{Phase: RetryWaiting, Attempt: 1, MaxAttempts: 3}, true},
		{"retry budget exhausted", StoredRun{Phase: RetryWaiting, Attempt: 3, MaxAttempts: 3}, false},
		{"already succeeded", StoredRun{Phase: Succeeded, Attempt: 1, MaxAttempts: 3}, false},
		{"already failed", StoredRun{Phase: Failed, Attempt: 3, MaxAttempts: 3}, false},
		{"cancel requested", StoredRun{Phase: CancelRequested, Attempt: 1, MaxAttempts: 3}, false},
		{"cancellation unconfirmed", StoredRun{Phase: CancelUnknown, Attempt: 1, MaxAttempts: 3}, false},
		// An attempt is already in flight in both phases: the run row advances to
		// them when the attempt starts and only leaves when its Job is observed.
		// Starting another would run the occurrence concurrently.
		{"running attempt is not startable", StoredRun{Phase: Running, Attempt: 1, MaxAttempts: 3}, false},
		{"creating attempt is not startable", StoredRun{Phase: CreatingAttempt, Attempt: 1, MaxAttempts: 3}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.CanStartAttempt(); got != tt.want {
				t.Fatalf("CanStartAttempt = %v, want %v", got, tt.want)
			}
		})
	}
}

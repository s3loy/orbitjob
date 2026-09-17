package jobrun

import (
	"errors"
	"testing"
)

func TestCanTransitionAcceptsEveryLegalEdge(t *testing.T) {
	legal := map[Phase][]Phase{
		Pending:         {CreatingAttempt, CancelRequested},
		CreatingAttempt: {Running, CancelRequested},
		Running:         {RetryWaiting, Succeeded, Failed, CancelRequested},
		RetryWaiting:    {CreatingAttempt, CancelRequested, Failed},
		CancelRequested: {Canceled, CancelUnknown},
	}
	for from, targets := range legal {
		for _, to := range targets {
			if !CanTransition(from, to) {
				t.Errorf("%s -> %s should be legal", from, to)
			}
		}
	}
}

func TestCanTransitionRejectsIllegalEdges(t *testing.T) {
	tests := []struct {
		from, to Phase
		why      string
	}{
		{Pending, Running, "a run must pass through attempt creation"},
		{Pending, Succeeded, "nothing ran, so it cannot have succeeded"},
		{CreatingAttempt, Succeeded, "an attempt must be observed running first"},
		{Running, Canceled, "a stop must be requested before it is confirmed"},
		{Running, CancelUnknown, "unknown cancellation requires a request first"},
		{Succeeded, Running, "terminal phases are final"},
		{Failed, RetryWaiting, "a failed run does not re-enter the retry queue"},
		{Canceled, Running, "a stopped run does not resume"},
		{CancelUnknown, Canceled, "an unconfirmed stop cannot become confirmed later"},
		{Pending, Pending, "self transitions are not progress"},
		{RetryWaiting, Succeeded, "the retry must actually run"},
	}
	for _, tt := range tests {
		if CanTransition(tt.from, tt.to) {
			t.Errorf("%s -> %s should be illegal: %s", tt.from, tt.to, tt.why)
		}
	}
}

func TestTerminal(t *testing.T) {
	for _, phase := range []Phase{Succeeded, Failed, Canceled} {
		if !Terminal(phase) {
			t.Errorf("%s should be terminal", phase)
		}
	}
	for _, phase := range []Phase{Pending, CreatingAttempt, Running, RetryWaiting, CancelRequested, CancelUnknown} {
		if Terminal(phase) {
			t.Errorf("%s should not be terminal", phase)
		}
	}
}

func TestTransitionReportsIllegalEdges(t *testing.T) {
	run := JobRun{ID: "run-1", Phase: Pending}
	moved, err := Transition(run, CreatingAttempt)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Phase != CreatingAttempt {
		t.Fatalf("phase = %s", moved.Phase)
	}

	unchanged, err := Transition(run, Succeeded)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("error = %v, want ErrInvalidTransition", err)
	}
	if unchanged.Phase != Pending {
		t.Fatal("a rejected transition must not mutate the run")
	}
}

package workflow

import (
	"testing"

	"orbitjob/internal/core/domain/jobrun"
)

// DerivePhase is the one place a workflow run's terminal outcome is decided,
// so its roll-up rules and their priority order are pinned here as a table:
// CancelUnknown dominates and keeps the run non-terminal; Failed beats
// Canceled beats Succeeded; skip decisions with reason canceled count as
// canceled; and a task with neither step nor decision holds the run open.

// taskSet builds the task-name slice DerivePhase walks.
func taskSet(names ...string) []string { return names }

// step is a terminal step state for one task.
func step(phase jobrun.Phase) StepState {
	return StepState{Phase: phase, Attempt: 1, DurationMs: 1000}
}

func TestDerivePhase(t *testing.T) {
	tests := []struct {
		name string
		// tasks is the full DAG task list; steps and decisions cover the tasks
		// that ran or were skip-decided.
		tasks     []string
		steps     map[string]StepState
		decisions map[string]TaskDecision
		wantPhase Phase
		wantDone  bool
	}{
		{
			name:      "every step succeeded",
			tasks:     taskSet("export", "load"),
			steps:     map[string]StepState{"export": step(jobrun.Succeeded), "load": step(jobrun.Succeeded)},
			wantPhase: PhaseSucceeded,
			wantDone:  true,
		},
		{
			name:  "one failed step fails the workflow under FailFast",
			tasks: taskSet("export", "load", "notify"),
			steps: map[string]StepState{
				"export": step(jobrun.Succeeded),
				"load":   step(jobrun.Failed),
			},
			decisions: map[string]TaskDecision{
				// FailFast stopped new work: notify is skip-decided.
				"notify": {Skipped: true, Reason: ReasonFailFast},
			},
			wantPhase: PhaseFailed,
			wantDone:  true,
		},
		{
			name:  "one failed step fails the workflow under Continue",
			tasks: taskSet("export", "load", "audit"),
			steps: map[string]StepState{
				"export": step(jobrun.Failed),
				"load":   step(jobrun.Succeeded),
				"audit":  step(jobrun.Succeeded),
			},
			// Continue ran the independent branches to completion; the roll-up
			// is policy-independent: the run as a whole did not succeed.
			wantPhase: PhaseFailed,
			wantDone:  true,
		},
		{
			name:      "a CancelUnknown step keeps the run non-terminal despite successes",
			tasks:     taskSet("export", "load"),
			steps:     map[string]StepState{"export": step(jobrun.Succeeded), "load": step(jobrun.CancelUnknown)},
			wantPhase: PhaseCancelUnknown,
			wantDone:  false,
		},
		{
			name:      "CancelUnknown dominates a failed step",
			tasks:     taskSet("export", "load"),
			steps:     map[string]StepState{"export": step(jobrun.Failed), "load": step(jobrun.CancelUnknown)},
			wantPhase: PhaseCancelUnknown,
			wantDone:  false,
		},
		{
			name:      "a canceled step cancels the workflow",
			tasks:     taskSet("export", "load"),
			steps:     map[string]StepState{"export": step(jobrun.Succeeded), "load": step(jobrun.Canceled)},
			wantPhase: PhaseCanceled,
			wantDone:  true,
		},
		{
			name:  "canceled skip decisions cancel a run whose steps all succeeded",
			tasks: taskSet("export", "fanout", "notify"),
			steps: map[string]StepState{
				"export": step(jobrun.Succeeded),
				"fanout": step(jobrun.Succeeded),
			},
			decisions: map[string]TaskDecision{
				// Cancellation began before notify was created; the in-flight
				// steps finished Succeeded before the patch landed.
				"notify": {Skipped: true, Reason: ReasonCanceled},
			},
			wantPhase: PhaseCanceled,
			wantDone:  true,
		},
		{
			name:  "condition-skipped tasks do not taint a successful run",
			tasks: taskSet("export", "cleanup"),
			steps: map[string]StepState{"export": step(jobrun.Succeeded)},
			decisions: map[string]TaskDecision{
				"cleanup": {Skipped: true, Reason: ReasonCondition},
			},
			wantPhase: PhaseSucceeded,
			wantDone:  true,
		},
		{
			name:      "deadline-skipped everything is a succeeded no-op run",
			tasks:     taskSet("never-created"),
			decisions: map[string]TaskDecision{"never-created": {Skipped: true, Reason: ReasonDeadline}},
			wantPhase: PhaseSucceeded,
			wantDone:  true,
		},
		{
			name:      "a task with neither step nor decision holds the run open",
			tasks:     taskSet("export", "load"),
			steps:     map[string]StepState{"export": step(jobrun.Succeeded)},
			wantPhase: PhaseRunning,
			wantDone:  false,
		},
		{
			name:      "a canceled-skip does not mask an unaccounted task",
			tasks:     taskSet("export", "hold"),
			decisions: map[string]TaskDecision{"export": {Skipped: true, Reason: ReasonCanceled}},
			wantPhase: PhaseRunning,
			wantDone:  false,
		},
		{
			name:      "a failed step does not mask an unaccounted task",
			tasks:     taskSet("export", "hold"),
			steps:     map[string]StepState{"export": step(jobrun.Failed)},
			wantPhase: PhaseRunning,
			wantDone:  false,
		},
		{
			name:      "empty task set derives success",
			tasks:     nil,
			wantPhase: PhaseSucceeded,
			wantDone:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			phase, done := DerivePhase(tt.tasks, tt.steps, tt.decisions)
			if phase != tt.wantPhase {
				t.Errorf("phase = %q, want %q", phase, tt.wantPhase)
			}
			if done != tt.wantDone {
				t.Errorf("done = %v, want %v", done, tt.wantDone)
			}
		})
	}
}

// Terminal agrees with the derivation's honesty rule: the phases DerivePhase
// reports as done are exactly the phases Terminal accepts.
func TestTerminalMatchesDerivePhaseDone(t *testing.T) {
	terminalPhases := map[Phase]bool{
		PhaseSucceeded: true, PhaseFailed: true, PhaseCanceled: true,
	}
	for _, phase := range []Phase{PhasePending, PhaseRunning, PhaseCancelRequested, PhaseSucceeded, PhaseFailed, PhaseCanceled, PhaseCancelUnknown} {
		if Terminal(phase) != terminalPhases[phase] {
			t.Errorf("Terminal(%q) disagrees with the derivation's done set", phase)
		}
	}
}

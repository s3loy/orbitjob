package workflow

import (
	"orbitjob/internal/core/domain/jobrun"
)

// DerivePhase derives a workflow run's phase from its tasks' ledger facts and
// returns whether the run has reached a terminal decision.
//
// A task is accounted for when it has a step (a job run grouped under the
// workflow run) or a skip decision in the run's task_decisions trail. While
// any task is unaccounted the workflow is not done and the second return is
// false.
//
// The roll-up rules, in the order they are applied:
//
//   - A step in CancelUnknown dominates everything and keeps the workflow
//     non-terminal at CancelUnknown. jobrun.Terminal excludes CancelUnknown
//     for exactly the same reason: a stop the platform could not confirm is
//     not an outcome, and guessing a roll-up over it would be a lie.
//   - Any failed step makes the workflow Failed, under either fail policy:
//     FailFast stopped new work because of it, Continue kept going but the
//     run as a whole did not succeed.
//   - Any canceled step, or any skip decided with reason canceled, makes the
//     workflow Canceled — cancel was requested and the run's work reflects it.
//   - Otherwise every executed step succeeded and every other task was
//     legitimately skipped: Succeeded.
//
// An empty task set derives Succeeded, but no valid DAG is empty (ValidateDAG
// refuses one), so in practice this only happens to a caller that skipped
// validation.
func DerivePhase(tasks []string, steps map[string]StepState, decisions map[string]TaskDecision) (Phase, bool) {
	unknown := false
	failed := false
	canceled := false
	accounted := true

	for _, task := range tasks {
		state, hasStep := steps[task]
		switch {
		case hasStep:
			switch state.Phase {
			case jobrun.CancelUnknown:
				unknown = true
			case jobrun.Failed:
				failed = true
			case jobrun.Canceled:
				canceled = true
			}
		default:
			decision, decided := decisions[task]
			if !decided || !decision.Skipped {
				accounted = false
				continue
			}
			if decision.Reason == ReasonCanceled {
				canceled = true
			}
		}
	}

	if unknown {
		return PhaseCancelUnknown, false
	}
	if !accounted {
		return PhaseRunning, false
	}
	if failed {
		return PhaseFailed, true
	}
	if canceled {
		return PhaseCanceled, true
	}
	return PhaseSucceeded, true
}

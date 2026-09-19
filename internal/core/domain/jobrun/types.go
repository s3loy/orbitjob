package jobrun

import (
	"errors"
	"time"
)

// Phase is the lifecycle state of one logical run. Terminal phases are
// Succeeded, Failed and Canceled; CancelUnknown is terminal but undecided,
// meaning the platform issued a stop it could not confirm.
type Phase string

const (
	Pending         Phase = "Pending"
	CreatingAttempt Phase = "CreatingAttempt"
	Running         Phase = "Running"
	RetryWaiting    Phase = "RetryWaiting"
	Succeeded       Phase = "Succeeded"
	Failed          Phase = "Failed"
	CancelRequested Phase = "CancelRequested"
	Canceled        Phase = "Canceled"
	CancelUnknown   Phase = "CancelUnknown"
)

// Trigger records why a run exists. Schedule and Manual runs are deduplicated by
// occurrence key; Retry runs are created by the retry policy for a prior attempt;
// Check runs are fired by the check scheduler for a probe-style definition;
// Workflow runs are the step runs of a workflow execution, grouped by the
// ledger's workflow_run_id; Function runs are invocations fired by the HTTP
// function surface, CR-first like Manual.
type Trigger string

const (
	Schedule Trigger = "Schedule"
	Manual   Trigger = "Manual"
	Retry    Trigger = "Retry"
	Check    Trigger = "Check"
	Workflow Trigger = "Workflow"
	Function Trigger = "Function"
)

// JobRun is one logical execution of a pinned definition revision. RevisionID is
// intentionally a string: it is the opaque database identity of the revision, and
// nothing in the domain may derive scheduling behaviour from its numeric value.
type JobRun struct {
	ID, SourceUID, RevisionID, OccurrenceKey string
	RevisionGeneration, Attempt              int
	Trigger                                  Trigger
	Phase                                    Phase
	// ScheduledFor is the occurrence the run is for, in civil time. A scheduler
	// that knows the instant it is firing writes it so schedule adherence can be
	// answered in SQL; a zero value means the trigger had no scheduled instant
	// (a manual run) and the column stays NULL.
	ScheduledFor time.Time
}

// Attempt is one platform-level execution try. Each Attempt maps to exactly one
// Kubernetes Job, so platform retry and Pod retry stay distinguishable in audit.
type Attempt struct {
	ID, JobRunID, KubernetesJobName, KubernetesJobUID string
	ObservedResourceVersion                           string
	Number                                            int
	Phase                                             Phase
	StartedAt                                         time.Time
}

// ErrInvalidTransition reports an attempt to move between run phases that the
// lifecycle does not allow.
var ErrInvalidTransition = errors.New("invalid job run transition")

// ErrRunTerminal reports that a phase write was refused because the run already
// reached a terminal phase. A finished run is final: an observation that arrives
// after it, however late, must not pull it back into the lifecycle. It is
// reported distinctly from a stale writer epoch so a caller can tell "the run is
// done" from "this writer was superseded".
var ErrRunTerminal = errors.New("run already terminal")

// CanTransition reports whether the run lifecycle permits from -> to. A run may
// only reach a terminal phase through the paths listed here; in particular
// Canceled requires passing through CancelRequested, so a stop is never recorded
// as complete without an observed termination.
func CanTransition(from, to Phase) bool {
	switch from {
	case Pending:
		return to == CreatingAttempt || to == CancelRequested
	case CreatingAttempt:
		return to == Running || to == CancelRequested
	case Running:
		return to == RetryWaiting || to == Succeeded || to == Failed || to == CancelRequested
	case RetryWaiting:
		// Failed is reachable from here because a run whose attempts are all
		// spent stops retrying and records the outcome; without this edge the
		// run would sit in RetryWaiting forever.
		return to == CreatingAttempt || to == CancelRequested || to == Failed
	case CancelRequested:
		return to == Canceled || to == CancelUnknown
	}
	return false
}

// Terminal reports whether no further transition is possible without a new run.
func Terminal(p Phase) bool {
	return p == Succeeded || p == Failed || p == Canceled
}

func Transition(run JobRun, to Phase) (JobRun, error) {
	if !CanTransition(run.Phase, to) {
		return run, ErrInvalidTransition
	}
	run.Phase = to
	return run, nil
}

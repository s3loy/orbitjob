package jobrun

import "time"

// StoredRun is the persisted progress of one logical run: what the scheduler and
// the JobRun controller need in order to decide the next action without trusting
// the CR's status subresource, which any writer with status permission can edit.
type StoredRun struct {
	ID          int64
	RevisionID  int64
	Phase       Phase
	Attempt     int
	MaxAttempts int
	Epoch       int64
	// ScheduledFor is the civil-time occurrence the scheduler recorded at row
	// creation; zero means the run had no scheduled instant (manual trigger or
	// a row written before the column existed).
	ScheduledFor time.Time
}

// NextAttempt is the attempt number the next Kubernetes Job should carry. It is
// derived from stored state so a re-reconcile after a controller restart lands on
// the same attempt number and therefore the same deterministic Job name.
func (s StoredRun) NextAttempt() int { return s.Attempt + 1 }

// AttemptInFlight reports whether the run has an attempt that has started and
// has not been observed terminal. The stored run advances to Running or
// CreatingAttempt when the attempt is created, and only leaves those phases when
// the Kubernetes Job is observed, so either phase with an attempt number set
// means a workload is live for this run.
func (s StoredRun) AttemptInFlight() bool {
	if s.Attempt < 1 {
		return false
	}
	return s.Phase == Running || s.Phase == CreatingAttempt
}

// CanStartAttempt reports whether another attempt is both permitted by policy
// and legal in the lifecycle. It is false while an attempt is in flight: that
// attempt's Job is live, so a second attempt would execute the same occurrence
// concurrently. It is also false once the phase has advanced past the attempt
// stage or the attempt budget is spent.
func (s StoredRun) CanStartAttempt() bool {
	if s.AttemptInFlight() {
		return false
	}
	if s.NextAttempt() > s.MaxAttempts {
		return false
	}
	switch s.Phase {
	case Pending, RetryWaiting:
		return true
	}
	return false
}

// PrunableRun identifies a terminal run that retention may drop. It lives in the
// domain so the store and the retention policy agree on one shape rather than
// each declaring a structurally identical type that will not interchange.
type PrunableRun struct {
	ID            int64
	OccurrenceKey string
	Phase         Phase
}

// OpenRun is a non-terminal run that must have a live representation in
// Kubernetes. It carries the revision it was pinned to, so repairing a run whose
// object went missing republishes the spec that run was created with rather than
// the definition's current one, and the occurrence's civil time so the repaired
// object keeps its scheduled-at annotation.
type OpenRun struct {
	ID            int64
	RevisionID    int64
	OccurrenceKey string
	Phase         Phase
	ScheduledFor  time.Time
}

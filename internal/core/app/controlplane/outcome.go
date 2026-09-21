package controlplane

import (
	"context"
	"time"

	"orbitjob/internal/core/domain/jobrun"
)

// TerminalOutcome is the fact that a run reached a terminal phase, stated with
// everything the outcome bookkeeping needs: which definition it ran for, which
// occurrence it was, when it was scheduled, and the wall-clock span of its
// newest attempt. The check read model and the SLI derivation both write from
// this one shape, so both describe the same fact the same way.
type TerminalOutcome struct {
	RunID         int64
	SourceUID     string
	OccurrenceKey string
	Phase         jobrun.Phase
	// ScheduledFor is the civil-time occurrence the scheduler recorded at row
	// creation; zero for runs without one (manual triggers).
	ScheduledFor time.Time
	// StartedAt and CompletedAt bound the newest attempt; zero means the run
	// finished without an attempt that observed both instants.
	StartedAt   time.Time
	CompletedAt time.Time
}

// OutcomeReader loads a run's terminal facts by ledger id.
type OutcomeReader interface {
	TerminalOutcome(ctx context.Context, tenantID string, runID int64) (TerminalOutcome, error)
}

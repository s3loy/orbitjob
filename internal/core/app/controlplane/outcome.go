package controlplane

import (
	"context"
	"time"

	"orbitjob/internal/core/domain/jobrun"
)

// TerminalOutcome is the fact that a run reached a terminal phase, stated with
// everything the outcome bookkeeping needs: which definition it ran for, which
// occurrence it was, when it was created and scheduled, and the wall-clock span
// of its newest attempt. Derived read models and SLI bookkeeping share this
// shape so they describe the same committed fact.
type TerminalOutcome struct {
	RunID         int64
	SourceUID     string
	OccurrenceKey string
	Phase         jobrun.Phase
	// CreatedAt is the ledger occurrence's creation instant. Unlike the newest
	// attempt's start, it remains stable across execution and bookkeeping retries.
	CreatedAt time.Time
	// ScheduledFor is the civil-time occurrence the scheduler recorded at row
	// creation; zero for runs without one (manual triggers).
	ScheduledFor time.Time
	// StartedAt and CompletedAt bound the newest attempt, not the whole
	// invocation. A zero value means that instant was not recorded.
	StartedAt   time.Time
	CompletedAt time.Time
}

// OutcomeReader loads a run's terminal facts by ledger id.
type OutcomeReader interface {
	TerminalOutcome(ctx context.Context, tenantID string, runID int64) (TerminalOutcome, error)
}

package function

import (
	"time"

	"orbitjob/internal/core/domain/jobrun"
)

// Read-model statuses for function_runs, mirroring check_runs' vocabulary.
// A function invocation lands here only when it is over: success and failed
// are the exit outcomes, canceled is a stop the platform observed. There is
// no pending or running state — the live progress of an invocation is its
// JobRun CR and its ledger row, not this table.
const (
	StatusSuccess  = "success"
	StatusFailed   = "failed"
	StatusCanceled = "canceled"
)

// CompletedRecord is one terminal function invocation written into the
// function_runs read model. It is derived from the run ledger, not produced
// by an executor: function invocations execute as Kubernetes Jobs, and this
// is the projection their outcomes land in — the same shape check_runs took
// when checks moved onto the run pipeline.
type CompletedRecord struct {
	// RunID is the deterministic UUID derived from the ledger occurrence key,
	// so a replayed recording addresses the same read-model row instead of
	// doubling history.
	RunID      string
	FunctionID int64
	// Status is StatusSuccess, StatusFailed or StatusCanceled, mapped from
	// the run's terminal phase. The ledger's honest answer to "what did this
	// invocation produce" is the exit outcome: there is no output column and
	// nothing here carries one.
	Status string
	// TriggeredAt is when the invocation was created (the ledger row's
	// created_at); a function invocation has no scheduled instant.
	TriggeredAt time.Time
	StartedAt   time.Time
	FinishedAt  time.Time
	DurationMs  int
}

// FunctionRun is one persisted read-model row, the admin read shape over
// function_runs.
type FunctionRun struct {
	ID          int64
	RunID       string
	TenantID    string
	FunctionID  int64
	Status      string
	TriggeredAt time.Time
	StartedAt   *time.Time
	FinishedAt  *time.Time
	DurationMs  *int
	Version     int
	CreatedAt   time.Time
}

// ReadModelStatus maps a run's terminal phase to the read-model status. It is
// the one place the mapping lives so the recorder and any future writer
// cannot drift: Succeeded becomes success, Failed becomes failed, Canceled
// becomes canceled, and a non-terminal phase has no row — the read model
// records outcomes, not progress.
func ReadModelStatus(phase jobrun.Phase) (string, bool) {
	switch phase {
	case jobrun.Succeeded:
		return StatusSuccess, true
	case jobrun.Failed:
		return StatusFailed, true
	case jobrun.Canceled:
		return StatusCanceled, true
	}
	return "", false
}

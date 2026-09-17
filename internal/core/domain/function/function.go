// Package function is the pure domain view of OrbitJob's serverless surface:
// a Function is a tenant-owned definition (image, command, args, timeout,
// retry budget) that materializes into an immutable revision exactly as a
// check does, and an invocation is an ordinary run in the ledger fired by an
// HTTP call. Function runs execute as Kubernetes Jobs through the unforked
// run pipeline; the only result channel is the exit outcome, which the
// function_runs read model records.
package function

import (
	"strconv"
	"strings"
	"time"
)

// SourceModeFunction is the job_definition_revisions source_mode a function
// materializes under. Revisions are unique per (source_mode, source_uid,
// generation), so function-sourced revisions live in their own namespace and
// can never collide with kubernetes, check or workflow rows.
const SourceModeFunction = "function"

// Definition statuses. Paused is the suspended-definition state: invoking a
// paused function is a conflict, not an error.
const (
	StatusActive = "active"
	StatusPaused = "paused"
)

// SourceUID is the stable revision identity of a function: the literal prefix
// "function-" plus the function's row id. It is the same string the ledger
// stores as job_run_control_plane.source_uid, the JobRun CR carries as its
// scheduledJobRef name and uid, and the SLI source_config names — one
// derivation addresses the definition everywhere, exactly as check-<id> does
// for checks.
func SourceUID(id int64) string {
	return "function-" + strconv.FormatInt(id, 10)
}

// IDFromSourceUID reverses SourceUID. The second return is false for any
// string the derivation could not have produced, so callers can route on it
// instead of string-matching a prefix. A workflow step's source_uid is the
// referenced ScheduledJob's identity and never carries this prefix unless a
// definition was named that way; callers that need certainty parse only after
// matching the full prefix convention.
func IDFromSourceUID(sourceUID string) (int64, bool) {
	const prefix = "function-"
	if !strings.HasPrefix(sourceUID, prefix) {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(sourceUID, prefix), 10, 64)
	if err != nil || id < 1 {
		return 0, false
	}
	return id, true
}

// Definition is one tenant-owned function row. Every execution-shaping field
// is boundary-validated at save time and pinned into the revision the
// operator materializes, so every invocation of a version executes the same
// spec. A function carries no schedule: it is invoke-only; a cron over the
// same container is a ScheduledJob's job.
type Definition struct {
	ID       int64
	TenantID string
	// ResourceGroupID is the optional isolation tier the row belongs to.
	// Empty means no group, which the "*" group segment of an ARN matches.
	ResourceGroupID string
	Name            string
	Description     string
	// Status is StatusActive or StatusPaused. Invocation of a paused function
	// is refused with the suspended-definition conflict.
	Status string
	// Image is required and non-empty. Digest-pinned form is the recommended
	// posture; every invocation is a cold pod and a mutable tag is the
	// function surface's supply-chain exposure.
	Image   string
	Command []string
	Args    []string
	// TimeoutSeconds bounds one invocation and becomes the rendered Job's
	// activeDeadlineSeconds, so enforcement survives an operator outage.
	TimeoutSeconds int
	// RetryLimit is the number of retries after the first attempt; the
	// rendered retry budget is RetryLimit+1, matching the checks convention.
	RetryLimit int
	// HistorySuccess and HistoryFailed bound retained invocations per
	// outcome, defaulting to the platform's 3/3, so retention sweeps function
	// runs like every other run.
	HistorySuccess int
	HistoryFailed  int
	Labels         map[string]any
	// Version is the optimistic-concurrency counter and the revision
	// generation: saving a function bumps it and flips the active revision
	// pointer in the same transaction.
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
	// DeletedAt is the soft-delete timestamp; zero means live.
	DeletedAt time.Time
}

// Package instance provides job instance domain types and operations.
package instance

import internal "orbitjob/internal/core/domain/instance"

type (
	Snapshot         = internal.Snapshot
	CreateInput      = internal.CreateInput
	CreateSpec       = internal.CreateSpec
	ClaimInput       = internal.ClaimInput
	ClaimSpec        = internal.ClaimSpec
	CompleteInput    = internal.CompleteInput
	CompleteSpec     = internal.CompleteSpec
	WorkerClaimInput = internal.WorkerClaimInput
	WorkerClaimSpec  = internal.WorkerClaimSpec
	DispatchInput    = internal.DispatchInput
	DispatchDecision = internal.DispatchDecision
	ValidationError  = internal.ValidationError
)

const (
	DefaultTenantID         = internal.DefaultTenantID
	DefaultIdempotencyScope = internal.DefaultIdempotencyScope

	TriggerSourceSchedule = internal.TriggerSourceSchedule
	TriggerSourceManual   = internal.TriggerSourceManual

	StatusPending    = internal.StatusPending
	StatusDispatched = internal.StatusDispatched
	StatusRunning    = internal.StatusRunning
	StatusRetryWait  = internal.StatusRetryWait
	StatusSuccess    = internal.StatusSuccess
	StatusFailed     = internal.StatusFailed
	StatusCanceled   = internal.StatusCanceled

	DispatchActionDispatch = internal.DispatchActionDispatch
	DispatchActionSkip     = internal.DispatchActionSkip
	DispatchActionReplace  = internal.DispatchActionReplace
)

var (
	NormalizeCreate      = internal.NormalizeCreate
	NormalizeClaim       = internal.NormalizeClaim
	NormalizeComplete    = internal.NormalizeComplete
	NormalizeWorkerClaim = internal.NormalizeWorkerClaim
	DecideDispatch       = internal.DecideDispatch
	ComputeRetryAt       = internal.ComputeRetryAt
)

// Package worker provides worker domain types and operations.
package worker

import internal "orbitjob/internal/core/domain/worker"

type (
	Snapshot        = internal.Snapshot
	HeartbeatInput  = internal.HeartbeatInput
	HeartbeatSpec   = internal.HeartbeatSpec
	ValidationError = internal.ValidationError
)

const (
	DefaultTenantID = internal.DefaultTenantID
	StatusOnline    = internal.StatusOnline
	StatusOffline   = internal.StatusOffline
	StatusDraining  = internal.StatusDraining
)

var NormalizeHeartbeat = internal.NormalizeHeartbeat

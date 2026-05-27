// Package job provides job domain types and operations.
package job

import internal "orbitjob/internal/core/domain/job"

type (
	Snapshot           = internal.Snapshot
	CreateInput        = internal.CreateInput
	CreateSpec         = internal.CreateSpec
	UpdateInput        = internal.UpdateInput
	UpdateSpec         = internal.UpdateSpec
	ChangeStatusSpec   = internal.ChangeStatusSpec
	ValidationError    = internal.ValidationError
	QuotaExceededError = internal.QuotaExceededError
)

const (
	DefaultTenantID   = internal.DefaultTenantID
	DefaultTimezone   = internal.DefaultTimezone
	DefaultTimeoutSec = internal.DefaultTimeoutSec
	DefaultRetryLimit = internal.DefaultRetryLimit

	TriggerTypeCron   = internal.TriggerTypeCron
	TriggerTypeManual = internal.TriggerTypeManual

	RetryBackoffFixed       = internal.RetryBackoffFixed
	RetryBackoffExponential = internal.RetryBackoffExponential

	HandlerTypeExec = internal.HandlerTypeExec
	HandlerTypeHTTP = internal.HandlerTypeHTTP

	ConcurrencyAllow   = internal.ConcurrencyAllow
	ConcurrencyForbid  = internal.ConcurrencyForbid
	ConcurrencyReplace = internal.ConcurrencyReplace

	MisfireSkip    = internal.MisfireSkip
	MisfireFireNow = internal.MisfireFireNow
	MisfireCatchUp = internal.MisfireCatchUp

	StatusActive = internal.StatusActive
	StatusPaused = internal.StatusPaused
	ActionPause  = internal.ActionPause
	ActionResume = internal.ActionResume
)

var (
	NormalizeCreate       = internal.NormalizeCreate
	NormalizeUpdate       = internal.NormalizeUpdate
	Pause                 = internal.Pause
	Resume                = internal.Resume
	NewQuotaExceededError = internal.NewQuotaExceededError
)

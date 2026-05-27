package domain

import internal "orbitjob/internal/core/domain"

// ErrorClass categorizes database errors for adaptive control.
type ErrorClass = internal.ErrorClass

const (
	ClassNone     = internal.ClassNone
	SkipWorthy    = internal.SkipWorthy
	BackoffWorthy = internal.BackoffWorthy
	FatalWorthy   = internal.FatalWorthy
)

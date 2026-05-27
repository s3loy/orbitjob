// Package resource provides resource-level errors.
package resource

import internal "orbitjob/internal/domain/resource"

// NotFoundError indicates a requested resource does not exist.
type NotFoundError = internal.NotFoundError

// ConflictError indicates an optimistic locking conflict.
type ConflictError = internal.ConflictError

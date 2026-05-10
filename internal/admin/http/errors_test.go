package http

import (
	"errors"
	"testing"

	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

func TestToAPIError_NotFound(t *testing.T) {
	got := toAPIError(&resource.NotFoundError{
		Resource: "job",
		ID:       42,
	})

	if got.Code != ErrCodeNotFound {
		t.Fatalf("expected code=%q, got %q", ErrCodeNotFound, got.Code)
	}
	if got.Message != "resource not found" {
		t.Fatalf("expected message=%q, got %q", "resource not found", got.Message)
	}
	if got.Field != "job" {
		t.Fatalf("expected field=%q, got %q", "job", got.Field)
	}
}

func TestToAPIError_Conflict(t *testing.T) {
	got := toAPIError(&resource.ConflictError{
		Resource: "job",
		ID:       42,
		Field:    "version",
		Message:  "stale job version",
	})

	if got.Code != ErrCodeConflict {
		t.Fatalf("expected code=%q, got %q", ErrCodeConflict, got.Code)
	}
	if got.Message != "stale job version" {
		t.Fatalf("expected message=%q, got %q", "stale job version", got.Message)
	}
	if got.Field != "version" {
		t.Fatalf("expected field=%q, got %q", "version", got.Field)
	}
}

func TestToAPIError_Validation(t *testing.T) {
	got := toAPIError(validation.New("cron_expr", "is required for cron jobs"))

	if got.Code != ErrCodeValidation {
		t.Fatalf("expected code=%q, got %q", ErrCodeValidation, got.Code)
	}
	if got.Message != "is required for cron jobs" {
		t.Fatalf("expected message=%q, got %q", "is required for cron jobs", got.Message)
	}
	if got.Field != "cron_expr" {
		t.Fatalf("expected field=%q, got %q", "cron_expr", got.Field)
	}
}

func TestToAPIError_ConflictFallbackField(t *testing.T) {
	got := toAPIError(&resource.ConflictError{
		Resource: "instance",
		ID:       "run-001",
		Field:    "",
		Message:  "version conflict",
	})

	if got.Code != ErrCodeConflict {
		t.Fatalf("expected code=%q, got %q", ErrCodeConflict, got.Code)
	}
	if got.Field != "instance" {
		t.Fatalf("expected field to fall back to Resource=%q, got %q", "instance", got.Field)
	}
}

func TestToAPIError_ConflictFallbackMessage(t *testing.T) {
	got := toAPIError(&resource.ConflictError{
		Resource: "job",
		ID:       42,
		Field:    "",
		Message:  "",
	})

	if got.Code != ErrCodeConflict {
		t.Fatalf("expected code=%q, got %q", ErrCodeConflict, got.Code)
	}
	if got.Message != "resource conflict" {
		t.Fatalf("expected fallback message=%q, got %q", "resource conflict", got.Message)
	}
	if got.Field != "job" {
		t.Fatalf("expected field to fall back to Resource=%q, got %q", "job", got.Field)
	}
}

func TestToAPIError_Internal(t *testing.T) {
	got := toAPIError(errors.New("connection refused"))

	if got.Code != ErrCodeInternal {
		t.Fatalf("expected code=%q, got %q", ErrCodeInternal, got.Code)
	}
	if got.Message != "an internal error occurred" {
		t.Fatalf("expected message=%q, got %q", "an internal error occurred", got.Message)
	}
	if got.Field != "" {
		t.Fatalf("expected no field for internal error, got %q", got.Field)
	}
}

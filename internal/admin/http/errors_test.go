package http

import (
	"testing"

	"orbitjob/internal/domain/resource"
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

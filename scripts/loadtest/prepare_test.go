package main

import (
	"context"
	"testing"
)

func TestQualificationLoadManifests(t *testing.T) {
	if err := ValidateLoadManifests("../../deploy/load/namespace.yaml", "../../deploy/load/operations-rbac.yaml"); err != nil {
		t.Fatal(err)
	}
}

func TestWithBackoffRetriesOnRateLimit(t *testing.T) {
	attempts := 0
	err := withBackoff(context.Background(), func() error {
		attempts++
		if attempts < 3 {
			return &APIError{StatusCode: 429, Message: "rate limited"}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestWithBackoffDoesNotRetryClientError(t *testing.T) {
	attempts := 0
	err := withBackoff(context.Background(), func() error {
		attempts++
		return &APIError{StatusCode: 400, Message: "bad request"}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry on 400)", attempts)
	}
}

func TestWithBackoffGivesUpAfterCap(t *testing.T) {
	attempts := 0
	err := withBackoff(context.Background(), func() error {
		attempts++
		return &APIError{StatusCode: 0, Message: "connection refused"}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 5 {
		t.Fatalf("attempts = %d, want 5 (initial + 4 retries)", attempts)
	}
}

package instance

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeResultCode_Truncation(t *testing.T) {
	result := normalizeResultCode(strings.Repeat("x", 50))
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(*result) != 32 {
		t.Fatalf("expected truncated to 32 chars, got %d", len(*result))
	}
}

func TestNormalizeComplete_WorkerIDTooLong(t *testing.T) {
	now := time.Now().UTC()
	_, err := NormalizeComplete(CompleteInput{
		InstanceID: 1,
		WorkerID:   strings.Repeat("w", 65),
		Now:        now,
	})
	if err == nil {
		t.Fatal("expected validation error for long worker_id, got nil")
	}
}

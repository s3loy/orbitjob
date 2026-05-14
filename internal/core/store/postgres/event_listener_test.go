package postgres

import (
	"testing"
)

func TestEventListener_Close_NilSafe(t *testing.T) {
	// Ensure Close doesn't panic on a nil listener
	var el *EventListener
	if err := el.Close(); err != nil {
		t.Fatalf("expected nil error for nil listener, got %v", err)
	}
}

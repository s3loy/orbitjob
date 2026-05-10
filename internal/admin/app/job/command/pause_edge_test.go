package command

import (
	"testing"
)

func TestNextStatusForAction_InvalidAction(t *testing.T) {
	_, err := nextStatusForAction("unknown", "active", 1)
	if err == nil {
		t.Fatal("expected error for invalid action, got nil")
	}
}

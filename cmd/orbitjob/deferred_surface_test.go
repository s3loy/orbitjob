package main

import (
	"context"
	"strings"
	"testing"
)

func TestDeferredExperimentCommandIsUnavailable(t *testing.T) {
	if _, exists := commands["experiments"]; exists {
		t.Fatal("deferred experiments command remains registered")
	}
	if strings.Contains(usageText, "experiments") {
		t.Fatal("help advertises deferred experiments command")
	}
	if code := run(context.Background(), []string{"experiments", "start"}); code != 1 {
		t.Fatalf("deferred command exit = %d, want unknown-command exit 1", code)
	}
}

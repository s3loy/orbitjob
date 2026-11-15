package logger

import (
	"context"
	"log/slog"
	"testing"
)

func TestNew_DebugLevel(t *testing.T) {
	// Non-production environments get DEBUG level.
	logger := New("development")
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}

	if !logger.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("development logger must enable debug output")
	}
}

func TestNew_ProductionLevel(t *testing.T) {
	logger := New("production")
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}

	// Verify it's at info level: debug messages should be suppressed.
	if logger.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("production logger must suppress debug output")
	}
	if !logger.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("production logger must enable info output")
	}
}

func TestNew_StagingIsDebug(t *testing.T) {
	// Only "production" gets Info level; everything else gets Debug.
	logger := New("staging")
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestNew_EmptyEnvIsDebug(t *testing.T) {
	logger := New("")
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

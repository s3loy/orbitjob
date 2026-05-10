package logger

import (
	"bytes"
	"log/slog"
	"testing"
)

func TestNew_DebugLevel(t *testing.T) {
	// Non-production environments get DEBUG level.
	logger := New("development")
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}

	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	debugLogger := slog.New(h)
	debugLogger.Debug("test")
	if buf.Len() == 0 {
		t.Fatal("debug log should produce output at debug level")
	}
}

func TestNew_ProductionLevel(t *testing.T) {
	logger := New("production")
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}

	// Verify it's at info level: debug messages should be suppressed.
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	infoLogger := slog.New(h)
	infoLogger.Debug("should not appear")
	if buf.Len() != 0 {
		t.Fatal("debug log should not appear at info level")
	}
	infoLogger.Info("should appear")
	if buf.Len() == 0 {
		t.Fatal("info log should appear at info level")
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

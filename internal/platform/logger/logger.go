package logger

import (
	"log/slog"
	"os"
)

// New returns a JSON-formatted slog logger.
// It logs at DEBUG level outside production and INFO in production.
func New(env string) *slog.Logger {
	level := slog.LevelDebug
	if env == "production" {
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}

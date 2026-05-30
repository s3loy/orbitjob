// Package logger provides structured logging setup.
package logger

import internal "orbitjob/internal/platform/logger"

// New returns a JSON-formatted slog logger.
var New = internal.New

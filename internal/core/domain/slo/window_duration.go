package slo

import (
	"time"

	"orbitjob/internal/domain/validation"
)

// ValidateWindowDuration requires whole seconds within the supported range.
func ValidateWindowDuration(duration time.Duration) error {
	if duration < time.Second {
		return validation.New("window_duration", "window_duration must be at least 1s")
	}
	if duration > MaxWindowDuration {
		return validation.New("window_duration", "window_duration must be at most 8760h")
	}
	if duration%time.Second != 0 {
		return validation.New("window_duration", "window_duration must be a whole number of seconds")
	}
	return nil
}

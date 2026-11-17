package slo

import (
	"testing"
	"time"

	"orbitjob/internal/domain/validation"
)

func TestNormalizeCreate_WindowDurationPrecision(t *testing.T) {
	for _, tc := range []struct {
		name     string
		duration time.Duration
		valid    bool
	}{
		{name: "negative", duration: -time.Second},
		{name: "zero", duration: 0},
		{name: "one nanosecond", duration: time.Nanosecond},
		{name: "one millisecond", duration: time.Millisecond},
		{name: "below one second", duration: 999 * time.Millisecond},
		{name: "fractional seconds", duration: 1500 * time.Millisecond},
		{name: "nanosecond remainder", duration: time.Second + time.Nanosecond},
		{name: "fraction below maximum", duration: MaxWindowDuration - time.Nanosecond},
		{name: "above maximum", duration: MaxWindowDuration + time.Second},
		{name: "one second", duration: time.Second, valid: true},
		{name: "two seconds", duration: 2 * time.Second, valid: true},
		{name: "one day", duration: 24 * time.Hour, valid: true},
		{name: "maximum", duration: MaxWindowDuration, valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := NormalizeCreate(CreateInput{
				Name: "availability", SLIID: 1, Target: 0.99,
				WindowDuration: tc.duration,
			})
			if tc.valid {
				if err != nil {
					t.Fatalf("valid duration rejected: %v", err)
				}
				if spec.WindowDuration != tc.duration {
					t.Fatalf("window duration = %v, want unchanged %v", spec.WindowDuration, tc.duration)
				}
				return
			}
			var validationErr *validation.Error
			if !validation.As(err, &validationErr) || validationErr.Field != "window_duration" {
				t.Fatalf("expected window_duration validation error, got %v", err)
			}
		})
	}
}

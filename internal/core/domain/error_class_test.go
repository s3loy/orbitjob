package domain

import "testing"

func TestErrorClassString(t *testing.T) {
	tests := []struct {
		class ErrorClass
		want  string
	}{
		{ClassNone, "none"},
		{SkipWorthy, "skip"},
		{BackoffWorthy, "backoff"},
		{FatalWorthy, "fatal"},
		{ErrorClass(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.class.String(); got != tt.want {
			t.Errorf("ErrorClass(%d).String() = %q, want %q", tt.class, got, tt.want)
		}
	}
}

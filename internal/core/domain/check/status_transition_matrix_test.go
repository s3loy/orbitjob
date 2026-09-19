package check

import (
	"testing"

	"orbitjob/internal/domain/validation"
)

// TestStatusTransitionMatrix walks both lifecycle actions across the statuses a
// stored check can carry. Each action must accept exactly one source state:
// pause from active, resume from paused. The store trusts this validation
// before writing, so a missing rejection here becomes a corrupted lifecycle in
// the database, not just a wrong response.
func TestStatusTransitionMatrix(t *testing.T) {
	tests := []struct {
		name      string
		apply     func(status string, version int) (string, error)
		from      string
		version   int
		wantTo    string
		wantField string // empty means the transition must succeed
	}{
		{"pause from active", Pause, StatusActive, 1, StatusPaused, ""},
		{"pause from paused", Pause, StatusPaused, 1, "", "status"},
		{"pause from empty status", Pause, "", 1, "", "status"},
		{"pause from foreign status", Pause, "deleted", 1, "", "status"},
		{"pause is case sensitive", Pause, "Active", 1, "", "status"},

		{"resume from paused", Resume, StatusPaused, 1, StatusActive, ""},
		{"resume from active", Resume, StatusActive, 1, "", "status"},
		{"resume from empty status", Resume, "", 1, "", "status"},
		{"resume from foreign status", Resume, "deleted", 1, "", "status"},
		{"resume is case sensitive", Resume, "Paused", 1, "", "status"},

		{"pause rejects version zero", Pause, StatusActive, 0, "", "version"},
		{"pause rejects negative version", Pause, StatusActive, -1, "", "version"},
		{"resume rejects version zero", Resume, StatusPaused, 0, "", "version"},
		{"resume rejects negative version", Resume, StatusPaused, -1, "", "version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.apply(tt.from, tt.version)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tt.wantTo {
					t.Fatalf("next status = %q, want %q", got, tt.wantTo)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected rejection, got next status %q", got)
			}
			assertValidationField(t, err, tt.wantField)
		})
	}
}

// Version is an optimistic-lock precondition, not a status rule: it must gate
// both actions identically, and one is the smallest value a persisted row can
// carry.
func TestStatusTransitionVersionBoundary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		apply   func(status string, version int) (string, error)
		status  string
		version int
		wantErr bool
	}{
		{"pause version one", Pause, StatusActive, 1, false},
		{"pause version two", Pause, StatusActive, 2, false},
		{"resume version one", Resume, StatusPaused, 1, false},
		{"resume version two", Resume, StatusPaused, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.apply(tc.status, tc.version)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// A successful pause followed by a successful resume must return the check to
// where it started; otherwise the lifecycle is lossy and repeated
// pause/resume cycles would drift it.
func TestPauseAndResumeAreInverses(t *testing.T) {
	paused, err := Pause(StatusActive, 1)
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	resumed, err := Resume(paused, 1)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resumed != StatusActive {
		t.Fatalf("round trip ended in %q, want %q", resumed, StatusActive)
	}
}

func assertValidationField(t *testing.T, err error, field string) {
	t.Helper()
	if !validation.Is(err) {
		t.Fatalf("expected validation error, got %T: %v", err, err)
	}
	var vErr *validation.Error
	if !validation.As(err, &vErr) {
		t.Fatal("expected error to unwrap as validation.Error")
	}
	if vErr.Field != field {
		t.Fatalf("field = %q, want %q", vErr.Field, field)
	}
}

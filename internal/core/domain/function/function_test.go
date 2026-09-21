package function

import (
	"testing"

	"orbitjob/internal/core/domain/jobrun"
)

// SourceUID is the one derivation that ties a function row to its revision,
// its ledger runs, its JobRun CRs and its SLI source configs. The round-trip
// and the rejection set are pinned because everything downstream routes on
// the bool: a prefix match that parsed garbage would silently reattribute one
// definition's history to another.

func TestSourceUIDFormat(t *testing.T) {
	if got := SourceUID(42); got != "function-42" {
		t.Errorf("SourceUID(42) = %q, want %q", got, "function-42")
	}
	if got := SourceUID(1); got != "function-1" {
		t.Errorf("SourceUID(1) = %q, want %q", got, "function-1")
	}
}

func TestSourceUIDRoundTrip(t *testing.T) {
	for _, id := range []int64{1, 42, 999999} {
		parsed, ok := IDFromSourceUID(SourceUID(id))
		if !ok {
			t.Fatalf("IDFromSourceUID(%q) reported false", SourceUID(id))
		}
		if parsed != id {
			t.Errorf("round-trip of %d produced %d", id, parsed)
		}
	}
}

func TestIDFromSourceUIDRejections(t *testing.T) {
	tests := []struct {
		sourceUID string
		reason    string
	}{
		{"", "empty string"},
		{"check-42", "the check prefix is a different family"},
		{"workflow-7", "the workflow prefix is a different family"},
		{"function", "prefix without separator"},
		{"function-", "prefix with no id"},
		{"function-abc", "non-numeric id"},
		{"function-0", "ids start at 1"},
		{"function--3", "negative ids are not derivations"},
		{"function-42x", "trailing garbage"},
		{"functions-42", "wrong prefix word"},
	}
	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			if id, ok := IDFromSourceUID(tt.sourceUID); ok {
				t.Errorf("IDFromSourceUID(%q) = (%d, true), want false", tt.sourceUID, id)
			}
		})
	}
}

// These constants are schema-facing: SourceModeFunction is the
// job_definition_revisions source_mode value and the statuses are the
// functions.status CHECK values. Renaming them in Go without the matching
// migration would silently orphan every stored row.
func TestSchemaFacingConstants(t *testing.T) {
	if SourceModeFunction != "function" {
		t.Errorf("SourceModeFunction = %q, want the migration's source_mode value", SourceModeFunction)
	}
	if StatusActive != "active" || StatusPaused != "paused" {
		t.Errorf("statuses = %q/%q, want the migration's CHECK values", StatusActive, StatusPaused)
	}
}

func TestReadModelStatus(t *testing.T) {
	terminal := map[jobrun.Phase]string{
		jobrun.Succeeded: StatusSuccess,
		jobrun.Failed:    StatusFailed,
		jobrun.Canceled:  StatusCanceled,
	}
	for phase, want := range terminal {
		got, ok := ReadModelStatus(phase)
		if !ok || got != want {
			t.Errorf("ReadModelStatus(%q) = (%q, %v), want (%q, true)", phase, got, ok, want)
		}
	}
	// The read model records outcomes, not progress: every non-terminal phase
	// has no row, and CancelUnknown is refused for the same honesty reason it
	// is refused everywhere else.
	for _, phase := range []jobrun.Phase{jobrun.Pending, jobrun.CreatingAttempt, jobrun.Running, jobrun.RetryWaiting, jobrun.CancelRequested, jobrun.CancelUnknown} {
		if got, ok := ReadModelStatus(phase); ok {
			t.Errorf("ReadModelStatus(%q) = (%q, true), want false", phase, got)
		}
	}
}

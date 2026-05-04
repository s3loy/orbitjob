package job

import "testing"

func TestPauseTransition(t *testing.T) {
	nextStatus, err := Pause(StatusActive, 3)
	if err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	if nextStatus != StatusPaused {
		t.Fatalf("expected next_status=%q, got %q", StatusPaused, nextStatus)
	}
}

func TestPauseTransitionValidationError(t *testing.T) {
	_, err := Pause(StatusPaused, 3)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if err.Error() != "status: only active jobs can be paused" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResumeTransition(t *testing.T) {
	nextStatus, err := Resume(StatusPaused, 7)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if nextStatus != StatusActive {
		t.Fatalf("expected next_status=%q, got %q", StatusActive, nextStatus)
	}
}

func TestResumeTransitionValidationError(t *testing.T) {
	_, err := Resume(StatusActive, 0)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if err.Error() != "version: must be >= 1" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkPause(b *testing.B) {
	b.Run("valid", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = Pause(StatusActive, 3)
		}
	})
	b.Run("invalid_state", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = Pause(StatusPaused, 3)
		}
	})
}

func BenchmarkResume(b *testing.B) {
	b.Run("valid", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = Resume(StatusPaused, 7)
		}
	})
	b.Run("invalid_state", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = Resume(StatusActive, 7)
		}
	})
}

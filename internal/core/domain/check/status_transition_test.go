package check

import "testing"

func TestPause(t *testing.T) {
	status, err := Pause(StatusActive, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != StatusPaused {
		t.Errorf("status = %q, want %q", status, StatusPaused)
	}
}

func TestPause_AlreadyPaused(t *testing.T) {
	_, err := Pause(StatusPaused, 1)
	if err == nil {
		t.Error("expected error for already paused check")
	}
}

func TestPause_InvalidVersion(t *testing.T) {
	_, err := Pause(StatusActive, 0)
	if err == nil {
		t.Error("expected error for invalid version")
	}
}

func TestResume(t *testing.T) {
	status, err := Resume(StatusPaused, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != StatusActive {
		t.Errorf("status = %q, want %q", status, StatusActive)
	}
}

func TestResume_AlreadyActive(t *testing.T) {
	_, err := Resume(StatusActive, 1)
	if err == nil {
		t.Error("expected error for already active check")
	}
}

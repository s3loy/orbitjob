package instance

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeComplete_Success(t *testing.T) {
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

	spec, err := NormalizeComplete(CompleteInput{
		InstanceID: 1,
		WorkerID:   "worker-a",
		Success:    true,
		ResultCode: "0",
		ErrorMsg:   "should be cleared",
		Now:        now,
		Attempt:    1,
		MaxAttempt: 3,
	})
	if err != nil {
		t.Fatalf("NormalizeComplete() error = %v", err)
	}
	if spec.Status != StatusSuccess {
		t.Fatalf("expected status=%q, got %q", StatusSuccess, spec.Status)
	}
	if spec.ErrorMsg != nil {
		t.Fatalf("expected error_msg=nil for success, got %q", *spec.ErrorMsg)
	}
	if spec.RetryAt != nil {
		t.Fatalf("expected retry_at=nil for success, got %v", *spec.RetryAt)
	}
	if spec.ResultCode == nil || *spec.ResultCode != "0" {
		t.Fatalf("expected result_code=%q, got %v", "0", spec.ResultCode)
	}
}

func TestNormalizeComplete_RetryWait(t *testing.T) {
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

	spec, err := NormalizeComplete(CompleteInput{
		InstanceID:           1,
		WorkerID:             "worker-a",
		Success:              false,
		ResultCode:           "1",
		ErrorMsg:             "some error",
		Now:                  now,
		Attempt:              1,
		MaxAttempt:           3,
		RetryBackoffSec:      10,
		RetryBackoffStrategy: "fixed",
	})
	if err != nil {
		t.Fatalf("NormalizeComplete() error = %v", err)
	}
	if spec.Status != StatusRetryWait {
		t.Fatalf("expected status=%q, got %q", StatusRetryWait, spec.Status)
	}
	if spec.RetryAt == nil {
		t.Fatal("expected retry_at to be set for retry_wait")
	}
	wantRetryAt := now.Add(10 * time.Second)
	if !spec.RetryAt.Equal(wantRetryAt) {
		t.Fatalf("expected retry_at=%v, got %v", wantRetryAt, *spec.RetryAt)
	}
	if spec.ErrorMsg == nil || *spec.ErrorMsg != "some error" {
		t.Fatalf("expected error_msg=%q, got %v", "some error", spec.ErrorMsg)
	}
}

func TestNormalizeComplete_Failed(t *testing.T) {
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

	spec, err := NormalizeComplete(CompleteInput{
		InstanceID: 1,
		WorkerID:   "worker-a",
		Success:    false,
		ResultCode: "1",
		ErrorMsg:   "final failure",
		Now:        now,
		Attempt:    3,
		MaxAttempt: 3,
	})
	if err != nil {
		t.Fatalf("NormalizeComplete() error = %v", err)
	}
	if spec.Status != StatusFailed {
		t.Fatalf("expected status=%q, got %q", StatusFailed, spec.Status)
	}
	if spec.RetryAt != nil {
		t.Fatalf("expected retry_at=nil for failed, got %v", *spec.RetryAt)
	}
}

func TestNormalizeComplete_EmptyResultCodeAndErrorMsg(t *testing.T) {
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

	spec, err := NormalizeComplete(CompleteInput{
		InstanceID: 1,
		WorkerID:   "worker-a",
		Success:    true,
		Now:        now,
		Attempt:    1,
		MaxAttempt: 1,
	})
	if err != nil {
		t.Fatalf("NormalizeComplete() error = %v", err)
	}
	if spec.ResultCode != nil {
		t.Fatalf("expected result_code=nil for empty input, got %q", *spec.ResultCode)
	}
	if spec.ErrorMsg != nil {
		t.Fatalf("expected error_msg=nil for empty input, got %q", *spec.ErrorMsg)
	}
}

func TestNormalizeComplete_InvalidInput(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name        string
		input       CompleteInput
		wantField   string
		wantMessage string
	}{
		{
			name: "instance id less than one",
			input: CompleteInput{
				WorkerID: "worker-a",
				Now:      now,
			},
			wantField:   "instance_id",
			wantMessage: "must be >= 1",
		},
		{
			name: "missing worker id",
			input: CompleteInput{
				InstanceID: 1,
				Now:        now,
			},
			wantField:   "worker_id",
			wantMessage: "is required",
		},
		{
			name: "missing now",
			input: CompleteInput{
				InstanceID: 1,
				WorkerID:   "worker-a",
			},
			wantField:   "now",
			wantMessage: "is required",
		},
		{
			name: "tenant id too long",
			input: CompleteInput{
				TenantID:   strings.Repeat("t", 65),
				InstanceID: 1,
				WorkerID:   "worker-a",
				Now:        now,
			},
			wantField:   "tenant_id",
			wantMessage: "must be <= 64 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeComplete(tt.input)
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			var validationErr *ValidationError
			if !AsValidationError(err, &validationErr) {
				t.Fatalf("expected ValidationError, got %T", err)
			}
			if validationErr.Field != tt.wantField {
				t.Fatalf("expected field=%q, got %q", tt.wantField, validationErr.Field)
			}
			if validationErr.Message != tt.wantMessage {
				t.Fatalf("expected message=%q, got %q", tt.wantMessage, validationErr.Message)
			}
		})
	}
}

func TestComputeRetryAt_Fixed(t *testing.T) {
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	got := ComputeRetryAt(now, 1, 10, "fixed")
	want := now.Add(10 * time.Second)
	if !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestComputeRetryAt_Exponential(t *testing.T) {
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 1, want: 10 * time.Second},
		{attempt: 2, want: 20 * time.Second},
		{attempt: 3, want: 40 * time.Second},
		{attempt: 4, want: 80 * time.Second},
	}

	for _, tt := range tests {
		got := ComputeRetryAt(now, tt.attempt, 10, "exponential")
		want := now.Add(tt.want)
		if !got.Equal(want) {
			t.Fatalf("attempt=%d: expected %v, got %v", tt.attempt, want, got)
		}
	}
}

func TestComputeRetryAt_ZeroBackoff(t *testing.T) {
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	got := ComputeRetryAt(now, 1, 0, "fixed")
	if !got.Equal(now) {
		t.Fatalf("expected %v for zero backoff, got %v", now, got)
	}
}

func TestComputeRetryAt_UnknownStrategy(t *testing.T) {
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	got := ComputeRetryAt(now, 1, 10, "unknown")
	want := now.Add(10 * time.Second)
	if !got.Equal(want) {
		t.Fatalf("expected fixed fallback %v, got %v", want, got)
	}
}

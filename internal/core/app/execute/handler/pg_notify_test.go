package handler

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/app/execute"
)

func TestPGNotify_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectExec("SELECT pg_notify").
		WithArgs("orbitjob_default_42", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	pn := NewPGNotify(db)
	task := execute.AssignedTask{
		RunID:       "run-abc",
		JobID:       42,
		TenantID:    "default",
		ScheduledAt: time.Now().UTC(),
		Attempt:     1,
		HandlerPayload: map[string]any{
			"body": map[string]any{"action": "test"},
		},
		TimeoutSec: 30,
	}

	result := pn.Execute(context.Background(), task)
	if !result.Success {
		t.Fatalf("expected success, got %s: %s", result.ResultCode, result.ErrorMsg)
	}
	if result.ResultCode != "notified" {
		t.Errorf("result code = %q, want notified", result.ResultCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestPGNotify_CustomChannel(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectExec("SELECT pg_notify").
		WithArgs("orbitjob_my_events", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	pn := NewPGNotify(db)
	task := execute.AssignedTask{
		RunID:       "run-abc",
		JobID:       42,
		TenantID:    "default",
		ScheduledAt: time.Now().UTC(),
		Attempt:     1,
		HandlerPayload: map[string]any{
			"channel": "my-events",
			"body":    map[string]any{"action": "test"},
		},
		TimeoutSec: 30,
	}

	result := pn.Execute(context.Background(), task)
	if !result.Success {
		t.Fatalf("expected success, got %s: %s", result.ResultCode, result.ErrorMsg)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestPGNotify_PayloadTooLarge(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	pn := NewPGNotify(db)
	pn.maxPayloadBytes = 50 // Shrink for test.

	task := execute.AssignedTask{
		RunID:       "run-abc",
		JobID:       42,
		TenantID:    "default",
		ScheduledAt: time.Now().UTC(),
		Attempt:     1,
		HandlerPayload: map[string]any{
			"body": map[string]any{"large": "this payload will definitely exceed fifty bytes"},
		},
		TimeoutSec: 30,
	}

	result := pn.Execute(context.Background(), task)
	if result.Success {
		t.Fatal("expected failure for oversized payload")
	}
	if result.ResultCode != "payload_too_large" {
		t.Errorf("result code = %q, want payload_too_large", result.ResultCode)
	}
}

func TestPGNotify_InvalidPayload(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	pn := NewPGNotify(db)

	tests := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{
			name:    "invalid channel type",
			payload: map[string]any{"channel": 123},
			want:    "invalid_payload",
		},
		{
			name:    "invalid body type",
			payload: map[string]any{"body": "not-an-object"},
			want:    "invalid_payload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := execute.AssignedTask{
				RunID:       "run-abc",
				JobID:       42,
				TenantID:    "default",
				ScheduledAt: time.Now().UTC(),
				Attempt:     1,
				HandlerPayload: tt.payload,
				TimeoutSec:  30,
			}
			result := pn.Execute(context.Background(), task)
			if result.Success {
				t.Errorf("expected failure")
			}
			if result.ResultCode != tt.want {
				t.Errorf("result code = %q, want %q", result.ResultCode, tt.want)
			}
		})
	}
}

func TestSanitizeChannel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello-world", "hello_world"},
		{"test_123", "test_123"},
		{"a@b#c", "abc"},
		{"", "orbitjob_default"},
		{"___", "___"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeChannel(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeChannel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveChannel(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	pn := NewPGNotify(db)

	tests := []struct {
		userChannel string
		tenantID    string
		jobID       int64
		want        string
	}{
		{"my-events", "default", 1, "orbitjob_my_events"},
		{"", "default", 42, "orbitjob_default_42"},
		{"a@b", "tenant-1", 99, "orbitjob_ab"},
	}

	for _, tt := range tests {
		t.Run(tt.userChannel, func(t *testing.T) {
			got := pn.resolveChannel(tt.userChannel, tt.tenantID, tt.jobID)
			if got != tt.want {
				t.Errorf("resolveChannel(%q, %q, %d) = %q, want %q", tt.userChannel, tt.tenantID, tt.jobID, got, tt.want)
			}
		})
	}
}

func TestPGNotify_NilDBPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil db")
		}
	}()
	_ = NewPGNotify(nil)
}

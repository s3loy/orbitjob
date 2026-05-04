package resource

import (
	"testing"
)

func TestNotFoundError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *NotFoundError
		want string
	}{
		{
			name: "job_not_found",
			err:  &NotFoundError{Resource: "job", ID: "job-123"},
			want: "job not found",
		},
		{
			name: "instance_not_found",
			err:  &NotFoundError{Resource: "instance", ID: 42},
			want: "instance not found",
		},
		{
			name: "empty_resource",
			err:  &NotFoundError{},
			want: " not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConflictError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *ConflictError
		want string
	}{
		{
			name: "with_message_returns_message",
			err: &ConflictError{
				Resource: "job",
				ID:       "job-123",
				Field:    "cron_expr",
				Message:  "job already scheduled at this time",
			},
			want: "job already scheduled at this time",
		},
		{
			name: "empty_message_falls_back_to_conflict",
			err: &ConflictError{
				Resource: "instance",
				ID:       "inst-1",
			},
			want: "instance conflict",
		},
		{
			name: "empty_resource_conflict",
			err:  &ConflictError{},
			want: " conflict",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

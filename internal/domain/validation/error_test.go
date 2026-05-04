package validation

import (
	"errors"
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		message string
		want    string
	}{
		{"with_field_and_message", "name", "required", "name: required"},
		{"empty_field", "", "generic error", "generic error"},
		{"empty_message", "field", "", "field: "},
		{"both_empty", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := New(tt.field, tt.message)
			if got == nil {
				t.Fatal("New() returned nil")
			}
			if got.Error() != tt.want {
				t.Errorf("Error() = %q, want %q", got.Error(), tt.want)
			}

			var target *Error
			if !errors.As(got, &target) {
				t.Errorf("New() did not return *Error")
			}
			if target.Field != tt.field {
				t.Errorf("Field = %q, want %q", target.Field, tt.field)
			}
			if target.Message != tt.message {
				t.Errorf("Message = %q, want %q", target.Message, tt.message)
			}
		})
	}
}

func TestErrorf(t *testing.T) {
	tests := []struct {
		name   string
		field  string
		format string
		args   []any
		want   string
	}{
		{
			name:   "with_args",
			field:  "port",
			format: "value %d out of range",
			args:   []any{8080},
			want:   "port: value 8080 out of range",
		},
		{
			name:   "no_args",
			field:  "name",
			format: "required",
			args:   nil,
			want:   "name: required",
		},
		{
			name:   "multiple_args",
			field:  "range",
			format: "[%d, %d] invalid",
			args:   []any{1, 100},
			want:   "range: [1, 100] invalid",
		},
		{
			name:   "string_args",
			field:  "type",
			format: "unsupported type %q",
			args:   []any{"boolean"},
			want:   `type: unsupported type "boolean"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Errorf(tt.field, tt.format, tt.args...)
			if got == nil {
				t.Fatal("Errorf() returned nil")
			}
			if got.Error() != tt.want {
				t.Errorf("Error() = %q, want %q", got.Error(), tt.want)
			}

			var target *Error
			if !errors.As(got, &target) {
				t.Errorf("Errorf() did not return *Error")
			}
			if target.Field != tt.field {
				t.Errorf("Field = %q, want %q", target.Field, tt.field)
			}
		})
	}
}

func TestError_Error_EdgeCases(t *testing.T) {
	cause := errors.New("wrapped")

	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{"nil_receiver", nil, "<nil>"},
		{"message_only", &Error{Message: "something failed"}, "something failed"},
		{"cause_only", &Error{Message: "wrap", Cause: cause}, "wrap: wrapped"},
		{"field_only", &Error{Field: "name", Message: "required"}, "name: required"},
		{"field_and_cause", &Error{Field: "name", Message: "invalid", Cause: cause}, "name: invalid: wrapped"},
		{"field_and_message_no_cause", &Error{Field: "port", Message: "out of range"}, "port: out of range"},
		{"message_and_cause_no_field", &Error{Message: "timeout", Cause: cause}, "timeout: wrapped"},
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

func TestIs_NonValidation(t *testing.T) {
	if Is(errors.New("plain error")) {
		t.Error("Is() should return false for non-validation errors")
	}
	if Is(nil) {
		t.Error("Is() should return false for nil")
	}
}

func TestAs_NonValidation(t *testing.T) {
	var target *Error
	if As(errors.New("plain error"), &target) {
		t.Error("As() should return false for non-validation errors")
	}
	if target != nil {
		t.Error("As() should set target to nil for non-validation errors")
	}

	target = nil
	if As(nil, &target) {
		t.Error("As() should return false for nil error")
	}
	if target != nil {
		t.Error("As() should set target to nil for nil error")
	}
}

func TestErrorHelpers(t *testing.T) {
	root := errors.New("root cause")
	err := &Error{
		Field:   "cron_expr",
		Message: "invalid cron_expr",
		Cause:   root,
	}

	if err.Error() != "cron_expr: invalid cron_expr: root cause" {
		t.Fatalf("unexpected error string: %q", err.Error())
	}
	if !errors.Is(err, root) {
		t.Fatalf("expected errors.Is to match wrapped cause")
	}
	if !Is(err) {
		t.Fatalf("expected Is() to detect validation error")
	}

	var target *Error
	if !As(err, &target) {
		t.Fatalf("expected As() to unwrap validation error")
	}
	if target.Field != "cron_expr" {
		t.Fatalf("expected field=cron_expr, got %q", target.Field)
	}
}

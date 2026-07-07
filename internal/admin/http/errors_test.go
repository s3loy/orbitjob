package http

import (
	"errors"
	"reflect"
	"testing"

	"github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"

	"orbitjob/internal/admin/http/apperror"
	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

func TestToAPIError_NotFound(t *testing.T) {
	got := toAPIError(&resource.NotFoundError{
		Resource: "job",
		ID:       42,
	})

	if got.Code != apperror.CodeNotFound {
		t.Fatalf("expected code=%q, got %q", apperror.CodeNotFound, got.Code)
	}
	if got.Message != "resource not found" {
		t.Fatalf("expected message=%q, got %q", "resource not found", got.Message)
	}
	if got.Field != "job" {
		t.Fatalf("expected field=%q, got %q", "job", got.Field)
	}
}

func TestToAPIError_Conflict(t *testing.T) {
	got := toAPIError(&resource.ConflictError{
		Resource: "job",
		ID:       42,
		Field:    "version",
		Message:  "stale job version",
	})

	if got.Code != apperror.CodeConflict {
		t.Fatalf("expected code=%q, got %q", apperror.CodeConflict, got.Code)
	}
	if got.Message != "stale job version" {
		t.Fatalf("expected message=%q, got %q", "stale job version", got.Message)
	}
	if got.Field != "version" {
		t.Fatalf("expected field=%q, got %q", "version", got.Field)
	}
}

func TestToAPIError_Validation(t *testing.T) {
	got := toAPIError(validation.New("cron_expr", "is required for cron jobs"))

	if got.Code != apperror.CodeValidation {
		t.Fatalf("expected code=%q, got %q", apperror.CodeValidation, got.Code)
	}
	if got.Message != "is required for cron jobs" {
		t.Fatalf("expected message=%q, got %q", "is required for cron jobs", got.Message)
	}
	if got.Field != "cron_expr" {
		t.Fatalf("expected field=%q, got %q", "cron_expr", got.Field)
	}
}

func TestToAPIError_ConflictFallbackField(t *testing.T) {
	got := toAPIError(&resource.ConflictError{
		Resource: "instance",
		ID:       "run-001",
		Field:    "",
		Message:  "version conflict",
	})

	if got.Code != apperror.CodeConflict {
		t.Fatalf("expected code=%q, got %q", apperror.CodeConflict, got.Code)
	}
	if got.Field != "instance" {
		t.Fatalf("expected field to fall back to Resource=%q, got %q", "instance", got.Field)
	}
}

func TestToAPIError_ConflictFallbackMessage(t *testing.T) {
	got := toAPIError(&resource.ConflictError{
		Resource: "job",
		ID:       42,
		Field:    "",
		Message:  "",
	})

	if got.Code != apperror.CodeConflict {
		t.Fatalf("expected code=%q, got %q", apperror.CodeConflict, got.Code)
	}
	if got.Message != "resource conflict" {
		t.Fatalf("expected fallback message=%q, got %q", "resource conflict", got.Message)
	}
	if got.Field != "job" {
		t.Fatalf("expected field to fall back to Resource=%q, got %q", "job", got.Field)
	}
}

func TestToAPIError_QuotaExceeded(t *testing.T) {
	got := toAPIError(domainjob.NewQuotaExceededError("max_jobs", 10))

	if got.Code != apperror.CodeQuotaExhausted {
		t.Fatalf("expected code=%q, got %q", apperror.CodeQuotaExhausted, got.Code)
	}
	if got.Message != "quota exhausted" {
		t.Fatalf("expected message=%q, got %q", "quota exhausted", got.Message)
	}
	if got.Field != "max_jobs" {
		t.Fatalf("expected field=%q, got %q", "max_jobs", got.Field)
	}
}

func TestToAPIError_Internal(t *testing.T) {
	got := toAPIError(errors.New("connection refused"))

	if got.Code != apperror.CodeInternal {
		t.Fatalf("expected code=%q, got %q", apperror.CodeInternal, got.Code)
	}
	if got.Message != "an internal error occurred" {
		t.Fatalf("expected message=%q, got %q", "an internal error occurred", got.Message)
	}
	if got.Field != "" {
		t.Fatalf("expected no field for internal error, got %q", got.Field)
	}
}

func TestToBindAPIError_MalformedRequest(t *testing.T) {
	got := toBindAPIError(errors.New("invalid character 'x' looking for beginning of object key string"))

	if got.Code != apperror.CodeMalformedRequest {
		t.Fatalf("expected code=%q, got %q", apperror.CodeMalformedRequest, got.Code)
	}
	if got.Message != "request could not be parsed" {
		t.Fatalf("expected message=%q, got %q", "request could not be parsed", got.Message)
	}
}

func TestToBindAPIError_Validation(t *testing.T) {
	type req struct {
		Name string `validate:"required"`
	}

	v := validator.New()
	err := v.Struct(req{})
	if err == nil {
		t.Fatal("expected validation error")
	}

	got := toBindAPIError(err)
	if got.Code != apperror.CodeValidation {
		t.Fatalf("expected code=%q, got %q", apperror.CodeValidation, got.Code)
	}
	if got.Field != "Name" {
		t.Fatalf("expected field=%q, got %q", "Name", got.Field)
	}
	if got.Message != "field is required" {
		t.Fatalf("expected message=%q, got %q", "field is required", got.Message)
	}
}

func TestHumanReadableTag(t *testing.T) {
	tests := []struct {
		tag  string
		want string
	}{
		{"required", "field is required"},
		{"min", "value is below minimum"},
		{"max", "value is above maximum"},
		{"oneof", "value must be one of the allowed options"},
		{"gte", "value is too small"},
		{"lte", "value is too large"},
		{"unknown", "field validation failed"},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			fe := fakeFieldError{tag: tt.tag}
			if got := humanReadableTag(fe); got != tt.want {
				t.Fatalf("expected message=%q, got %q", tt.want, got)
			}
		})
	}
}

type fakeFieldError struct {
	tag string
}

func (f fakeFieldError) Tag() string               { return f.tag }
func (f fakeFieldError) ActualTag() string         { return f.tag }
func (f fakeFieldError) Namespace() string         { return "" }
func (f fakeFieldError) StructNamespace() string   { return "" }
func (f fakeFieldError) Field() string             { return "" }
func (f fakeFieldError) StructField() string       { return "" }
func (f fakeFieldError) Value() interface{}        { return nil }
func (f fakeFieldError) Param() string             { return "" }
func (f fakeFieldError) Kind() reflect.Kind        { return reflect.String }
func (f fakeFieldError) Type() reflect.Type        { return nil }
func (f fakeFieldError) Translate(_ ut.Translator) string { return "" }
func (f fakeFieldError) Error() string             { return "" }

package validation

import (
	"errors"
	"testing"
)

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

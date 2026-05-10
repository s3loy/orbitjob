package instance

import (
	"strings"
	"testing"
)

func TestNormalizeOptionalString_Empty(t *testing.T) {
	val := "   "
	result, err := normalizeOptionalString(&val, "test_field", 64)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil for whitespace-only string, got %q", *result)
	}
}

func TestNormalizeOptionalString_TooLong(t *testing.T) {
	val := strings.Repeat("x", 65)
	_, err := normalizeOptionalString(&val, "test_field", 64)
	if err == nil {
		t.Fatal("expected error for too-long string, got nil")
	}
}

func TestNormalizeOptionalString_Nil(t *testing.T) {
	result, err := normalizeOptionalString(nil, "test_field", 64)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil for nil input, got %q", *result)
	}
}

package postgres

import (
	"testing"
)

func TestLabelValueList(t *testing.T) {
	t.Run("nil labels", func(t *testing.T) {
		result := labelValueList(nil)
		if result != nil {
			t.Fatalf("expected nil for nil input, got %v", result)
		}
	})

	t.Run("empty labels", func(t *testing.T) {
		result := labelValueList(map[string]any{})
		if result != nil {
			t.Fatalf("expected nil for empty input, got %v", result)
		}
	})

	t.Run("string values only", func(t *testing.T) {
		result := labelValueList(map[string]any{
			"queue": "video",
			"zone":  "us-east-1",
			"tier":  "high",
		})
		if len(result) != 3 {
			t.Fatalf("expected 3 values, got %d", len(result))
		}
		// Order is not guaranteed; check membership
		seen := make(map[string]bool)
		for _, v := range result {
			seen[v] = true
		}
		if !seen["video"] || !seen["us-east-1"] || !seen["high"] {
			t.Fatalf("expected video, us-east-1, high, got %v", result)
		}
	})

	t.Run("mixed types - filters non-strings", func(t *testing.T) {
		result := labelValueList(map[string]any{
			"queue": "video",
			"count": 5,
			"zone":  "us-east-1",
			"flag":  true,
		})
		if len(result) != 2 {
			t.Fatalf("expected 2 string values, got %d: %v", len(result), result)
		}
	})

	t.Run("no string values", func(t *testing.T) {
		result := labelValueList(map[string]any{
			"count": 5,
			"flag":  true,
			"rate":  3.14,
		})
		if len(result) != 0 {
			t.Fatalf("expected empty slice when no string values, got %v", result)
		}
	})

	t.Run("single entry string", func(t *testing.T) {
		result := labelValueList(map[string]any{
			"queue": "video",
		})
		if len(result) != 1 || result[0] != "video" {
			t.Fatalf("expected [video], got %v", result)
		}
	})
}

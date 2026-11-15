package sli

import (
	"fmt"
	"math"
)

// ValidateCriteria accepts only facts actually produced by run outcomes.
func ValidateCriteria(kind string, criteria map[string]any) error {
	if kind == TypeLatency {
		if _, ok := criteria["duration_ms"]; !ok {
			return fmt.Errorf("latency sli requires duration_ms criteria")
		}
	}
	for key, value := range criteria {
		switch key {
		case "status":
			status, ok := value.(string)
			if !ok || (status != "success" && status != "failed") {
				return fmt.Errorf("status must be success or failed")
			}
		case "duration_ms":
			if _, _, err := NumericCondition(value); err != nil {
				return err
			}
		default:
			return fmt.Errorf("criteria key %q has no producer on run outcomes", key)
		}
	}
	return nil
}

// NumericCondition parses one finite nonnegative duration threshold.
func NumericCondition(value any) (string, float64, error) {
	op := "=="
	if object, ok := value.(map[string]any); ok {
		var valid bool
		op, valid = object["op"].(string)
		if !valid {
			return "", 0, fmt.Errorf("numeric condition missing op")
		}
		if len(object) != 2 {
			return "", 0, fmt.Errorf("numeric condition requires only op and value")
		}
		value = object["value"]
	}
	switch op {
	case "<", "<=", ">", ">=", "==", "!=":
	default:
		return "", 0, fmt.Errorf("unsupported numeric operator %q", op)
	}
	var threshold float64
	switch n := value.(type) {
	case float64:
		threshold = n
	case float32:
		threshold = float64(n)
	case int:
		threshold = float64(n)
	case int64:
		threshold = float64(n)
	case int32:
		threshold = float64(n)
	case uint:
		threshold = float64(n)
	case uint64:
		threshold = float64(n)
	default:
		return "", 0, fmt.Errorf("numeric threshold must be a number")
	}
	if math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < 0 {
		return "", 0, fmt.Errorf("duration threshold must be finite and nonnegative")
	}
	return op, threshold, nil
}

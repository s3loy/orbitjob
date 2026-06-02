package evaluate

import (
	"fmt"

	"orbitjob/internal/core/domain/check"
)

const (
	SeverityOK       = "ok"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
	SeverityUnknown  = "unknown"
)

// Result holds the outcome of evaluating a check run.
type Result struct {
	OverallSeverity string          `json:"overall_severity"`
	Passed          int             `json:"passed"`
	Failed          int             `json:"failed"`
	Results         []RuleResult    `json:"results"`
}

// RuleResult holds the outcome of a single assertion rule.
type RuleResult struct {
	Metric   string  `json:"metric"`
	Actual   float64 `json:"actual"`
	Expected float64 `json:"expected"`
	Operator string  `json:"operator"`
	Passed   bool    `json:"passed"`
	Severity string  `json:"severity"`
}

// Evaluator evaluates handler output against assertion rules.
type Evaluator struct{}

// NewEvaluator creates a new Evaluator.
func NewEvaluator() *Evaluator {
	return &Evaluator{}
}

// Evaluate runs all assertion rules against the handler output and returns
// the overall severity along with per-rule results.
func (e *Evaluator) Evaluate(output map[string]any, rules []check.AssertionRule) Result {
	if len(rules) == 0 {
		return Result{OverallSeverity: SeverityOK, Passed: 0, Failed: 0, Results: nil}
	}

	var results []RuleResult
	var passed, failed int
	var hasWarning, hasCritical, hasUnknown bool

	for _, rule := range rules {
		rr := e.evaluateRule(output, rule)
		results = append(results, rr)

		if rr.Passed {
			passed++
		} else {
			failed++
			switch rr.Severity {
			case SeverityCritical:
				hasCritical = true
			case SeverityWarning:
				hasWarning = true
			case SeverityUnknown:
				hasUnknown = true
			}
		}
	}

	overall := SeverityOK
	switch {
	case hasCritical:
		overall = SeverityCritical
	case hasWarning:
		overall = SeverityWarning
	case hasUnknown && failed > 0:
		overall = SeverityUnknown
	}

	return Result{
		OverallSeverity: overall,
		Passed:          passed,
		Failed:          failed,
		Results:         results,
	}
}

func (e *Evaluator) evaluateRule(output map[string]any, rule check.AssertionRule) RuleResult {
	actualValue, ok := output[rule.Metric]
	if !ok {
		return RuleResult{
			Metric:   rule.Metric,
			Actual:   0,
			Expected: rule.Threshold,
			Operator: rule.Operator,
			Passed:   false,
			Severity: SeverityUnknown,
		}
	}

	actual, err := toFloat64(actualValue)
	if err != nil {
		return RuleResult{
			Metric:   rule.Metric,
			Actual:   0,
			Expected: rule.Threshold,
			Operator: rule.Operator,
			Passed:   false,
			Severity: SeverityUnknown,
		}
	}

	triggered := compare(actual, rule.Threshold, rule.Operator)
	severity := SeverityOK
	if triggered {
		severity = rule.Severity
	}

	return RuleResult{
		Metric:   rule.Metric,
		Actual:   actual,
		Expected: rule.Threshold,
		Operator: rule.Operator,
		Passed:   !triggered,
		Severity: severity,
	}
}

func toFloat64(v any) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case float32:
		return float64(val), nil
	case int:
		return float64(val), nil
	case int32:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case uint:
		return float64(val), nil
	case uint32:
		return float64(val), nil
	case uint64:
		return float64(val), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", v)
	}
}

func compare(actual, expected float64, operator string) bool {
	switch operator {
	case ">":
		return actual > expected
	case "<":
		return actual < expected
	case "==":
		return actual == expected
	case "!=":
		return actual != expected
	case ">=":
		return actual >= expected
	case "<=":
		return actual <= expected
	default:
		return false
	}
}

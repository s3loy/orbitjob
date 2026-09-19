package workflow

import (
	"fmt"
)

// Condition is a task's execution gate, evaluated once when all of the task's
// dependencies are terminal. A nil condition means unconditional execution.
type Condition struct {
	Type  ConditionType
	Rules []Rule
}

// ConditionType selects how a condition's rules combine.
type ConditionType string

const (
	// ConditionAll passes only when every rule passes, and short-circuits on
	// the first failure.
	ConditionAll ConditionType = "all"
	// ConditionAny passes when at least one rule passes, and short-circuits
	// on the first pass.
	ConditionAny ConditionType = "any"
)

// Field names the ledger-visible fact a rule reads. The set is closed on
// purpose: these are the fields the run ledger can actually answer, so
// evaluation stays a pure function of stored rows. Exit codes and outputs are
// deliberately absent — the ledger does not carry them.
type Field string

const (
	// FieldPhase reads the step's lifecycle phase. Post-terminal the values
	// that matter are Succeeded, Failed and Canceled.
	FieldPhase Field = "phase"
	// FieldAttempts reads how many attempts the upstream step spent.
	FieldAttempts Field = "attempts"
	// FieldDurationMs reads the terminal attempt's wall-clock duration in
	// milliseconds.
	FieldDurationMs Field = "duration_ms"
)

// Operator is the comparison a rule applies. The string operators compare
// phase names; the numeric operators compare attempt counts and durations.
type Operator string

const (
	OpEq    Operator = "eq"
	OpNe    Operator = "ne"
	OpIn    Operator = "in"
	OpNotIn Operator = "not_in"
	OpLt    Operator = "lt"
	OpLe    Operator = "le"
	OpGt    Operator = "gt"
	OpGe    Operator = "ge"
)

// Rule is one comparison against one upstream task's facts. The old workflow
// domain's free-form field string and interface{} value are gone: Task and
// Field are split so validation can prove a rule addresses a declared
// dependency, and the comparands are typed by operator class.
type Rule struct {
	// Task names the upstream task whose facts the rule reads. It must appear
	// in the depending task's dependsOn, so a rule can neither reference
	// not-yet-existing state nor create hidden ordering.
	Task     string
	Field    Field
	Operator Operator
	// Values carries the comparands of the string operators: exactly one for
	// eq and ne, one or more for in and not_in, empty for numeric operators.
	Values []string
	// Number carries the comparand of the numeric operators; string operators
	// leave it nil.
	Number *int64
}

// EvaluateCondition reports whether a task's condition passes against the
// terminal states of its upstream tasks. The semantics are the deleted
// domain's, recovered intact: a nil condition and an empty rule set both mean
// execute; all short-circuits on the first failing rule, any on the first
// passing rule; an unknown condition type is an error, never a silent pass.
//
// The one change from the old evaluator is the operand grammar: rules address
// a named task's field instead of looking the raw field string up as a map
// key, which in the old code could only ever compare whole status structs and
// never a nested field.
//
// A rule whose task has no recorded state is an error, not a failed match:
// evaluation happens exactly once, after every dependency is terminal, so a
// missing state means the caller built the map wrong or validated the DAG
// wrong, and both are bugs worth surfacing.
func EvaluateCondition(cond *Condition, states map[string]StepState) (bool, error) {
	if cond == nil {
		return true, nil
	}
	if len(cond.Rules) == 0 {
		return true, nil
	}

	switch cond.Type {
	case ConditionAll:
		for _, rule := range cond.Rules {
			pass, err := evaluateRule(rule, states)
			if err != nil {
				return false, err
			}
			if !pass {
				return false, nil
			}
		}
		return true, nil
	case ConditionAny:
		for _, rule := range cond.Rules {
			pass, err := evaluateRule(rule, states)
			if err != nil {
				return false, err
			}
			if pass {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, fmt.Errorf("unknown condition type: %s", cond.Type)
	}
}

func evaluateRule(rule Rule, states map[string]StepState) (bool, error) {
	state, ok := states[rule.Task]
	if !ok {
		return false, fmt.Errorf("condition rule has no recorded state for task %q", rule.Task)
	}

	if isStringOperator(rule.Operator) {
		if rule.Field != FieldPhase {
			return false, fmt.Errorf("condition on %q: operator %q does not apply to field %q", rule.Task, rule.Operator, rule.Field)
		}
		if rule.Number != nil {
			return false, fmt.Errorf("condition on %q: operator %q takes values, not a number", rule.Task, rule.Operator)
		}
		phase := string(state.Phase)
		switch rule.Operator {
		case OpEq:
			if len(rule.Values) != 1 {
				return false, fmt.Errorf("condition on %q: operator %q requires exactly one value", rule.Task, rule.Operator)
			}
			return phase == rule.Values[0], nil
		case OpNe:
			if len(rule.Values) != 1 {
				return false, fmt.Errorf("condition on %q: operator %q requires exactly one value", rule.Task, rule.Operator)
			}
			return phase != rule.Values[0], nil
		case OpIn:
			if len(rule.Values) < 1 {
				return false, fmt.Errorf("condition on %q: operator %q requires at least one value", rule.Task, rule.Operator)
			}
			return contains(rule.Values, phase), nil
		case OpNotIn:
			if len(rule.Values) < 1 {
				return false, fmt.Errorf("condition on %q: operator %q requires at least one value", rule.Task, rule.Operator)
			}
			return !contains(rule.Values, phase), nil
		}
	}

	if !isNumericOperator(rule.Operator) {
		return false, fmt.Errorf("condition on %q: unknown operator: %s", rule.Task, rule.Operator)
	}
	if rule.Field == FieldPhase {
		return false, fmt.Errorf("condition on %q: operator %q does not apply to field %q", rule.Task, rule.Operator, rule.Field)
	}
	if rule.Number == nil {
		return false, fmt.Errorf("condition on %q: operator %q requires a number", rule.Task, rule.Operator)
	}
	var actual int64
	switch rule.Field {
	case FieldAttempts:
		actual = int64(state.Attempt)
	case FieldDurationMs:
		actual = state.DurationMs
	default:
		return false, fmt.Errorf("condition on %q: field %q has no numeric value", rule.Task, rule.Field)
	}
	switch rule.Operator {
	case OpLt:
		return actual < *rule.Number, nil
	case OpLe:
		return actual <= *rule.Number, nil
	case OpGt:
		return actual > *rule.Number, nil
	case OpGe:
		return actual >= *rule.Number, nil
	}
	return false, fmt.Errorf("condition on %q: unknown operator: %s", rule.Task, rule.Operator)
}

func isStringOperator(op Operator) bool {
	return op == OpEq || op == OpNe || op == OpIn || op == OpNotIn
}

func isNumericOperator(op Operator) bool {
	return op == OpLt || op == OpLe || op == OpGt || op == OpGe
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

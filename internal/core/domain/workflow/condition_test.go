package workflow

import (
	"strings"
	"testing"

	"orbitjob/internal/core/domain/jobrun"
)

// EvaluateCondition carries the deleted domain's semantics over intact: nil
// and empty mean execute, all short-circuits on the first failure, any on the
// first pass, and an unknown condition type is an error. What changed is the
// operand grammar, so these tests pin the closed field and operator sets on
// top of the recovered control flow.

func states() map[string]StepState {
	return map[string]StepState{
		"export": {Phase: jobrun.Succeeded, Attempt: 1, DurationMs: 12000},
		"load":   {Phase: jobrun.Failed, Attempt: 3, DurationMs: 45000},
	}
}

func TestEvaluateCondition_NilAndEmptyMeanExecute(t *testing.T) {
	pass, err := EvaluateCondition(nil, states())
	if err != nil || !pass {
		t.Errorf("nil condition = (%v, %v), want (true, nil)", pass, err)
	}
	empty := &Condition{Type: ConditionAll}
	pass, err = EvaluateCondition(empty, states())
	if err != nil || !pass {
		t.Errorf("empty rules = (%v, %v), want (true, nil)", pass, err)
	}
}

func TestEvaluateCondition_All(t *testing.T) {
	cond := &Condition{Type: ConditionAll, Rules: []Rule{
		{Task: "export", Field: FieldPhase, Operator: OpEq, Values: []string{"Succeeded"}},
		{Task: "load", Field: FieldPhase, Operator: OpIn, Values: []string{"Succeeded", "Failed"}},
	}}
	pass, err := EvaluateCondition(cond, states())
	if err != nil || !pass {
		t.Errorf("all passing rules = (%v, %v), want (true, nil)", pass, err)
	}

	cond = &Condition{Type: ConditionAll, Rules: []Rule{
		{Task: "export", Field: FieldPhase, Operator: OpEq, Values: []string{"Succeeded"}},
		{Task: "export", Field: FieldPhase, Operator: OpEq, Values: []string{"Failed"}},
	}}
	pass, err = EvaluateCondition(cond, states())
	if err != nil || pass {
		t.Errorf("one failing rule = (%v, %v), want (false, nil)", pass, err)
	}
}

func TestEvaluateCondition_Any(t *testing.T) {
	cond := &Condition{Type: ConditionAny, Rules: []Rule{
		{Task: "load", Field: FieldPhase, Operator: OpNe, Values: []string{"Succeeded"}},
		{Task: "load", Field: FieldPhase, Operator: OpEq, Values: []string{"Succeeded"}},
	}}
	pass, err := EvaluateCondition(cond, states())
	if err != nil || !pass {
		t.Errorf("one passing rule = (%v, %v), want (true, nil)", pass, err)
	}

	cond = &Condition{Type: ConditionAny, Rules: []Rule{
		{Task: "load", Field: FieldPhase, Operator: OpEq, Values: []string{"Succeeded"}},
		{Task: "load", Field: FieldAttempts, Operator: OpLt, Number: int64p(1)},
	}}
	pass, err = EvaluateCondition(cond, states())
	if err != nil || pass {
		t.Errorf("no passing rule = (%v, %v), want (false, nil)", pass, err)
	}
}

// The short-circuits are load-bearing: a rule after the decisive one must
// never be evaluated. Each proof places a rule that would return an error
// (a task with no recorded state) after the decisive rule, so a scan-everything
// evaluator turns (false, nil) or (true, nil) into an error and fails here.
func TestEvaluateCondition_ShortCircuit(t *testing.T) {
	missingTask := Rule{Task: "does-not-exist", Field: FieldPhase, Operator: OpEq, Values: []string{"Succeeded"}}

	all := &Condition{Type: ConditionAll, Rules: []Rule{
		{Task: "export", Field: FieldPhase, Operator: OpEq, Values: []string{"Failed"}}, // decides: fail
		missingTask, // must not run
	}}
	pass, err := EvaluateCondition(all, states())
	if err != nil {
		t.Fatalf("all did not short-circuit on first failure: %v", err)
	}
	if pass {
		t.Fatal("all passed a failing rule")
	}

	any := &Condition{Type: ConditionAny, Rules: []Rule{
		{Task: "export", Field: FieldPhase, Operator: OpEq, Values: []string{"Succeeded"}}, // decides: pass
		missingTask, // must not run
	}}
	pass, err = EvaluateCondition(any, states())
	if err != nil {
		t.Fatalf("any did not short-circuit on first pass: %v", err)
	}
	if !pass {
		t.Fatal("any failed a passing rule")
	}
}

func TestEvaluateCondition_PhaseOperators(t *testing.T) {
	tests := []struct {
		name     string
		task     string
		operator Operator
		values   []string
		want     bool
	}{
		{"eq matches", "export", OpEq, []string{"Succeeded"}, true},
		{"eq mismatches", "export", OpEq, []string{"Failed"}, false},
		{"ne mismatches", "export", OpNe, []string{"Succeeded"}, false},
		{"ne matches", "export", OpNe, []string{"Failed"}, true},
		{"in contains", "load", OpIn, []string{"Succeeded", "Failed"}, true},
		{"in omits", "load", OpIn, []string{"Succeeded"}, false},
		{"not_in omits", "load", OpNotIn, []string{"Failed"}, false},
		{"not_in contains", "load", OpNotIn, []string{"Succeeded"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := &Condition{Type: ConditionAll, Rules: []Rule{
				{Task: tt.task, Field: FieldPhase, Operator: tt.operator, Values: tt.values},
			}}
			pass, err := EvaluateCondition(cond, states())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pass != tt.want {
				t.Errorf("got %v, want %v", pass, tt.want)
			}
		})
	}
}

func TestEvaluateCondition_NumericOperators(t *testing.T) {
	tests := []struct {
		name     string
		field    Field
		task     string
		operator Operator
		number   int64
		want     bool
	}{
		{"attempts lt", FieldAttempts, "load", OpLt, 4, true},
		{"attempts le", FieldAttempts, "load", OpLe, 3, true},
		{"attempts gt", FieldAttempts, "load", OpGt, 3, false},
		{"attempts ge", FieldAttempts, "load", OpGe, 4, false},
		{"duration lt", FieldDurationMs, "export", OpLt, 12001, true},
		{"duration le", FieldDurationMs, "export", OpLe, 12000, true},
		{"duration gt", FieldDurationMs, "export", OpGt, 11999, true},
		{"duration ge", FieldDurationMs, "export", OpGe, 12001, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := &Condition{Type: ConditionAll, Rules: []Rule{
				{Task: tt.task, Field: tt.field, Operator: tt.operator, Number: int64p(tt.number)},
			}}
			pass, err := EvaluateCondition(cond, states())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pass != tt.want {
				t.Errorf("got %v, want %v", pass, tt.want)
			}
		})
	}
}

// A rule whose task has no recorded state is an error, never a silent false:
// evaluation happens exactly once, after every dependency is terminal, so
// absence means the caller built the state map or validated the DAG wrong.
func TestEvaluateCondition_MissingStateIsAnError(t *testing.T) {
	cond := &Condition{Type: ConditionAll, Rules: []Rule{
		{Task: "nowhere", Field: FieldPhase, Operator: OpEq, Values: []string{"Succeeded"}},
	}}
	if _, err := EvaluateCondition(cond, states()); err == nil {
		t.Fatal("missing state accepted")
	}
}

func TestEvaluateCondition_MalformedRulesAreErrors(t *testing.T) {
	tests := []struct {
		name string
		rule Rule
	}{
		{
			name: "unknown operator",
			rule: Rule{Task: "export", Field: FieldPhase, Operator: "matches", Values: []string{"Succeeded"}},
		},
		{
			name: "unknown field",
			rule: Rule{Task: "export", Field: "exit_code", Operator: OpEq, Values: []string{"0"}},
		},
		{
			name: "eq with two values",
			rule: Rule{Task: "export", Field: FieldPhase, Operator: OpEq, Values: []string{"Succeeded", "Failed"}},
		},
		{
			name: "eq with no values",
			rule: Rule{Task: "export", Field: FieldPhase, Operator: OpEq},
		},
		{
			name: "in with no values",
			rule: Rule{Task: "export", Field: FieldPhase, Operator: OpIn},
		},
		{
			name: "numeric operator on phase",
			rule: Rule{Task: "export", Field: FieldPhase, Operator: OpLt, Number: int64p(1)},
		},
		{
			name: "string operator with number",
			rule: Rule{Task: "export", Field: FieldPhase, Operator: OpEq, Number: int64p(1)},
		},
		{
			name: "numeric operator without number",
			rule: Rule{Task: "export", Field: FieldAttempts, Operator: OpGt},
		},
		{
			name: "string operator with values on numeric field",
			rule: Rule{Task: "export", Field: FieldAttempts, Operator: OpIn, Values: []string{"1"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := &Condition{Type: ConditionAll, Rules: []Rule{tt.rule}}
			_, err := EvaluateCondition(cond, states())
			if err == nil {
				t.Fatal("malformed rule accepted")
			}
			if !strings.Contains(err.Error(), tt.rule.Task) && tt.rule.Task != "" {
				t.Errorf("error %q does not name the offending task", err)
			}
		})
	}
}

func int64p(v int64) *int64 { return &v }

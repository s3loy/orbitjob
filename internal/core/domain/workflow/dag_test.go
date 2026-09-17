package workflow

import (
	"regexp"
	"testing"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
)

// The DAG validation tests recover the deleted domain's validate_test.go
// intent (valid, cycle, duplicate, topological order) against the new view
// shape, where dependency edges live on each task's DependsOn instead of a
// separate Edge list, and add the checks the CR grammar needs: non-empty job
// references and condition rules that only read declared dependencies.

func TestValidateDAG_ValidChain(t *testing.T) {
	dag := DAG{
		Tasks: []Task{
			{Name: "export", JobRef: "nightly-export"},
			{Name: "load", JobRef: "nightly-load", DependsOn: []string{"export"}},
			{Name: "notify", JobRef: "slack-notify", DependsOn: []string{"load"}},
		},
	}
	if err := ValidateDAG(dag); err != nil {
		t.Errorf("expected valid DAG, got error: %v", err)
	}
}

func TestValidateDAG_Cycle(t *testing.T) {
	dag := DAG{
		Tasks: []Task{
			{Name: "a", JobRef: "job-a", DependsOn: []string{"c"}},
			{Name: "b", JobRef: "job-b", DependsOn: []string{"a"}},
			{Name: "c", JobRef: "job-c", DependsOn: []string{"b"}},
		},
	}
	if err := ValidateDAG(dag); err == nil {
		t.Error("expected cycle error, got nil")
	}
}

func TestValidateDAG_DuplicateTaskName(t *testing.T) {
	dag := DAG{
		Tasks: []Task{
			{Name: "export", JobRef: "nightly-export"},
			{Name: "export", JobRef: "other-export"},
		},
	}
	if err := ValidateDAG(dag); err == nil {
		t.Error("expected duplicate name error, got nil")
	}
}

func TestValidateDAG_Rejections(t *testing.T) {
	tests := []struct {
		name string
		dag  DAG
	}{
		{
			name: "no tasks",
			dag:  DAG{},
		},
		{
			name: "empty task name",
			dag: DAG{Tasks: []Task{
				{Name: "", JobRef: "job-a"},
			}},
		},
		{
			name: "empty jobRef",
			dag: DAG{Tasks: []Task{
				{Name: "export", JobRef: ""},
			}},
		},
		{
			name: "unknown dependency",
			dag: DAG{Tasks: []Task{
				{Name: "load", JobRef: "job-b", DependsOn: []string{"export"}},
			}},
		},
		{
			name: "self dependency",
			dag: DAG{Tasks: []Task{
				{Name: "load", JobRef: "job-b", DependsOn: []string{"load"}},
			}},
		},
		{
			name: "dependency declared twice",
			dag: DAG{Tasks: []Task{
				{Name: "a", JobRef: "job-a"},
				{Name: "b", JobRef: "job-b", DependsOn: []string{"a", "a"}},
			}},
		},
		{
			name: "condition rule reads a task outside dependsOn",
			dag: DAG{Tasks: []Task{
				{Name: "a", JobRef: "job-a"},
				{Name: "b", JobRef: "job-b"},
				{Name: "c", JobRef: "job-c", DependsOn: []string{"a"},
					Condition: &Condition{Type: ConditionAll, Rules: []Rule{
						{Task: "b", Field: FieldPhase, Operator: OpEq, Values: []string{"Succeeded"}},
					}}},
			}},
		},
		{
			name: "condition rule with empty task",
			dag: DAG{Tasks: []Task{
				{Name: "a", JobRef: "job-a"},
				{Name: "b", JobRef: "job-b", DependsOn: []string{"a"},
					Condition: &Condition{Type: ConditionAll, Rules: []Rule{
						{Task: "", Field: FieldPhase, Operator: OpEq, Values: []string{"Succeeded"}},
					}}},
			}},
		},
		{
			name: "condition with unknown type",
			dag: DAG{Tasks: []Task{
				{Name: "a", JobRef: "job-a"},
				{Name: "b", JobRef: "job-b", DependsOn: []string{"a"},
					Condition: &Condition{Type: "some", Rules: []Rule{
						{Task: "a", Field: FieldPhase, Operator: OpEq, Values: []string{"Succeeded"}},
					}}},
			}},
		},
		{
			name: "condition without rules",
			dag: DAG{Tasks: []Task{
				{Name: "a", JobRef: "job-a"},
				{Name: "b", JobRef: "job-b", DependsOn: []string{"a"},
					Condition: &Condition{Type: ConditionAll}},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateDAG(tt.dag); err == nil {
				t.Errorf("expected rejection, got nil")
			}
		})
	}
}

func TestTopologicalSort_Diamond(t *testing.T) {
	dag := DAG{
		Tasks: []Task{
			{Name: "extract", JobRef: "job-extract"},
			{Name: "transform-a", JobRef: "job-ta", DependsOn: []string{"extract"}},
			{Name: "transform-b", JobRef: "job-tb", DependsOn: []string{"extract"}},
			{Name: "load", JobRef: "job-load", DependsOn: []string{"transform-a", "transform-b"}},
		},
	}
	order, err := TopologicalSort(dag)
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}
	if len(order) != 4 {
		t.Fatalf("expected 4 tasks, got %d", len(order))
	}
	position := make(map[string]int, len(order))
	for i, name := range order {
		position[name] = i
	}
	// Every dependency must precede its dependent; the parallel transforms may
	// come in either order.
	if position["extract"] > position["transform-a"] || position["extract"] > position["transform-b"] {
		t.Errorf("transforms scheduled before their dependency: %v", order)
	}
	if position["transform-a"] > position["load"] || position["transform-b"] > position["load"] {
		t.Errorf("load scheduled before its dependencies: %v", order)
	}
}

func TestTopologicalSort_CycleIsAnError(t *testing.T) {
	dag := DAG{
		Tasks: []Task{
			{Name: "a", JobRef: "job-a", DependsOn: []string{"b"}},
			{Name: "b", JobRef: "job-b", DependsOn: []string{"a"}},
		},
	}
	if _, err := TopologicalSort(dag); err == nil {
		t.Error("expected cycle error, got nil")
	}
}

func TestDAGFromSpec(t *testing.T) {
	zero := int64(30000)
	spec := v1alpha1.WorkflowJobSpec{
		TimeoutSeconds: 3600,
		FailPolicy:     v1alpha1.Continue,
		Tasks: []v1alpha1.WorkflowTaskSpec{
			{Name: "export", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "nightly-export"}},
			{
				Name:      "load",
				JobRef:    v1alpha1.WorkflowTaskJobRef{Name: "nightly-load"},
				DependsOn: []string{"export"},
				Condition: &v1alpha1.WorkflowConditionSpec{
					Type: v1alpha1.ConditionAll,
					Rules: []v1alpha1.WorkflowRule{
						{Task: "export", Field: v1alpha1.RuleFieldPhase, Operator: v1alpha1.OperatorEq, Values: []string{"Succeeded"}},
						{Task: "export", Field: v1alpha1.RuleFieldDurationMs, Operator: v1alpha1.OperatorLe, Number: &zero},
					},
				},
			},
		},
	}

	dag := DAGFromSpec(spec)
	if dag.FailPolicy != FailPolicyContinue {
		t.Errorf("FailPolicy = %q, want %q", dag.FailPolicy, FailPolicyContinue)
	}
	if dag.TimeoutSeconds != 3600 {
		t.Errorf("TimeoutSeconds = %d, want 3600", dag.TimeoutSeconds)
	}
	if len(dag.Tasks) != 2 {
		t.Fatalf("converted %d tasks, want 2", len(dag.Tasks))
	}
	load := dag.Tasks[1]
	if load.JobRef != "nightly-load" {
		t.Errorf("JobRef = %q, want nightly-load", load.JobRef)
	}
	if len(load.DependsOn) != 1 || load.DependsOn[0] != "export" {
		t.Errorf("DependsOn = %v, want [export]", load.DependsOn)
	}
	if load.Condition == nil || len(load.Condition.Rules) != 2 {
		t.Fatalf("condition not carried: %+v", load.Condition)
	}
	if load.Condition.Rules[0].Values[0] != "Succeeded" {
		t.Errorf("phase comparand lost: %+v", load.Condition.Rules[0])
	}
	if load.Condition.Rules[1].Number == nil || *load.Condition.Rules[1].Number != 30000 {
		t.Errorf("numeric comparand lost: %+v", load.Condition.Rules[1])
	}

	// An unset fail policy defaults to FailFast, the spec's documented default.
	unset := DAGFromSpec(v1alpha1.WorkflowJobSpec{Tasks: []v1alpha1.WorkflowTaskSpec{
		{Name: "export", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "j"}},
	}})
	if unset.FailPolicy != FailPolicyFailFast {
		t.Errorf("default FailPolicy = %q, want %q", unset.FailPolicy, FailPolicyFailFast)
	}
}

func TestStepOccurrenceKey(t *testing.T) {
	var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

	key := StepOccurrenceKey("wf-uid", "runoccurrencekey", "export")
	if !hex64.MatchString(key) {
		t.Fatalf("step key %q is not 64 lowercase hex characters", key)
	}

	// Determinism is the contract: a restarted or re-elected walker re-derives
	// the same key, and the ledger's (source_uid, occurrence_key) dedup is what
	// turns that into "a task never runs twice".
	again := StepOccurrenceKey("wf-uid", "runoccurrencekey", "export")
	if key != again {
		t.Fatalf("step key not deterministic: %q then %q", key, again)
	}

	// Distinctness: a different task of the same run, and the same task of a
	// different run, must never collide.
	if key == StepOccurrenceKey("wf-uid", "runoccurrencekey", "load") {
		t.Error("two tasks of one run derived the same step key")
	}
	if key == StepOccurrenceKey("wf-uid-other", "runoccurrencekey", "export") {
		t.Error("one task of two workflows derived the same step key")
	}
	if key == StepOccurrenceKey("wf-uid", "runoccurrencekey-other", "export") {
		t.Error("one task of two runs derived the same step key")
	}
}

// Within one workflow run the source uid and the run occurrence key are
// constant, system-generated segments (a Kubernetes UID and a 64-hex digest,
// neither of which contains the "|" delimiter), so the derivation is
// injective over task names: two different tasks of one run can never share
// a step key, however their names are punctuated.
func TestStepOccurrenceKeyInjectivePerRun(t *testing.T) {
	const uid = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	const runKey = "0f4d1f5e2f9b0e4a5f6b2d1c8e7a9f3b2c5d4e6f8a0b1c2d3e4f5a6b7c8d9e0f"

	seen := make(map[string]string)
	for _, task := range []string{"export", "load", "a|b", "|", "wfstep|x|y", "a|b|c"} {
		key := StepOccurrenceKey(uid, runKey, task)
		if other, collision := seen[key]; collision {
			t.Errorf("tasks %q and %q derived the same step key", other, task)
		}
		seen[key] = task
	}
}

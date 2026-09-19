package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
)

// ValidateDAG is the admission-critical check a WorkflowJob revision must
// pass before it can become active: non-empty task set, unique non-empty task
// names, resolvable job references, dependency edges that name declared
// tasks, condition rules that only read the owning task's own dependencies,
// and no cycles. It recovers the deleted domain's ValidateDAG intent and adds
// the two rules the CR grammar needs: job references must be non-empty, and a
// condition rule may only read a task in the referencing task's dependsOn.
//
// Validation happens at projection, in the operator's WorkflowJob reconcile;
// an invalid spec earns a status condition and no active revision. v1alpha1's
// own WorkflowJobSpec.Validate covers the CR-shape fields independently; this
// is the authoritative check for the converted view, and it re-checks the
// shared invariants so neither boundary can be bypassed by the other.
func ValidateDAG(dag DAG) error {
	if len(dag.Tasks) == 0 {
		return fmt.Errorf("workflow must have at least one task")
	}

	names := make(map[string]bool, len(dag.Tasks))
	for _, task := range dag.Tasks {
		if task.Name == "" {
			return fmt.Errorf("task name cannot be empty")
		}
		if names[task.Name] {
			return fmt.Errorf("duplicate task name: %s", task.Name)
		}
		names[task.Name] = true
	}

	for _, task := range dag.Tasks {
		if task.JobRef == "" {
			return fmt.Errorf("task %s: jobRef must name a ScheduledJob definition", task.Name)
		}
		dependencies := make(map[string]bool, len(task.DependsOn))
		for _, dep := range task.DependsOn {
			if dep == task.Name {
				return fmt.Errorf("task %s depends on itself", task.Name)
			}
			if !names[dep] {
				return fmt.Errorf("task %s depends on unknown task %s", task.Name, dep)
			}
			if dependencies[dep] {
				return fmt.Errorf("task %s depends on %s twice", task.Name, dep)
			}
			dependencies[dep] = true
		}
		if task.Condition == nil {
			continue
		}
		if task.Condition.Type != ConditionAll && task.Condition.Type != ConditionAny {
			return fmt.Errorf("task %s: unknown condition type: %s", task.Name, task.Condition.Type)
		}
		if len(task.Condition.Rules) == 0 {
			return fmt.Errorf("task %s: condition must have at least one rule", task.Name)
		}
		for _, rule := range task.Condition.Rules {
			if rule.Task == "" {
				return fmt.Errorf("task %s: condition rule task cannot be empty", task.Name)
			}
			if !dependencies[rule.Task] {
				return fmt.Errorf("task %s: condition rule reads %s, which is not in its dependsOn", task.Name, rule.Task)
			}
		}
	}

	if hasCycle(dag) {
		return fmt.Errorf("workflow contains a cycle")
	}
	return nil
}

// hasCycle detects cycles with the deleted domain's depth-first search,
// carried over intact.
func hasCycle(dag DAG) bool {
	graph := make(map[string][]string)
	for _, task := range dag.Tasks {
		for _, dep := range task.DependsOn {
			graph[dep] = append(graph[dep], task.Name)
		}
	}

	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var dfs func(node string) bool
	dfs = func(node string) bool {
		visited[node] = true
		recStack[node] = true
		for _, neighbor := range graph[node] {
			if !visited[neighbor] {
				if dfs(neighbor) {
					return true
				}
			} else if recStack[neighbor] {
				return true
			}
		}
		recStack[node] = false
		return false
	}

	for _, task := range dag.Tasks {
		if !visited[task.Name] {
			if dfs(task.Name) {
				return true
			}
		}
	}
	return false
}

// TopologicalSort returns the task names in dependency order (Kahn's
// algorithm, carried over from the deleted domain). An error means the DAG
// contains a cycle; ValidateDAG is the admission gate, so callers that
// validate first should never see it.
func TopologicalSort(dag DAG) ([]string, error) {
	inDegree := make(map[string]int, len(dag.Tasks))
	graph := make(map[string][]string)
	for _, task := range dag.Tasks {
		inDegree[task.Name] += 0
	}
	for _, task := range dag.Tasks {
		for _, dep := range task.DependsOn {
			graph[dep] = append(graph[dep], task.Name)
			inDegree[task.Name]++
		}
	}

	queue := make([]string, 0, len(dag.Tasks))
	for _, task := range dag.Tasks {
		if inDegree[task.Name] == 0 {
			queue = append(queue, task.Name)
		}
	}

	order := make([]string, 0, len(dag.Tasks))
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		order = append(order, current)
		for _, neighbor := range graph[current] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(order) != len(dag.Tasks) {
		return nil, fmt.Errorf("workflow contains a cycle")
	}
	return order, nil
}

// DAGFromSpec converts a WorkflowJob spec into the walker's view. The
// conversion is total: every field the advance pass consumes is copied, and
// anything it must not consume (task-level execution detail, which stays
// owned by the referenced definitions) has no place in the DAG to lose.
// An unset fail policy defaults to FailFast, the spec's documented default.
func DAGFromSpec(spec v1alpha1.WorkflowJobSpec) DAG {
	dag := DAG{
		FailPolicy:     FailPolicyFailFast,
		TimeoutSeconds: int(spec.TimeoutSeconds),
		Tasks:          make([]Task, 0, len(spec.Tasks)),
	}
	if spec.FailPolicy == v1alpha1.Continue {
		dag.FailPolicy = FailPolicyContinue
	}
	for _, t := range spec.Tasks {
		task := Task{
			Name:      t.Name,
			JobRef:    t.JobRef.Name,
			DependsOn: append([]string(nil), t.DependsOn...),
		}
		if t.Condition != nil {
			cond := &Condition{Type: ConditionType(t.Condition.Type)}
			for _, r := range t.Condition.Rules {
				cond.Rules = append(cond.Rules, Rule{
					Task:     r.Task,
					Field:    Field(r.Field),
					Operator: Operator(r.Operator),
					Values:   append([]string(nil), r.Values...),
					Number:   r.Number,
				})
			}
			task.Condition = cond
		}
		dag.Tasks = append(dag.Tasks, task)
	}
	return dag
}

// StepOccurrenceKey derives the deterministic occurrence key of one workflow
// step: sha256 over "wfstep|<workflow source uid>|<workflow run occurrence
// key>|<task name>". It is the same derivation for every walker epoch, so a
// restarted or re-elected walker re-derives the same key, the ledger's
// (source_uid, occurrence_key) dedup absorbs the repeat insert, and a task
// that already ran is never created twice.
//
// The key is not reversible: a walker with step rows in hand maps them back
// to tasks by re-deriving this key per task of the pinned DAG.
func StepOccurrenceKey(workflowSourceUID, workflowOccurrenceKey, taskName string) string {
	raw := fmt.Sprintf("wfstep|%s|%s|%s", workflowSourceUID, workflowOccurrenceKey, taskName)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

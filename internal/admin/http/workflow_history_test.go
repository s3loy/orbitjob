package http

import (
	"context"
	"testing"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	domainworkflow "orbitjob/internal/core/domain/workflow"
)

type pinnedWorkflowDefinitions struct {
	*fakeWorkflowDefinitions
	pinned       WorkflowDefinition
	seenTenant   string
	seenRevision int64
}

func (f *pinnedWorkflowDefinitions) GetRevision(_ context.Context, tenant string, id int64) (WorkflowDefinition, error) {
	f.seenTenant = tenant
	f.seenRevision = id
	return f.pinned, nil
}
func TestWorkflowHistoryUsesPinnedRevisionAndOccurrenceMapping(t *testing.T) {
	active := WorkflowDefinition{ID: 9, SourceUID: "workflow-uid", Spec: v1alpha1.WorkflowJobSpec{Tasks: []v1alpha1.WorkflowTaskSpec{{Name: "new-task"}}}}
	pinned := WorkflowDefinition{ID: 3, SourceUID: "workflow-uid", Spec: v1alpha1.WorkflowJobSpec{Tasks: []v1alpha1.WorkflowTaskSpec{{Name: "first", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "shared-job"}}, {Name: "second", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "shared-job"}, DependsOn: []string{"first"}}, {Name: "skipped"}}}}
	defs := &pinnedWorkflowDefinitions{fakeWorkflowDefinitions: &fakeWorkflowDefinitions{}, pinned: pinned}
	defs.seed(active)
	runs := &fakeWorkflowRuns{runs: map[int64]domainworkflow.Run{12: {ID: 12, TenantID: usecaseTestTenant, SourceUID: "workflow-uid", RevisionID: 3, OccurrenceKey: "run-key", TaskDecisions: map[string]domainworkflow.TaskDecision{"skipped": {Skipped: true, Reason: "condition"}}}}, steps: map[int64][]domainworkflow.StepRun{12: {
		{ID: 40, SourceUID: "same-job-uid", OccurrenceKey: domainworkflow.StepOccurrenceKey("workflow-uid", "run-key", "second")},
		{ID: 41, SourceUID: "same-job-uid", OccurrenceKey: domainworkflow.StepOccurrenceKey("workflow-uid", "run-key", "first")},
		{ID: 42, SourceUID: "same-job-uid", OccurrenceKey: "unknown-key"},
	}}}
	out, err := NewGetWorkflowRunUseCase(defs, runs).Get(context.Background(), WorkflowRunGetInput{WorkflowID: 9, RunID: 12, TenantID: usecaseTestTenant})
	if err != nil {
		t.Fatal(err)
	}
	if !out.PinnedRevisionAvailable || defs.seenRevision != 3 || defs.seenTenant != usecaseTestTenant || out.Tasks == nil || len(*out.Tasks) != 3 || (*out.Tasks)[0].Name != "first" {
		t.Fatalf("not pinned %+v", out)
	}
	if out.Steps[0].TaskName != "second" || out.Steps[1].TaskName != "first" || out.Steps[2].TaskName != "" {
		t.Fatalf("guessed mapping %+v", out.Steps)
	}
	if !out.TaskDecisions["skipped"].Skipped || out.TaskDecisions["skipped"].Reason != "condition" {
		t.Fatalf("lost decisions %+v", out)
	}
	// A returned revision of another source is a store defect, never a graph.
	defs.pinned.SourceUID = "foreign-source"
	if _, err := NewGetWorkflowRunUseCase(defs, runs).Get(context.Background(), WorkflowRunGetInput{WorkflowID: 9, RunID: 12, TenantID: usecaseTestTenant}); err == nil {
		t.Fatal("foreign pinned revision accepted")
	}
}

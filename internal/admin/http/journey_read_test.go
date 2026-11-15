package http

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/core/domain/workflow"
	"orbitjob/internal/domain/resource"
)

type historicalDefinitions struct {
	*fakeWorkflowDefinitions
	rows map[int64]WorkflowDefinition
}

func (h historicalDefinitions) GetRevision(_ context.Context, tenant string, id int64) (WorkflowDefinition, error) {
	if tenant != usecaseTestTenant {
		return WorkflowDefinition{}, &resource.NotFoundError{Resource: "workflow"}
	}
	row, ok := h.rows[id]
	if !ok {
		return WorkflowDefinition{}, &resource.NotFoundError{Resource: "workflow"}
	}
	return row, nil
}
func TestInactiveWorkflowParentHistoryPreservesSourceAndMutationBoundary(t *testing.T) {
	def := workflowTestDef()
	defs := historicalDefinitions{fakeWorkflowDefinitions: &fakeWorkflowDefinitions{}, rows: map[int64]WorkflowDefinition{5: def}}
	run := workflow.Run{ID: 12, TenantID: usecaseTestTenant, SourceUID: def.SourceUID, RevisionID: 5, Phase: workflow.PhaseSucceeded}
	runs := &fakeWorkflowRuns{runs: map[int64]workflow.Run{12: run}, bySource: map[string][]workflow.Run{def.SourceUID: {run}}}
	input := WorkflowRunGetInput{WorkflowID: 5, RunID: 12, TenantID: usecaseTestTenant}
	out, err := NewGetWorkflowRunUseCase(defs, runs).Get(context.Background(), input)
	if err != nil || !out.PinnedRevisionAvailable || out.Tasks == nil {
		t.Fatalf("old history unavailable: %+v %v", out, err)
	}
	if rows, err := NewListWorkflowRunsUseCase(defs, runs).List(context.Background(), WorkflowRunListInput{WorkflowID: 5, TenantID: usecaseTestTenant}); err != nil || len(rows) != 1 {
		t.Fatalf("old parent list: %+v %v", rows, err)
	}
	pub := &fakeWorkflowPublisher{}
	if _, err := NewCancelWorkflowRunUseCase(defs, runs, pub).Cancel(context.Background(), WorkflowCancelInput{WorkflowID: 5, RunID: 12, TenantID: usecaseTestTenant}); err == nil || len(pub.canceled) != 0 {
		t.Fatal("inactive mutation allowed")
	}
	for _, bad := range []WorkflowRunGetInput{{WorkflowID: 5, RunID: 12, TenantID: usecaseTestTenantB}, {WorkflowID: 5, RunID: 12, TenantID: usecaseTestTenant, ResourceGroupID: "group"}} {
		if _, err := NewGetWorkflowRunUseCase(defs, runs).Get(context.Background(), bad); err == nil {
			t.Fatal("foreign/scope history exposed")
		}
	}
	run.SourceUID = "replacement"
	runs.runs[12] = run
	if _, err := NewGetWorkflowRunUseCase(defs, runs).Get(context.Background(), input); err == nil {
		t.Fatal("foreign source exposed through old parent")
	}
}

type functionAttemptReader struct {
	*fakeFunctionRuns
	attempts []function.Attempt
	known    bool
	err      error
	calls    int
}

func (f *functionAttemptReader) AttemptsByRunID(context.Context, string, int64, string) ([]function.Attempt, bool, error) {
	f.calls++
	return f.attempts, f.known, f.err
}
func TestFunctionRunAttemptsAreRetainedFactsAndRespectParentScope(t *testing.T) {
	const uuid = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	store := &fakeFunctionStore{}
	store.seed(function.Definition{ID: 7, TenantID: usecaseTestTenant, ResourceGroupID: "mine"})
	reader := &functionAttemptReader{fakeFunctionRuns: &fakeFunctionRuns{byRunID: map[string]function.FunctionRun{uuid: {RunID: uuid, FunctionID: 7, TenantID: usecaseTestTenant}}}}
	uc := NewGetFunctionRunUseCase(reader, store)
	in := FunctionRunGetInput{FunctionID: 7, RunID: uuid, TenantID: usecaseTestTenant, ResourceGroupID: "mine"}
	out, err := uc.Get(context.Background(), in)
	if err != nil || out.Attempts != nil || out.LastAttempt != nil {
		t.Fatalf("unknown metadata invented: %+v %v", out, err)
	}
	reader.known = true
	out, err = uc.Get(context.Background(), in)
	if err != nil || out.Attempts == nil || len(out.Attempts) != 0 || out.LastAttempt != nil {
		t.Fatalf("no-attempt distinction lost: %+v %v", out, err)
	}
	reader.attempts = []function.Attempt{{AttemptNumber: 2, Phase: "Failed"}, {AttemptNumber: 4, Phase: "Succeeded"}}
	out, err = uc.Get(context.Background(), in)
	if err != nil || len(out.Attempts) != 2 || out.LastAttempt == nil || *out.LastAttempt != 4 {
		t.Fatalf("last retained attempt: %+v %v", out, err)
	}
	in.ResourceGroupID = "other"
	calls := reader.calls
	if _, err := uc.Get(context.Background(), in); err == nil || reader.calls != calls {
		t.Fatal("attempt read crossed group boundary")
	}
	in.ResourceGroupID = "mine"
	reader.err = errors.New("database unavailable")
	if _, err := uc.Get(context.Background(), in); err == nil {
		t.Fatal("metadata database failure hidden")
	}
}

func TestFunctionAttemptMetadataOpenAPINullability(t *testing.T) {
	doc := ServiceOpenAPIDocument()
	model := doc.Components.Schemas["FunctionRunItem"]
	attempts := model.Properties["attempts"]
	if attempts.Type != "array" || !attempts.Nullable || !model.Properties["last_attempt"].Nullable {
		t.Fatalf("unknown attempt contract drift: %+v", model)
	}
}

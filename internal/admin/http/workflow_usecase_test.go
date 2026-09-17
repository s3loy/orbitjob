package http

import (
	"context"
	"errors"
	"testing"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	domainworkflow "orbitjob/internal/core/domain/workflow"
	"orbitjob/internal/domain/resource"
)

// Use-case coverage for the workflow surface: the scope-first refusals, the
// WorkflowRun custom resource a manual trigger renders, the definition
// boundary a run route enforces on its own prefix, and the cancel contract.

type fakeWorkflowDefinitions struct {
	byID map[int64]WorkflowDefinition
}

func (f *fakeWorkflowDefinitions) seed(defs ...WorkflowDefinition) {
	if f.byID == nil {
		f.byID = map[int64]WorkflowDefinition{}
	}
	for _, def := range defs {
		f.byID[def.ID] = def
	}
}

func (f *fakeWorkflowDefinitions) ListActive(ctx context.Context, in WorkflowDefinitionListInput) ([]WorkflowDefinition, error) {
	out := make([]WorkflowDefinition, 0, len(f.byID))
	for _, def := range f.byID {
		out = append(out, def)
	}
	return out, nil
}

func (f *fakeWorkflowDefinitions) GetActive(ctx context.Context, tenantID string, id int64) (WorkflowDefinition, error) {
	def, ok := f.byID[id]
	if !ok {
		return WorkflowDefinition{}, &resource.NotFoundError{Resource: "workflow", ID: id}
	}
	return def, nil
}

type fakeWorkflowRuns struct {
	runs     map[int64]domainworkflow.Run
	steps    map[int64][]domainworkflow.StepRun
	bySource map[string][]domainworkflow.Run
}

func (f *fakeWorkflowRuns) RunByID(ctx context.Context, tenantID string, id int64) (domainworkflow.Run, bool, error) {
	run, ok := f.runs[id]
	if !ok {
		return domainworkflow.Run{}, false, nil
	}
	return run, true, nil
}

func (f *fakeWorkflowRuns) Steps(ctx context.Context, tenantID string, workflowRunID int64) ([]domainworkflow.StepRun, error) {
	return f.steps[workflowRunID], nil
}

func (f *fakeWorkflowRuns) RunsForDefinition(ctx context.Context, tenantID, sourceUID string, limit, offset int) ([]domainworkflow.Run, error) {
	return f.bySource[sourceUID], nil
}

type fakeWorkflowPublisher struct {
	created     []v1alpha1.WorkflowRun
	createdFlag bool
	canceled    []string
	cancelErr   error
}

func (f *fakeWorkflowPublisher) Create(ctx context.Context, run v1alpha1.WorkflowRun) (v1alpha1.WorkflowRun, bool, error) {
	f.created = append(f.created, run)
	return run, f.createdFlag, nil
}

func (f *fakeWorkflowPublisher) RequestCancel(ctx context.Context, namespace, name string) error {
	if f.cancelErr != nil {
		return f.cancelErr
	}
	f.canceled = append(f.canceled, namespace+"/"+name)
	return nil
}

func workflowTestDef() WorkflowDefinition {
	return WorkflowDefinition{
		ID: 5, Name: "nightly-dag", Namespace: "orbitjob",
		SourceUID: "wf-uid-nightly", Generation: 2,
		Spec: v1alpha1.WorkflowJobSpec{
			Schedule: "@daily",
			Tasks: []v1alpha1.WorkflowTaskSpec{
				{Name: "extract", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "extract-job"}},
				{Name: "load", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "load-job"}, DependsOn: []string{"extract"}},
			},
		},
	}
}

func TestListWorkflowsUseCase_ScopedCallerRefused(t *testing.T) {
	defs := &fakeWorkflowDefinitions{}
	defs.seed(workflowTestDef())

	// A workflow revision has no resource group to be limited by, so a scoped
	// caller is refused outright instead of being served a wider view.
	_, err := (&ListWorkflowsUseCase{definitions: defs}).List(context.Background(),
		WorkflowListInput{TenantID: usecaseTestTenant, ResourceGroupID: "group-9"})
	var scope *resource.ScopeError
	if !errors.As(err, &scope) {
		t.Fatalf("expected a scope refusal, got %v", err)
	}

	out, err := (&ListWorkflowsUseCase{definitions: defs}).List(context.Background(),
		WorkflowListInput{TenantID: usecaseTestTenant})
	if err != nil {
		t.Fatalf("unscoped list: %v", err)
	}
	if len(out) != 1 || out[0].Name != "nightly-dag" {
		t.Fatalf("unexpected list: %+v", out)
	}
	// An unset fail policy is the documented FailFast default, reported as the
	// effective value rather than as an empty string.
	if out[0].FailPolicy != "FailFast" {
		t.Errorf("fail policy = %q, want the effective default", out[0].FailPolicy)
	}
}

func TestTriggerWorkflowUseCase_PublishesWorkflowRunCR(t *testing.T) {
	defs := &fakeWorkflowDefinitions{}
	defs.seed(workflowTestDef())
	publisher := &fakeWorkflowPublisher{createdFlag: true}
	uc := &TriggerWorkflowUseCase{definitions: defs, publisher: publisher}

	out, err := uc.Trigger(context.Background(), WorkflowTriggerInput{
		WorkflowID: 5, TenantID: usecaseTestTenant, ActorID: "key-123",
	})
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if !out.Created {
		t.Error("expected created=true for a fresh trigger")
	}
	if len(publisher.created) != 1 {
		t.Fatalf("expected one created CR, got %d", len(publisher.created))
	}
	run := publisher.created[0]

	if run.Spec.Trigger != v1alpha1.Manual {
		t.Errorf("trigger = %q, want %q -- the WorkflowRun resource is the manual-trigger transport", run.Spec.Trigger, v1alpha1.Manual)
	}
	if run.Spec.Actor != "key-123" {
		t.Errorf("actor = %q, want the authenticated key", run.Spec.Actor)
	}
	if run.Spec.WorkflowRef.Name != "nightly-dag" || run.Spec.WorkflowRef.UID != "wf-uid-nightly" {
		t.Errorf("workflowRef = %+v, want the definition's identity", run.Spec.WorkflowRef)
	}
	if run.Spec.DefinitionRevision != 5 {
		t.Errorf("definitionRevision = %d, want the active revision id 5", run.Spec.DefinitionRevision)
	}
	if want := v1alpha1.WorkflowRunObjectName("nightly-dag", run.Spec.OccurrenceKey); run.Name != want {
		t.Errorf("name = %q, want %q", run.Name, want)
	}
	if run.Namespace != "orbitjob" {
		t.Errorf("namespace = %q, want the definition's namespace", run.Namespace)
	}
	if err := run.Spec.Validate(); err != nil {
		t.Errorf("published spec fails the CR's own validation: %v", err)
	}
}

func TestTriggerWorkflowUseCase_SuspendedWorkflowIsConflict(t *testing.T) {
	def := workflowTestDef()
	def.Spec.Suspend = true
	defs := &fakeWorkflowDefinitions{}
	defs.seed(def)

	_, err := (&TriggerWorkflowUseCase{definitions: defs, publisher: &fakeWorkflowPublisher{}}).
		Trigger(context.Background(), WorkflowTriggerInput{WorkflowID: 5, TenantID: usecaseTestTenant, ActorID: "key-123"})
	var conflict *resource.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected a conflict for a suspended workflow, got %v", err)
	}
}

func TestTriggerWorkflowUseCase_ScopedCallerRefused(t *testing.T) {
	defs := &fakeWorkflowDefinitions{}
	defs.seed(workflowTestDef())

	_, err := (&TriggerWorkflowUseCase{definitions: defs, publisher: &fakeWorkflowPublisher{}}).
		Trigger(context.Background(), WorkflowTriggerInput{
			WorkflowID: 5, TenantID: usecaseTestTenant, ActorID: "key-123", ResourceGroupID: "group-9",
		})
	var scope *resource.ScopeError
	if !errors.As(err, &scope) {
		t.Fatalf("expected a scope refusal, got %v", err)
	}
}

func TestTriggerWorkflowUseCase_IdempotencyKeyStable(t *testing.T) {
	defs := &fakeWorkflowDefinitions{}
	defs.seed(workflowTestDef())
	publisher := &fakeWorkflowPublisher{}
	uc := &TriggerWorkflowUseCase{definitions: defs, publisher: publisher}

	first, err := uc.Trigger(context.Background(), WorkflowTriggerInput{
		WorkflowID: 5, TenantID: usecaseTestTenant, ActorID: "key-123", IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatalf("first trigger: %v", err)
	}
	second, err := uc.Trigger(context.Background(), WorkflowTriggerInput{
		WorkflowID: 5, TenantID: usecaseTestTenant, ActorID: "key-123", IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatalf("replayed trigger: %v", err)
	}
	if first.Name != second.Name || first.OccurrenceKey != second.OccurrenceKey {
		t.Fatalf("replay derived %s/%s, want %s/%s",
			second.Name, second.OccurrenceKey, first.Name, first.OccurrenceKey)
	}
}

func TestWorkflowRunRoutes_VerifyDefinitionOwnership(t *testing.T) {
	defs := &fakeWorkflowDefinitions{}
	defs.seed(workflowTestDef())
	runs := &fakeWorkflowRuns{
		runs: map[int64]domainworkflow.Run{
			12: {ID: 12, TenantID: usecaseTestTenant, SourceUID: "wf-uid-nightly",
				OccurrenceKey: "key-12", Trigger: "Manual", Phase: domainworkflow.PhaseRunning},
			// Another workflow's run: same tenant, different definition.
			13: {ID: 13, TenantID: usecaseTestTenant, SourceUID: "wf-uid-other",
				OccurrenceKey: "key-13", Phase: domainworkflow.PhaseRunning},
		},
		steps: map[int64][]domainworkflow.StepRun{
			12: {{ID: 21, WorkflowRunID: 12, SourceUID: "extract-job", Phase: "Succeeded", Attempt: 1}},
		},
		bySource: map[string][]domainworkflow.Run{
			"wf-uid-nightly": {{ID: 12, TenantID: usecaseTestTenant, SourceUID: "wf-uid-nightly",
				OccurrenceKey: "key-12", Trigger: "Manual", Phase: domainworkflow.PhaseRunning}},
		},
	}

	items, err := (&ListWorkflowRunsUseCase{definitions: defs, runs: runs}).List(context.Background(),
		WorkflowRunListInput{WorkflowID: 5, TenantID: usecaseTestTenant})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(items) != 1 || items[0].ID != 12 {
		t.Fatalf("unexpected run list: %+v", items)
	}

	item, err := (&GetWorkflowRunUseCase{definitions: defs, runs: runs}).Get(context.Background(),
		WorkflowRunGetInput{WorkflowID: 5, RunID: 12, TenantID: usecaseTestTenant})
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if len(item.Steps) != 1 || item.Steps[0].SourceUID != "extract-job" {
		t.Fatalf("unexpected steps: %+v", item.Steps)
	}

	// A run of a different definition must be indistinguishable from a run
	// that does not exist: the route names the workflow, and the answer leaks
	// nothing about other workflows' history.
	if _, err := (&GetWorkflowRunUseCase{definitions: defs, runs: runs}).Get(context.Background(),
		WorkflowRunGetInput{WorkflowID: 5, RunID: 13, TenantID: usecaseTestTenant}); err == nil {
		t.Fatal("expected not-found for another workflow's run")
	}
	var notFound *resource.NotFoundError
	if _, err := (&GetWorkflowRunUseCase{definitions: defs, runs: runs}).Get(context.Background(),
		WorkflowRunGetInput{WorkflowID: 5, RunID: 13, TenantID: usecaseTestTenant}); !errors.As(err, &notFound) {
		t.Fatalf("expected a NotFoundError, got %v", err)
	}
}

func TestCancelWorkflowRunUseCase_TerminalRunNeedsNoPatch(t *testing.T) {
	defs := &fakeWorkflowDefinitions{}
	defs.seed(workflowTestDef())
	runs := &fakeWorkflowRuns{
		runs: map[int64]domainworkflow.Run{
			12: {ID: 12, SourceUID: "wf-uid-nightly", OccurrenceKey: "key-12", Phase: domainworkflow.PhaseSucceeded},
		},
	}
	publisher := &fakeWorkflowPublisher{}
	uc := &CancelWorkflowRunUseCase{definitions: defs, runs: runs, publisher: publisher}

	out, err := uc.Cancel(context.Background(), WorkflowCancelInput{WorkflowID: 5, RunID: 12, TenantID: usecaseTestTenant})
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if len(publisher.canceled) != 0 {
		t.Fatalf("a terminal workflow must not be patched, but %v was patched", publisher.canceled)
	}
	if out.Phase != "Succeeded" {
		t.Errorf("phase = %q, want the observed terminal phase", out.Phase)
	}
}

func TestCancelWorkflowRunUseCase_PatchesCancelRequested(t *testing.T) {
	defs := &fakeWorkflowDefinitions{}
	defs.seed(workflowTestDef())
	runs := &fakeWorkflowRuns{
		runs: map[int64]domainworkflow.Run{
			12: {ID: 12, SourceUID: "wf-uid-nightly", OccurrenceKey: "key-12", Phase: domainworkflow.PhaseRunning},
		},
	}
	publisher := &fakeWorkflowPublisher{}
	uc := &CancelWorkflowRunUseCase{definitions: defs, runs: runs, publisher: publisher}

	out, err := uc.Cancel(context.Background(), WorkflowCancelInput{WorkflowID: 5, RunID: 12, TenantID: usecaseTestTenant})
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	want := "orbitjob/" + v1alpha1.WorkflowRunObjectName("nightly-dag", "key-12")
	if len(publisher.canceled) != 1 || publisher.canceled[0] != want {
		t.Fatalf("patched %v, want %s", publisher.canceled, want)
	}
	if out.Name != want[len("orbitjob/"):] {
		t.Errorf("result name = %q, want the derived object name", out.Name)
	}
}

func TestCancelWorkflowRunUseCase_MissingCRIsConflict(t *testing.T) {
	defs := &fakeWorkflowDefinitions{}
	defs.seed(workflowTestDef())
	runs := &fakeWorkflowRuns{
		runs: map[int64]domainworkflow.Run{
			12: {ID: 12, SourceUID: "wf-uid-nightly", OccurrenceKey: "key-12", Phase: domainworkflow.PhaseRunning},
		},
	}
	// Ledger-open but cluster-absent: the CR was lost or pruned. That is a
	// conflict the caller can act on, not a 404 -- the ledger row exists.
	publisher := &fakeWorkflowPublisher{cancelErr: &resource.ConflictError{
		Resource: "workflow run resource", ID: "orbitjob/nightly-dag-key-12",
	}}
	uc := &CancelWorkflowRunUseCase{definitions: defs, runs: runs, publisher: publisher}

	_, err := uc.Cancel(context.Background(), WorkflowCancelInput{WorkflowID: 5, RunID: 12, TenantID: usecaseTestTenant})
	var conflict *resource.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected a conflict for a missing custom resource, got %v", err)
	}
}

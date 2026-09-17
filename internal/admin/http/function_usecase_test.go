package http

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	domainfunction "orbitjob/internal/core/domain/function"
	"orbitjob/internal/domain/resource"
)

// Use-case coverage for the function surface, over in-memory implementations
// of the consumer-side store interfaces. The real repositories land with the
// storage workstream against the core contracts; these fakes exist so the
// normalization, visibility, and CR-shaping rules are pinned now.

type fakeFunctionStore struct {
	defs map[int64]domainfunction.Definition
	err  error
}

func (f *fakeFunctionStore) GetForTenant(ctx context.Context, tenantID string, id int64) (domainfunction.Definition, bool, error) {
	if f.err != nil {
		return domainfunction.Definition{}, false, f.err
	}
	def, ok := f.defs[id]
	if !ok || def.TenantID != tenantID {
		return domainfunction.Definition{}, false, nil
	}
	return def, true, nil
}

func (f *fakeFunctionStore) ListForTenant(ctx context.Context, tenantID string) ([]domainfunction.Definition, error) {
	out := make([]domainfunction.Definition, 0)
	for _, def := range f.defs {
		if def.TenantID == tenantID {
			out = append(out, def)
		}
	}
	return out, nil
}

func (f *fakeFunctionStore) seed(defs ...domainfunction.Definition) {
	if f.defs == nil {
		f.defs = map[int64]domainfunction.Definition{}
	}
	for _, def := range defs {
		f.defs[def.ID] = def
	}
}

type fakeFunctionRuns struct {
	byFunction map[int64][]domainfunction.FunctionRun
	byRunID    map[string]domainfunction.FunctionRun
}

func (f *fakeFunctionRuns) RunsByFunction(ctx context.Context, tenantID string, functionID int64, limit int) ([]domainfunction.FunctionRun, error) {
	rows := f.byFunction[functionID]
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (f *fakeFunctionRuns) RunByRunID(ctx context.Context, tenantID string, functionID int64, runID string) (domainfunction.FunctionRun, bool, error) {
	row, ok := f.byRunID[runID]
	if !ok || row.TenantID != tenantID || row.FunctionID != functionID {
		return domainfunction.FunctionRun{}, false, nil
	}
	return row, true, nil
}

type fakeFunctionRevisions struct {
	revision functionRevision
	err      error
}

func (f *fakeFunctionRevisions) ActiveRevision(ctx context.Context, tenantID, sourceUID string) (functionRevision, error) {
	return f.revision, f.err
}

type fakeJobRunPublisher struct {
	published []v1alpha1.JobRun
	created   bool
	getQueue  []v1alpha1.JobRun
}

func (f *fakeJobRunPublisher) Publish(ctx context.Context, run v1alpha1.JobRun) (v1alpha1.JobRun, bool, error) {
	f.published = append(f.published, run)
	return run, f.created, nil
}

func (f *fakeJobRunPublisher) Get(ctx context.Context, namespace, name string) (v1alpha1.JobRun, error) {
	if len(f.getQueue) == 0 {
		return v1alpha1.JobRun{}, nil
	}
	next := f.getQueue[0]
	f.getQueue = f.getQueue[1:]
	return next, nil
}

func invokeTestDef() domainfunction.Definition {
	return domainfunction.Definition{
		ID: 3, TenantID: "tenant-a", Name: "resize",
		Status:         domainfunction.StatusActive,
		Image:          "registry.example/resize@sha256:abcd",
		TimeoutSeconds: 30, RetryLimit: 1, Version: 2,
	}
}

func newTestInvokeUC(defs domainfunction.Definition, revision functionRevision, publisher *fakeJobRunPublisher) *InvokeFunctionUseCase {
	store := &fakeFunctionStore{}
	store.seed(defs)
	return &InvokeFunctionUseCase{
		reader:       store,
		revisions:    &fakeFunctionRevisions{revision: revision},
		publisher:    publisher,
		pollInterval: time.Millisecond,
	}
}

func TestInvokeFunctionUseCase_PublishesFunctionTriggerCR(t *testing.T) {
	publisher := &fakeJobRunPublisher{created: true}
	uc := newTestInvokeUC(invokeTestDef(), functionRevision{ID: 41, Namespace: "orbitjob"}, publisher)

	out, err := uc.Invoke(context.Background(), FunctionInvokeInput{
		FunctionID: 3, TenantID: "tenant-a", ActorID: "key-123",
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if !out.Created {
		t.Error("expected created=true for a fresh invocation")
	}
	if len(publisher.published) != 1 {
		t.Fatalf("expected one published CR, got %d", len(publisher.published))
	}
	run := publisher.published[0]

	if run.Spec.Trigger != v1alpha1.Function {
		t.Errorf("trigger = %q, want %q", run.Spec.Trigger, v1alpha1.Function)
	}
	if run.Spec.Actor != "key-123" {
		t.Errorf("actor = %q, want the authenticated key", run.Spec.Actor)
	}
	if run.Spec.DefinitionRevision != 41 {
		t.Errorf("definitionRevision = %d, want the pinned revision id 41", run.Spec.DefinitionRevision)
	}
	if run.Spec.TimeoutSeconds != 30 {
		t.Errorf("timeoutSeconds = %d, want the definition's 30", run.Spec.TimeoutSeconds)
	}
	if run.Spec.ScheduledJobRef.Name != "function-3" || run.Spec.ScheduledJobRef.UID != "function-3" {
		t.Errorf("scheduledJobRef = %+v, want the function's stable identity", run.Spec.ScheduledJobRef)
	}
	if want := v1alpha1.RunObjectName("function-3", run.Spec.OccurrenceKey); run.Name != want {
		t.Errorf("name = %q, want %q", run.Name, want)
	}
	if run.Namespace != "orbitjob" {
		t.Errorf("namespace = %q, want the revision's source_namespace", run.Namespace)
	}
	if len(run.OwnerReferences) != 0 {
		t.Error("an invocation CR must carry no owner references: the ref would point at a ScheduledJob that does not exist and the GC would reap the run")
	}
	if len(run.Spec.OccurrenceKey) != 64 {
		t.Errorf("occurrence key = %q, want 64 hex characters", run.Spec.OccurrenceKey)
	}
}

func TestInvokeFunctionUseCase_IdempotencyKeyResolvesSameRun(t *testing.T) {
	publisher := &fakeJobRunPublisher{}
	uc := newTestInvokeUC(invokeTestDef(), functionRevision{ID: 41, Namespace: "orbitjob"}, publisher)

	first, err := uc.Invoke(context.Background(), FunctionInvokeInput{
		FunctionID: 3, TenantID: "tenant-a", ActorID: "key-123", IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatalf("first invoke: %v", err)
	}
	second, err := uc.Invoke(context.Background(), FunctionInvokeInput{
		FunctionID: 3, TenantID: "tenant-a", ActorID: "key-123", IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatalf("second invoke: %v", err)
	}
	if first.OccurrenceKey != second.OccurrenceKey || first.Name != second.Name {
		t.Fatalf("replayed invocation derived %s/%s, want %s/%s",
			second.Name, second.OccurrenceKey, first.Name, first.OccurrenceKey)
	}

	third, err := uc.Invoke(context.Background(), FunctionInvokeInput{
		FunctionID: 3, TenantID: "tenant-a", ActorID: "key-123",
	})
	if err != nil {
		t.Fatalf("unkeyed invoke: %v", err)
	}
	if third.OccurrenceKey == first.OccurrenceKey {
		t.Fatal("an unkeyed invocation must be its own run, not the keyed one")
	}
}

func TestInvokeFunctionUseCase_PausedFunctionIsConflict(t *testing.T) {
	def := invokeTestDef()
	def.Status = domainfunction.StatusPaused
	uc := newTestInvokeUC(def, functionRevision{ID: 41, Namespace: "orbitjob"}, &fakeJobRunPublisher{})

	_, err := uc.Invoke(context.Background(), FunctionInvokeInput{
		FunctionID: 3, TenantID: "tenant-a", ActorID: "key-123",
	})
	var conflict *resource.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected a conflict for a paused function, got %v", err)
	}
}

func TestInvokeFunctionUseCase_MissingRevisionIsConflict(t *testing.T) {
	// The CRD requires definitionRevision before publish, so a function whose
	// revision has not been materialized yet cannot be invoked -- a conflict
	// the caller can retry, not a 500.
	uc := newTestInvokeUC(invokeTestDef(), functionRevision{ID: 0, Namespace: "orbitjob"}, &fakeJobRunPublisher{})

	_, err := uc.Invoke(context.Background(), FunctionInvokeInput{
		FunctionID: 3, TenantID: "tenant-a", ActorID: "key-123",
	})
	var conflict *resource.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected a conflict for a missing revision, got %v", err)
	}
}

func TestInvokeFunctionUseCase_ScopedCallerCannotSeeOtherGroups(t *testing.T) {
	def := invokeTestDef()
	def.ResourceGroupID = "group-9"
	uc := newTestInvokeUC(def, functionRevision{ID: 41, Namespace: "orbitjob"}, &fakeJobRunPublisher{})

	_, err := uc.Invoke(context.Background(), FunctionInvokeInput{
		FunctionID: 3, TenantID: "tenant-a", ActorID: "key-123", ResourceGroupID: "group-2",
	})
	var notFound *resource.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected not-found for a function outside the caller's group, got %v", err)
	}
}

func TestInvokeFunctionUseCase_WaitPollsToTerminalPhase(t *testing.T) {
	publisher := &fakeJobRunPublisher{
		created: true,
		getQueue: []v1alpha1.JobRun{
			{Status: v1alpha1.JobRunStatus{Phase: "Running"}},
			{Status: v1alpha1.JobRunStatus{Phase: ""}},
			{Status: v1alpha1.JobRunStatus{Phase: "Succeeded"}},
		},
	}
	uc := newTestInvokeUC(invokeTestDef(), functionRevision{ID: 41, Namespace: "orbitjob"}, publisher)

	out, err := uc.Invoke(context.Background(), FunctionInvokeInput{
		FunctionID: 3, TenantID: "tenant-a", ActorID: "key-123", WaitSeconds: 5,
	})
	if err != nil {
		t.Fatalf("invoke with wait: %v", err)
	}
	if out.Phase != "Succeeded" {
		t.Fatalf("phase = %q, want the terminal phase the poll observed", out.Phase)
	}
	if len(publisher.getQueue) != 0 {
		t.Fatalf("poll did not consume the queue: %d left", len(publisher.getQueue))
	}
}

func TestInvokeFunctionUseCase_WaitExpiryStillReturnsReference(t *testing.T) {
	// A wait that expires is not an error: the caller always gets the
	// reference, and the phase observed last -- never a fabricated terminal
	// phase.
	publisher := &fakeJobRunPublisher{
		created: true,
		getQueue: []v1alpha1.JobRun{
			{Status: v1alpha1.JobRunStatus{Phase: "Running"}},
		},
	}
	uc := newTestInvokeUC(invokeTestDef(), functionRevision{ID: 41, Namespace: "orbitjob"}, publisher)

	out, err := uc.Invoke(context.Background(), FunctionInvokeInput{
		FunctionID: 3, TenantID: "tenant-a", ActorID: "key-123", WaitSeconds: 1,
	})
	if err != nil {
		t.Fatalf("invoke with expiring wait: %v", err)
	}
	if out.Phase != "Running" {
		t.Fatalf("phase = %q, want the last observed non-terminal phase", out.Phase)
	}
}

func TestInvokeFunctionUseCase_RejectsWaitOverCap(t *testing.T) {
	uc := newTestInvokeUC(invokeTestDef(), functionRevision{ID: 41, Namespace: "orbitjob"}, &fakeJobRunPublisher{})

	_, err := uc.Invoke(context.Background(), FunctionInvokeInput{
		FunctionID: 3, TenantID: "tenant-a", ActorID: "key-123", WaitSeconds: 61,
	})
	if err == nil {
		t.Fatal("expected a validation error for a wait over the cap")
	}
}

func TestListFunctionsUseCase_GroupVisibility(t *testing.T) {
	store := &fakeFunctionStore{}
	store.seed(
		domainfunction.Definition{ID: 1, TenantID: "tenant-a", Name: "ungrouped", Status: "active"},
		domainfunction.Definition{ID: 2, TenantID: "tenant-a", Name: "mine", ResourceGroupID: "group-9", Status: "active"},
		domainfunction.Definition{ID: 3, TenantID: "tenant-b", Name: "theirs", Status: "active"},
	)

	unscoped, err := (&ListFunctionsUseCase{lister: store}).List(context.Background(),
		FunctionListInput{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("unscoped list: %v", err)
	}
	if len(unscoped) != 2 {
		t.Fatalf("unscoped caller sees %d functions, want 2 (its own tenant's)", len(unscoped))
	}

	scoped, err := (&ListFunctionsUseCase{lister: store}).List(context.Background(),
		FunctionListInput{TenantID: "tenant-a", ResourceGroupID: "group-9"})
	if err != nil {
		t.Fatalf("scoped list: %v", err)
	}
	if len(scoped) != 1 || scoped[0].ID != 2 {
		t.Fatalf("scoped caller sees %+v, want only its group's function", scoped)
	}
}

func TestGetFunctionUseCase_VisibilityAndNotFound(t *testing.T) {
	store := &fakeFunctionStore{}
	store.seed(domainfunction.Definition{ID: 2, TenantID: "tenant-a", ResourceGroupID: "group-9", Name: "mine"})

	if _, err := (GetFunctionUseCase{reader: store}).Get(context.Background(),
		FunctionGetInput{ID: 2, TenantID: "tenant-a"}); err != nil {
		t.Fatalf("unscoped get: %v", err)
	}
	if _, err := (GetFunctionUseCase{reader: store}).Get(context.Background(),
		FunctionGetInput{ID: 2, TenantID: "tenant-a", ResourceGroupID: "group-2"}); err == nil {
		t.Fatal("expected not-found for a function outside the caller's group")
	}
	if _, err := (GetFunctionUseCase{reader: store}).Get(context.Background(),
		FunctionGetInput{ID: 99, TenantID: "tenant-a"}); err == nil {
		t.Fatal("expected not-found for a missing function")
	}
}

func TestFunctionRunReads_VerifyFunctionFirst(t *testing.T) {
	store := &fakeFunctionStore{}
	store.seed(domainfunction.Definition{ID: 3, TenantID: "tenant-a", Name: "resize"})
	runs := &fakeFunctionRuns{
		byFunction: map[int64][]domainfunction.FunctionRun{
			3: {{ID: 1, RunID: "uuid-1", TenantID: "tenant-a", FunctionID: 3, Status: "success"}},
		},
		byRunID: map[string]domainfunction.FunctionRun{
			"uuid-1": {ID: 1, RunID: "uuid-1", TenantID: "tenant-a", FunctionID: 3, Status: "success"},
		},
	}

	items, err := (ListFunctionRunsUseCase{runs: runs, reader: store}).List(context.Background(),
		FunctionRunListInput{FunctionID: 3, TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(items) != 1 || items[0].RunID != "uuid-1" {
		t.Fatalf("unexpected run list: %+v", items)
	}

	item, err := (GetFunctionRunUseCase{runs: runs, reader: store}).Get(context.Background(),
		FunctionRunGetInput{FunctionID: 3, RunID: "uuid-1", TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if item.Status != "success" {
		t.Errorf("status = %q, want success", item.Status)
	}

	// A run id belonging to another function is not found -- the route names
	// both ids, and both must match.
	if _, err := (GetFunctionRunUseCase{runs: runs, reader: store}).Get(context.Background(),
		FunctionRunGetInput{FunctionID: 4, RunID: "uuid-1", TenantID: "tenant-a"}); err == nil {
		t.Fatal("expected not-found for another function's run")
	}
	if _, err := (ListFunctionRunsUseCase{runs: runs, reader: store}).List(context.Background(),
		FunctionRunListInput{FunctionID: 99, TenantID: "tenant-a"}); err == nil {
		t.Fatal("expected not-found listing runs of a missing function")
	}
}

// The invoke body the OpenAPI document advertises must stay in step with the
// result the use case returns: the reference fields are the contract callers
// program against.
func TestFunctionInvokeResult_JSONShape(t *testing.T) {
	b, err := json.Marshal(FunctionInvokeResult{
		Namespace: "orbitjob", Name: "function-3-1a2b3c4d",
		OccurrenceKey: "abc", Trigger: "Function", Phase: "Succeeded", Created: true,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"namespace"`, `"name"`, `"occurrence_key"`, `"trigger"`, `"phase"`, `"created"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("result json missing %s: %s", want, b)
		}
	}
}

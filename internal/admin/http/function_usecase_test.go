package http

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	domainfunction "orbitjob/internal/core/domain/function"
)

// usecaseTestTenant is a 26-character tenant id, the shape tenants.id values
// take and the read model's inputs validate.
const (
	usecaseTestTenant  = "01JBB0W9YRXG4SZV2QKM78N3PD"
	usecaseTestTenantB = "01JBB0W9YRXG4SZV2QKM78N3PE"
)

// Use-case coverage for the function surface, over in-memory implementations
// of the consumer-side store interfaces. The real repositories land with the
// storage workstream against the core contracts; these fakes exist so the
// normalization, visibility, and read-model rules are pinned now. The invoke
// use case lives in internal/core/app/functioninvoke and carries its own
// suite; the handler-level invoke contract is pinned on handler_function_test.

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

func TestListFunctionsUseCase_GroupVisibility(t *testing.T) {
	store := &fakeFunctionStore{}
	store.seed(
		domainfunction.Definition{ID: 1, TenantID: usecaseTestTenant, Name: "ungrouped", Status: "active"},
		domainfunction.Definition{ID: 2, TenantID: usecaseTestTenant, Name: "mine", ResourceGroupID: "group-9", Status: "active"},
		domainfunction.Definition{ID: 3, TenantID: usecaseTestTenantB, Name: "theirs", Status: "active"},
	)

	unscoped, err := (&ListFunctionsUseCase{lister: store}).List(context.Background(),
		FunctionListInput{TenantID: usecaseTestTenant})
	if err != nil {
		t.Fatalf("unscoped list: %v", err)
	}
	if len(unscoped) != 2 {
		t.Fatalf("unscoped caller sees %d functions, want 2 (its own tenant's)", len(unscoped))
	}

	scoped, err := (&ListFunctionsUseCase{lister: store}).List(context.Background(),
		FunctionListInput{TenantID: usecaseTestTenant, ResourceGroupID: "group-9"})
	if err != nil {
		t.Fatalf("scoped list: %v", err)
	}
	if len(scoped) != 1 || scoped[0].ID != 2 {
		t.Fatalf("scoped caller sees %+v, want only its group's function", scoped)
	}
}

func TestGetFunctionUseCase_VisibilityAndNotFound(t *testing.T) {
	store := &fakeFunctionStore{}
	store.seed(domainfunction.Definition{ID: 2, TenantID: usecaseTestTenant, ResourceGroupID: "group-9", Name: "mine"})

	if _, err := (&GetFunctionUseCase{reader: store}).Get(context.Background(),
		FunctionGetInput{ID: 2, TenantID: usecaseTestTenant}); err != nil {
		t.Fatalf("unscoped get: %v", err)
	}
	if _, err := (&GetFunctionUseCase{reader: store}).Get(context.Background(),
		FunctionGetInput{ID: 2, TenantID: usecaseTestTenant, ResourceGroupID: "group-2"}); err == nil {
		t.Fatal("expected not-found for a function outside the caller's group")
	}
	if _, err := (&GetFunctionUseCase{reader: store}).Get(context.Background(),
		FunctionGetInput{ID: 99, TenantID: usecaseTestTenant}); err == nil {
		t.Fatal("expected not-found for a missing function")
	}
}

func TestFunctionRunReads_VerifyFunctionFirst(t *testing.T) {
	store := &fakeFunctionStore{}
	store.seed(domainfunction.Definition{ID: 3, TenantID: usecaseTestTenant, Name: "resize"})
	runs := &fakeFunctionRuns{
		byFunction: map[int64][]domainfunction.FunctionRun{
			3: {{ID: 1, RunID: "uuid-1", TenantID: usecaseTestTenant, FunctionID: 3, Status: "success"}},
		},
		byRunID: map[string]domainfunction.FunctionRun{
			"uuid-1": {ID: 1, RunID: "uuid-1", TenantID: usecaseTestTenant, FunctionID: 3, Status: "success"},
		},
	}

	items, err := (&ListFunctionRunsUseCase{runs: runs, reader: store}).List(context.Background(),
		FunctionRunListInput{FunctionID: 3, TenantID: usecaseTestTenant})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(items) != 1 || items[0].RunID != "uuid-1" {
		t.Fatalf("unexpected run list: %+v", items)
	}

	item, err := (&GetFunctionRunUseCase{runs: runs, reader: store}).Get(context.Background(),
		FunctionRunGetInput{FunctionID: 3, RunID: "uuid-1", TenantID: usecaseTestTenant})
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if item.Status != "success" {
		t.Errorf("status = %q, want success", item.Status)
	}

	// A run id belonging to another function is not found -- the route names
	// both ids, and both must match.
	if _, err := (&GetFunctionRunUseCase{runs: runs, reader: store}).Get(context.Background(),
		FunctionRunGetInput{FunctionID: 4, RunID: "uuid-1", TenantID: usecaseTestTenant}); err == nil {
		t.Fatal("expected not-found for another function's run")
	}
	if _, err := (&ListFunctionRunsUseCase{runs: runs, reader: store}).List(context.Background(),
		FunctionRunListInput{FunctionID: 99, TenantID: usecaseTestTenant}); err == nil {
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

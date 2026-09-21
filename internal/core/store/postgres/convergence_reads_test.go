package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/domain/resource"
)

// The four admin-read capabilities the Functions/Workflows HTTP surface
// consumed through consumer-side fakes until the storage work landed: a
// function run by its deterministic run id, a workflow definition's run
// history, the active function-sourced revision an invocation pins, and the
// workflow-definition projection with its decoded task DAG. The sqlmock pins
// hold each query to the tenant GUC, the newest-first ordering and the
// not-found contract the surface was written against.

// newWorkflowDefinitionRepoMock is the plain-converter harness the
// WorkflowDefinitionRepository pins run on: no slice or time binds, so the
// default driver converter matches.
func newWorkflowDefinitionRepoMock(t *testing.T) (*WorkflowDefinitionRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewWorkflowDefinitionRepository(db), mock
}

// ---------------------------------------------------------------------------
// FunctionRunStore: RunByRunID
// ---------------------------------------------------------------------------

func TestFunctionRunRepository_RunByRunIDReturnsTheRow(t *testing.T) {
	_, fnRuns, mock := newFunctionRepoMock(t)
	triggered := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	finished := triggered.Add(40 * time.Second)

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`FROM function_runs`).
		WithArgs(tenantFixture(), int64(9), "uuid-2").
		WillReturnRows(sqlmock.NewRows(functionRunRowColumns()).
			AddRow(2, "uuid-2", tenantFixture(), 9, "failed", triggered, triggered.Add(5*time.Second), finished, 35000, 1, finished))
	mock.ExpectCommit()

	run, found, err := fnRuns.RunByRunID(context.Background(), tenantFixture(), 9, "uuid-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("run not found")
	}
	if run.RunID != "uuid-2" || run.FunctionID != 9 || run.Status != "failed" {
		t.Fatalf("row scan drifted: %+v", run)
	}
	if run.DurationMs == nil || *run.DurationMs != 35000 {
		t.Fatalf("duration = %v, want 35000", run.DurationMs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFunctionRunRepository_RunByRunIDNotFoundIsCalm(t *testing.T) {
	_, fnRuns, mock := newFunctionRepoMock(t)

	// A foreign function's run id must be indistinguishable from a missing
	// one: the function predicate rides beside the run id. A not-found read
	// leaves the transaction to the deferred rollback, the house shape.
	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`FROM function_runs`).
		WithArgs(tenantFixture(), int64(9), "uuid-other-function").
		WillReturnRows(sqlmock.NewRows(functionRunRowColumns()))
	mock.ExpectRollback()

	run, found, err := fnRuns.RunByRunID(context.Background(), tenantFixture(), 9, "uuid-other-function")
	if err != nil {
		t.Fatalf("a missing run is not an error: %v", err)
	}
	if found || run.ID != 0 {
		t.Fatalf("found=%v run=%+v, want false and the zero row", found, run)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFunctionRunRepository_RunByRunIDDBErrorRollsBack(t *testing.T) {
	_, fnRuns, mock := newFunctionRepoMock(t)

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`FROM function_runs`).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, _, err := fnRuns.RunByRunID(context.Background(), tenantFixture(), 9, "uuid-2"); err == nil {
		t.Fatal("expected the database error to surface")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// WorkflowRunStore: RunsForDefinition
// ---------------------------------------------------------------------------

func TestWorkflowRunRepository_RunsForDefinitionListsNewestFirst(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	expectWorkflowTx(mock, "default")
	mock.ExpectQuery(`FROM workflow_run_control_plane WHERE tenant_id=\$1 AND source_uid=\$2`).
		WithArgs("default", "wf-nightly", 25, 10).
		WillReturnRows(workflowRunRow(31, "Failed", "{}").
			AddRow(22, "wf-nightly", int64(5), "occ-2", "Manual", "walker", "Succeeded", "{}", workflowRowTime, workflowRowTime))
	mock.ExpectCommit()

	runs, err := repo.RunsForDefinition(context.Background(), "default", "wf-nightly", 25, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("listed %d runs, want 2", len(runs))
	}
	if runs[0].ID != 31 || runs[1].ID != 22 {
		t.Fatalf("order = %d,%d; the page must be newest-first", runs[0].ID, runs[1].ID)
	}
	// No phase filter: terminal and open runs alike are history once they
	// exist, unlike OpenRuns' terminal exclusion.
	if runs[0].Phase != "Failed" || runs[1].Phase != "Succeeded" {
		t.Fatalf("phases = %s, %s", runs[0].Phase, runs[1].Phase)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestWorkflowRunRepository_RunsForDefinitionDefaultsThePage(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	expectWorkflowTx(mock, "default")
	mock.ExpectQuery(`FROM workflow_run_control_plane WHERE tenant_id=\$1 AND source_uid=\$2`).
		WithArgs("default", "wf-nightly", defaultRunPage, 0).
		WillReturnRows(workflowRunRow(31, "Failed", "{}"))
	mock.ExpectCommit()

	runs, err := repo.RunsForDefinition(context.Background(), "default", "wf-nightly", 0, -3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("listed %d runs, want 1", len(runs))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestWorkflowRunRepository_RunsForDefinitionDBErrorRollsBack(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	expectWorkflowTx(mock, "default")
	mock.ExpectQuery(`FROM workflow_run_control_plane WHERE tenant_id=\$1 AND source_uid=\$2`).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, err := repo.RunsForDefinition(context.Background(), "default", "wf-nightly", 10, 0); err == nil {
		t.Fatal("expected the database error to surface")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ControlPlaneRepository: ActiveFunctionRevision
// ---------------------------------------------------------------------------

func TestActiveFunctionRevisionReturnsIDAndNamespace(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)

	expectControlPlaneTx(mock, tenantFixture())
	mock.ExpectQuery(`FROM job_definition_revisions`).
		WithArgs("function", "function-9", tenantFixture()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "source_namespace"}).
			AddRow(int64(14), "finance"))
	mock.ExpectCommit()

	id, namespace, err := repo.ActiveFunctionRevision(context.Background(), tenantFixture(), "function-9")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 14 || namespace != "finance" {
		t.Fatalf("id=%d namespace=%q, want 14/finance", id, namespace)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestActiveFunctionRevisionRefusesAnUnsyncedDefinition(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)

	// A definition whose revision the sync loop has not materialized yet has
	// no active row; that is NotFoundError, the invoke path's "no active
	// revision yet" answer.
	expectControlPlaneTx(mock, tenantFixture())
	mock.ExpectQuery(`FROM job_definition_revisions`).
		WithArgs("function", "function-9", tenantFixture()).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, _, err := repo.ActiveFunctionRevision(context.Background(), tenantFixture(), "function-9")
	var nf *resource.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want *resource.NotFoundError", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// WorkflowDefinitionRepository
// ---------------------------------------------------------------------------

var workflowDefinitionScanColumns = []string{
	"id", "source_name", "source_namespace", "source_uid", "generation", "normalized_spec", "created_at",
}

func workflowDefinitionRow(id int64) *sqlmock.Rows {
	return sqlmock.NewRows(workflowDefinitionScanColumns).
		AddRow(id, "nightly-pipeline", "finance", "wf-uid-1", int64(4),
			[]byte(`{"schedule":"0 3 * * *","failPolicy":"Continue","tasks":[`+
				`{"name":"extract","jobRef":{"name":"extract-job"}},`+
				`{"name":"load","jobRef":{"name":"load-job"},"dependsOn":["extract"]}]}`),
			workflowRowTime)
}

func TestWorkflowDefinitionRepository_ListsActiveNewestFirst(t *testing.T) {
	repo, mock := newWorkflowDefinitionRepoMock(t)

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`FROM job_definition_revisions`).
		WithArgs(tenantFixture(), "workflow", 50, 0).
		WillReturnRows(workflowDefinitionRow(14).AddRow(9, "nightly-pipeline", "finance", "wf-uid-2", int64(2),
			[]byte(`{"schedule":"*/5 * * * *","tasks":[{"name":"only","jobRef":{"name":"solo-job"}}]}`),
			workflowRowTime))
	mock.ExpectCommit()

	defs, err := repo.ListActiveWorkflowDefinitions(context.Background(), tenantFixture(), 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("listed %d definitions, want 2", len(defs))
	}
	if defs[0].ID != 14 || defs[0].SourceUID != "wf-uid-1" || defs[0].Generation != 4 {
		t.Fatalf("first row drifted: %+v", defs[0])
	}
	if defs[0].Spec.Schedule != "0 3 * * *" || len(defs[0].Spec.Tasks) != 2 || defs[0].Spec.Tasks[1].DependsOn[0] != "extract" {
		t.Fatalf("task DAG decode drifted: %+v", defs[0].Spec)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestWorkflowDefinitionRepository_GetActiveDecodesTheDAG(t *testing.T) {
	repo, mock := newWorkflowDefinitionRepoMock(t)

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`FROM job_definition_revisions`).
		WithArgs(tenantFixture(), int64(14), "workflow").
		WillReturnRows(workflowDefinitionRow(14))
	mock.ExpectCommit()

	def, err := repo.ActiveWorkflowDefinition(context.Background(), tenantFixture(), 14)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.Name != "nightly-pipeline" || def.Namespace != "finance" {
		t.Fatalf("identity drifted: %+v", def)
	}
	if def.Spec.FailPolicy != "Continue" || def.Spec.Tasks[0].JobRef.Name != "extract-job" {
		t.Fatalf("spec drifted: %+v", def.Spec)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestWorkflowDefinitionRepository_GetActiveNotFound(t *testing.T) {
	repo, mock := newWorkflowDefinitionRepoMock(t)

	// An inactive revision is history, not the workflow; a non-workflow
	// revision is another surface's row. Both hide behind not found.
	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`FROM job_definition_revisions`).
		WithArgs(tenantFixture(), int64(99), "workflow").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err := repo.ActiveWorkflowDefinition(context.Background(), tenantFixture(), 99)
	var nf *resource.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want *resource.NotFoundError", err)
	}
	if nf.Resource != "workflow" {
		t.Fatalf("resource = %q, want workflow", nf.Resource)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

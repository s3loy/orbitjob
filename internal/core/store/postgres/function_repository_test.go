package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/domain/resource"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// sqlNullString mirrors what nullableText/nullableGroup hand the driver: an
// empty value becomes SQL NULL.
func sqlNullString(s string) driver.Value {
	return sql.NullString{String: s, Valid: s != ""}
}

func newFunctionRepoMock(t *testing.T) (*FunctionRepository, *FunctionRunRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewFunctionRepository(db), NewFunctionRunRepository(db), mock
}

func functionRowColumns() []string {
	return []string{
		"id", "name", "description", "tenant_id", "resource_group_id", "status",
		"image", "command", "args", "timeout_seconds", "retry_limit",
		"history_success", "history_failed", "labels", "version", "created_at", "updated_at",
	}
}

func functionRow(id int64, overrides map[string]any) []driver.Value {
	row := []driver.Value{
		id, "charge", "greet the caller", "tenant26chars0000000000000", "group26chars000000000000", "active",
		"registry.example/app@sha256:0123456789abcdef", []byte(`["/app"]`), []byte(`["--once"]`),
		60, 2, 3, 3, []byte(`{"team":"edge"}`), 4,
		time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC), time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC),
	}
	for i, col := range functionRowColumns() {
		if v, ok := overrides[col]; ok {
			row[i] = v
		}
	}
	return row
}

// tenantFixture is the 26-character tenant id every test scopes to.
func tenantFixture() string { return "tenant26chars0000000000000" }

func gucExpectations(mock sqlmock.Sqlmock, tenant string) {
	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs(tenant).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

// ---------------------------------------------------------------------------
// FunctionRepository: CreateForTenant
// ---------------------------------------------------------------------------

func TestFunctionRepository_CreateReturnsAssignedIdentity(t *testing.T) {
	repo, _, mock := newFunctionRepoMock(t)
	created := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`INSERT INTO functions`).
		WithArgs("charge", sqlNullString(""), tenantFixture(), sqlNullString("group26chars000000000000"),
			"active", "registry.example/app@sha256:0123456789abcdef",
			[]byte(`["/app"]`), []byte(`["--once"]`), 60, 2, 3, 3, []byte(`{"team":"edge"}`), 1).
		WillReturnRows(sqlmock.NewRows(functionRowColumns()).AddRows(functionRow(9, map[string]any{
			"version": 1, "created_at": created, "updated_at": created,
		})))
	mock.ExpectCommit()

	def := function.Definition{
		Name:            "charge",
		TenantID:        tenantFixture(),
		ResourceGroupID: "group26chars000000000000",
		Image:           "registry.example/app@sha256:0123456789abcdef",
		Command:         []string{"/app"},
		Args:            []string{"--once"},
		TimeoutSeconds:  60,
		RetryLimit:      2,
		HistorySuccess:  3,
		HistoryFailed:   3,
		Labels:          map[string]any{"team": "edge"},
	}
	got, err := repo.CreateForTenant(context.Background(), tenantFixture(), def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != 9 || got.Version != 1 {
		t.Fatalf("assigned identity = (id %d, version %d), want (9, 1)", got.ID, got.Version)
	}
	if got.Status != "active" || got.Image == "" {
		t.Fatalf("returned definition lost its status or image: %+v", got)
	}
	if got.ResourceGroupID != "group26chars000000000000" {
		t.Fatalf("resource group = %q, want the saved group", got.ResourceGroupID)
	}
	if len(got.Command) != 1 || got.Command[0] != "/app" || len(got.Args) != 1 || got.Args[0] != "--once" {
		t.Fatalf("command/args round-trip failed: %+v", got)
	}
	if got.Labels["team"] != "edge" {
		t.Fatalf("labels round-trip failed: %+v", got.Labels)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFunctionRepository_CreateDefaultsBlankStatusToActive(t *testing.T) {
	repo, _, mock := newFunctionRepoMock(t)

	gucExpectations(mock, tenantFixture())
	// The status argument must be the column-vocabulary value, not an empty
	// string the CHECK would refuse. Numeric bounds are not the repo's to
	// default: they pass through and the table's CHECK is the loud backstop
	// for a caller that skipped boundary validation.
	mock.ExpectQuery(`INSERT INTO functions`).
		WithArgs("charge", sqlNullString(""), tenantFixture(), sqlNullString(""),
			"active", "img", []byte(`[]`), []byte(`[]`), 0, 0, 0, 0, []byte(`{}`), 1).
		WillReturnRows(sqlmock.NewRows(functionRowColumns()).AddRows(functionRow(9, map[string]any{
			"status": "active", "description": nil, "resource_group_id": nil,
			"image": "img", "command": []byte(`[]`), "args": []byte(`[]`), "labels": []byte(`{}`),
			"timeout_seconds": 0, "retry_limit": 0, "history_success": 0, "history_failed": 0,
		})))
	mock.ExpectCommit()

	def := function.Definition{Name: "charge", Image: "img", TenantID: tenantFixture()}
	if _, err := repo.CreateForTenant(context.Background(), tenantFixture(), def); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFunctionRepository_CreateDBErrorRollsBack(t *testing.T) {
	repo, _, mock := newFunctionRepoMock(t)

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`INSERT INTO functions`).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	_, err := repo.CreateForTenant(context.Background(), tenantFixture(), function.Definition{Name: "x", Image: "img"})
	if err == nil || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("error = %v, want the wrapped db cause", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// FunctionRepository: UpdateForTenant
// ---------------------------------------------------------------------------

func TestFunctionRepository_UpdateBumpsVersionAgainstPinnedOne(t *testing.T) {
	repo, _, mock := newFunctionRepoMock(t)

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`UPDATE functions SET`).
		WithArgs(tenantFixture(), int64(9), "charge", sqlNullString("updated"), "paused", "img2",
			[]byte(`["/app","-v"]`), []byte(`[]`), 120, 1, 5, 5, []byte(`{}`), 4).
		WillReturnRows(sqlmock.NewRows(functionRowColumns()).AddRows(functionRow(9, map[string]any{
			"version": 5, "description": "updated", "status": "paused", "image": "img2",
			"command": []byte(`["/app","-v"]`), "args": []byte(`[]`), "timeout_seconds": 120,
			"retry_limit": 1, "history_success": 5, "history_failed": 5, "labels": []byte(`{}`),
		})))
	mock.ExpectCommit()

	def := function.Definition{
		ID: 9, Name: "charge", Description: "updated", Status: function.StatusPaused,
		Image: "img2", Command: []string{"/app", "-v"}, TimeoutSeconds: 120,
		RetryLimit: 1, HistorySuccess: 5, HistoryFailed: 5, Version: 4,
	}
	got, err := repo.UpdateForTenant(context.Background(), tenantFixture(), def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Version != 5 {
		t.Fatalf("version = %d, want the bumped 5", got.Version)
	}
	if got.Command[1] != "-v" {
		t.Fatalf("command round-trip failed: %+v", got.Command)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFunctionRepository_UpdateStaleVersionIsAConflict(t *testing.T) {
	repo, _, mock := newFunctionRepoMock(t)

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`UPDATE functions SET`).
		WillReturnError(sql.ErrNoRows)
	// The existence probe finds the row: the write lost to a concurrent save.
	mock.ExpectQuery(`SELECT id FROM functions`).
		WithArgs(tenantFixture(), int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(9))
	mock.ExpectRollback()

	_, err := repo.UpdateForTenant(context.Background(), tenantFixture(), function.Definition{ID: 9, Version: 3, Image: "img"})
	var conflict *resource.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error = %v, want a ConflictError", err)
	}
	if conflict.Field != "version" {
		t.Fatalf("conflict field = %q, want version", conflict.Field)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFunctionRepository_UpdateMissingIsNotFound(t *testing.T) {
	repo, _, mock := newFunctionRepoMock(t)

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`UPDATE functions SET`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT id FROM functions`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err := repo.UpdateForTenant(context.Background(), tenantFixture(), function.Definition{ID: 404, Version: 1, Image: "img"})
	var notFound *resource.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("error = %v, want a NotFoundError", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// FunctionRepository: GetForTenant / List / Active
// ---------------------------------------------------------------------------

func TestFunctionRepository_GetReturnsLiveDefinition(t *testing.T) {
	repo, _, mock := newFunctionRepoMock(t)

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`FROM functions`).
		WithArgs(tenantFixture(), int64(9)).
		WillReturnRows(sqlmock.NewRows(functionRowColumns()).AddRows(functionRow(9, nil)))
	mock.ExpectCommit()

	got, found, err := repo.GetForTenant(context.Background(), tenantFixture(), 9)
	if err != nil || !found {
		t.Fatalf("found = %v, err = %v; want found", found, err)
	}
	if got.ID != 9 || got.Status != "active" || got.TimeoutSeconds != 60 || got.RetryLimit != 2 {
		t.Fatalf("definition scan drifted: %+v", got)
	}
	if got.Description != "greet the caller" {
		t.Fatalf("description = %q, want the stored text", got.Description)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFunctionRepository_GetTreatsMissingDeletedAndForeignAsNotFound(t *testing.T) {
	repo, _, mock := newFunctionRepoMock(t)

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`FROM functions`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, found, err := repo.GetForTenant(context.Background(), tenantFixture(), 404)
	if err != nil {
		t.Fatalf("not-found must not be an error: %v", err)
	}
	if found {
		t.Fatal("found = true for a missing definition")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFunctionRepository_ListScopesToLiveRows(t *testing.T) {
	repo, _, mock := newFunctionRepoMock(t)

	gucExpectations(mock, tenantFixture())
	// The plain list must not carry the active-status predicate.
	mock.ExpectQuery(`FROM functions\s+WHERE tenant_id = \$1 AND deleted_at IS NULL\s+ORDER BY id ASC`).
		WillReturnRows(sqlmock.NewRows(functionRowColumns()).
			AddRows(functionRow(9, nil)).
			AddRows(functionRow(10, map[string]any{"status": "paused"})))
	mock.ExpectCommit()

	defs, err := repo.ListForTenant(context.Background(), tenantFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("listed %d definitions, want 2", len(defs))
	}
	if defs[0].Status != "active" || defs[1].Status != "paused" {
		t.Fatalf("status scan drifted: %q, %q", defs[0].Status, defs[1].Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFunctionRepository_ActiveAddsTheStatusPredicate(t *testing.T) {
	repo, _, mock := newFunctionRepoMock(t)

	gucExpectations(mock, tenantFixture())
	// The sync loop's input is the active slice only.
	mock.ExpectQuery(`AND status = 'active'`).
		WillReturnRows(sqlmock.NewRows(functionRowColumns()).AddRows(functionRow(9, nil)))
	mock.ExpectCommit()

	defs, err := repo.ActiveForTenant(context.Background(), tenantFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 1 || defs[0].ID != 9 {
		t.Fatalf("active list = %+v, want only function 9", defs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFunctionRepository_ListScanErrorRollsBack(t *testing.T) {
	repo, _, mock := newFunctionRepoMock(t)

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`FROM functions`).
		WillReturnRows(sqlmock.NewRows(functionRowColumns()).AddRow("not-an-int", nil, nil, nil, nil, nil,
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil))
	mock.ExpectRollback()

	if _, err := repo.ListForTenant(context.Background(), tenantFixture()); err == nil {
		t.Fatal("expected a scan error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// FunctionRepository: DeleteForTenant
// ---------------------------------------------------------------------------

func TestFunctionRepository_DeleteSoftDeletes(t *testing.T) {
	repo, _, mock := newFunctionRepoMock(t)

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`UPDATE functions\s+SET deleted_at = now\(\)`).
		WithArgs(tenantFixture(), int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(9))
	mock.ExpectCommit()

	if err := repo.DeleteForTenant(context.Background(), tenantFixture(), 9); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFunctionRepository_DeleteMissingIsNotFound(t *testing.T) {
	repo, _, mock := newFunctionRepoMock(t)

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`UPDATE functions`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	err := repo.DeleteForTenant(context.Background(), tenantFixture(), 404)
	var notFound *resource.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("error = %v, want a NotFoundError", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// FunctionRunRepository: RecordCompleted
// ---------------------------------------------------------------------------

func completedFunctionRecord() function.CompletedRecord {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	return function.CompletedRecord{
		RunID:       "0f1e2d3c-4b5a-6c7d-8e9f-0a1b2c3d4e5f",
		FunctionID:  9,
		Status:      function.StatusSuccess,
		TriggeredAt: now.Add(-time.Minute),
		StartedAt:   now.Add(-30 * time.Second),
		FinishedAt:  now,
		DurationMs:  30000,
	}
}

func TestFunctionRunRepository_RecordsCompletedWithUpsertGuard(t *testing.T) {
	repo, fnRuns, mock := newFunctionRepoMock(t)
	rec := completedFunctionRecord()

	gucExpectations(mock, tenantFixture())
	// The ON CONFLICT guard is the idempotency contract: a replayed recording
	// with the same deterministic run id must land as a no-op, not a second
	// history row.
	mock.ExpectQuery(`INSERT INTO function_runs[\s\S]*ON CONFLICT \(run_id\) DO NOTHING`).
		WithArgs(rec.RunID, tenantFixture(), rec.FunctionID, rec.Status,
			rec.TriggeredAt, rec.StartedAt, rec.FinishedAt, rec.DurationMs).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(77))
	mock.ExpectCommit()

	if err := fnRuns.RecordCompleted(context.Background(), tenantFixture(), rec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
	_ = repo
}

func TestFunctionRunRepository_RecordsCanceledWithoutAStart(t *testing.T) {
	_, fnRuns, mock := newFunctionRepoMock(t)
	rec := completedFunctionRecord()
	rec.Status = function.StatusCanceled
	rec.StartedAt = time.Time{}
	rec.DurationMs = 0

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`INSERT INTO function_runs`).
		WithArgs(rec.RunID, tenantFixture(), rec.FunctionID, rec.Status,
			rec.TriggeredAt, time.Time{}, rec.FinishedAt, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(78))
	mock.ExpectCommit()

	if err := fnRuns.RecordCompleted(context.Background(), tenantFixture(), rec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFunctionRunRepository_BackfillsStartForExitOutcomes(t *testing.T) {
	_, fnRuns, mock := newFunctionRepoMock(t)
	rec := completedFunctionRecord()
	rec.StartedAt = time.Time{}
	rec.DurationMs = -5

	gucExpectations(mock, tenantFixture())
	// The status/timestamps CHECK refuses an exit outcome without a start; the
	// trigger instant is the honest floor. A negative duration clamps to 0.
	mock.ExpectQuery(`INSERT INTO function_runs`).
		WithArgs(rec.RunID, tenantFixture(), rec.FunctionID, rec.Status,
			rec.TriggeredAt, rec.TriggeredAt, rec.FinishedAt, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(79))
	mock.ExpectCommit()

	if err := fnRuns.RecordCompleted(context.Background(), tenantFixture(), rec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFunctionRunRepository_RejectsNonTerminalShapes(t *testing.T) {
	_, fnRuns, mock := newFunctionRepoMock(t)

	cases := map[string]func(*function.CompletedRecord){
		"blank run id":        func(r *function.CompletedRecord) { r.RunID = "" },
		"zero function id":    func(r *function.CompletedRecord) { r.FunctionID = 0 },
		"non-terminal status": func(r *function.CompletedRecord) { r.Status = "running" },
		"empty status":        func(r *function.CompletedRecord) { r.Status = "" },
		"zero triggered at":   func(r *function.CompletedRecord) { r.TriggeredAt = time.Time{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			rec := completedFunctionRecord()
			mutate(&rec)
			if err := fnRuns.RecordCompleted(context.Background(), tenantFixture(), rec); err == nil {
				t.Fatalf("invalid record accepted: %+v", rec)
			}
		})
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("refused input reached the database: %v", err)
	}
}

func TestFunctionRunRepository_RecordDBErrorRollsBack(t *testing.T) {
	_, fnRuns, mock := newFunctionRepoMock(t)
	rec := completedFunctionRecord()

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`INSERT INTO function_runs`).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	err := fnRuns.RecordCompleted(context.Background(), tenantFixture(), rec)
	if err == nil || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("error = %v, want the wrapped db cause", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// FunctionRunRepository: RunsByFunction
// ---------------------------------------------------------------------------

func functionRunRowColumns() []string {
	return []string{"id", "run_id", "tenant_id", "function_id", "status", "triggered_at",
		"started_at", "finished_at", "duration_ms", "version", "created_at"}
}

func TestFunctionRunRepository_RunsByFunctionListsNewestFirst(t *testing.T) {
	_, fnRuns, mock := newFunctionRepoMock(t)
	triggered := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	started := triggered.Add(5 * time.Second)
	finished := triggered.Add(35 * time.Second)

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`FROM function_runs`).
		WithArgs(tenantFixture(), int64(9), 10).
		WillReturnRows(sqlmock.NewRows(functionRunRowColumns()).
			AddRow(2, "uuid-2", tenantFixture(), 9, "failed", triggered, started, finished, 12000, 1, finished).
			AddRow(1, "uuid-1", tenantFixture(), 9, "success", triggered.Add(-time.Minute), nil, nil, nil, 1, triggered))
	mock.ExpectCommit()

	runs, err := fnRuns.RunsByFunction(context.Background(), tenantFixture(), 9, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("listed %d runs, want 2", len(runs))
	}
	if runs[0].Status != "failed" || runs[0].DurationMs == nil || *runs[0].DurationMs != 12000 {
		t.Fatalf("newest run scan drifted: %+v", runs[0])
	}
	if runs[0].StartedAt == nil || !runs[0].StartedAt.Equal(started) {
		t.Fatalf("started_at = %v, want %v", runs[0].StartedAt, started)
	}
	// A canceled-before-start row scans back with nil instants, not zeros.
	if runs[1].StartedAt != nil || runs[1].FinishedAt != nil || runs[1].DurationMs != nil {
		t.Fatalf("nullable instants scanned as non-nil: %+v", runs[1])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFunctionRunRepository_RunsByFunctionDefaultsThePage(t *testing.T) {
	_, fnRuns, mock := newFunctionRepoMock(t)

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`FROM function_runs`).
		WithArgs(tenantFixture(), int64(9), defaultRunPage).
		WillReturnRows(sqlmock.NewRows(functionRunRowColumns()))
	mock.ExpectCommit()

	runs, err := fnRuns.RunsByFunction(context.Background(), tenantFixture(), 9, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runs != nil {
		t.Fatalf("empty page returned %+v, want nil", runs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFunctionRunRepository_RunsByFunctionDBErrorRollsBack(t *testing.T) {
	_, fnRuns, mock := newFunctionRepoMock(t)

	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery(`FROM function_runs`).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, err := fnRuns.RunsByFunction(context.Background(), tenantFixture(), 9, 10); err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

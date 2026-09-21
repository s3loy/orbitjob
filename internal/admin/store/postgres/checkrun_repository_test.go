package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	checkrunquery "orbitjob/internal/admin/app/checkrun/query"
)

func TestCheckRunRepository_Get(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	scheduled := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	started := scheduled.Add(2 * time.Second)
	finished := scheduled.Add(5 * time.Second)
	severity := "critical"
	duration := 3000

	expectTenantTx(t, mock, "tenant-a")
	// run_id is read through ::text and output/evaluation_result arrive as
	// JSON documents that must land as maps.
	mock.ExpectQuery(`SELECT (.+) FROM check_runs WHERE tenant_id = \$1 AND id = \$2`).
		WithArgs("tenant-a", int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "run_id", "tenant_id", "check_id", "status", "severity",
			"output", "evaluation_result", "scheduled_at", "started_at",
			"finished_at", "duration_ms", "version", "created_at",
		}).AddRow(
			9, "run-abc", "tenant-a", 3, "failed", &severity,
			[]byte(`{"status_code":503}`), []byte(`{"overall_severity":"critical"}`),
			scheduled, started, finished, &duration, 1, finished,
		))
	mock.ExpectCommit()

	out, err := NewCheckRunRepository(db).Get(context.Background(), "tenant-a", 9)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ID != 9 || out.RunID != "run-abc" || out.TenantID != "tenant-a" || out.CheckID != 3 {
		t.Fatalf("identity did not survive the scan: %+v", out)
	}
	if out.Status != "failed" || out.Severity == nil || *out.Severity != "critical" {
		t.Fatalf("status fields did not survive the scan: %+v", out)
	}
	if out.Output["status_code"] != float64(503) {
		t.Errorf("output = %v, want status_code 503", out.Output)
	}
	if out.EvaluationResult["overall_severity"] != "critical" {
		t.Errorf("evaluation_result = %v", out.EvaluationResult)
	}
	if out.StartedAt == nil || out.FinishedAt == nil || out.DurationMs == nil || *out.DurationMs != 3000 {
		t.Fatalf("timing fields did not survive the scan: %+v", out)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestCheckRunRepository_Get_NullJSONAndTimes(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	scheduled := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM check_runs`).
		WithArgs("tenant-a", int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "run_id", "tenant_id", "check_id", "status", "severity",
			"output", "evaluation_result", "scheduled_at", "started_at",
			"finished_at", "duration_ms", "version", "created_at",
		}).AddRow(
			9, "run-abc", "tenant-a", 3, "pending", nil,
			nil, nil, scheduled, nil, nil, nil, 1, scheduled,
		))
	mock.ExpectCommit()

	out, err := NewCheckRunRepository(db).Get(context.Background(), "tenant-a", 9)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A run that never executed has no output, no severity and no timeline.
	if out.Output != nil || out.EvaluationResult != nil || out.Severity != nil {
		t.Fatalf("expected empty observation fields: %+v", out)
	}
	if out.StartedAt != nil || out.FinishedAt != nil || out.DurationMs != nil {
		t.Fatalf("expected empty timing fields: %+v", out)
	}
}

func TestCheckRunRepository_Get_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM check_runs`).
		WithArgs("tenant-a", int64(99)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err = NewCheckRunRepository(db).Get(context.Background(), "tenant-a", 99)
	assertNotFound(t, err, "check_run")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestCheckRunRepository_Get_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM check_runs`).
		WithArgs("tenant-a", int64(9)).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, err := NewCheckRunRepository(db).Get(context.Background(), "tenant-a", 9); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestCheckRunRepository_List(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	created := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	severity := "ok"
	duration := 120
	checkID := int64(3)
	status := "success"

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM check_runs`).
		WithArgs("tenant-a", &checkID, &status).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT (.+) FROM check_runs WHERE tenant_id = \$1 (.+) ORDER BY created_at DESC LIMIT \$4 OFFSET \$5`).
		WithArgs("tenant-a", &checkID, &status, 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "run_id", "check_id", "status", "severity", "duration_ms", "created_at",
		}).AddRow(9, "run-abc", 3, "success", &severity, &duration, created))
	mock.ExpectCommit()

	items, total, err := NewCheckRunRepository(db).List(context.Background(), checkrunquery.ListCheckRunsInput{
		TenantID: "tenant-a",
		CheckID:  &checkID,
		Status:   &status,
		Limit:    20,
		Offset:   0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].ID != 9 || items[0].RunID != "run-abc" || items[0].CheckID != 3 {
		t.Fatalf("item identity did not survive the scan: %+v", items[0])
	}
	if items[0].Severity == nil || *items[0].Severity != "ok" || items[0].DurationMs == nil || *items[0].DurationMs != 120 {
		t.Fatalf("optional fields did not survive the scan: %+v", items[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestCheckRunRepository_ListClampsLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM check_runs`).
		WithArgs("tenant-a", nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	// A caller-supplied limit of 500 is clamped to 100 before it is bound.
	mock.ExpectQuery(`SELECT (.+) FROM check_runs`).
		WithArgs("tenant-a", nil, nil, 100, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "run_id", "check_id", "status", "severity", "duration_ms", "created_at",
		}))
	mock.ExpectCommit()

	if _, _, err := NewCheckRunRepository(db).List(context.Background(), checkrunquery.ListCheckRunsInput{
		TenantID: "tenant-a", Limit: 500,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestCheckRunRepository_List_DefaultLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM check_runs`).
		WithArgs("tenant-a", nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	// Zero or negative limits fall back to the default page of 20.
	mock.ExpectQuery(`SELECT (.+) FROM check_runs`).
		WithArgs("tenant-a", nil, nil, 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "run_id", "check_id", "status", "severity", "duration_ms", "created_at",
		}))
	mock.ExpectCommit()

	if _, _, err := NewCheckRunRepository(db).List(context.Background(), checkrunquery.ListCheckRunsInput{
		TenantID: "tenant-a", Limit: -5,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestCheckRunRepository_List_CountError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM check_runs`).
		WithArgs("tenant-a", nil, nil).
		WillReturnError(errors.New("count failed"))
	mock.ExpectRollback()

	if _, _, err := NewCheckRunRepository(db).List(context.Background(), checkrunquery.ListCheckRunsInput{
		TenantID: "tenant-a",
	}); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

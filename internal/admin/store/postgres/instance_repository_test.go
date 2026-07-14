package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNewInstanceRepository(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewInstanceRepository(db)
	if repo == nil {
		t.Fatal("expected non-nil repository")
	}
	if repo.db != db {
		t.Fatal("expected db to be set")
	}
}

var instColumns = []string{
	"id", "run_id", "tenant_id", "job_id", "trigger_source", "status",
	"priority", "effective_priority", "partition_key",
	"idempotency_key", "idempotency_scope", "routing_key", "worker_id",
	"attempt", "max_attempt", "scheduled_at", "started_at", "finished_at",
	"lease_expires_at", "dispatched_at", "retry_at", "result_code", "error_msg",
	"trace_id", "created_at", "updated_at", "version",
}

func TestInstanceRepository_List_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows(instColumns).
		AddRow(
			int64(1), "run-001", "default", int64(42), "schedule", "success",
			5, 5, "tenant-a:batch",
			"ik-001", "job_instance_create", "rk-001", "worker-1",
			1, 3, now, now, now,
			now, now, now, "200", nil,
			"trace-001", now, now, 1,
		).
		AddRow(
			int64(2), "run-002", "default", int64(42), "manual", "running",
			3, 3, nil,
			nil, "job_instance_create", nil, nil,
			1, 3, now, nil, nil,
			nil, now, nil, nil, nil,
			nil, now, now, 1,
		)

	mock.ExpectQuery(`SELECT (.+) FROM job_instances
			WHERE tenant_id = \$1
			  AND \(\$2 = '' OR status = \$2\)
			ORDER BY created_at DESC
			LIMIT \$3 OFFSET \$4`).
		WithArgs("default", "", 50, 0).
		WillReturnRows(rows)

	repo := NewInstanceRepository(db)
	items, err := repo.List(context.Background(), "default", "", 50, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].ID != 1 || items[0].RunID != "run-001" {
		t.Fatalf("unexpected first item: id=%d runID=%s", items[0].ID, items[0].RunID)
	}
	if items[0].WorkerID == nil || *items[0].WorkerID != "worker-1" {
		t.Fatalf("expected first item WorkerID=worker-1, got %v", items[0].WorkerID)
	}
	// Second row has nils — verify pointer fields are nil
	if items[1].PartitionKey != nil {
		t.Fatalf("expected second item PartitionKey=nil, got %v", *items[1].PartitionKey)
	}
	if items[1].WorkerID != nil {
		t.Fatalf("expected second item WorkerID=nil, got %v", *items[1].WorkerID)
	}
	if items[1].StartedAt != nil {
		t.Fatalf("expected second item StartedAt=nil, got %v", items[1].StartedAt)
	}
}

func TestInstanceRepository_List_FilterByStatus(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows(instColumns).
		AddRow(
			int64(1), "run-001", "default", int64(42), "schedule", "running",
			5, 5, "tenant-a:batch",
			"ik-001", "job_instance_create", "rk-001", "worker-1",
			1, 3, now, now, nil,
			nil, now, nil, nil, nil,
			"trace-001", now, now, 1,
		)

	mock.ExpectQuery(`SELECT (.+) FROM job_instances
			WHERE tenant_id = \$1
			  AND \(\$2 = '' OR status = \$2\)
			ORDER BY created_at DESC
			LIMIT \$3 OFFSET \$4`).
		WithArgs("default", "running", 50, 0).
		WillReturnRows(rows)

	repo := NewInstanceRepository(db)
	items, err := repo.List(context.Background(), "default", "running", 50, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Status != "running" {
		t.Fatalf("expected status=running, got %s", items[0].Status)
	}
}

func TestInstanceRepository_List_Empty(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	rows := sqlmock.NewRows(instColumns)

	mock.ExpectQuery(`SELECT (.+) FROM job_instances
			WHERE tenant_id = \$1
			  AND \(\$2 = '' OR status = \$2\)
			ORDER BY created_at DESC
			LIMIT \$3 OFFSET \$4`).
		WithArgs("default", "", 50, 0).
		WillReturnRows(rows)

	repo := NewInstanceRepository(db)
	items, err := repo.List(context.Background(), "default", "", 50, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
}

func TestInstanceRepository_List_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT (.+) FROM job_instances`).
		WithArgs("default", "", 50, 0).
		WillReturnError(errors.New("connection refused"))

	repo := NewInstanceRepository(db)
	_, err = repo.List(context.Background(), "default", "", 50, 0)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestInstanceRepository_GetByRunID_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows(instColumns).
		AddRow(
			int64(1), "run-001", "default", int64(42), "manual", "success",
			5, 5, nil,
			nil, "job_instance_create", nil, nil,
			1, 3, now, now, now,
			nil, now, nil, "200", nil,
			"trace-001", now, now, 1,
		)

	mock.ExpectQuery(`SELECT (.+) FROM job_instances
			WHERE tenant_id = \$1
			  AND run_id = \$2`).
		WithArgs("tenant-a", "run-001").
		WillReturnRows(rows)

	repo := NewInstanceRepository(db)
	snap, err := repo.GetByRunID(context.Background(), "tenant-a", "run-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.RunID != "run-001" {
		t.Fatalf("expected RunID=run-001, got %s", snap.RunID)
	}
	if snap.ID != 1 {
		t.Fatalf("expected ID=1, got %d", snap.ID)
	}
}

func TestInstanceRepository_GetByRunID_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT (.+) FROM job_instances
			WHERE tenant_id = \$1
			  AND run_id = \$2`).
		WithArgs("tenant-a", "run-missing").
		WillReturnError(sql.ErrNoRows)

	repo := NewInstanceRepository(db)
	_, err = repo.GetByRunID(context.Background(), "tenant-a", "run-missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestInstanceRepository_List_ScanError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Pass a string for the "id" column (column 0, scanned as *int64) to trigger a Scan error.
	rows := sqlmock.NewRows(instColumns).
		AddRow(
			"not_an_int64", "run-001", "default", int64(42), "schedule", "running",
			5, 5, nil,
			nil, "job_instance_create", nil, nil,
			1, 3, time.Now(), nil, nil,
			nil, time.Now(), nil, nil, nil,
			nil, time.Now(), time.Now(), 1,
		)

	mock.ExpectQuery(`SELECT (.+) FROM job_instances
			WHERE tenant_id = \$1
			  AND \(\$2 = '' OR status = \$2\)
			ORDER BY created_at DESC
			LIMIT \$3 OFFSET \$4`).
		WithArgs("default", "", 50, 0).
		WillReturnRows(rows)

	repo := NewInstanceRepository(db)
	_, err = repo.List(context.Background(), "default", "", 50, 0)
	if err == nil {
		t.Fatal("expected scan error")
	}
}

func TestInstanceRepository_List_RowsError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// RowError(0) causes rows.Err() to return an error after the first row iteration attempt.
	rows := sqlmock.NewRows(instColumns).
		AddRow(
			int64(1), "run-001", "default", int64(42), "schedule", "running",
			5, 5, nil,
			nil, "job_instance_create", nil, nil,
			1, 3, time.Now(), nil, nil,
			nil, time.Now(), nil, nil, nil,
			nil, time.Now(), time.Now(), 1,
		).
		RowError(0, errors.New("iteration failure"))

	mock.ExpectQuery(`SELECT (.+) FROM job_instances
			WHERE tenant_id = \$1
			  AND \(\$2 = '' OR status = \$2\)
			ORDER BY created_at DESC
			LIMIT \$3 OFFSET \$4`).
		WithArgs("default", "", 50, 0).
		WillReturnRows(rows)

	repo := NewInstanceRepository(db)
	_, err = repo.List(context.Background(), "default", "", 50, 0)
	if err == nil {
		t.Fatal("expected rows iteration error")
	}
}

func TestInstanceRepository_GetByRunID_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT (.+) FROM job_instances
			WHERE tenant_id = \$1
			  AND run_id = \$2`).
		WithArgs("tenant-a", "run-001").
		WillReturnError(errors.New("connection refused"))

	repo := NewInstanceRepository(db)
	_, err = repo.GetByRunID(context.Background(), "tenant-a", "run-001")
	if err == nil {
		t.Fatal("expected error")
	}
}

var attemptColumns = []string{
	"attempt_no", "worker_id", "status", "started_at", "finished_at",
	"result_code", "error_msg",
}

func TestInstanceRepository_ListAttempts_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	workerID := "worker-1"
	resultCode := "200"
	errorMsg := "timeout"
	rows := sqlmock.NewRows(attemptColumns).
		AddRow(1, workerID, "success", now, now, resultCode, nil).
		AddRow(2, nil, "failed", now, now, nil, errorMsg)

	mock.ExpectQuery(`SELECT a\.attempt_no, a\.worker_id, a\.status, a\.started_at, a\.finished_at,\s*a\.result_code, a\.error_msg\s+FROM job_instance_attempts a\s+JOIN job_instances i ON a\.tenant_id = i\.tenant_id AND a\.instance_id = i\.id\s+WHERE i\.tenant_id = \$1 AND i\.run_id = \$2\s+ORDER BY a\.attempt_no ASC`).
		WithArgs("default", "run-001").
		WillReturnRows(rows)

	repo := NewInstanceRepository(db)
	items, err := repo.ListAttempts(context.Background(), "default", "run-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].AttemptNo != 1 {
		t.Fatalf("expected attempt_no=1, got %d", items[0].AttemptNo)
	}
	if items[0].WorkerID == nil || *items[0].WorkerID != workerID {
		t.Fatalf("expected worker_id=%q, got %v", workerID, items[0].WorkerID)
	}
	if items[0].ResultCode == nil || *items[0].ResultCode != resultCode {
		t.Fatalf("expected result_code=%q, got %v", resultCode, items[0].ResultCode)
	}
	if items[0].ErrorMsg != nil {
		t.Fatalf("expected error_msg=nil, got %v", *items[0].ErrorMsg)
	}
	if items[1].WorkerID != nil {
		t.Fatalf("expected nil worker_id, got %v", *items[1].WorkerID)
	}
	if items[1].ResultCode != nil {
		t.Fatalf("expected nil result_code, got %v", *items[1].ResultCode)
	}
	if items[1].ErrorMsg == nil || *items[1].ErrorMsg != errorMsg {
		t.Fatalf("expected error_msg=%q, got %v", errorMsg, items[1].ErrorMsg)
	}
}

func TestInstanceRepository_ListAttempts_Empty(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	rows := sqlmock.NewRows(attemptColumns)
	mock.ExpectQuery(`SELECT a\.attempt_no, a\.worker_id, a\.status, a\.started_at, a\.finished_at,\s*a\.result_code, a\.error_msg\s+FROM job_instance_attempts a\s+JOIN job_instances i ON a\.tenant_id = i\.tenant_id AND a\.instance_id = i\.id\s+WHERE i\.tenant_id = \$1 AND i\.run_id = \$2\s+ORDER BY a\.attempt_no ASC`).
		WithArgs("default", "run-001").
		WillReturnRows(rows)

	repo := NewInstanceRepository(db)
	items, err := repo.ListAttempts(context.Background(), "default", "run-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
}

func TestInstanceRepository_ListAttempts_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT a\.attempt_no, a\.worker_id, a\.status, a\.started_at, a\.finished_at,
\s+a\.result_code, a\.error_msg
\s+FROM job_instance_attempts a
\s+JOIN job_instances i ON a\.tenant_id = i\.tenant_id AND a\.instance_id = i\.id
\s+WHERE i\.tenant_id = \$1 AND i\.run_id = \$2
\s+ORDER BY a\.attempt_no ASC`).
		WithArgs("default", "run-001").
		WillReturnError(errors.New("connection refused"))

	repo := NewInstanceRepository(db)
	_, err = repo.ListAttempts(context.Background(), "default", "run-001")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestInstanceRepository_ListAttempts_ScanError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	rows := sqlmock.NewRows(attemptColumns).
		AddRow("not-an-int", nil, "success", nil, nil, nil, nil)
	mock.ExpectQuery(`SELECT a\.attempt_no, a\.worker_id, a\.status, a\.started_at, a\.finished_at,\s*a\.result_code, a\.error_msg\s+FROM job_instance_attempts a\s+JOIN job_instances i ON a\.tenant_id = i\.tenant_id AND a\.instance_id = i\.id\s+WHERE i\.tenant_id = \$1 AND i\.run_id = \$2\s+ORDER BY a\.attempt_no ASC`).
		WithArgs("default", "run-001").
		WillReturnRows(rows)

	repo := NewInstanceRepository(db)
	_, err = repo.ListAttempts(context.Background(), "default", "run-001")
	if err == nil {
		t.Fatal("expected scan error")
	}
}

func TestInstanceRepository_ListAttempts_RowsError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	rows := sqlmock.NewRows(attemptColumns).
		AddRow(1, nil, "success", nil, nil, nil, nil).
		RowError(0, errors.New("iteration failure"))
	mock.ExpectQuery(`SELECT a\.attempt_no, a\.worker_id, a\.status, a\.started_at, a\.finished_at,\s*a\.result_code, a\.error_msg\s+FROM job_instance_attempts a\s+JOIN job_instances i ON a\.tenant_id = i\.tenant_id AND a\.instance_id = i\.id\s+WHERE i\.tenant_id = \$1 AND i\.run_id = \$2\s+ORDER BY a\.attempt_no ASC`).
		WithArgs("default", "run-001").
		WillReturnRows(rows)

	repo := NewInstanceRepository(db)
	_, err = repo.ListAttempts(context.Background(), "default", "run-001")
	if err == nil {
		t.Fatal("expected rows iteration error")
	}
}

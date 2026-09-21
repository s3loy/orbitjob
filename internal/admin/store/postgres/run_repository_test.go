package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	runquery "orbitjob/internal/admin/app/run/query"
	"orbitjob/internal/domain/resource"
)

// The admin side of the run ledger is read-only by construction: orbitjob_admin
// holds SELECT and nothing else on the control-plane tables. These tests pin
// what the reads promise the API layer: tenant scoping on every query, the
// column order the scans depend on, NULL attempt columns for a run that has not
// started, and "another tenant's row" reading as NotFoundError.

func newRunRepoMock(t *testing.T) (*RunRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewRunRepository(db), mock
}

// expectRunTenantTx opens the admin read transaction: BEGIN, then the
// session-scoped-until-commit set_config that binds the RLS policies.
func expectRunTenantTx(mock sqlmock.Sqlmock, tenantID string) {
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

var runListColumns = []string{"id", "tenant_id", "source_uid", "revision_id", "occurrence_key",
	"trigger", "actor", "phase", "attempt", "max_attempts", "created_at", "updated_at"}

var runAttemptColumns = []string{"id", "run_id", "attempt_number", "phase", "kubernetes_job_name",
	"kubernetes_job_uid", "observed_resource_version", "started_at", "completed_at", "created_at"}

func TestRunRepository_List(t *testing.T) {
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	repo, mock := newRunRepoMock(t)
	expectRunTenantTx(mock, "tenant-a")
	rows := sqlmock.NewRows(runListColumns)
	rows.AddRow(7, "tenant-a", "nightly", 11, "occ-1", "Schedule", "key-9", "Running", 2, 3, now, now)
	rows.AddRow(6, "tenant-a", "nightly", 11, "occ-2", "Manual", "key-1", "Succeeded", 1, 3, now, now)
	mock.ExpectQuery("FROM job_run_control_plane r").
		WithArgs("tenant-a", "Running", 50, 0).
		WillReturnRows(rows)
	mock.ExpectCommit()

	got, err := repo.List(context.Background(), runquery.ListInput{
		TenantID: "tenant-a", Phase: "Running", Limit: 50, Offset: 0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d runs, want 2", len(got))
	}
	// rtrim(occurrence_key) means the caller sees the key that was written.
	if got[0].ID != 7 || got[0].OccurrenceKey != "occ-1" || got[0].Phase != "Running" || got[0].Attempt != 2 {
		t.Fatalf("first row did not survive the scan: %+v", got[0])
	}
	if got[1].Trigger != "Manual" || got[1].Actor != "key-1" {
		t.Fatalf("second row did not survive the scan: %+v", got[1])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRunRepository_ListEmptyPageIsNotAnError(t *testing.T) {
	repo, mock := newRunRepoMock(t)
	expectRunTenantTx(mock, "tenant-a")
	mock.ExpectQuery("FROM job_run_control_plane r").
		WillReturnRows(sqlmock.NewRows(runListColumns))
	mock.ExpectCommit()

	got, err := repo.List(context.Background(), runquery.ListInput{TenantID: "tenant-a", Limit: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %+v, want an empty page", got)
	}
}

func TestRunRepository_GetRunWithAttemptTrail(t *testing.T) {
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	repo, mock := newRunRepoMock(t)
	expectRunTenantTx(mock, "tenant-a")

	// The LEFT JOIN returns one row per attempt; the run columns repeat.
	runCols := append([]string{}, runListColumns...)
	detailRows := sqlmock.NewRows(append(runCols, runAttemptColumns...))
	detailRows.AddRow(7, "tenant-a", "nightly", 11, "occ-1", "Schedule", "key-9", "Running", 2, 3, now, now,
		1, 7, 1, "Failed", "oj-nightly-00000001", "uid-1", "rv-1", now, now, now)
	detailRows.AddRow(7, "tenant-a", "nightly", 11, "occ-1", "Schedule", "key-9", "Running", 2, 3, now, now,
		2, 7, 2, "Running", "oj-nightly-00000002", nil, nil, nil, nil, now)
	mock.ExpectQuery("LEFT JOIN job_run_attempts_control_plane").
		WithArgs("tenant-a", int64(7)).
		WillReturnRows(detailRows)
	mock.ExpectCommit()

	got, err := repo.Get(context.Background(), runquery.GetInput{TenantID: "tenant-a", ID: 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != 7 || got.Phase != "Running" || got.MaxAttempts != 3 {
		t.Fatalf("run row did not survive the scan: %+v", got)
	}
	if len(got.Attempts) != 2 {
		t.Fatalf("got %d attempts, want 2", len(got.Attempts))
	}
	first, second := got.Attempts[0], got.Attempts[1]
	if first.AttemptNumber != 1 || first.Phase != "Failed" || first.KubernetesJobUID == nil || *first.KubernetesJobUID != "uid-1" {
		t.Fatalf("first attempt did not survive the scan: %+v", first)
	}
	if second.KubernetesJobUID != nil || second.StartedAt != nil {
		t.Fatalf("NULL attempt columns must stay nil: %+v", second)
	}
}

func TestRunRepository_GetRunWithoutAttempts(t *testing.T) {
	now := time.Now()
	repo, mock := newRunRepoMock(t)
	expectRunTenantTx(mock, "tenant-a")

	// A run with no attempt yet returns one row with NULL attempt columns, so
	// "has not started" stays distinguishable from "does not exist".
	detailRows := sqlmock.NewRows(append(append([]string{}, runListColumns...), runAttemptColumns...))
	detailRows.AddRow(9, "tenant-a", "hourly", 12, "occ-9", "Schedule", "", "Pending", 0, 1, now, now,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	mock.ExpectQuery("LEFT JOIN job_run_attempts_control_plane").
		WithArgs("tenant-a", int64(9)).
		WillReturnRows(detailRows)
	mock.ExpectCommit()

	got, err := repo.Get(context.Background(), runquery.GetInput{TenantID: "tenant-a", ID: 9})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != 9 || got.Attempt != 0 {
		t.Fatalf("run row did not survive the scan: %+v", got)
	}
	if got.Attempts == nil || len(got.Attempts) != 0 {
		t.Fatalf("got attempts %+v, want an empty trail", got.Attempts)
	}
}

func TestRunRepository_GetMissingRunIsNotFound(t *testing.T) {
	repo, mock := newRunRepoMock(t)
	expectRunTenantTx(mock, "tenant-a")
	// No rows: the join found nothing for this tenant, which must read as
	// NotFoundError rather than an empty run.
	mock.ExpectQuery("LEFT JOIN job_run_attempts_control_plane").
		WithArgs("tenant-a", int64(99)).
		WillReturnRows(sqlmock.NewRows(append(append([]string{}, runListColumns...), runAttemptColumns...)))
	mock.ExpectRollback()

	_, err := repo.Get(context.Background(), runquery.GetInput{TenantID: "tenant-a", ID: 99})
	var nf *resource.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want *resource.NotFoundError", err)
	}
	if nf.Resource != "run" {
		t.Fatalf("not-found names resource %q, want %q", nf.Resource, "run")
	}
}

func TestRunRepository_ListAttempts(t *testing.T) {
	now := time.Now()
	repo, mock := newRunRepoMock(t)
	expectRunTenantTx(mock, "tenant-a")
	rows := sqlmock.NewRows(runAttemptColumns)
	rows.AddRow(2, 7, 2, "Running", "oj-nightly-00000002", nil, "rv-2", nil, nil, now)
	rows.AddRow(1, 7, 1, "Failed", "oj-nightly-00000001", "uid-1", "rv-1", now, now, now)
	mock.ExpectQuery("JOIN job_run_control_plane r").
		WithArgs("tenant-a", int64(7)).
		WillReturnRows(rows)
	mock.ExpectCommit()

	got, err := repo.ListAttempts(context.Background(), runquery.GetInput{TenantID: "tenant-a", ID: 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0].AttemptNumber != 2 || got[1].AttemptNumber != 1 {
		t.Fatalf("unexpected trail: %+v", got)
	}
	if got[1].KubernetesJobUID == nil || *got[1].KubernetesJobUID != "uid-1" {
		t.Fatalf("job uid did not survive the scan: %+v", got[1])
	}
}

func TestRunRepository_GetCancelTarget(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock := newRunRepoMock(t)
		expectRunTenantTx(mock, "tenant-a")
		mock.ExpectQuery("JOIN job_definition_revisions d").
			WithArgs("tenant-a", int64(7)).
			WillReturnRows(sqlmock.NewRows([]string{"phase", "occurrence_key", "source_name", "source_namespace"}).
				AddRow("Running", "occ-1", "nightly", "jobs"))
		mock.ExpectCommit()

		got, err := repo.GetCancelTarget(context.Background(), runquery.GetInput{TenantID: "tenant-a", ID: 7})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Phase != "Running" || got.OccurrenceKey != "occ-1" ||
			got.ScheduledJobName != "nightly" || got.Namespace != "jobs" {
			t.Fatalf("cancel target did not survive the scan: %+v", got)
		}
	})

	t.Run("a run outside the tenant reads as not found", func(t *testing.T) {
		repo, mock := newRunRepoMock(t)
		expectRunTenantTx(mock, "tenant-a")
		mock.ExpectQuery("JOIN job_definition_revisions d").
			WillReturnError(sql.ErrNoRows)
		mock.ExpectRollback()

		_, err := repo.GetCancelTarget(context.Background(), runquery.GetInput{TenantID: "tenant-a", ID: 99})
		var nf *resource.NotFoundError
		if !errors.As(err, &nf) {
			t.Fatalf("got %v, want *resource.NotFoundError", err)
		}
	})
}

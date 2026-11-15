package postgres

import (
	"context"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"orbitjob/internal/core/domain/jobrun"

	"orbitjob/internal/platform/scan"
)

func TestCompletedReadModelsAcceptDuplicateReplay(t *testing.T) {
	t.Run("check", func(t *testing.T) {
		repo, mock := newCheckRunRepoMock(t)
		mock.ExpectBegin()
		expectSetConfig(mock, coreFixtureStorageRegressionsTenantID)
		mock.ExpectQuery("INSERT INTO check_runs").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		mock.ExpectCommit()
		if err := repo.RecordCompleted(context.Background(), coreFixtureStorageRegressionsTenantID, completedRecord()); err != nil {
			t.Fatalf("duplicate replay must succeed: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("function", func(t *testing.T) {
		_, repo, mock := newFunctionRepoMock(t)
		gucExpectations(mock, tenantFixture())
		mock.ExpectQuery("INSERT INTO function_runs").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		mock.ExpectCommit()
		if err := repo.RecordCompleted(context.Background(), tenantFixture(), completedFunctionRecord()); err != nil {
			t.Fatalf("duplicate replay must succeed: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCancelUnknownSelectedForRetentionCanBeDeleted(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, coreFixtureStorageRegressionsTenantID)
	mock.ExpectQuery("SELECT id FROM job_run_control_plane").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(77)))
	mock.ExpectQuery("DELETE FROM job_run_control_plane").
		WithArgs(int64(77), coreFixtureStorageRegressionsTenantID, retentionPhaseSet()).
		WillReturnRows(sqlmock.NewRows([]string{"occurrence_key", "phase", "actor"}).
			AddRow("occ-unknown", string(jobrun.CancelUnknown), "scheduler"))
	expectControlPlaneAudit(mock, "scheduler")
	mock.ExpectCommit()
	deleted, err := repo.DeleteRun(context.Background(), coreFixtureStorageRegressionsTenantID, 77)
	if err != nil || !deleted {
		t.Fatalf("retention-eligible row was not deleted: deleted=%v error=%v", deleted, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

var coreFixtureStorageRegressionsTenantID = scan.GenerateID()

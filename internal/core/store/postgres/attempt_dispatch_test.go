package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/platform/scan"
)

func expectDispatchTransaction(mock sqlmock.Sqlmock, stage jobrun.DispatchStage) {
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WillReturnResult(sqlmock.NewResult(0, 0))
	expectExecutionSourceLocks(mock, stage)
	mock.ExpectQuery(`SELECT workflow_run_id`).WillReturnRows(sqlmock.NewRows([]string{"workflow_run_id"}).AddRow(nil))
	mock.ExpectQuery(`SELECT r.phase,r.attempt,r.actor`).WillReturnRows(sqlmock.NewRows([]string{"phase", "attempt", "actor", "uid", "id", "dispatch_stage"}).AddRow("CreatingAttempt", 1, "operator", "", 1, string(stage)))
	if stage == jobrun.DispatchPrepared {
		expectExecutionControlCheck(mock)
	}
}

func expectDispatchReservation(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WillReturnResult(sqlmock.NewResult(0, 0))
	expectExecutionSourceLocks(mock, jobrun.DispatchPrepared)
	expectExecutionControlCheck(mock)
	mock.ExpectQuery(`SELECT workflow_run_id`).WillReturnRows(sqlmock.NewRows([]string{"workflow_run_id"}).AddRow(nil))
	mock.ExpectQuery(`SELECT id FROM job_run_control_plane`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`INSERT INTO job_run_attempts_control_plane`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`UPDATE job_run_control_plane`).WillReturnRows(sqlmock.NewRows([]string{"actor"}).AddRow("operator"))
	mock.ExpectExec(`INSERT INTO audit_events`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
}

func expectMayHaveStarted(mock sqlmock.Sqlmock) {
	expectDispatchTransaction(mock, jobrun.DispatchPrepared)
	mock.ExpectExec(`UPDATE job_run_attempts_control_plane SET dispatch_stage=`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO audit_events`).WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestDispatchMarkerCommitFailureNeverCallsExternalCreator(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectDispatchReservation(mock)
	expectMayHaveStarted(mock)
	commitErr := errors.New("commit acknowledgement lost")
	mock.ExpectCommit().WillReturnError(commitErr)
	called := false
	err := repo.CreateAttemptWithEnsure(context.Background(), scan.GenerateID(), 1, jobrun.Attempt{Number: 1, KubernetesJobName: "billing-extract"}, func(context.Context) (jobrun.Attempt, error) {
		called = true
		return jobrun.Attempt{}, nil
	})
	if !errors.Is(err, commitErr) || called {
		t.Fatalf("unconfirmed marker permitted send: err=%v called=%t", err, called)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBindingCommitFailureNeverResendsOnReentry(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectDispatchReservation(mock)
	expectMayHaveStarted(mock)
	mock.ExpectCommit()
	expectDispatchTransaction(mock, jobrun.DispatchMayHaveStarted)
	mock.ExpectExec(`UPDATE job_run_attempts_control_plane`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE job_run_control_plane`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO audit_events`).WillReturnResult(sqlmock.NewResult(0, 1))
	commitErr := errors.New("UID binding acknowledgement lost")
	mock.ExpectCommit().WillReturnError(commitErr)
	// Model the conservative outcome where only the earlier marker persisted.
	expectDispatchReservation(mock)
	expectDispatchTransaction(mock, jobrun.DispatchMayHaveStarted)
	mock.ExpectRollback()

	tenant := scan.GenerateID()
	intent := jobrun.Attempt{Number: 1, KubernetesJobName: "billing-extract"}
	created := 0
	create := func(context.Context) (jobrun.Attempt, error) {
		created++
		return jobrun.Attempt{Number: 1, KubernetesJobName: intent.KubernetesJobName, KubernetesJobUID: scan.GenerateID(), Phase: jobrun.Running}, nil
	}
	if err := repo.CreateAttemptWithEnsure(context.Background(), tenant, 1, intent, create); !errors.Is(err, commitErr) {
		t.Fatal(err)
	}
	if err := repo.CreateAttemptWithEnsure(context.Background(), tenant, 1, intent, create); !errors.Is(err, jobrun.ErrDispatchMayHaveStarted) {
		t.Fatalf("reentry did not require observation: %v", err)
	}
	if created != 1 {
		t.Fatalf("external creation calls=%d, want 1", created)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

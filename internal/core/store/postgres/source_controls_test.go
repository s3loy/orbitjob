package postgres

import (
	"github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/jobrun"
)

func expectNoSourceControl(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT version,schedule_paused,execution_paused,tombstoned,actor,updated_at`).WillReturnRows(sqlmock.NewRows([]string{"version", "schedule_paused", "execution_paused", "tombstoned", "actor", "updated_at"}))
}

func expectActiveTenant(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT execution_tenant_active`).WillReturnRows(sqlmock.NewRows([]string{"active"}).AddRow(true))
}

func expectExecutionSourceLocks(mock sqlmock.Sqlmock, stage jobrun.DispatchStage) {
	mock.ExpectQuery(`SELECT d.source_mode,r.source_uid,w.source_uid`).WillReturnRows(sqlmock.NewRows([]string{"source_mode", "source_uid", "parent_source_uid", "dispatch_stage"}).AddRow("kubernetes", "nightly", nil, string(stage)))
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectExecutionControlCheck(mock sqlmock.Sqlmock) {
	expectActiveTenant(mock)
	expectNoSourceControl(mock)
}

func expectWorkflowStepLocks(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT source_uid FROM workflow_run_control_plane`).WillReturnRows(sqlmock.NewRows([]string{"source_uid"}).AddRow("wf-nightly"))
	expectWorkflowSourceLock(mock, coreFixtureWorkflowRepositoryTenantID, "kubernetes", "nightly", 9)
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs(sourceLockKey(coreFixtureWorkflowRepositoryTenantID, "workflow", "wf-nightly")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT phase FROM workflow_run_control_plane`).WillReturnRows(sqlmock.NewRows([]string{"phase"}).AddRow("Running"))
}

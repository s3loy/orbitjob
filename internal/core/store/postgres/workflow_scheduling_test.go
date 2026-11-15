package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/workflow"
	"orbitjob/internal/domain/resource"
)

func expectWorkflowIdentityLock(mock sqlmock.Sqlmock, existing *sqlmock.Rows) {
	expectWorkflowTx(mock, coreFixtureWorkflowRepositoryTenantID)
	expectWorkflowSourceLock(mock, coreFixtureWorkflowRepositoryTenantID, "workflow", "wf-nightly", 5)
	mock.ExpectQuery("SELECT .+ FROM workflow_run_control_plane").WithArgs("wf-nightly", "occ-1", coreFixtureWorkflowRepositoryTenantID).WillReturnRows(existing)
}

func TestWorkflowForbidScheduledRejectionIsAnAtomicTerminalReceipt(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)
	run := workflowRun()
	run.Trigger = jobrun.Schedule
	run.ScheduledFor = &workflowRowTime
	run.ConcurrencyPolicy = "Forbid"
	expectWorkflowIdentityLock(mock, sqlmock.NewRows(workflowRunScanColumns))
	mock.ExpectQuery("SELECT id FROM job_definition_revisions").WithArgs(int64(5), coreFixtureWorkflowRepositoryTenantID, "wf-nightly", workflowRowTime).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(5))
	expectExecutionControlCheck(mock)
	mock.ExpectQuery("SELECT normalized_spec::text FROM job_definition_revisions").WithArgs(coreFixtureWorkflowRepositoryTenantID, "workflow", "wf-nightly").WillReturnRows(sqlmock.NewRows([]string{"normalized_spec"}).AddRow(`{"concurrencyPolicy":"Forbid"}`))
	mock.ExpectQuery("SELECT count").WithArgs(coreFixtureWorkflowRepositoryTenantID, "wf-nightly", workflowTerminalSet()).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery("INSERT INTO workflow_run_control_plane").WithArgs(coreFixtureWorkflowRepositoryTenantID, "wf-nightly", int64(5), "occ-1", "Schedule", "walker", "Rejected", workflowRowTime, "concurrency_forbid", "", 0).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
	mock.ExpectQuery("SELECT .+ FROM workflow_run_control_plane").WillReturnRows(sqlmock.NewRows(workflowRunScanColumns).AddRow(11, "wf-nightly", int64(5), "occ-1", "Schedule", "walker", "Rejected", "{}", workflowRowTime, workflowRowTime, workflowRowTime, "concurrency_forbid", "", 0, "{}"))
	expectControlPlaneAudit(mock, "walker")
	mock.ExpectCommit()
	stored, created, err := repo.CreateRunForTenant(context.Background(), coreFixtureWorkflowRepositoryTenantID, run)
	if err != nil || !created || stored.Phase != workflow.PhaseRejected || stored.Reason != "concurrency_forbid" || !workflow.Terminal(stored.Phase) {
		t.Fatalf("receipt=%+v created=%v err=%v", stored, created, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowForbidResolvesSameOccurrenceBeforeOverlapCheck(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)
	run := workflowRun()
	run.ConcurrencyPolicy = "Forbid"
	expectWorkflowIdentityLock(mock, workflowRunRow(11, "Running", "{}"))
	mock.ExpectCommit()
	stored, created, err := repo.CreateRunForTenant(context.Background(), coreFixtureWorkflowRepositoryTenantID, run)
	if err != nil || created || stored.ID != 11 {
		t.Fatalf("stored=%+v created=%v err=%v", stored, created, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowScheduledGuardRejectsInactiveRevision(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)
	run := workflowRun()
	run.ConcurrencyPolicy = "Forbid"
	run.ScheduledFor = &workflowRowTime
	expectWorkflowIdentityLock(mock, sqlmock.NewRows(workflowRunScanColumns))
	mock.ExpectQuery("SELECT id FROM job_definition_revisions").WithArgs(int64(5), coreFixtureWorkflowRepositoryTenantID, "wf-nightly", workflowRowTime).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()
	_, _, err := repo.CreateRunForTenant(context.Background(), coreFixtureWorkflowRepositoryTenantID, run)
	var conflict *resource.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowForbidManualWaitsWithoutRejectedLedger(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)
	run := workflowRun()
	run.Trigger = jobrun.Manual
	expectWorkflowIdentityLock(mock, sqlmock.NewRows(workflowRunScanColumns))
	expectExecutionControlCheck(mock)
	mock.ExpectQuery("SELECT normalized_spec::text FROM job_definition_revisions").WithArgs(coreFixtureWorkflowRepositoryTenantID, "workflow", "wf-nightly").WillReturnRows(sqlmock.NewRows([]string{"normalized_spec"}).AddRow(`{"concurrencyPolicy":"Forbid"}`))
	mock.ExpectQuery("SELECT count").WithArgs(coreFixtureWorkflowRepositoryTenantID, "wf-nightly", workflowTerminalSet()).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectRollback()
	row, created, err := repo.CreateRunForTenant(context.Background(), coreFixtureWorkflowRepositoryTenantID, run)
	if created || row.ID != 0 || !errors.Is(err, jobrun.ErrAdmissionBlocked) {
		t.Fatalf("manual blocked row=%+v created=%t err=%v", row, created, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

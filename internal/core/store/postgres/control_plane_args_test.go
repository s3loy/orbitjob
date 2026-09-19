package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/lib/pq"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/jobrun"
)

// pqFaithfulConverter mirrors lib/pq's driver.NamedValueChecker: slice
// parameters become PostgreSQL array literals the way a real connection sends
// them, and everything else falls through to the standard conversion. Without
// it, statements that take []string phase sets cannot even reach the mock.
type pqFaithfulConverter struct{}

func (pqFaithfulConverter) ConvertValue(v any) (driver.Value, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice && rv.Type().Elem().Kind() != reflect.Uint8 {
		return pq.Array(v).Value()
	}
	return driver.DefaultParameterConverter.ConvertValue(v)
}

// terminalPhaseSet is the exact parameter updateRunPhaseSQL, countOpenRunsSQL
// and deleteRunSQL receive for "not yet finished".
func terminalPhaseSet() []string {
	return []string{"Succeeded", "Failed", "Canceled"}
}

// storedRunRowColumns is the projection selectOccurrenceSQL scans, in order.
var storedRunRowColumns = []string{"id", "revision_id", "phase", "attempt", "max_attempts", "scheduled_for"}

// retentionPhaseSet is what prunableRunsSQL receives: the terminal phases plus
// CancelUnknown, whose stop was issued but never observed.
func retentionPhaseSet() []string {
	return []string{"Succeeded", "Failed", "Canceled", "CancelUnknown"}
}

func TestUpdateRunPhaseCarriesTheTerminalPhaseSet(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, "default")
	// The sixth argument is the terminal set the phase predicate compares
	// against; asserting it exactly keeps the SQL form of jobrun.Terminal from
	// drifting from the domain.
	mock.ExpectQuery("WITH prior").
		WithArgs(int64(77), "default", "Running", 1, sqlmock.AnyArg(), terminalPhaseSet()).
		WillReturnRows(sqlmock.NewRows([]string{"phase", "prior_phase", "prior_attempt", "run_actor"}).
			AddRow("Running", "CreatingAttempt", 0, "scheduler"))
	expectControlPlaneAudit(mock, "scheduler")
	mock.ExpectCommit()

	changed, err := repo.UpdateRunPhase(context.Background(), "default", 77, jobrun.Running, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("a real phase change must be reported as changed")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCountOpenRunsCarriesTheTerminalPhaseSet(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, "default")
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM job_run_control_plane").
		WithArgs("default", "nightly", terminalPhaseSet()).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectCommit()

	count, err := repo.CountOpenRuns(context.Background(), "default", "nightly")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPrunableRunsRanksPerOutcome(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, "default")
	// The workflow_run_id predicate is the retention ruling's exclusion: steps
	// belong to the workflow prune's pass, never to the per-definition sweep.
	mock.ExpectQuery("phase = ANY\\(\\$3\\) AND workflow_run_id IS NULL").
		WithArgs("default", "nightly", retentionPhaseSet(), "Succeeded", 3, 5).
		WillReturnRows(sqlmock.NewRows([]string{"id", "occurrence_key", "phase"}).
			AddRow(81, "occ-3", "Failed").
			AddRow(82, "occ-4", "CancelUnknown"))
	mock.ExpectCommit()

	runs, err := repo.PrunableRuns(context.Background(), "default", "nightly", 3, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 prunable runs, got %d", len(runs))
	}
	if runs[0].Phase != jobrun.Failed || runs[1].Phase != jobrun.CancelUnknown {
		t.Fatalf("unexpected phases: %+v", runs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDeleteRunCarriesTheTerminalPhaseSet(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, "default")
	// The same exclusion as the listing: a single-run delete must not take a
	// workflow step out from under its run.
	mock.ExpectQuery("AND workflow_run_id IS NULL").
		WithArgs(int64(77), "default", terminalPhaseSet()).
		WillReturnRows(sqlmock.NewRows([]string{"occurrence_key", "phase", "actor"}).
			AddRow("occ-1", "Succeeded", "scheduler"))
	expectControlPlaneAudit(mock, "scheduler")
	mock.ExpectCommit()

	deleted, err := repo.DeleteRun(context.Background(), "default", 77)
	if err != nil || !deleted {
		t.Fatalf("deleted=%v err=%v, want deleted with no error", deleted, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateOccurrenceForTenantFloorsMaxAttemptsAtOne(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, "default")
	// A caller that passes zero or negative attempts still gets one attempt,
	// because the run row's max_attempts column requires at least one.
	mock.ExpectQuery("INSERT INTO job_run_control_plane").
		WithArgs("default", "nightly", int64(11), "occ-1", "Schedule", "scheduler", "Pending", 1, nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(77))
	mock.ExpectQuery("SELECT id, revision_id, phase, attempt, max_attempts, scheduled_for FROM job_run_control_plane").
		WithArgs("nightly", "occ-1", "default").
		WillReturnRows(sqlmock.NewRows(storedRunRowColumns).AddRow(77, 11, "Pending", 0, 1, nil))
	expectControlPlaneAudit(mock, "scheduler")
	mock.ExpectCommit()

	state, _, err := repo.CreateOccurrenceForTenant(context.Background(), "default", jobrun.JobRun{
		SourceUID: "nightly", RevisionID: "11", OccurrenceKey: "occ-1", Trigger: jobrun.Schedule,
	}, 0, "scheduler")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.MaxAttempts != 1 {
		t.Fatalf("max_attempts = %d, want the floor of 1", state.MaxAttempts)
	}
}

func TestApplyRevisionForTenantRefusesWhenTheStoredRevisionCannotBeRead(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)

	// The insert found the generation already projected, but reading it back
	// failed: the conflict check cannot be made, so nothing is decided.
	expectControlPlaneTx(mock, "default")
	mock.ExpectQuery("INSERT INTO job_definition_revisions").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT id, spec_hash FROM job_definition_revisions").
		WillReturnError(errors.New("read back failed"))
	mock.ExpectRollback()

	if _, err := repo.ApplyRevisionForTenant(context.Background(), "default", controlPlaneRevision()); err == nil {
		t.Fatal("expected the failed read-back to refuse the projection")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRevisionByID_DBError(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, "default")
	mock.ExpectQuery("SELECT .+ FROM job_definition_revisions WHERE id=\\$1 AND tenant_id=\\$2").
		WithArgs(int64(11), "default").
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, err := repo.RevisionByID(context.Background(), "default", 11); err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRunByOccurrence_DBError(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, "default")
	mock.ExpectQuery("SELECT id, revision_id, phase, attempt, max_attempts, scheduled_for FROM job_run_control_plane").
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, _, err := repo.RunByOccurrence(context.Background(), "default", "nightly", "occ-1"); err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestOpenRuns_DBError(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, "default")
	mock.ExpectQuery("FROM job_run_control_plane").
		WithArgs("default", "nightly", terminalPhaseSet()).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, err := repo.OpenRuns(context.Background(), "default", "nightly"); err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateAttemptForTenant_CarriesTheRunPhaseAndCounter(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, "default")
	// The insert carries the attempt identity; the run update moves to the
	// caller's phase and the attempt number, guarded by the terminal set.
	mock.ExpectQuery("INSERT INTO job_run_attempts_control_plane").
		WithArgs("default", int64(77), 2, "Running", "oj-nightly-2", "uid-2", "rv-9", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(6))
	mock.ExpectQuery("UPDATE job_run_control_plane").
		WithArgs("Running", 2, sqlmock.AnyArg(), int64(77), "default", terminalPhaseSet()).
		WillReturnRows(sqlmock.NewRows([]string{"actor"}).AddRow("scheduler"))
	expectControlPlaneAudit(mock, "scheduler")
	mock.ExpectCommit()

	attempt := jobrun.Attempt{
		Number: 2, KubernetesJobName: "oj-nightly-2", KubernetesJobUID: "uid-2",
		ObservedResourceVersion: "rv-9", Phase: jobrun.Running,
		StartedAt: time.Now().UTC(),
	}
	if err := repo.CreateAttemptForTenant(context.Background(), "default", 77, attempt, jobrun.Running); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateAttemptForTenant_ZeroStartedAtIsStoredAsNull(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, "default")
	mock.ExpectQuery("INSERT INTO job_run_attempts_control_plane").
		WithArgs("default", int64(77), 1, "CreatingAttempt", "oj-nightly-1", "uid-1", "", nil).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(6))
	mock.ExpectQuery("UPDATE job_run_control_plane").
		WithArgs("CreatingAttempt", 1, sqlmock.AnyArg(), int64(77), "default", terminalPhaseSet()).
		WillReturnRows(sqlmock.NewRows([]string{"actor"}).AddRow("scheduler"))
	expectControlPlaneAudit(mock, "scheduler")
	mock.ExpectCommit()

	attempt := jobrun.Attempt{
		Number: 1, KubernetesJobName: "oj-nightly-1", KubernetesJobUID: "uid-1",
		Phase: jobrun.CreatingAttempt,
	}
	if err := repo.CreateAttemptForTenant(context.Background(), "default", 77, attempt, jobrun.CreatingAttempt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateAttemptPhase_DBError(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, "default")
	mock.ExpectQuery("WITH prior").
		WithArgs("oj-nightly-1", "default", "Running", "", "rv-1", false, sqlmock.AnyArg()).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, _, err := repo.UpdateAttemptPhase(context.Background(), "default", "oj-nightly-1", "Running", "", "rv-1"); err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateAttemptPhase_TerminalObservationStampsCompletedAtOnce(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, "default")
	// $6 is jobrun.Terminal of the incoming phase: true only for Succeeded,
	// Failed and Canceled.
	mock.ExpectQuery("WITH prior").
		WithArgs("oj-nightly-1", "default", "Failed", "uid-1", "", true, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "run_id", "attempt_number", "prior_phase", "phase", "actor"}).
			AddRow(5, 77, 1, "Running", "Failed", "scheduler"))
	expectControlPlaneAudit(mock, "scheduler")
	mock.ExpectCommit()

	if _, _, err := repo.UpdateAttemptPhase(context.Background(), "default", "oj-nightly-1", "Failed", "uid-1", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateAttemptPhase_NonTerminalObservationSkipsCompletedAt(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, "default")
	mock.ExpectQuery("WITH prior").
		WithArgs("oj-nightly-1", "default", "Running", "", "rv-2", false, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "run_id", "attempt_number", "prior_phase", "phase", "actor"}).
			AddRow(5, 77, 1, "CreatingAttempt", "Running", "scheduler"))
	expectControlPlaneAudit(mock, "scheduler")
	mock.ExpectCommit()

	runID, attemptNumber, err := repo.UpdateAttemptPhase(context.Background(), "default", "oj-nightly-1", "Running", "", "rv-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runID != 77 || attemptNumber != 1 {
		t.Fatalf("got run=%d attempt=%d, want run=77 attempt=1", runID, attemptNumber)
	}
}

func TestRunByOccurrenceSurfacesScanErrors(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, "default")
	// A row whose attempt column is not a number must surface as an error, not
	// as a silently wrong stored state.
	mock.ExpectQuery("SELECT id, revision_id, phase, attempt, max_attempts, scheduled_for FROM job_run_control_plane").
		WillReturnRows(sqlmock.NewRows(storedRunRowColumns).AddRow(77, "not-a-revision", "Pending", 0, 3, nil))
	mock.ExpectRollback()

	if _, _, err := repo.RunByOccurrence(context.Background(), "default", "nightly", "occ-1"); err == nil {
		t.Fatal("expected a scan error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

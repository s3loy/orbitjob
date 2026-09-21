package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/workflow"
	"orbitjob/internal/domain/resource"
)

// The workflow repository's writes are the same guarded moves the run ledger
// makes, over two tables joined by workflow_run_id. The sqlmock tests pin each
// guard at the SQL boundary: the tenant GUC every transaction binds, the dedup
// identities that converge instead of doubling, the terminal guard on the
// phase write, the per-task merge of the decision trail, and the
// steps-then-row order the prune's RESTRICT contract demands.

func newWorkflowRepoMock(t *testing.T) (*WorkflowRunRepository, sqlmock.Sqlmock) {
	t.Helper()
	// Statement parameters include []string phase sets and time.Time stamps,
	// so the mock needs the same pq-faithful converter the run-ledger tests use.
	db, mock, err := sqlmock.New(sqlmock.ValueConverterOption(pqFaithfulConverter{}))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewWorkflowRunRepository(db), mock
}

// expectWorkflowTx opens the tenant-scoped transaction every operation runs
// inside: BEGIN, then the transaction-local set_config binding RLS. The
// setting name and the tenant id are asserted as the exact two bind
// arguments, so a repository that skipped the GUC -- or bound the wrong
// tenant -- fails the test instead of leaking another tenant's rows.
func expectWorkflowTx(mock sqlmock.Sqlmock, tenantID string) {
	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs(tenantSetting, tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

var (
	workflowRunScanColumns   = strings.Fields("id, source_uid, revision_id, occurrence_key, trigger, actor, phase, task_decisions, created_at, updated_at")
	workflowStepScanColumns  = strings.Fields("id, workflow_run_id, source_uid, revision_id, occurrence_key, trigger, phase, attempt, created_at, updated_at")
	workflowPhaseScanColumns = []string{"phase", "task_decisions"}

	workflowRowTime = time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
)

// workflowTerminalSet is the exact parameter openWorkflowRunsSQL and
// updateWorkflowPhaseSQL receive for "not yet finished": the SQL form of
// workflow.Terminal, CancelUnknown deliberately absent.
func workflowTerminalSet() []string {
	return []string{"Succeeded", "Failed", "Canceled"}
}

// workflowPruneSet is what the retention statements receive: the terminal
// phases plus CancelUnknown, matching the job-run ledger's retention set.
func workflowPruneSet() []string {
	return []string{"Succeeded", "Failed", "Canceled", "CancelUnknown"}
}

func workflowRunRow(id int64, phase string, decisions string) *sqlmock.Rows {
	return sqlmock.NewRows(workflowRunScanColumns).
		AddRow(id, "wf-nightly", int64(5), "occ-1", "Schedule", "walker", phase, decisions, workflowRowTime, workflowRowTime)
}

func workflowStepRow(id int64) *sqlmock.Rows {
	return sqlmock.NewRows(workflowStepScanColumns).
		AddRow(id, int64(11), "nightly", int64(9), "stepkey-1", "Workflow", "Pending", 0, workflowRowTime, workflowRowTime)
}

func workflowRun() workflow.Run {
	return workflow.Run{
		SourceUID: "wf-nightly", RevisionID: 5, OccurrenceKey: "occ-1",
		Trigger: jobrun.Schedule, Actor: "walker",
	}
}

func TestWorkflowOperationsBindTheTenantGuc(t *testing.T) {
	// Every operation opens its transaction with set_config naming the tenant
	// exactly; a second call under another tenant must carry that other id.
	repo, mock := newWorkflowRepoMock(t)

	expectWorkflowTx(mock, "tenant-a")
	mock.ExpectQuery("SELECT .+ FROM workflow_run_control_plane WHERE id=\\$1 AND tenant_id=\\$2").
		WithArgs(int64(11), "tenant-a").
		WillReturnRows(workflowRunRow(11, "Running", "{}"))
	mock.ExpectCommit()

	expectWorkflowTx(mock, "tenant-b")
	mock.ExpectQuery("SELECT .+ FROM workflow_run_control_plane WHERE id=\\$1 AND tenant_id=\\$2").
		WithArgs(int64(11), "tenant-b").
		WillReturnRows(sqlmock.NewRows(workflowRunScanColumns))
	mock.ExpectCommit()

	if _, found, err := repo.RunByID(context.Background(), "tenant-a", 11); err != nil || !found {
		t.Fatalf("tenant-a lookup: found=%v err=%v", found, err)
	}
	if _, found, err := repo.RunByID(context.Background(), "tenant-b", 11); err != nil || found {
		t.Fatalf("tenant-b lookup must not find the row: found=%v err=%v", found, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateWorkflowRunForTenantCreatesAndAudits(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	expectWorkflowTx(mock, "default")
	mock.ExpectQuery("INSERT INTO workflow_run_control_plane").
		WithArgs("default", "wf-nightly", int64(5), "occ-1", "Schedule", "walker", "Pending").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
	mock.ExpectQuery("SELECT .+ FROM workflow_run_control_plane WHERE source_uid=\\$1 AND occurrence_key=\\$2 AND tenant_id=\\$3").
		WithArgs("wf-nightly", "occ-1", "default").
		WillReturnRows(workflowRunRow(11, "Pending", "{}"))
	expectControlPlaneAudit(mock, "walker")
	mock.ExpectCommit()

	stored, created, err := repo.CreateRunForTenant(context.Background(), "default", workflowRun())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Fatal("created = false, want true for a fresh occurrence")
	}
	if stored.ID != 11 || stored.Phase != workflow.PhasePending || stored.Trigger != jobrun.Schedule {
		t.Fatalf("unexpected stored run: %+v", stored)
	}
	if stored.TaskDecisions == nil || len(stored.TaskDecisions) != 0 {
		t.Fatalf("task decisions = %#v, want an empty trail the walker need not guard", stored.TaskDecisions)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateWorkflowRunForTenantDedupConverges(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	// The occurrence was recorded by another writer; the loser reads the
	// winner's row back and reports created=false, and must not write an
	// audit row for a run it did not create.
	expectWorkflowTx(mock, "default")
	mock.ExpectQuery("INSERT INTO workflow_run_control_plane").
		WithArgs("default", "wf-nightly", int64(5), "occ-1", "Schedule", "walker", "Pending").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT .+ FROM workflow_run_control_plane WHERE source_uid=\\$1 AND occurrence_key=\\$2 AND tenant_id=\\$3").
		WithArgs("wf-nightly", "occ-1", "default").
		WillReturnRows(workflowRunRow(11, "Running", "{}"))
	mock.ExpectCommit()

	stored, created, err := repo.CreateRunForTenant(context.Background(), "default", workflowRun())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Fatal("created = true for an occurrence another writer already recorded")
	}
	if stored.ID != 11 || stored.Phase != workflow.PhaseRunning {
		t.Fatalf("the winner's row must be returned unchanged: %+v", stored)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateWorkflowRunForTenantUniqueViolationIsTyped(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	// A UNIQUE violation the ON CONFLICT clause does not cover -- a primary
	// key after a sequence-desyncing restore, say -- surfaces as the typed
	// conflict naming the constraint, never as a raw driver error.
	expectWorkflowTx(mock, "default")
	mock.ExpectQuery("INSERT INTO workflow_run_control_plane").
		WillReturnError(&pq.Error{
			Code:       "23505",
			Constraint: "workflow_run_control_plane_source_uid_occurrence_key_key",
		})
	mock.ExpectRollback()

	_, _, err := repo.CreateRunForTenant(context.Background(), "default", workflowRun())
	var conflict *resource.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want a typed ConflictError", err)
	}
	if !strings.Contains(conflict.Error(), "workflow_run_control_plane_source_uid_occurrence_key_key") {
		t.Fatalf("conflict %q must name the violated constraint", conflict.Error())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateWorkflowRunForTenantRefusesBadInput(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	tests := []struct {
		name string
		run  workflow.Run
	}{
		{name: "missing source uid", run: workflow.Run{OccurrenceKey: "occ-1", Actor: "walker"}},
		{name: "missing occurrence key", run: workflow.Run{SourceUID: "wf-nightly", Actor: "walker"}},
		{name: "a workflow run states why it exists", run: workflow.Run{SourceUID: "wf-nightly", OccurrenceKey: "occ-1", Actor: "walker"}},
		{name: "a caller-supplied finished phase is refused", run: workflow.Run{
			SourceUID: "wf-nightly", OccurrenceKey: "occ-1", Trigger: jobrun.Schedule,
			Phase: workflow.PhaseSucceeded, Actor: "walker",
		}},
		{name: "a blank actor is refused", run: workflow.Run{SourceUID: "wf-nightly", OccurrenceKey: "occ-1", Trigger: jobrun.Schedule}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := repo.CreateRunForTenant(context.Background(), "default", tc.run)
			if err == nil {
				t.Fatal("expected a refusal")
			}
		})
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("refused input reached the database: %v", err)
	}
}

func TestWorkflowRunLookups(t *testing.T) {
	t.Run("by occurrence found and the decision trail decodes", func(t *testing.T) {
		repo, mock := newWorkflowRepoMock(t)
		expectWorkflowTx(mock, "default")
		mock.ExpectQuery("SELECT .+ FROM workflow_run_control_plane WHERE source_uid=\\$1 AND occurrence_key=\\$2 AND tenant_id=\\$3").
			WithArgs("wf-nightly", "occ-1", "default").
			WillReturnRows(workflowRunRow(11, "Running", `{"build":{"skipped":true,"reason":"fail_fast"}}`))
		mock.ExpectCommit()

		run, found, err := repo.RunByOccurrence(context.Background(), "default", "wf-nightly", "occ-1")
		if err != nil || !found {
			t.Fatalf("found=%v err=%v", found, err)
		}
		decision, ok := run.TaskDecisions["build"]
		if !ok || !decision.Skipped || decision.Reason != workflow.ReasonFailFast {
			t.Fatalf("decision trail = %#v, want build skipped for %s", run.TaskDecisions, workflow.ReasonFailFast)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectations: %v", err)
		}
	})

	t.Run("by occurrence not found is calm", func(t *testing.T) {
		repo, mock := newWorkflowRepoMock(t)
		expectWorkflowTx(mock, "default")
		mock.ExpectQuery("SELECT .+ FROM workflow_run_control_plane").
			WillReturnRows(sqlmock.NewRows(workflowRunScanColumns))
		mock.ExpectCommit()

		if _, found, err := repo.RunByOccurrence(context.Background(), "default", "wf-nightly", "occ-1"); err != nil || found {
			t.Fatalf("found=%v err=%v, want a calm not-found", found, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectations: %v", err)
		}
	})

	t.Run("by id database errors roll the transaction back", func(t *testing.T) {
		repo, mock := newWorkflowRepoMock(t)
		expectWorkflowTx(mock, "default")
		mock.ExpectQuery("SELECT .+ FROM workflow_run_control_plane WHERE id=\\$1 AND tenant_id=\\$2").
			WithArgs(int64(11), "default").
			WillReturnError(errors.New("db down"))
		mock.ExpectRollback()

		if _, _, err := repo.RunByID(context.Background(), "default", 11); err == nil {
			t.Fatal("expected error")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectations: %v", err)
		}
	})
}

func TestOpenWorkflowRunsCarriesTheTerminalPhaseSet(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	// The second bind is the terminal set: exactly workflow.Terminal, so a
	// CancelUnknown run stays open -- a stop nobody could confirm is not an
	// outcome, and the walker must keep observing it.
	expectWorkflowTx(mock, "default")
	mock.ExpectQuery("FROM workflow_run_control_plane WHERE tenant_id").
		WithArgs("default", workflowTerminalSet()).
		WillReturnRows(workflowRunRow(11, "Running", "{}").AddRow(12, "wf-nightly", int64(5), "occ-2", "Manual", "walker", "CancelUnknown", "{}", workflowRowTime, workflowRowTime))
	mock.ExpectCommit()

	runs, err := repo.OpenRuns(context.Background(), "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("got %d open runs, want 2", len(runs))
	}
	if runs[0].Phase != workflow.PhaseRunning || runs[1].Phase != workflow.PhaseCancelUnknown {
		t.Fatalf("open phases = %s, %s; CancelUnknown must stay open", runs[0].Phase, runs[1].Phase)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestWorkflowStepsListedByRunOrderedByID(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	expectWorkflowTx(mock, "default")
	mock.ExpectQuery("FROM job_run_control_plane WHERE workflow_run_id=\\$1").
		WithArgs(int64(11), "default").
		WillReturnRows(workflowStepRow(21).AddRow(22, int64(11), "nightly", int64(9), "stepkey-2", "Workflow", "Running", 1, workflowRowTime, workflowRowTime))
	mock.ExpectCommit()

	steps, err := repo.Steps(context.Background(), "default", 11)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(steps) != 2 || steps[0].ID != 21 || steps[1].ID != 22 {
		t.Fatalf("steps = %+v, want ids 21 then 22 so walker decisions are reproducible", steps)
	}
	for _, step := range steps {
		if step.WorkflowRunID != 11 || step.Trigger != jobrun.Workflow {
			t.Fatalf("step %+v is not grouped under run 11 as a Workflow run", step)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateWorkflowStepForTenantStampsTheGroupingPointer(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)
	step := workflow.StepRun{SourceUID: "nightly", RevisionID: 9, OccurrenceKey: "stepkey-1", Trigger: jobrun.Workflow}

	expectWorkflowTx(mock, "default")
	// The grouping pointer must name one of this tenant's workflow runs: the
	// FK alone would accept a foreign tenant's row, so the tenant-scoped read
	// runs before the insert.
	mock.ExpectQuery("SELECT id FROM workflow_run_control_plane").
		WithArgs(int64(11), "default").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
	// The tenth bind is the pointer itself: this is what makes the run a step,
	// and plain occurrences pass NULL in its place.
	mock.ExpectQuery("INSERT INTO job_run_control_plane").
		WithArgs("default", "nightly", int64(9), "stepkey-1", "Workflow", "walker", "Pending", 3, nil, int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(21))
	mock.ExpectQuery("SELECT .+ FROM job_run_control_plane WHERE source_uid=\\$1 AND occurrence_key=\\$2 AND tenant_id=\\$3").
		WithArgs("nightly", "stepkey-1", "default").
		WillReturnRows(workflowStepRow(21))
	expectControlPlaneAudit(mock, "walker")
	mock.ExpectCommit()

	stored, created, err := repo.CreateStepForTenant(context.Background(), "default", 11, step, 3, "walker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Fatal("created = false, want true for a fresh step")
	}
	if stored.ID != 21 || stored.WorkflowRunID != 11 || stored.Trigger != jobrun.Workflow || stored.Phase != jobrun.Pending {
		t.Fatalf("unexpected stored step: %+v", stored)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateWorkflowStepForTenantFloorsMaxAttemptsAtOne(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)
	step := workflow.StepRun{SourceUID: "nightly", RevisionID: 9, OccurrenceKey: "stepkey-1", Trigger: jobrun.Workflow}

	expectWorkflowTx(mock, "default")
	mock.ExpectQuery("SELECT id FROM workflow_run_control_plane").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
	// The step row's max_attempts column requires at least one, so a caller
	// passing zero still gets the floor of 1.
	mock.ExpectQuery("INSERT INTO job_run_control_plane").
		WithArgs("default", "nightly", int64(9), "stepkey-1", "Workflow", "walker", "Pending", 1, nil, int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(21))
	mock.ExpectQuery("SELECT .+ FROM job_run_control_plane").
		WillReturnRows(workflowStepRow(21))
	expectControlPlaneAudit(mock, "walker")
	mock.ExpectCommit()

	if _, _, err := repo.CreateStepForTenant(context.Background(), "default", 11, step, 0, "walker"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateWorkflowStepForTenantDedupConverges(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)
	step := workflow.StepRun{SourceUID: "nightly", RevisionID: 9, OccurrenceKey: "stepkey-1", Trigger: jobrun.Workflow}

	// A replayed tick finds the step already recorded: the winner's row comes
	// back with created=false and no audit row for a step this writer did not
	// create.
	expectWorkflowTx(mock, "default")
	mock.ExpectQuery("SELECT id FROM workflow_run_control_plane").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
	mock.ExpectQuery("INSERT INTO job_run_control_plane").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT .+ FROM job_run_control_plane").
		WillReturnRows(workflowStepRow(21))
	mock.ExpectCommit()

	stored, created, err := repo.CreateStepForTenant(context.Background(), "default", 11, step, 3, "walker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Fatal("created = true for a step another writer already recorded")
	}
	if stored.ID != 21 || stored.WorkflowRunID != 11 {
		t.Fatalf("the winner's row must be returned unchanged: %+v", stored)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateWorkflowStepForTenantRefusesAWorkflowRunOutsideTheTenant(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	expectWorkflowTx(mock, "default")
	mock.ExpectQuery("SELECT id FROM workflow_run_control_plane").
		WithArgs(int64(99), "default").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	_, _, err := repo.CreateStepForTenant(context.Background(), "default", 99, workflow.StepRun{
		SourceUID: "nightly", RevisionID: 9, OccurrenceKey: "stepkey-1", Trigger: jobrun.Workflow,
	}, 3, "walker")
	var notFound *resource.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("err = %v, want a typed NotFoundError", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateWorkflowStepForTenantUniqueViolationIsTyped(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	expectWorkflowTx(mock, "default")
	mock.ExpectQuery("SELECT id FROM workflow_run_control_plane").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
	mock.ExpectQuery("INSERT INTO job_run_control_plane").
		WillReturnError(&pq.Error{Code: "23505", Constraint: "job_run_control_plane_source_uid_occurrence_key_key"})
	mock.ExpectRollback()

	_, _, err := repo.CreateStepForTenant(context.Background(), "default", 11, workflow.StepRun{
		SourceUID: "nightly", RevisionID: 9, OccurrenceKey: "stepkey-1", Trigger: jobrun.Workflow,
	}, 3, "walker")
	var conflict *resource.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want a typed ConflictError", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateWorkflowStepForTenantRefusesBadInput(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	good := workflow.StepRun{SourceUID: "nightly", RevisionID: 9, OccurrenceKey: "stepkey-1", Trigger: jobrun.Workflow}
	tests := []struct {
		name          string
		workflowRunID int64
		step          workflow.StepRun
		actor         string
	}{
		{name: "missing workflow run id", workflowRunID: 0, step: good, actor: "walker"},
		{name: "missing step identity", workflowRunID: 11, step: workflow.StepRun{Trigger: jobrun.Workflow}, actor: "walker"},
		{name: "a step travels as trigger Workflow", workflowRunID: 11,
			step:  workflow.StepRun{SourceUID: "nightly", RevisionID: 9, OccurrenceKey: "stepkey-1", Trigger: jobrun.Manual},
			actor: "walker"},
		{name: "a caller-supplied finished phase is refused", workflowRunID: 11,
			step:  workflow.StepRun{SourceUID: "nightly", RevisionID: 9, OccurrenceKey: "stepkey-1", Trigger: jobrun.Workflow, Phase: jobrun.Succeeded},
			actor: "walker"},
		{name: "a blank actor is refused", workflowRunID: 11,
			step: workflow.StepRun{SourceUID: "nightly", RevisionID: 9, OccurrenceKey: "stepkey-1", Trigger: jobrun.Workflow}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := repo.CreateStepForTenant(context.Background(), "default", tc.workflowRunID, tc.step, 3, tc.actor)
			if err == nil {
				t.Fatal("expected a refusal")
			}
		})
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("refused input reached the database: %v", err)
	}
}

func TestUpdateWorkflowPhaseAdvancesAndAudits(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	expectWorkflowTx(mock, "default")
	// The write carries the locked row's guard rails: the terminal set that
	// refuses finished runs, and the empty merge for a caller with no
	// decisions to record.
	mock.ExpectQuery("WITH prior").
		WithArgs(int64(11), "default", "Succeeded", "{}", sqlmock.AnyArg(), workflowTerminalSet()).
		WillReturnRows(sqlmock.NewRows([]string{"phase", "prior_phase", "prior_decisions", "run_actor"}).
			AddRow("Succeeded", "Running", "{}", "walker"))
	expectControlPlaneAudit(mock, "walker")
	mock.ExpectCommit()

	changed, err := repo.UpdatePhase(context.Background(), "default", 11, workflow.PhaseSucceeded, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("changed = false for a real phase advance")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateWorkflowPhaseMergesDecisionsPerTask(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	// The decision trail binds as jsonb with the task name as its key, so the
	// statement concatenates it onto the stored trail instead of replacing it.
	expectWorkflowTx(mock, "default")
	mock.ExpectQuery("WITH prior").
		WithArgs(int64(11), "default", "Running", `{"build":{"skipped":true,"reason":"fail_fast"}}`, sqlmock.AnyArg(), workflowTerminalSet()).
		WillReturnRows(sqlmock.NewRows([]string{"phase", "prior_phase", "prior_decisions", "run_actor"}).
			AddRow("Running", "Running", "{}", "walker"))
	// A decisions-only write changes the row but moves no phase: no audit row.
	mock.ExpectCommit()

	changed, err := repo.UpdatePhase(context.Background(), "default", 11, workflow.PhaseRunning, map[string]workflow.TaskDecision{
		"build": {Skipped: true, Reason: workflow.ReasonFailFast},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("a merged decision changed the stored run and must be reported")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateWorkflowPhaseNoOpIsCalm(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	// The statement refuses to write when the phase holds and the merge adds
	// nothing; the read-back confirms the run already holds exactly the state
	// the caller asked for and reports (false, nil), the same calm answer the
	// job run's phase write gives a resync.
	expectWorkflowTx(mock, "default")
	mock.ExpectQuery("WITH prior").
		WithArgs(int64(11), "default", "Running", "{}", sqlmock.AnyArg(), workflowTerminalSet()).
		WillReturnRows(sqlmock.NewRows([]string{"phase", "prior_phase", "prior_decisions", "run_actor"}))
	mock.ExpectQuery("SELECT phase, task_decisions::text FROM workflow_run_control_plane").
		WithArgs(int64(11), "default").
		WillReturnRows(sqlmock.NewRows(workflowPhaseScanColumns).AddRow("Running", "{}"))
	mock.ExpectCommit()

	changed, err := repo.UpdatePhase(context.Background(), "default", 11, workflow.PhaseRunning, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Fatal("re-reporting the stored state is not a change")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateWorkflowPhaseRefusesAfterTerminal(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	// The stored run is Succeeded; the caller tries to drag it back to
	// Running with a fresh decision in hand. The write was refused by the
	// terminal guard, and the refusal is typed: terminal workflow bookkeeping
	// fires once, and a late observer must not re-fire it.
	expectWorkflowTx(mock, "default")
	mock.ExpectQuery("WITH prior").
		WithArgs(int64(11), "default", "Running", `{"deploy":{"skipped":true,"reason":"canceled"}}`, sqlmock.AnyArg(), workflowTerminalSet()).
		WillReturnRows(sqlmock.NewRows([]string{"phase", "prior_phase", "prior_decisions", "run_actor"}))
	mock.ExpectQuery("SELECT phase, task_decisions::text FROM workflow_run_control_plane").
		WithArgs(int64(11), "default").
		WillReturnRows(sqlmock.NewRows(workflowPhaseScanColumns).AddRow("Succeeded", "{}"))
	mock.ExpectRollback()

	_, err := repo.UpdatePhase(context.Background(), "default", 11, workflow.PhaseRunning, map[string]workflow.TaskDecision{
		"deploy": {Skipped: true, Reason: workflow.ReasonCanceled},
	})
	var conflict *resource.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want a typed ConflictError", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateWorkflowPhaseReReportingTerminalStateIsCalm(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	// A writer that re-reports the exact terminal state -- same phase, no new
	// decisions -- is not a refusal: it observed what is already stored, and
	// the terminal bookkeeping must not treat it as one.
	expectWorkflowTx(mock, "default")
	mock.ExpectQuery("WITH prior").
		WithArgs(int64(11), "default", "Succeeded", "{}", sqlmock.AnyArg(), workflowTerminalSet()).
		WillReturnRows(sqlmock.NewRows([]string{"phase", "prior_phase", "prior_decisions", "run_actor"}))
	mock.ExpectQuery("SELECT phase, task_decisions::text FROM workflow_run_control_plane").
		WithArgs(int64(11), "default").
		WillReturnRows(sqlmock.NewRows(workflowPhaseScanColumns).AddRow("Succeeded", "{}"))
	mock.ExpectCommit()

	changed, err := repo.UpdatePhase(context.Background(), "default", 11, workflow.PhaseSucceeded, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Fatal("re-reporting the stored terminal state is not a change")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateWorkflowPhaseRefusesAnUnknownRun(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	expectWorkflowTx(mock, "default")
	mock.ExpectQuery("WITH prior").
		WillReturnRows(sqlmock.NewRows([]string{"phase", "prior_phase", "prior_decisions", "run_actor"}))
	mock.ExpectQuery("SELECT phase, task_decisions::text FROM workflow_run_control_plane").
		WithArgs(int64(99), "default").
		WillReturnRows(sqlmock.NewRows(workflowPhaseScanColumns))
	mock.ExpectRollback()

	_, err := repo.UpdatePhase(context.Background(), "default", 99, workflow.PhaseRunning, nil)
	var notFound *resource.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("err = %v, want a typed NotFoundError", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateWorkflowPhaseRefusesAnEmptyPhase(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)
	if _, err := repo.UpdatePhase(context.Background(), "default", 11, "", nil); err == nil {
		t.Fatal("expected a refusal")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("refused input reached the database: %v", err)
	}
}

func TestPrunableWorkflowRunsRankedPerOutcome(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	// The binds pin the retention set (terminal plus CancelUnknown) and the
	// per-outcome keep limits, the same ranked shape the job-run ledger uses.
	expectWorkflowTx(mock, "default")
	mock.ExpectQuery("FROM workflow_run_control_plane").
		WithArgs("default", "wf-nightly", workflowPruneSet(), "Succeeded", 3, 5).
		WillReturnRows(sqlmock.NewRows([]string{"id", "occurrence_key", "phase"}).
			AddRow(81, "occ-3", "Failed").
			AddRow(82, "occ-4", "CancelUnknown"))
	mock.ExpectCommit()

	runs, err := repo.PrunableRuns(context.Background(), "default", "wf-nightly", 3, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 2 || runs[0].Phase != workflow.PhaseFailed || runs[1].Phase != workflow.PhaseCancelUnknown {
		t.Fatalf("unexpected prunable runs: %+v", runs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPrunableWorkflowRunsFloorsNegativeKeepsAtZero(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	expectWorkflowTx(mock, "default")
	mock.ExpectQuery("FROM workflow_run_control_plane").
		WithArgs("default", "wf-nightly", workflowPruneSet(), "Succeeded", 0, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "occurrence_key", "phase"}))
	mock.ExpectCommit()

	if _, err := repo.PrunableRuns(context.Background(), "default", "wf-nightly", -1, -2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPruneRunDeletesStepsThenTheRow(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	expectWorkflowTx(mock, "default")
	// The row is locked and its phase re-checked in the same transaction, so
	// eligibility is not trusted from the caller's listing.
	mock.ExpectQuery("SELECT occurrence_key, phase, actor FROM workflow_run_control_plane").
		WithArgs(int64(11), "default").
		WillReturnRows(sqlmock.NewRows([]string{"occurrence_key", "phase", "actor"}).AddRow("occ-1", "Succeeded", "walker"))
	// Steps delete first; each removal is attributed before the grouping row
	// that explains it goes away.
	mock.ExpectQuery("DELETE FROM job_run_control_plane").
		WithArgs(int64(11), "default").
		WillReturnRows(sqlmock.NewRows([]string{"id", "occurrence_key", "phase", "actor"}).
			AddRow(21, "stepkey-1", "Succeeded", "walker").
			AddRow(22, "stepkey-2", "Canceled", "walker"))
	expectControlPlaneAudit(mock, "walker")
	expectControlPlaneAudit(mock, "walker")
	// Then the workflow row, still phase-guarded in the statement itself.
	mock.ExpectQuery("DELETE FROM workflow_run_control_plane").
		WithArgs(int64(11), "default", workflowPruneSet()).
		WillReturnRows(sqlmock.NewRows([]string{"occurrence_key", "phase", "actor"}).AddRow("occ-1", "Succeeded", "walker"))
	expectControlPlaneAudit(mock, "walker")
	mock.ExpectCommit()

	deleted, stepsDeleted, err := repo.PruneRun(context.Background(), "default", 11)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleted {
		t.Fatal("deleted = false for an eligible pruned run")
	}
	if stepsDeleted != 2 {
		t.Fatalf("stepsDeleted = %d, want 2", stepsDeleted)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPruneRunLeavesUnknownRowsAlone(t *testing.T) {
	t.Run("a run that does not exist is a calm not-deleted", func(t *testing.T) {
		repo, mock := newWorkflowRepoMock(t)
		expectWorkflowTx(mock, "default")
		mock.ExpectQuery("SELECT occurrence_key, phase, actor FROM workflow_run_control_plane").
			WithArgs(int64(99), "default").
			WillReturnRows(sqlmock.NewRows([]string{"occurrence_key", "phase", "actor"}))
		mock.ExpectCommit()

		deleted, stepsDeleted, err := repo.PruneRun(context.Background(), "default", 99)
		if err != nil || deleted || stepsDeleted != 0 {
			t.Fatalf("deleted=%v steps=%d err=%v, want a calm not-deleted", deleted, stepsDeleted, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectations: %v", err)
		}
	})

	t.Run("a run outside the retention set is not pruned", func(t *testing.T) {
		repo, mock := newWorkflowRepoMock(t)
		expectWorkflowTx(mock, "default")
		mock.ExpectQuery("SELECT occurrence_key, phase, actor FROM workflow_run_control_plane").
			WillReturnRows(sqlmock.NewRows([]string{"occurrence_key", "phase", "actor"}).AddRow("occ-1", "Running", "walker"))
		mock.ExpectCommit()

		deleted, stepsDeleted, err := repo.PruneRun(context.Background(), "default", 11)
		if err != nil || deleted || stepsDeleted != 0 {
			t.Fatalf("deleted=%v steps=%d err=%v, want an untouched in-flight run", deleted, stepsDeleted, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectations: %v", err)
		}
	})
}

func TestPruneRunRefusesLoudlyWhenTheRowVanishesMidPrune(t *testing.T) {
	repo, mock := newWorkflowRepoMock(t)

	// The eligibility read locked the row and read Succeeded; if the row
	// delete still matches nothing, something moved underneath the retention
	// set. The error rolls the whole prune -- steps included -- back, which is
	// the atomicity the steps-then-row order exists for.
	expectWorkflowTx(mock, "default")
	mock.ExpectQuery("SELECT occurrence_key, phase, actor FROM workflow_run_control_plane").
		WillReturnRows(sqlmock.NewRows([]string{"occurrence_key", "phase", "actor"}).AddRow("occ-1", "Succeeded", "walker"))
	mock.ExpectQuery("DELETE FROM job_run_control_plane").
		WillReturnRows(sqlmock.NewRows([]string{"id", "occurrence_key", "phase", "actor"}).
			AddRow(21, "stepkey-1", "Succeeded", "walker"))
	expectControlPlaneAudit(mock, "walker")
	mock.ExpectQuery("DELETE FROM workflow_run_control_plane").
		WillReturnRows(sqlmock.NewRows([]string{"occurrence_key", "phase", "actor"}))
	mock.ExpectRollback()

	deleted, stepsDeleted, err := repo.PruneRun(context.Background(), "default", 11)
	if err == nil {
		t.Fatal("expected the vanished row to fail loudly")
	}
	if deleted || stepsDeleted != 0 {
		t.Fatalf("deleted=%v steps=%d, want nothing reported for a rolled-back prune", deleted, stepsDeleted)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestWorkflowTaskDecisionHelpers(t *testing.T) {
	t.Run("encode", func(t *testing.T) {
		// A nil or empty trail must travel as '{}' -- 'null'::jsonb does not
		// concatenate -- and a populated one marshals per task name.
		empty, err := encodeTaskDecisions(nil)
		if err != nil || empty != "{}" {
			t.Fatalf("empty = %q, %v; want {}", empty, err)
		}
		encoded, err := encodeTaskDecisions(map[string]workflow.TaskDecision{
			"build": {Skipped: true, Reason: workflow.ReasonDeadline},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if encoded != `{"build":{"skipped":true,"reason":"deadline"}}` {
			t.Fatalf("encoded = %s, want the per-task jsonb shape", encoded)
		}
	})

	t.Run("decode", func(t *testing.T) {
		// The column is NOT NULL with an object CHECK, so '{}' decodes to an
		// empty non-nil map the walker need not guard.
		decoded, err := decodeTaskDecisions([]byte("{}"))
		if err != nil || decoded == nil || len(decoded) != 0 {
			t.Fatalf("decoded = %#v, %v; want an empty non-nil map", decoded, err)
		}
		trail, err := decodeTaskDecisions([]byte(`{"deploy":{"skipped":true,"reason":"condition"}}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !trail["deploy"].Skipped || trail["deploy"].Reason != workflow.ReasonCondition {
			t.Fatalf("trail = %#v, want deploy skipped for condition", trail)
		}
		if _, err := decodeTaskDecisions([]byte("not-json")); err == nil {
			t.Fatal("expected a decode refusal")
		}
	})

	t.Run("would change", func(t *testing.T) {
		stored := map[string]workflow.TaskDecision{"build": {Skipped: true, Reason: workflow.ReasonFailFast}}
		if decisionsWouldChange(stored, nil) {
			t.Fatal("no incoming decisions is never a change")
		}
		if decisionsWouldChange(stored, map[string]workflow.TaskDecision{"build": {Skipped: true, Reason: workflow.ReasonFailFast}}) {
			t.Fatal("a decision the trail already holds is not a change")
		}
		if !decisionsWouldChange(stored, map[string]workflow.TaskDecision{"deploy": {Skipped: true}}) {
			t.Fatal("a new task name is a change")
		}
		if !decisionsWouldChange(stored, map[string]workflow.TaskDecision{"build": {Skipped: true, Reason: workflow.ReasonCanceled}}) {
			t.Fatal("a changed verdict for a known task is a change")
		}
	})
}

func TestUniqueViolationClassifier(t *testing.T) {
	if _, violated := uniqueViolation(errors.New("not a pq error")); violated {
		t.Fatal("a plain error is not a unique violation")
	}
	if _, violated := uniqueViolation(&pq.Error{Code: "23503"}); violated {
		t.Fatal("a foreign-key violation is not a unique violation")
	}
	pqErr, violated := uniqueViolation(&pq.Error{Code: "23505", Constraint: "some_key"})
	if !violated || pqErr.Constraint != "some_key" {
		t.Fatalf("violated=%v constraint=%q, want the 23505 classified with its constraint", violated, pqErr.Constraint)
	}
}

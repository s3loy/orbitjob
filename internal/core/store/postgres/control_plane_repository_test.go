package postgres

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/domain/resource"

	"orbitjob/internal/platform/scan"
)

// The control-plane repository owns the run ledger's writes, and every one of
// them is a guarded move: revisions refuse a conflicting reprojection, runs
// refuse to leave a terminal phase, attempts refuse a second Job for one
// attempt number. The sqlmock tests pin each guard at the SQL boundary: which
// statements run, which arguments they carry, and which domain error surfaces
// when the database says no.

func newControlPlaneRepoMock(t *testing.T) (*ControlPlaneRepository, sqlmock.Sqlmock) {
	t.Helper()
	// Several ledger statements pass []string phase sets as parameters. A real
	// PostgreSQL connection converts those through its NamedValueChecker, so the
	// mock carries a converter that mirrors pq instead of the default one,
	// which only knows []byte slices.
	db, mock, err := sqlmock.New(sqlmock.ValueConverterOption(pqFaithfulConverter{}))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewControlPlaneRepository(db), mock
}

// expectControlPlaneTx opens the tenant-scoped transaction every operation runs
// inside: BEGIN, then the transaction-local set_config that binds RLS. The
// control plane sets the GUC through WithTenant, which passes the setting name
// and the value as two bind arguments.
func expectControlPlaneTx(mock sqlmock.Sqlmock, tenantID string) {
	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs(tenantSetting, tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

// expectControlPlaneAudit is the audit insert every ledger change carries:
// tenant, actor, event type, resource type, resource id, diff.
func expectControlPlaneAudit(mock sqlmock.Sqlmock, actor string) {
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs(sqlmock.AnyArg(), actor, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
}

var revisionScanColumns = []string{"id", "source_mode", "source_uid", "source_namespace", "source_name",
	"generation", "spec_hash", "normalized_spec", "actor", "created_at"}

func controlPlaneRevision() revision.Revision {
	return revision.Revision{
		Identity: revision.Identity{
			SourceMode: "kubernetes", SourceUID: "nightly", Namespace: "jobs", Name: "nightly",
		},
		Generation:     3,
		SpecHash:       "hash-3",
		NormalizedSpec: `{"schedule":"*/5 * * * *"}`,
		Actor:          "operator",
	}
}

func TestValidateActor(t *testing.T) {
	tests := []struct {
		name    string
		actor   string
		wantErr bool
	}{
		{name: "a plain actor is accepted", actor: coreFixtureControlPlaneRepositoryActorKeyID},
		{name: "an actor of exactly 64 runes is the accepted bound", actor: strings.Repeat("a", 64)},
		{name: "an empty actor is refused", actor: "", wantErr: true},
		{name: "a whitespace actor is refused", actor: "   ", wantErr: true},
		{name: "an actor wider than the audit column is refused", actor: strings.Repeat("a", 65), wantErr: true},
		{name: "the bound counts runes, not bytes", actor: strings.Repeat("é", 64)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateActor(tc.actor)
			if tc.wantErr && err == nil {
				t.Fatalf("actor of %d runes accepted, want refusal", utf8.RuneCountInString(tc.actor))
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
		})
	}
}

func TestWithTenantTransactionGuards(t *testing.T) {
	// A repository with no database handle is refused before anything is sent.
	nilRepo := &ControlPlaneRepository{}
	err := nilRepo.WithTenantTransaction(context.Background(), coreFixtureControlPlaneRepositoryTenantID, func(context.Context, *sql.Tx) error { return nil })
	if err == nil {
		t.Fatal("nil database accepted")
	}

	repo, mock := newControlPlaneRepoMock(t)
	mock.ExpectBegin()
	mock.ExpectRollback()
	// An empty tenant id is refused after BEGIN and the transaction is rolled
	// back, so the pooled connection returns without a half-set tenant GUC.
	err = repo.WithTenantTransaction(context.Background(), "", func(context.Context, *sql.Tx) error { return nil })
	if err == nil {
		t.Fatal("empty tenant accepted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestApplyRevisionForTenantCreatesAndActivates(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	rev := controlPlaneRevision()

	expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	expectNoSourceControl(mock)
	mock.ExpectQuery("INSERT INTO job_definition_revisions").
		WithArgs(coreFixtureControlPlaneRepositoryTenantID, "kubernetes", "nightly", "jobs", "nightly", int64(3), "hash-3", rev.NormalizedSpec, "operator", nil).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
	mock.ExpectExec("UPDATE job_definition_revisions SET is_active=false").
		WithArgs("kubernetes", "nightly", coreFixtureControlPlaneRepositoryTenantID, int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE job_definition_revisions r SET is_active=true").
		WithArgs(int64(11), coreFixtureControlPlaneRepositoryTenantID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectControlPlaneAudit(mock, "operator")
	mock.ExpectCommit()

	id, err := repo.ApplyRevisionForTenant(context.Background(), coreFixtureControlPlaneRepositoryTenantID, rev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 11 {
		t.Fatalf("id = %d, want 11", id)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestApplyRevisionForTenantCarriesTheResourceGroup(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	rev := controlPlaneRevision()
	// A check-sourced revision inherits its check row's group; the insert must
	// carry it so runs stamped from this revision report the same tier.
	rev.ResourceGroupID = coreFixtureControlPlaneRevisionGroupID

	expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	expectNoSourceControl(mock)
	mock.ExpectQuery("INSERT INTO job_definition_revisions").
		WithArgs(coreFixtureControlPlaneRepositoryTenantID, "kubernetes", "nightly", "jobs", "nightly", int64(3), "hash-3", rev.NormalizedSpec, "operator", coreFixtureControlPlaneRevisionGroupID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
	mock.ExpectExec("UPDATE job_definition_revisions SET is_active=false").
		WithArgs("kubernetes", "nightly", coreFixtureControlPlaneRepositoryTenantID, int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE job_definition_revisions r SET is_active=true").
		WithArgs(int64(11), coreFixtureControlPlaneRepositoryTenantID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectControlPlaneAudit(mock, "operator")
	mock.ExpectCommit()

	if _, err := repo.ApplyRevisionForTenant(context.Background(), coreFixtureControlPlaneRepositoryTenantID, rev); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestApplyRevisionForTenantIdempotentReplayRecordsNothing(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)

	// The generation was projected before with the same hash and the pointer
	// already sits on this revision: nothing changed, so no audit row is
	// written. A replay must not look like a change.
	expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	expectNoSourceControl(mock)
	mock.ExpectQuery("INSERT INTO job_definition_revisions").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT id, spec_hash FROM job_definition_revisions").
		WithArgs("kubernetes", "nightly", int64(3), coreFixtureControlPlaneRepositoryTenantID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "spec_hash"}).AddRow(int64(11), "hash-3"))
	mock.ExpectExec("UPDATE job_definition_revisions SET is_active=false").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE job_definition_revisions r SET is_active=true").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	id, err := repo.ApplyRevisionForTenant(context.Background(), coreFixtureControlPlaneRepositoryTenantID, controlPlaneRevision())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 11 {
		t.Fatalf("id = %d, want the stored revision 11", id)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestApplyRevisionForTenantRefusesHashConflict(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)

	// The same generation arriving with a different spec hash is two writers
	// racing or a non-deterministic normalizer; either way the write is
	// refused and the transaction rolls back.
	expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	expectNoSourceControl(mock)
	mock.ExpectQuery("INSERT INTO job_definition_revisions").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT id, spec_hash FROM job_definition_revisions").
		WillReturnRows(sqlmock.NewRows([]string{"id", "spec_hash"}).AddRow(int64(11), "hash-from-another-writer"))
	mock.ExpectRollback()

	_, err := repo.ApplyRevisionForTenant(context.Background(), coreFixtureControlPlaneRepositoryTenantID, controlPlaneRevision())
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("got %v, want ErrRevisionConflict", err)
	}
}

func TestApplyRevisionForTenantRefusesBadInputBeforeTheDatabase(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)

	rev := controlPlaneRevision()
	rev.Identity.SourceUID = ""
	if _, err := repo.ApplyRevisionForTenant(context.Background(), coreFixtureControlPlaneRepositoryTenantID, rev); err == nil {
		t.Fatal("missing identity accepted")
	}

	rev = controlPlaneRevision()
	rev.Actor = "  "
	if _, err := repo.ApplyRevisionForTenant(context.Background(), coreFixtureControlPlaneRepositoryTenantID, rev); err == nil {
		t.Fatal("blank actor accepted")
	}

	// No expectations were queued, so any database call would have failed the
	// mock; a met set with zero expectations proves nothing was sent.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("refused input reached the database: %v", err)
	}
}

func TestRevisionByID(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	t.Run("found", func(t *testing.T) {
		repo, mock := newControlPlaneRepoMock(t)
		expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
		mock.ExpectQuery("SELECT .+ FROM job_definition_revisions WHERE id=\\$1 AND tenant_id=\\$2").
			WithArgs(int64(11), coreFixtureControlPlaneRepositoryTenantID).
			WillReturnRows(sqlmock.NewRows(revisionScanColumns).AddRow(
				11, "kubernetes", "nightly", "jobs", "nightly", 3, "hash-3", `{}`, "operator", now,
			))
		mock.ExpectCommit()

		rev, err := repo.RevisionByID(context.Background(), coreFixtureControlPlaneRepositoryTenantID, 11)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rev.ID != 11 || rev.Identity.SourceUID != "nightly" || rev.SpecHash != "hash-3" || rev.Actor != "operator" {
			t.Fatalf("revision did not survive the scan: %+v", rev)
		}
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock := newControlPlaneRepoMock(t)
		expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
		mock.ExpectQuery("SELECT .+ FROM job_definition_revisions WHERE id=\\$1 AND tenant_id=\\$2").
			WithArgs(int64(99), coreFixtureControlPlaneRepositoryTenantID).
			WillReturnError(sql.ErrNoRows)
		mock.ExpectRollback()

		_, err := repo.RevisionByID(context.Background(), coreFixtureControlPlaneRepositoryTenantID, 99)
		var nf *resource.NotFoundError
		if !errors.As(err, &nf) {
			t.Fatalf("got %v, want *resource.NotFoundError", err)
		}
	})
}

func TestActiveRevision(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
	mock.ExpectQuery("FROM job_definition_revisions").
		WithArgs("kubernetes", "nightly", coreFixtureControlPlaneRepositoryTenantID).
		WillReturnRows(sqlmock.NewRows(revisionScanColumns).AddRow(
			11, "kubernetes", "nightly", "jobs", "nightly", 3, "hash-3", `{}`, "operator", time.Now(),
		))
	mock.ExpectCommit()

	rev, err := repo.ActiveRevision(context.Background(), coreFixtureControlPlaneRepositoryTenantID, "nightly")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rev.ID != 11 {
		t.Fatalf("id = %d, want 11", rev.ID)
	}

	repo, mock = newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
	mock.ExpectQuery("FROM job_definition_revisions").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err = repo.ActiveRevision(context.Background(), coreFixtureControlPlaneRepositoryTenantID, "missing")
	var nf *resource.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want *resource.NotFoundError", err)
	}
}

func TestActiveRevisionsListsOnlyActive(t *testing.T) {
	now := time.Now()

	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
	rows := sqlmock.NewRows(revisionScanColumns)
	rows.AddRow(11, "kubernetes", "nightly", "jobs", "nightly", 3, "h1", `{}`, "operator", now)
	rows.AddRow(12, "kubernetes", "hourly", "jobs", "hourly", 1, "h2", `{}`, "operator", now)
	mock.ExpectQuery("FROM job_definition_revisions").
		WithArgs(coreFixtureControlPlaneRepositoryTenantID, []string{"kubernetes", "check", "function"}).
		WillReturnRows(rows)
	mock.ExpectCommit()

	revs, err := repo.ActiveRevisions(context.Background(), coreFixtureControlPlaneRepositoryTenantID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(revs) != 2 || revs[0].Identity.SourceUID != "nightly" || revs[1].Identity.SourceUID != "hourly" {
		t.Fatalf("unexpected revisions: %+v", revs)
	}

	repo, mock = newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
	mock.ExpectQuery("FROM job_definition_revisions").
		WillReturnRows(sqlmock.NewRows(revisionScanColumns))
	mock.ExpectCommit()

	revs, err = repo.ActiveRevisions(context.Background(), coreFixtureControlPlaneRepositoryTenantID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(revs) != 0 {
		t.Fatalf("expected no revisions, got %d", len(revs))
	}
}

// storedRunColumns names the projection selectOccurrenceSQL scans, so a row
// builder that drifts from it fails the test rather than the run.
var storedRunColumns = strings.Fields("id, revision_id, phase, attempt, max_attempts, scheduled_for, attempt_uid, attempt_job_name")

func expectNewSourceAdmission(mock sqlmock.Sqlmock, tenant, uid, mode string, revision int64) {
	mock.ExpectQuery("SELECT source_mode, source_uid").WithArgs(revision, tenant).
		WillReturnRows(sqlmock.NewRows([]string{"source_mode", "source_uid"}).AddRow(mode, uid))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(sourceLockKey(tenant, mode, uid)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, revision_id, phase, attempt, max_attempts, scheduled_for").WillReturnRows(sqlmock.NewRows(storedRunColumns))
	expectExecutionControlCheck(mock)
	mock.ExpectQuery("SELECT normalized_spec::text").WithArgs(tenant, mode, uid).WillReturnRows(sqlmock.NewRows([]string{"spec"}).AddRow(`{"concurrencyPolicy":"Allow"}`))
}

func TestCreateOccurrenceForTenantCreatesAndAudits(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	run := jobrun.JobRun{
		SourceUID: "nightly", RevisionID: "11", OccurrenceKey: "occ-1",
		Trigger: jobrun.Schedule, Phase: jobrun.Pending,
	}

	expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
	expectNewSourceAdmission(mock, coreFixtureControlPlaneRepositoryTenantID, "nightly", "kubernetes", 11)
	mock.ExpectQuery("INSERT INTO job_run_control_plane").
		WithArgs(coreFixtureControlPlaneRepositoryTenantID, "nightly", int64(11), "occ-1", "Schedule", "scheduler", "Pending", 3, nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(77))
	mock.ExpectQuery("SELECT id, revision_id, phase, attempt, max_attempts, scheduled_for").
		WithArgs("nightly", "occ-1", coreFixtureControlPlaneRepositoryTenantID).
		WillReturnRows(sqlmock.NewRows(storedRunColumns).AddRow(77, 11, "Pending", 0, 3, nil, "", ""))
	expectControlPlaneAudit(mock, "scheduler")
	mock.ExpectCommit()

	state, created, err := repo.CreateOccurrenceForTenant(context.Background(), coreFixtureControlPlaneRepositoryTenantID, run, 3, "scheduler")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Fatal("created = false, want true for a fresh occurrence")
	}
	if state.ID != 77 || state.RevisionID != 11 || state.Phase != jobrun.Pending || state.MaxAttempts != 3 {
		t.Fatalf("unexpected stored state: %+v", state)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// TestCreateOccurrenceForTenantWritesScheduledFor pins the adherence column's
// write contract (0002, design 5.2b): a scheduler that knows the civil-time
// occurrence records it at row creation, and the stored run reads it back.
func TestCreateOccurrenceForTenantWritesScheduledFor(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	scheduled := time.Date(2026, 9, 18, 3, 0, 0, 0, time.UTC)

	expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
	expectNewSourceAdmission(mock, coreFixtureControlPlaneRepositoryTenantID, "check-42", "check", 11)
	mock.ExpectQuery("SELECT is_active AND COALESCE").WillReturnRows(sqlmock.NewRows([]string{"eligible"}).AddRow(true))
	mock.ExpectQuery("INSERT INTO job_run_control_plane").
		WithArgs(coreFixtureControlPlaneRepositoryTenantID, "check-42", int64(11), "occ-1", "Check",
			"orbitjob-check-scheduler", "Pending", 3, scheduled, nil).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(78))
	mock.ExpectQuery("SELECT id, revision_id, phase, attempt, max_attempts, scheduled_for").
		WithArgs("check-42", "occ-1", coreFixtureControlPlaneRepositoryTenantID).
		WillReturnRows(sqlmock.NewRows(storedRunColumns).AddRow(78, 11, "Pending", 0, 3, scheduled, "", ""))
	expectControlPlaneAudit(mock, "orbitjob-check-scheduler")
	mock.ExpectCommit()

	state, created, err := repo.CreateOccurrenceForTenant(context.Background(), coreFixtureControlPlaneRepositoryTenantID, jobrun.JobRun{
		SourceUID:     "check-42",
		RevisionID:    "11",
		OccurrenceKey: "occ-1",
		Trigger:       jobrun.Check,
		Phase:         jobrun.Pending,
		ScheduledFor:  scheduled,
	}, 3, "orbitjob-check-scheduler")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Fatal("created = false, want true for a fresh occurrence")
	}
	if !state.ScheduledFor.Equal(scheduled) {
		t.Fatalf("scheduled_for = %v, want %v", state.ScheduledFor, scheduled)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateOccurrenceForTenantLostRaceConverges(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)

	// Two schedulers raced on one occurrence; the loser must get the winner's
	// id and created=false, and must not write an audit row for a run it did
	// not create.
	expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
	mock.ExpectQuery("SELECT source_mode, source_uid").WithArgs(int64(11), coreFixtureControlPlaneRepositoryTenantID).
		WillReturnRows(sqlmock.NewRows([]string{"source_mode", "source_uid"}).AddRow("kubernetes", "nightly"))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(sourceLockKey(coreFixtureControlPlaneRepositoryTenantID, "kubernetes", "nightly")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, revision_id, phase, attempt, max_attempts, scheduled_for").
		WillReturnRows(sqlmock.NewRows(storedRunColumns).AddRow(77, 11, "Pending", 0, 3, nil, "", ""))
	mock.ExpectCommit()

	_, created, err := repo.CreateOccurrenceForTenant(context.Background(), coreFixtureControlPlaneRepositoryTenantID, jobrun.JobRun{
		SourceUID: "nightly", RevisionID: "11", OccurrenceKey: "occ-1", Trigger: jobrun.Schedule,
	}, 3, "scheduler")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Fatal("created = true for an occurrence another writer already recorded")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateOccurrenceForTenantRefusesBadInput(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)

	base := jobrun.JobRun{SourceUID: "nightly", RevisionID: "11", OccurrenceKey: "occ-1", Trigger: jobrun.Schedule}
	withPhase := base
	withPhase.Phase = jobrun.Succeeded
	withBadRevision := base
	withBadRevision.RevisionID = "not-a-number"

	tests := []struct {
		name  string
		run   jobrun.JobRun
		actor string
	}{
		{name: "missing source uid", run: jobrun.JobRun{OccurrenceKey: "occ-1", RevisionID: "11"}, actor: "scheduler"},
		{name: "missing occurrence key", run: jobrun.JobRun{SourceUID: "nightly", RevisionID: "11"}, actor: "scheduler"},
		{name: "a caller-supplied finished phase is refused", run: withPhase, actor: "scheduler"},
		{name: "a revision id that is not an id is refused", run: withBadRevision, actor: "scheduler"},
		{name: "a blank actor is refused", run: base},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := repo.CreateOccurrenceForTenant(context.Background(), coreFixtureControlPlaneRepositoryTenantID, tc.run, 3, tc.actor)
			if err == nil {
				t.Fatal("expected a refusal")
			}
		})
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("refused input reached the database: %v", err)
	}
}

func TestRunByOccurrence(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock := newControlPlaneRepoMock(t)
		expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
		mock.ExpectQuery("SELECT id, revision_id, phase, attempt, max_attempts, scheduled_for").
			WithArgs("nightly", "occ-1", coreFixtureControlPlaneRepositoryTenantID).
			WillReturnRows(sqlmock.NewRows(storedRunColumns).AddRow(77, 11, "Running", 2, 3, nil, "", ""))
		mock.ExpectCommit()

		state, found, err := repo.RunByOccurrence(context.Background(), coreFixtureControlPlaneRepositoryTenantID, "nightly", "occ-1")
		if err != nil || !found {
			t.Fatalf("found=%v err=%v, want found with no error", found, err)
		}
		if state.Attempt != 2 || state.Phase != jobrun.Running {
			t.Fatalf("unexpected state: %+v", state)
		}
	})

	t.Run("absence is not an error", func(t *testing.T) {
		repo, mock := newControlPlaneRepoMock(t)
		expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
		mock.ExpectQuery("SELECT id, revision_id, phase, attempt, max_attempts, scheduled_for").
			WillReturnError(sql.ErrNoRows)
		mock.ExpectCommit()

		_, found, err := repo.RunByOccurrence(context.Background(), coreFixtureControlPlaneRepositoryTenantID, "nightly", "missing")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if found {
			t.Fatal("found = true for a run that was never created")
		}
	})
}

func TestUpdateRunPhase(t *testing.T) {
	t.Run("a real change is written and audited", func(t *testing.T) {
		repo, mock := newControlPlaneRepoMock(t)
		expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
		mock.ExpectQuery("WITH prior").
			WithArgs(int64(77), coreFixtureControlPlaneRepositoryTenantID, "Running", 1, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"phase", "prior_phase", "prior_attempt", "run_actor"}).
				AddRow("Running", "Pending", 0, "scheduler"))
		expectControlPlaneAudit(mock, "scheduler")
		mock.ExpectCommit()

		changed, err := repo.UpdateRunPhase(context.Background(), coreFixtureControlPlaneRepositoryTenantID, 77, jobrun.Running, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !changed {
			t.Fatal("a real phase change must be reported as changed")
		}
	})

	t.Run("an idempotent replay changes nothing", func(t *testing.T) {
		repo, mock := newControlPlaneRepoMock(t)
		expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
		mock.ExpectQuery("WITH prior").
			WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("SELECT phase, attempt FROM job_run_control_plane").
			WithArgs(int64(77), coreFixtureControlPlaneRepositoryTenantID).
			WillReturnRows(sqlmock.NewRows([]string{"phase", "attempt"}).AddRow("Running", 1))
		mock.ExpectCommit()

		changed, err := repo.UpdateRunPhase(context.Background(), coreFixtureControlPlaneRepositoryTenantID, 77, jobrun.Running, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if changed {
			t.Fatal("a replay of the stored state must not report a phase change")
		}
	})

	t.Run("a terminal run is final", func(t *testing.T) {
		repo, mock := newControlPlaneRepoMock(t)
		expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
		mock.ExpectQuery("WITH prior").
			WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("SELECT phase, attempt FROM job_run_control_plane").
			WillReturnRows(sqlmock.NewRows([]string{"phase", "attempt"}).AddRow("Succeeded", 1))
		mock.ExpectRollback()

		_, err := repo.UpdateRunPhase(context.Background(), coreFixtureControlPlaneRepositoryTenantID, 77, jobrun.Running, 2)
		if !errors.Is(err, jobrun.ErrRunTerminal) {
			t.Fatalf("got %v, want jobrun.ErrRunTerminal", err)
		}
	})

	t.Run("a missing run is a not-found", func(t *testing.T) {
		repo, mock := newControlPlaneRepoMock(t)
		expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
		mock.ExpectQuery("WITH prior").
			WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("SELECT phase, attempt FROM job_run_control_plane").
			WillReturnError(sql.ErrNoRows)
		mock.ExpectRollback()

		_, err := repo.UpdateRunPhase(context.Background(), coreFixtureControlPlaneRepositoryTenantID, 99, jobrun.Running, 1)
		var nf *resource.NotFoundError
		if !errors.As(err, &nf) {
			t.Fatalf("got %v, want *resource.NotFoundError", err)
		}
	})

	t.Run("a refusal the guards cannot name surfaces as an error", func(t *testing.T) {
		repo, mock := newControlPlaneRepoMock(t)
		expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
		mock.ExpectQuery("WITH prior").
			WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("SELECT phase, attempt FROM job_run_control_plane").
			WillReturnRows(sqlmock.NewRows([]string{"phase", "attempt"}).AddRow("Running", 0))
		mock.ExpectRollback()

		// The run sits in Running attempt 0 while the write asked for Running
		// attempt 1: the predicate refused it for a reason the guards above do
		// not cover, and that must surface rather than vanish.
		if _, err := repo.UpdateRunPhase(context.Background(), coreFixtureControlPlaneRepositoryTenantID, 77, jobrun.Running, 1); err == nil {
			t.Fatal("an unexplained refusal was swallowed")
		}
	})
}

func TestCountOpenRuns(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM job_run_control_plane").
		WithArgs(coreFixtureControlPlaneRepositoryTenantID, "nightly", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectCommit()

	count, err := repo.CountOpenRuns(context.Background(), coreFixtureControlPlaneRepositoryTenantID, "nightly")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
}

func TestOpenRuns(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
	rows := sqlmock.NewRows([]string{"id", "revision_id", "occurrence_key", "phase", "scheduled_for", "trigger", "actor"})
	rows.AddRow(77, 11, "occ-1", "Running", nil, "Manual", coreFixtureControlPlaneRepositoryManualKeyID)
	rows.AddRow(78, 11, "occ-2", "Pending", nil, "Workflow", "workflow-walker")
	mock.ExpectQuery("FROM job_run_control_plane").
		WithArgs(coreFixtureControlPlaneRepositoryTenantID, "nightly", sqlmock.AnyArg()).
		WillReturnRows(rows)
	mock.ExpectCommit()

	runs, err := repo.OpenRuns(context.Background(), coreFixtureControlPlaneRepositoryTenantID, "nightly")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 2 || runs[0].OccurrenceKey != "occ-1" || runs[1].Phase != jobrun.Pending || runs[0].Trigger != jobrun.Manual || runs[0].Actor != coreFixtureControlPlaneRepositoryManualKeyID || runs[1].Trigger != jobrun.Workflow {
		t.Fatalf("unexpected runs: %+v", runs)
	}
}

func TestPrunableRunsClampsNegativeKeeps(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
	mock.ExpectQuery("FROM job_run_control_plane").
		WithArgs(coreFixtureControlPlaneRepositoryTenantID, "nightly", sqlmock.AnyArg(), "Succeeded", 0, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "occurrence_key", "phase"}))
	mock.ExpectCommit()

	// Negative keep values mean "keep none", not "refuse": a retention sweep
	// with a misconfigured limit must still run.
	runs, err := repo.PrunableRuns(context.Background(), coreFixtureControlPlaneRepositoryTenantID, "nightly", -1, -5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected no prunable runs, got %d", len(runs))
	}
}

func TestDeleteRun(t *testing.T) {
	t.Run("a terminal run is deleted and its pruning recorded", func(t *testing.T) {
		repo, mock := newControlPlaneRepoMock(t)
		expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
		mock.ExpectQuery("SELECT id FROM job_run_control_plane").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(77)))
		mock.ExpectQuery("DELETE FROM job_run_control_plane").
			WithArgs(int64(77), coreFixtureControlPlaneRepositoryTenantID, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"occurrence_key", "phase", "actor"}).
				AddRow("occ-1", "Succeeded", "scheduler"))
		expectControlPlaneAudit(mock, "scheduler")
		mock.ExpectCommit()

		deleted, err := repo.DeleteRun(context.Background(), coreFixtureControlPlaneRepositoryTenantID, 77)
		if err != nil || !deleted {
			t.Fatalf("deleted=%v err=%v, want deleted with no error", deleted, err)
		}
	})

	t.Run("a run that no longer qualifies is reported, not an error", func(t *testing.T) {
		repo, mock := newControlPlaneRepoMock(t)
		expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
		// The phase predicate refused the delete because the run started
		// executing between the scan and the delete; retention will see it
		// again on the next sweep.
		mock.ExpectQuery("SELECT id FROM job_run_control_plane").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(77)))
		mock.ExpectQuery("DELETE FROM job_run_control_plane").
			WillReturnError(sql.ErrNoRows)
		mock.ExpectCommit()

		deleted, err := repo.DeleteRun(context.Background(), coreFixtureControlPlaneRepositoryTenantID, 77)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if deleted {
			t.Fatal("deleted = true for a run the statement refused to touch")
		}
	})
}

func TestCreateAttemptForTenant(t *testing.T) {
	attempt := jobrun.Attempt{
		Number: 1, KubernetesJobName: "oj-nightly-1", KubernetesJobUID: "uid-1",
		Phase: jobrun.Running,
	}

	t.Run("the attempt and the run counter move in one transaction", func(t *testing.T) {
		repo, mock := newControlPlaneRepoMock(t)
		expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
		expectExecutionSourceLocks(mock, jobrun.DispatchPrepared)
		expectExecutionControlCheck(mock)
		mock.ExpectQuery("SELECT workflow_run_id FROM job_run_control_plane").WillReturnRows(sqlmock.NewRows([]string{"workflow_run_id"}).AddRow(nil))
		mock.ExpectQuery(regexp.QuoteMeta(lockRunForAttemptSQL)).WithArgs(int64(77), coreFixtureControlPlaneRepositoryTenantID).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(77))
		mock.ExpectQuery("INSERT INTO job_run_attempts_control_plane").
			WithArgs(coreFixtureControlPlaneRepositoryTenantID, int64(77), 1, "Running", "oj-nightly-1", "uid-1", "", sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(5))
		mock.ExpectQuery("UPDATE job_run_control_plane").
			WithArgs("CreatingAttempt", 1, sqlmock.AnyArg(), int64(77), coreFixtureControlPlaneRepositoryTenantID, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"actor"}).AddRow("scheduler"))
		expectControlPlaneAudit(mock, "scheduler")
		mock.ExpectCommit()

		if err := repo.CreateAttemptForTenant(context.Background(), coreFixtureControlPlaneRepositoryTenantID, 77, attempt, jobrun.CreatingAttempt); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("a second writer on one attempt number is a conflict", func(t *testing.T) {
		repo, mock := newControlPlaneRepoMock(t)
		expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
		expectExecutionSourceLocks(mock, jobrun.DispatchPrepared)
		expectExecutionControlCheck(mock)
		mock.ExpectQuery("SELECT workflow_run_id FROM job_run_control_plane").WillReturnRows(sqlmock.NewRows([]string{"workflow_run_id"}).AddRow(nil))
		mock.ExpectQuery(regexp.QuoteMeta(lockRunForAttemptSQL)).WithArgs(int64(77), coreFixtureControlPlaneRepositoryTenantID).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(77))
		mock.ExpectQuery("INSERT INTO job_run_attempts_control_plane").
			WillReturnError(sql.ErrNoRows)
		mock.ExpectRollback()

		err := repo.CreateAttemptForTenant(context.Background(), coreFixtureControlPlaneRepositoryTenantID, 77, attempt, jobrun.CreatingAttempt)
		if !errors.Is(err, ErrAttemptConflict) {
			t.Fatalf("got %v, want ErrAttemptConflict", err)
		}
	})

	t.Run("a run that finished between read and write takes the attempt row down with it", func(t *testing.T) {
		repo, mock := newControlPlaneRepoMock(t)
		expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
		expectExecutionSourceLocks(mock, jobrun.DispatchPrepared)
		expectExecutionControlCheck(mock)
		mock.ExpectQuery("SELECT workflow_run_id FROM job_run_control_plane").WillReturnRows(sqlmock.NewRows([]string{"workflow_run_id"}).AddRow(nil))
		mock.ExpectQuery(regexp.QuoteMeta(lockRunForAttemptSQL)).WithArgs(int64(77), coreFixtureControlPlaneRepositoryTenantID).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(77))
		mock.ExpectQuery("INSERT INTO job_run_attempts_control_plane").
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(5))
		mock.ExpectQuery("UPDATE job_run_control_plane").
			WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("SELECT phase, attempt FROM job_run_control_plane").WillReturnRows(sqlmock.NewRows([]string{"phase", "attempt"}).AddRow("Succeeded", 1))
		mock.ExpectRollback()

		err := repo.CreateAttemptForTenant(context.Background(), coreFixtureControlPlaneRepositoryTenantID, 77, attempt, jobrun.CreatingAttempt)
		if !errors.Is(err, jobrun.ErrRunTerminal) {
			t.Fatalf("got %v, want jobrun.ErrRunTerminal", err)
		}
	})

	t.Run("a bad attempt identity is refused", func(t *testing.T) {
		repo, _ := newControlPlaneRepoMock(t)

		if err := repo.CreateAttemptForTenant(context.Background(), coreFixtureControlPlaneRepositoryTenantID, 77,
			jobrun.Attempt{Number: 0, KubernetesJobName: "oj-nightly-1"}, jobrun.CreatingAttempt); err == nil {
			t.Fatal("attempt zero accepted")
		}
		if err := repo.CreateAttemptForTenant(context.Background(), coreFixtureControlPlaneRepositoryTenantID, 77,
			jobrun.Attempt{Number: 1}, jobrun.CreatingAttempt); err == nil {
			t.Fatal("an attempt with no Kubernetes Job name accepted")
		}
		if err := repo.CreateAttemptForTenant(context.Background(), coreFixtureControlPlaneRepositoryTenantID, 77,
			jobrun.Attempt{Number: 1, KubernetesJobName: "oj-nightly-1"}, jobrun.CreatingAttempt); err == nil {
			t.Fatal("an attempt with no Kubernetes Job UID accepted")
		}
	})
}

func TestUpdateAttemptPhase(t *testing.T) {
	t.Run("a real observation is written and audited", func(t *testing.T) {
		repo, mock := newControlPlaneRepoMock(t)
		expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
		mock.ExpectQuery("WITH prior").
			WithArgs("oj-nightly-1", coreFixtureControlPlaneRepositoryTenantID, "Succeeded", "uid-1", "rv-2", true, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "run_id", "attempt_number", "prior_phase", "phase", "actor", "ownership_match"}).
				AddRow(5, 77, 1, "Running", "Succeeded", "scheduler", true))
		expectControlPlaneAudit(mock, "scheduler")
		mock.ExpectCommit()

		runID, attemptNumber, err := repo.UpdateAttemptPhase(context.Background(), coreFixtureControlPlaneRepositoryTenantID, "oj-nightly-1", "Succeeded", "uid-1", "rv-2")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if runID != 77 || attemptNumber != 1 {
			t.Fatalf("got run=%d attempt=%d, want run=77 attempt=1", runID, attemptNumber)
		}
	})

	t.Run("a resync of an unchanged phase records nothing", func(t *testing.T) {
		repo, mock := newControlPlaneRepoMock(t)
		expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
		mock.ExpectQuery("WITH prior").
			WillReturnRows(sqlmock.NewRows([]string{"id", "run_id", "attempt_number", "prior_phase", "phase", "actor", "ownership_match"}).
				AddRow(5, 77, 1, "Running", "Running", "scheduler", true))
		mock.ExpectCommit()

		if _, _, err := repo.UpdateAttemptPhase(context.Background(), coreFixtureControlPlaneRepositoryTenantID, "oj-nightly-1", "Running", "uid-1", "rv-3"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectations: %v", err)
		}
	})

	t.Run("a replacement job with the same name is refused", func(t *testing.T) {
		repo, mock := newControlPlaneRepoMock(t)
		expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
		mock.ExpectQuery("WITH prior").
			WithArgs("oj-nightly-1", coreFixtureControlPlaneRepositoryTenantID, "Running", "replacement-uid", "rv-4", false, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "run_id", "attempt_number", "prior_phase", "phase", "actor", "ownership_match"}).
				AddRow(5, 77, 1, "CreatingAttempt", "CreatingAttempt", "scheduler", false))
		mock.ExpectRollback()

		_, _, err := repo.UpdateAttemptPhase(context.Background(), coreFixtureControlPlaneRepositoryTenantID, "oj-nightly-1", "Running", "replacement-uid", "rv-4")
		if !errors.Is(err, ErrAttemptOwnership) {
			t.Fatalf("got %v, want ErrAttemptOwnership", err)
		}
	})

	t.Run("a job no attempt owns is refused", func(t *testing.T) {
		repo, mock := newControlPlaneRepoMock(t)
		expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
		mock.ExpectQuery("WITH prior").
			WillReturnError(sql.ErrNoRows)
		mock.ExpectRollback()

		_, _, err := repo.UpdateAttemptPhase(context.Background(), coreFixtureControlPlaneRepositoryTenantID, "oj-orphan-1", "Succeeded", "", "")
		if !errors.Is(err, ErrNoAttempt) {
			t.Fatalf("got %v, want ErrNoAttempt", err)
		}
	})

	t.Run("a blank job name or phase is refused", func(t *testing.T) {
		repo, _ := newControlPlaneRepoMock(t)

		if _, _, err := repo.UpdateAttemptPhase(context.Background(), coreFixtureControlPlaneRepositoryTenantID, "", "Running", "", ""); err == nil {
			t.Fatal("blank job name accepted")
		}
		if _, _, err := repo.UpdateAttemptPhase(context.Background(), coreFixtureControlPlaneRepositoryTenantID, "oj-nightly-1", "", "", ""); err == nil {
			t.Fatal("blank phase accepted")
		}
	})
}

var coreFixtureControlPlaneRepositoryTenantID = scan.GenerateID()

var coreFixtureControlPlaneRepositoryManualKeyID = scan.GenerateID()

var coreFixtureControlPlaneRepositoryActorKeyID = scan.GenerateID()

var coreFixtureControlPlaneRevisionGroupID = scan.GenerateID()

func TestTerminalOutcomeReadsCreationTimeWithNewestAttemptSpan(t *testing.T) {
	created := time.Date(2026, 10, 19, 1, 2, 3, 0, time.UTC)
	started, completed := created.Add(time.Minute), created.Add(2*time.Minute)
	for _, canceledBeforeStart := range []bool{false, true} {
		name := "retried execution"
		if canceledBeforeStart {
			name = "canceled before start"
		}
		t.Run(name, func(t *testing.T) {
			repo, mock := newControlPlaneRepoMock(t)
			expectControlPlaneTx(mock, coreFixtureControlPlaneRepositoryTenantID)
			phase := jobrun.Succeeded
			var startValue, completedValue any = started, completed
			if canceledBeforeStart {
				phase, startValue, completedValue = jobrun.Canceled, nil, nil
			}
			mock.ExpectQuery("SELECT source_uid, occurrence_key, scheduled_for, created_at, phase, started_at, completed_at").
				WithArgs(int64(77), coreFixtureControlPlaneRepositoryTenantID).
				WillReturnRows(sqlmock.NewRows([]string{"source_uid", "occurrence_key", "scheduled_for", "created_at", "phase", "started_at", "completed_at"}).
					AddRow("function-9", "occ-77", nil, created, string(phase), startValue, completedValue))
			mock.ExpectCommit()

			outcome, err := repo.TerminalOutcome(context.Background(), coreFixtureControlPlaneRepositoryTenantID, 77)
			if err != nil {
				t.Fatal(err)
			}
			if outcome.RunID != 77 || outcome.Phase != phase || !outcome.CreatedAt.Equal(created) {
				t.Fatalf("ledger creation identity was not read: %+v", outcome)
			}
			if canceledBeforeStart {
				if !outcome.StartedAt.IsZero() || !outcome.CompletedAt.IsZero() {
					t.Fatalf("missing attempt acquired a span: %+v", outcome)
				}
			} else if !outcome.StartedAt.Equal(started) || !outcome.CompletedAt.Equal(completed) {
				t.Fatalf("newest attempt span changed: %+v", outcome)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

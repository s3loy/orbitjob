//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// The workflow history retainer's sweep input. The scheduler's ActiveRevisions
// deliberately stops short of workflow-sourced revisions, so this narrower
// listing is the only store path that hands them out — and these pins hold it
// to that contract at the SQL boundary: the source_mode filter and the
// "workflow" bind are what keep the retainer from silently sweeping nothing.

func TestActiveWorkflowRevisionsFiltersWorkflowSourceMode(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)
	now := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)

	expectControlPlaneTx(mock, tenantFixture())
	// The predicate is pinned in full: a regression to the scheduler's
	// source_mode = ANY($2) over the kubernetes-and-check list would match
	// neither this SQL shape nor the "workflow" bind.
	mock.ExpectQuery(`is_active AND source_mode = \$2 AND tenant_id=\$1`).
		WithArgs(tenantFixture(), "workflow").
		WillReturnRows(sqlmock.NewRows(revisionScanColumns).AddRow(
			14, "workflow", "wf-uid-1", "finance", "nightly-pipeline", 4, "hash-4",
			`{"schedule":"0 2 * * *","tasks":[{"name":"extract","jobRef":{"name":"extract-job"}}]}`,
			"operator", now,
		))
	mock.ExpectCommit()

	revs, err := repo.ActiveWorkflowRevisions(context.Background(), tenantFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(revs) != 1 {
		t.Fatalf("listed %d revisions, want 1", len(revs))
	}
	rev := revs[0]
	if rev.ID != 14 || rev.Identity.SourceMode != "workflow" || rev.Identity.SourceUID != "wf-uid-1" ||
		rev.Identity.Namespace != "finance" || rev.Identity.Name != "nightly-pipeline" {
		t.Fatalf("revision did not survive the scan: %+v", rev)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestActiveWorkflowRevisionsDBErrorRollsBack(t *testing.T) {
	repo, mock := newControlPlaneRepoMock(t)

	expectControlPlaneTx(mock, tenantFixture())
	mock.ExpectQuery(`FROM job_definition_revisions`).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, err := repo.ActiveWorkflowRevisions(context.Background(), tenantFixture()); err == nil {
		t.Fatal("expected the database error to surface")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

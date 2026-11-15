//go:build integration

package postgres_test

import (
	"testing"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/platform/scan"
)

func TestExecutionIsolationSummaryUsesCommittedLedgerAndTenantScope(t *testing.T) {
	f, repo, e := seedObservedExecution(t)
	if err := repo.MarkExecutionUnknown(t.Context(), f.tenant, e, jobrun.ReasonJobNotFound); err != nil {
		t.Fatal(err)
	}
	stored := requireExecution(t, f, repo, e.Run.ID)
	rows, err := repo.ExecutionIsolationSummary(t.Context(), f.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Phase != jobrun.ExecutionUnknown || rows[0].Count != 1 || stored.Run.UnknownSince == nil || !rows[0].OldestAt.Equal(*stored.Run.UnknownSince) {
		t.Fatalf("lost ledger isolation: rows=%+v stored=%+v", rows, stored.Run)
	}
	foreign, err := repo.ExecutionIsolationSummary(t.Context(), scan.GenerateID())
	if err != nil || len(foreign) != 0 {
		t.Fatalf("cross-tenant isolation leaked: rows=%+v err=%v", foreign, err)
	}
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET phase='CancelUnknown' WHERE id=$1`, e.Run.ID); err != nil {
		t.Fatal(err)
	}
	rows, err = repo.ExecutionIsolationSummary(t.Context(), f.tenant)
	if err != nil || len(rows) != 1 || rows[0].Phase != jobrun.CancelUnknown || rows[0].Count != 1 || !rows[0].OldestAt.Equal(*stored.Run.UnknownSince) {
		t.Fatalf("cancellation lost original uncertainty age: rows=%+v err=%v", rows, err)
	}
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET phase='Canceled' WHERE id=$1`, e.Run.ID); err != nil {
		t.Fatal(err)
	}
	rows, err = repo.ExecutionIsolationSummary(t.Context(), f.tenant)
	if err != nil || len(rows) != 0 {
		t.Fatalf("terminal run remains in current obligations: rows=%+v err=%v", rows, err)
	}
}

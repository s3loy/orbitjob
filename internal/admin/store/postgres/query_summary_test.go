package postgres

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	runquery "orbitjob/internal/admin/app/run/query"
)

func TestRunSummaryBindsFullPredicateAndActualPhases(t *testing.T) {
	repo, mock := newRunRepoMock(t)
	expectRunTenantTx(mock, "tenant-a")
	mock.ExpectQuery(`SELECT phase, count\(\*\).*FROM job_run_control_plane`).WithArgs("tenant-a", "", "source", "occ", "2026-10-19T00:00:00Z", "2026-10-20T00:00:00Z").WillReturnRows(sqlmock.NewRows([]string{"phase", "count"}).AddRow("Pending", 25).AddRow("FuturePhase", 2))
	mock.ExpectCommit()
	out, err := repo.Summary(context.Background(), runquery.ListInput{TenantID: "tenant-a", SourceUID: "source", OccurrenceKey: "occ", CreatedFrom: "2026-10-19T00:00:00Z", CreatedBefore: "2026-10-20T00:00:00Z"})
	if err != nil || out.Total != 27 || out.Phases["FuturePhase"] != 2 {
		t.Fatalf("%+v %v", out, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

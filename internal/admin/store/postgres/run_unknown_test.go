package postgres

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	runquery "orbitjob/internal/admin/app/run/query"
	"orbitjob/internal/platform/scan"
)

func TestRunRepositoryPreservesUnknownEvidence(t *testing.T) {
	now := time.Now().UTC()
	ownerUID := scan.GenerateID()
	for _, detail := range []bool{false, true} {
		t.Run(map[bool]string{false: "attempts", true: "detail"}[detail], func(t *testing.T) {
			repo, mock := newRunRepoMock(t)
			expectRunTenantTx(mock, runStoreFixtureTenantID)
			values := []any{int64(1), int64(7), 1, "ExecutionUnknown", "owned-job", "owned-job-uid", "17", now, nil, now, ownerUID, now, "JobNotFound"}
			var items []runquery.AttemptItem
			if detail {
				columns := append(append([]string{}, runListColumns...), runAttemptColumns...)
				rows := sqlmock.NewRows(columns)
				rows.AddRow(7, runStoreFixtureTenantID, "source", 11, "occurrence", "Manual", "operator", "ExecutionUnknown", 1, 2, now, now,
					values[0], values[1], values[2], values[3], values[4], values[5], values[6], values[7], values[8], values[9], values[10], values[11], values[12])
				mock.ExpectQuery("LEFT JOIN job_run_attempts_control_plane").WithArgs(runStoreFixtureTenantID, int64(7)).WillReturnRows(rows)
				mock.ExpectCommit()
				got, err := repo.Get(context.Background(), runquery.GetInput{TenantID: runStoreFixtureTenantID, ID: 7})
				if err != nil {
					t.Fatal(err)
				}
				items = got.Attempts
			} else {
				rows := sqlmock.NewRows(runAttemptColumns)
				rows.AddRow(values[0], values[1], values[2], values[3], values[4], values[5], values[6], values[7], values[8], values[9], values[10], values[11], values[12])
				mock.ExpectQuery("JOIN job_run_control_plane r").WithArgs(runStoreFixtureTenantID, int64(7)).WillReturnRows(rows)
				mock.ExpectCommit()
				var err error
				items, err = repo.ListAttempts(context.Background(), runquery.GetInput{TenantID: runStoreFixtureTenantID, ID: 7})
				if err != nil {
					t.Fatal(err)
				}
			}
			if len(items) != 1 || items[0].OwnerRunUID == nil || *items[0].OwnerRunUID != ownerUID ||
				items[0].ExecutionUnknownSince == nil || !items[0].ExecutionUnknownSince.Equal(now) ||
				items[0].ExecutionUnknownReason == nil || *items[0].ExecutionUnknownReason != "JobNotFound" {
				t.Fatalf("unknown identity/evidence lost: %+v", items)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

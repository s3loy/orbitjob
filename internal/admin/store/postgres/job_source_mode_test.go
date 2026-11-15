package postgres

import (
	"database/sql"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	jobquery "orbitjob/internal/admin/app/job/query"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/platform/scan"
	"testing"
)

func TestJobReadsOnlyScheduledJobRevisions(t *testing.T) {
	for _, list := range []bool{false, true} {
		t.Run(map[bool]string{false: "get", true: "list"}[list], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			tenant := scan.GenerateID()
			expectTenantTx(t, mock, tenant)
			if list {
				mock.ExpectQuery(`FROM job_definition_revisions WHERE tenant_id = \$1 AND source_mode = 'kubernetes' AND is_active`).WithArgs(tenant, 10, 0, "", "", "").WillReturnRows(sqlmock.NewRows([]string{"id"}))
				mock.ExpectCommit()
				_, err = NewJobRepository(db).List(t.Context(), jobquery.ListInput{TenantID: tenant, Limit: 10})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				mock.ExpectQuery(`FROM job_definition_revisions WHERE tenant_id = \$1 AND id = \$2 AND source_mode = 'kubernetes' AND is_active`).WithArgs(tenant, int64(9)).WillReturnError(sql.ErrNoRows)
				mock.ExpectRollback()
				_, err = NewJobRepository(db).Get(t.Context(), jobquery.GetInput{TenantID: tenant, ID: 9})
				var missing *resource.NotFoundError
				if !errors.As(err, &missing) {
					t.Fatalf("cross-kind revision: %v", err)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

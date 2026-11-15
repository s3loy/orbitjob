package postgres

import (
	"context"
	"database/sql"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/platform/scan"
	"testing"
)

func TestEvidenceScopeUsesTenantAndPersistedParent(t *testing.T) {
	for _, kind := range []string{"checkrun", "slo", "sloalert"} {
		for _, missing := range []bool{false, true} {
			t.Run(kind+map[bool]string{true: "/missing", false: "/stored"}[missing], func(t *testing.T) {
				db, mock, err := sqlmock.New()
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = db.Close() }()
				tenant, group := scan.GenerateID(), scan.GenerateID()
				expectTenantTx(t, mock, tenant)
				pattern := `FROM slos WHERE tenant_id=\$1 AND id=\$2`
				if kind == "checkrun" {
					pattern = `(?s)FROM check_runs r JOIN checks c ON c.tenant_id=r.tenant_id AND c.id=r.check_id.*WHERE r.tenant_id=\$1 AND r.id=\$2`
				}
				if kind == "sloalert" {
					pattern = `(?s)FROM budget_alerts a JOIN slos s ON s.tenant_id=a.tenant_id AND s.id=a.slo_id.*WHERE a.tenant_id=\$1 AND a.id=\$2`
				}
				query := mock.ExpectQuery(pattern).WithArgs(tenant, int64(7))
				if missing {
					query.WillReturnError(sql.ErrNoRows)
					mock.ExpectRollback()
				} else {
					query.WillReturnRows(sqlmock.NewRows([]string{"resource_group_id"}).AddRow(group))
					mock.ExpectCommit()
				}
				repo := NewEvidenceScopeRepository(db)
				var got string
				switch kind {
				case "checkrun":
					got, err = repo.CheckRunGroup(context.Background(), tenant, 7)
				case "slo":
					got, err = repo.SLOGroup(context.Background(), tenant, 7)
				case "sloalert":
					got, err = repo.AlertGroup(context.Background(), tenant, 7)
				}
				if missing {
					var absent *resource.NotFoundError
					if !errors.As(err, &absent) {
						t.Fatalf("missing scope=%q err=%v", got, err)
					}
				} else if err != nil || got != group {
					t.Fatalf("scope=%q err=%v", got, err)
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

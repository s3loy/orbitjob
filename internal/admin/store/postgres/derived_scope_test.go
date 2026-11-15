package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	checkrunquery "orbitjob/internal/admin/app/checkrun/query"
	alertquery "orbitjob/internal/admin/app/sloalert/query"
	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/platform/scan"
)

func TestDerivedReadsFilterParentResourceGroup(t *testing.T) {
	for _, kind := range []string{"check_run", "budget", "budget_alert"} {
		t.Run(kind, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			tenant, group := scan.GenerateID(), scan.GenerateID()
			expectTenantTx(t, mock, tenant)
			mock.ExpectQuery(`(?s)SELECT .*EXISTS .*resource_group_id`).WithArgs(tenant, int64(9), group).WillReturnError(sql.ErrNoRows)
			if kind == "budget" {
				mock.ExpectQuery(`SELECT EXISTS`).WithArgs(tenant, int64(9), group).WillReturnRows(sqlmock.NewRows([]string{"visible"}).AddRow(false))
			}
			mock.ExpectRollback()
			switch kind {
			case "check_run":
				r, ok := any(NewCheckRunRepository(db)).(interface {
					Get(context.Context, string, string, int64) (checkrunquery.GetResult, error)
				})
				if !ok {
					t.Fatal("check run read cannot carry caller group")
				}
				_, err = r.Get(t.Context(), tenant, group, 9)
			case "budget":
				r, ok := any(NewBudgetReadRepository(db)).(interface {
					GetCurrent(context.Context, string, string, int64) (slo.Budget, error)
				})
				if !ok {
					t.Fatal("budget read cannot carry caller group")
				}
				_, err = r.GetCurrent(t.Context(), tenant, group, 9)
			case "budget_alert":
				r, ok := any(NewBudgetAlertReadRepository(db)).(interface {
					Get(context.Context, string, string, int64) (alertquery.BudgetAlertItem, error)
				})
				if !ok {
					t.Fatal("alert read cannot carry caller group")
				}
				_, err = r.Get(t.Context(), tenant, group, 9)
			}
			var missing *resource.NotFoundError
			if !errors.As(err, &missing) {
				t.Fatalf("other-group read: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/domain/resource"
)

func managedFunctionWrite() function.ManagedWrite {
	return function.ManagedWrite{ActorID: "key-1", IdempotencyKey: "receipt-1", PayloadHash: "hash-1",
		Definition: function.Definition{ID: 9, TenantID: tenantFixture(), Name: "charge", Status: function.StatusActive,
			Image: "app:v1", TimeoutSeconds: 60, Version: 4},
	}
}

func TestFunctionManagementCreateAuditsBeforeCommit(t *testing.T) {
	repo, _, mock := newFunctionRepoMock(t)
	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery("INSERT INTO functions").WillReturnRows(sqlmock.NewRows(functionRowColumns()).AddRow(functionRow(9, map[string]any{"version": 1})...))
	mock.ExpectExec("INSERT INTO audit_events").WithArgs(tenantFixture(), "key-1", "create", "9", sqlmock.AnyArg(), "receipt-1").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	d, created, err := repo.CreateManaged(context.Background(), managedFunctionWrite())
	if err != nil || !created || d.ID != 9 {
		t.Fatalf("definition = %+v, created = %t, error = %v", d, created, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFunctionManagementCreateAuditFailureRollsBack(t *testing.T) {
	repo, _, mock := newFunctionRepoMock(t)
	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery("INSERT INTO functions").WillReturnRows(sqlmock.NewRows(functionRowColumns()).AddRow(functionRow(9, nil)...))
	failure := errors.New("audit unavailable")
	mock.ExpectExec("INSERT INTO audit_events").WillReturnError(failure)
	mock.ExpectRollback()
	_, _, err := repo.CreateManaged(context.Background(), managedFunctionWrite())
	if !errors.Is(err, failure) {
		t.Fatalf("error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFunctionManagementCreationReceiptReplaysWithoutAnotherAudit(t *testing.T) {
	for _, matching := range []bool{true, false} {
		t.Run(map[bool]string{true: "same input", false: "different input"}[matching], func(t *testing.T) {
			repo, _, mock := newFunctionRepoMock(t)
			in := managedFunctionWrite()
			in.Definition.ResourceGroupID = functionGroupID
			hash := in.PayloadHash
			if !matching {
				hash = "different"
			}
			gucExpectations(mock, tenantFixture())
			mock.ExpectQuery("INSERT INTO functions").WillReturnRows(sqlmock.NewRows(functionRowColumns()))
			mock.ExpectQuery("SELECT id, create_payload_hash").WithArgs(tenantFixture(), "key-1", "receipt-1").WillReturnRows(sqlmock.NewRows([]string{"id", "hash", "group", "created_at"}).AddRow(9, hash, in.Definition.ResourceGroupID, time.Now().UTC()))
			if matching {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			d, created, err := repo.CreateManaged(context.Background(), in)
			if matching {
				if err != nil || created || d.ID != 9 || d.Version != 1 {
					t.Fatalf("definition = %+v, created = %t, error = %v", d, created, err)
				}
			} else {
				var conflict *resource.ConflictError
				if !errors.As(err, &conflict) {
					t.Fatalf("error = %v", err)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFunctionManagementUpdatePinsVersionAndAuditsAtomically(t *testing.T) {
	repo, _, mock := newFunctionRepoMock(t)
	in := managedFunctionWrite()
	in.CallerGroupID = functionGroupID
	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery("SELECT .* FROM functions .* FOR UPDATE").WithArgs(tenantFixture(), int64(9), sqlNullString(in.CallerGroupID)).WillReturnRows(sqlmock.NewRows(functionRowColumns()).AddRow(functionRow(9, nil)...))
	mock.ExpectQuery("UPDATE functions SET").WillReturnRows(sqlmock.NewRows(functionRowColumns()).AddRow(functionRow(9, map[string]any{"version": 5, "status": function.StatusPaused})...))
	mock.ExpectExec("INSERT INTO audit_events").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	d, err := repo.UpdateManaged(context.Background(), in)
	if err != nil || d.Version != 5 || d.ResourceGroupID != in.CallerGroupID {
		t.Fatalf("definition = %+v, error = %v", d, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFunctionManagementStaleOrOutOfGroupCannotWrite(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(map[bool]string{true: "invisible", false: "stale"}[missing], func(t *testing.T) {
			repo, _, mock := newFunctionRepoMock(t)
			gucExpectations(mock, tenantFixture())
			rows := sqlmock.NewRows(functionRowColumns())
			if !missing {
				rows.AddRow(functionRow(9, map[string]any{"version": 5})...)
			}
			mock.ExpectQuery("SELECT .* FROM functions .* FOR UPDATE").WillReturnRows(rows)
			mock.ExpectRollback()
			_, err := repo.UpdateManaged(context.Background(), managedFunctionWrite())
			if missing {
				var e *resource.NotFoundError
				if !errors.As(err, &e) {
					t.Fatalf("error = %v", err)
				}
			} else {
				var e *resource.ConflictError
				if !errors.As(err, &e) {
					t.Fatalf("error = %v", err)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFunctionManagementUpdateAuditFailureRollsBack(t *testing.T) {
	repo, _, mock := newFunctionRepoMock(t)
	gucExpectations(mock, tenantFixture())
	mock.ExpectQuery("SELECT .* FROM functions .* FOR UPDATE").WillReturnRows(sqlmock.NewRows(functionRowColumns()).AddRow(functionRow(9, nil)...))
	mock.ExpectQuery("UPDATE functions SET").WillReturnRows(sqlmock.NewRows(functionRowColumns()).AddRow(functionRow(9, map[string]any{"version": 5})...))
	mock.ExpectExec("INSERT INTO audit_events").WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()
	_, err := repo.UpdateManaged(context.Background(), managedFunctionWrite())
	if !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

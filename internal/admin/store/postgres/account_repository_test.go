package postgres

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/lib/pq/pqerror"
	"orbitjob/internal/admin/app/account"
)

func TestAccountRepositorySanitizesDatabaseDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		code pqerror.Code
		want error
	}{{"OJ401", account.ErrUnauthenticated}, {"OJ403", account.ErrForbidden}, {"OJ400", account.ErrInvalid}, {"OJ409", account.ErrConflict}, {"OJ412", account.ErrLastAdmin}, {"OJ428", account.ErrRecentAuth}, {"23505", account.ErrConflict}, {"23514", account.ErrInvalid}, {"XX000", nil}} {
		t.Run(string(tc.code), func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT orbitjob_identity($1,$2::jsonb)`)).WithArgs("setup", sqlmock.AnyArg()).WillReturnError(&pq.Error{Code: tc.code, Message: "private-password-value", Detail: "session-secret-value"})
			err = NewAccountRepository(db).Call(context.Background(), "setup", map[string]string{"password_hash": "private-password-value"}, nil)
			if err == nil {
				t.Fatal("SQL failure swallowed")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("got=%v want=%v", err, tc.want)
			}
			if strings.Contains(err.Error(), "private-password-value") || strings.Contains(err.Error(), "session-secret-value") {
				t.Fatal("secret leaked through SQL diagnostics")
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestAccountRepositoryTransactionSerializesAndRollsBack(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[fail], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			mock.ExpectBegin()
			query := regexp.QuoteMeta(`SELECT orbitjob_identity($1,$2::jsonb)`)
			mock.ExpectQuery(query).WithArgs("lock", "null").WillReturnRows(sqlmock.NewRows([]string{"result"}).AddRow(`{}`))
			request := mock.ExpectQuery(query).WithArgs("update_member", sqlmock.AnyArg())
			if fail {
				request.WillReturnError(&pq.Error{Code: "OJ403"})
				mock.ExpectRollback()
			} else {
				request.WillReturnRows(sqlmock.NewRows([]string{"result"}).AddRow(`{}`))
				mock.ExpectCommit()
			}
			err = NewAccountRepository(db).Transaction(context.Background(), func(repository account.Repository) error {
				return repository.Call(context.Background(), "update_member", map[string]string{"session_id": "opaque-id"}, nil)
			})
			if fail && !errors.Is(err, account.ErrForbidden) {
				t.Fatalf("expected refusal: %v", err)
			}
			if !fail && err != nil {
				t.Fatal(err)
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

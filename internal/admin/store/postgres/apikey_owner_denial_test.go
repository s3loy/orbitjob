package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"orbitjob/internal/domain/resource"
)

// Platform keys have no tenant. An owner lookup must treat their NULL owner
// like a missing key rather than expose a distinct internal error response.
func TestAPIKeyOwnerMissingAndPlatformAreIndistinguishable(t *testing.T) {
	for _, owner := range []any{nil, "", "missing-row"} {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		query := mock.ExpectQuery(`SELECT tenant_id FROM orbitjob_find_key_tenant\(\$1\)`).WithArgs("key")
		if owner == "missing-row" {
			query.WillReturnError(sql.ErrNoRows)
		} else {
			query.WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow(owner))
		}
		tenant, err := NewAPIKeyRepository(db).FindKeyTenant(context.Background(), "key")
		var missing *resource.NotFoundError
		if tenant != "" || !errors.As(err, &missing) || missing.Resource != "api_key" {
			t.Fatalf("owner=%v tenant=%s err=%v", owner, tenant, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
	}
}

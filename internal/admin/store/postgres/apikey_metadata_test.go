package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/domain/resource"
)

func TestAPIKeyMetadataIsTenantScopedAndOmitsSecrets(t *testing.T) {
	for _, tc := range []struct {
		name            string
		queryErr, error bool
		group           string
	}{
		{name: "ungrouped"}, {name: "grouped", group: "ci"}, {name: "missing", queryErr: true, error: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			mock.ExpectBegin()
			mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).WithArgs("own").WillReturnResult(sqlmock.NewResult(0, 0))
			query := mock.ExpectQuery(`SELECT name, COALESCE\(resource_group_id, ''\) FROM api_keys WHERE tenant_id = \$1 AND id = \$2`).WithArgs("own", "key")
			if tc.queryErr {
				query.WillReturnError(sql.ErrNoRows)
				mock.ExpectRollback()
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"name", "resource_group_id"}).AddRow("Daily operator", tc.group))
				mock.ExpectCommit()
			}
			out, err := NewAPIKeyRepository(db).Metadata(context.Background(), "own", "key")
			if (err != nil) != tc.error {
				t.Fatal(err)
			}
			if !tc.error && (out.Name != "Daily operator" || out.ResourceGroupID != tc.group) {
				t.Fatalf("wrong metadata %+v", out)
			}
			if tc.queryErr {
				var nf *resource.NotFoundError
				if !errors.As(err, &nf) {
					t.Fatalf("missing not masked: %v", err)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAPIKeyRenameCommitsAuditAtomically(t *testing.T) {
	for _, tc := range []struct {
		name     string
		affected int64
		auditErr bool
		wantErr  bool
	}{
		{name: "success", affected: 1}, {name: "foreign or missing", wantErr: true}, {name: "audit failure", affected: 1, auditErr: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			mock.ExpectBegin()
			mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).WithArgs("own").WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec(`UPDATE api_keys SET name = \$1 WHERE tenant_id = \$2 AND id = \$3`).WithArgs("New label", "own", "key").WillReturnResult(sqlmock.NewResult(0, tc.affected))
			if tc.affected > 0 {
				auditWrite := mock.ExpectExec(`INSERT INTO audit_events`).WithArgs("own", "actor", audit.EventUpdate, audit.ResourceAPIKey, "key", []byte(`{"name":"New label"}`))
				if tc.auditErr {
					auditWrite.WillReturnError(errors.New("audit unavailable"))
				} else {
					auditWrite.WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			if tc.wantErr {
				mock.ExpectRollback()
			} else {
				mock.ExpectCommit()
			}
			err = NewAPIKeyRepository(db).Rename(context.Background(), "own", "key", "New label", audit.Event{TenantID: "own", ActorID: "actor", EventType: audit.EventUpdate, ResourceType: audit.ResourceAPIKey, ResourceID: "key", Diff: map[string]any{"name": "New label"}})
			if (err != nil) != tc.wantErr {
				t.Fatalf("unexpected err=%v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

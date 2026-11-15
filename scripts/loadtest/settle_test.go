package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"orbitjob/internal/platform/scan"
)

func TestSettleRejectsInvalidArtifactsBeforeDatabaseRead(t *testing.T) {
	tenant := scan.GenerateID()
	for _, created := range [][]CreatedDefinition{
		nil,
		{{Tenant: "invalid", RevisionID: 1}},
		{{Tenant: tenant, RevisionID: 0}},
		{{Tenant: tenant, RevisionID: -1}},
		{{Tenant: tenant, RevisionID: 1}, {Tenant: scan.GenerateID(), RevisionID: 1}},
	} {
		if _, err := settleInFlight(context.Background(), nil, created); err == nil {
			t.Fatalf("invalid artifact accepted: %+v", created)
		}
	}
}

func TestSettleCountsEveryTenantAndDeduplicatesRevisions(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	tenants := []string{scan.GenerateID(), scan.GenerateID()}
	sort.Strings(tenants)
	created := []CreatedDefinition{{Tenant: tenants[0], RevisionID: 4}, {Tenant: tenants[0], RevisionID: 4}, {Tenant: tenants[1], RevisionID: 8}}
	for i, tenant := range tenants {
		mock.ExpectBegin()
		mock.ExpectExec(`SELECT set_config\('app.tenant_id', \$1, true\)`).WithArgs(tenant).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`SELECT count\(\*\).*WHERE revision_id = ANY`).WithArgs(pq.Array([]int64{int64(4 + i*4)})).
			WillReturnRows(sqlmock.NewRows([]string{"observed", "pending"}).AddRow(4, i+1))
		mock.ExpectRollback()
	}
	count, err := settleInFlight(context.Background(), db, created)
	if err != nil || count != 3 {
		t.Fatalf("in flight=%d, err=%v", count, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSettleNeverTurnsDatabaseFailureIntoZero(t *testing.T) {
	for _, stage := range []string{"begin", "tenant guard", "query", "scan", "rollback", "empty"} {
		t.Run(stage, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			failure := errors.New("driver-credential-canary")
			begin := mock.ExpectBegin()
			if stage == "begin" {
				begin.WillReturnError(failure)
			} else {
				guard := mock.ExpectExec("SELECT set_config")
				if stage == "tenant guard" {
					guard.WillReturnError(failure)
				} else {
					guard.WillReturnResult(sqlmock.NewResult(0, 0))
					query := mock.ExpectQuery("SELECT count")
					switch stage {
					case "query":
						query.WillReturnError(failure)
					case "scan":
						query.WillReturnRows(sqlmock.NewRows([]string{"observed", "pending"}).AddRow("malformed", 0))
					case "empty":
						query.WillReturnRows(sqlmock.NewRows([]string{"observed", "pending"}).AddRow(0, 0))
					default:
						query.WillReturnRows(sqlmock.NewRows([]string{"observed", "pending"}).AddRow(2, 0))
					}
				}
				rollback := mock.ExpectRollback()
				if stage == "rollback" {
					rollback.WillReturnError(failure)
				}
			}
			_, err = settleInFlight(context.Background(), db, []CreatedDefinition{{Tenant: scan.GenerateID(), RevisionID: 1}})
			if err == nil || strings.Contains(err.Error(), failure.Error()) {
				t.Fatalf("failure was accepted or exposed driver diagnostics: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRunSettleRequiresBoundedArtifactsAndPrivateDSN(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ORBITJOB_DSN", "")
	for _, args := range [][]string{
		{}, {"--run-id", "../outside"}, {"--run-id", "missing", "--run-root", root},
	} {
		if err := runSettle(args); err == nil {
			t.Fatalf("accepted invalid command: %v", args)
		}
	}
	dir := filepath.Join(root, "run")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "created-definitions.json")
	args := []string{"--run-id", "run", "--run-root", root}
	if err := os.WriteFile(path, []byte("malformed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runSettle(args); err == nil {
		t.Fatal("accepted malformed definitions")
	}
	if err := os.WriteFile(path, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runSettle(args); err == nil || !strings.Contains(err.Error(), "ORBITJOB_DSN") {
		t.Fatalf("missing DSN: %v", err)
	}
	t.Setenv("ORBITJOB_DSN", "driver-credential-canary")
	data, err := json.Marshal([]CreatedDefinition{{Tenant: scan.GenerateID(), RevisionID: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runSettle(args); err == nil || strings.Contains(err.Error(), "driver-credential-canary") {
		t.Fatalf("invalid DSN was accepted or exposed: %v", err)
	}
}

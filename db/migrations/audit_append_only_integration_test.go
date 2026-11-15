//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"testing"
)

func TestApplicationAuditTrailIsAppendOnly(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	owner := ownerDB(t)
	seedAuditRows(t, ctx, owner)
	for _, table := range []string{"audit_events", "audit_events_default"} {
		for _, role := range []string{"orbitjob_admin", "orbitjob_runtime", "orbitjob_operator"} {
			for _, privilege := range []string{"UPDATE", "DELETE", "TRUNCATE"} {
				var granted bool
				if err := owner.QueryRowContext(ctx, `SELECT has_table_privilege($1,$2,$3)`, role, "public."+table, privilege).Scan(&granted); err != nil {
					t.Fatal(err)
				}
				if granted {
					t.Fatalf("%s retains %s on %s", role, privilege, table)
				}
			}
		}
	}
	withRoleConn(t, "orbitjob_admin", func(ctx context.Context, conn *sql.Conn) {
		if _, err := conn.ExecContext(ctx, `SET app.tenant_id='`+migrationAlphaID+`'`); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"audit_events", "audit_events_default"} {
			for _, query := range []string{"UPDATE " + table + " SET resource_id='rewritten'", "DELETE FROM " + table} {
				if _, err := conn.ExecContext(ctx, query); err == nil {
					t.Fatalf("application audit mutation succeeded: %s", query)
				}
			}
		}
	})
}

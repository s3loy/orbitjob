//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	platformmigrate "orbitjob/internal/platform/migrate"
)

func TestAuditResourceIdentityUpgradePreservesPartitionHistory(t *testing.T) {
	prepareMigrationRoles(t)
	resetPublicSchema(t)
	ctx := ctx30(t)
	migrations, err := platformmigrate.Load(os.DirFS("."), ".")
	if err != nil {
		t.Fatal(err)
	}
	var previous []platformmigrate.Migration
	for _, migration := range migrations {
		if migration.Version < 28 {
			previous = append(previous, migration)
		}
	}
	migrator := openMigrationDB(t, testRoleDSN(t, "orbitjob_migrator"))
	if _, err := platformmigrate.Execute(ctx, migrator, previous, platformmigrate.Options{}); err != nil {
		t.Fatal(err)
	}
	owner := ownerDB(t)
	seedTenantIDs(t, ctx, owner, migrationAlphaID, migrationBetaID)
	if _, err := migrator.ExecContext(ctx, `CREATE TABLE audit_events_203001 PARTITION OF audit_events
		FOR VALUES FROM ('2030-01-01') TO ('2030-02-01')`); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ExecContext(ctx, `INSERT INTO audit_events(tenant_id,actor_type,event_type,resource_type,resource_id,created_at)
		VALUES ($1,'system','legacy','jobs','legacy/default','2020-01-01'),
		       ($1,'system','legacy','jobs','legacy/monthly','2030-01-15');
	`, migrationAlphaID); err != nil {
		t.Fatal(err)
	}
	if _, err := platformmigrate.Execute(ctx, migrator, migrations, platformmigrate.Options{}); err != nil {
		t.Fatal(err)
	}
	if n := countAsOwner(t, ctx, owner, `SELECT count(*) FROM audit_events WHERE event_type='legacy' AND resource_id IN ('legacy/default','legacy/monthly')`); n != 2 {
		t.Fatalf("migration changed historical audit rows: %d", n)
	}
	identity := strings.Repeat("n", 63) + "/" + strings.Repeat("j", 253)
	withRoleConn(t, "orbitjob_admin", func(ctx context.Context, conn *sql.Conn) {
		if _, err := conn.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,false)`, migrationAlphaID); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO audit_events(tenant_id,actor_type,event_type,resource_type,resource_id,created_at)
			VALUES ($1,'api_key','identity_test','jobs',$2,'2020-01-02'),($1,'api_key','identity_test','jobs',$2,'2030-01-16')`, migrationAlphaID, identity); err != nil {
			t.Fatalf("full Kubernetes identity must fit both existing partitions: %v", err)
		}
		var count int
		if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE resource_id=$1`, identity).Scan(&count); err != nil || count != 2 {
			t.Fatalf("identity changed or disappeared: count=%d err=%v", count, err)
		}
	})
}

func countAsOwner(t *testing.T, ctx context.Context, db *sql.DB, query string) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

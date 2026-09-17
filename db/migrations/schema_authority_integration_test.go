//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// Schema authority: the relation set the migration ledger declares and the
// relation set the database ends up with must be identical -- no extra, no
// missing. The baseline's own existence check (the DO block near the end of
// 0001_baseline.up.sql) can only name what must exist; the extra direction is
// what this file adds, because a leftover CREATE in a migration file or a
// stray table in the database is invisible to a missing-only check.

var createTableRE = regexp.MustCompile(`(?m)^CREATE TABLE IF NOT EXISTS (\w+)`)

// migrationRelations parses the relation names the migration ledger creates,
// partitions included (audit_events_default is created by a second CREATE
// TABLE line with a PARTITION OF clause). Parsing the files keeps one
// authority: when a table is added to or removed from any migration, the
// expected set moves by itself. Until migration 0004 the baseline was the
// whole ledger, which is why this used to read 0001 alone.
func migrationRelations(t *testing.T) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var declared strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		data, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		declared.Write(data)
		declared.WriteByte('\n')
	}
	names := createTableRE.FindAllStringSubmatch(declared.String(), -1)
	if len(names) == 0 {
		t.Fatalf("no CREATE TABLE statements found in the migration ledger; the parser is broken")
	}
	out := make(map[string]bool, len(names))
	for _, m := range names {
		out[m[1]] = true
	}
	return out
}

// appliedRelations lists the ordinary and partitioned relations in public, the
// two relkinds CREATE TABLE produces. schema_migrations is excluded: it is the
// migration runner's tracking table (internal/platform/migrate/run.go:83), not
// part of the schema the baseline declares.
func appliedRelations(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind IN ('r', 'p')
		  AND c.relname <> 'schema_migrations'
		ORDER BY c.relname
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

// TestRelationSetMatchesBaseline is the authority property: both directions of
// the set difference must be empty.
func TestRelationSetMatchesBaseline(t *testing.T) {
	applyAllMigrations(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := ownerDB(t)

	want := migrationRelations(t)
	got, err := appliedRelations(ctx, db)
	if err != nil {
		t.Fatalf("list applied relations: %v", err)
	}

	var missing, extra []string
	for name := range want {
		if !got[name] {
			missing = append(missing, name)
		}
	}
	for name := range got {
		if !want[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) != 0 || len(extra) != 0 {
		t.Fatalf("relation set diverged from the baseline: missing=%v extra=%v", missing, extra)
	}
}

// TestAuthorityDetectsAnExtraRelation is the red proof for the extra
// direction. A table created outside the baseline must show up in the
// difference, or the comparison is decorative. The probe is created inside one
// transaction that is rolled back.
func TestAuthorityDetectsAnExtraRelation(t *testing.T) {
	applyAllMigrations(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := ownerDB(t)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`CREATE TABLE public.authority_probe_extra (id bigint)`); err != nil {
		t.Fatalf("create probe: %v", err)
	}

	got, err := appliedRelations(ctx, tx)
	if err != nil {
		t.Fatalf("list relations under mutation: %v", err)
	}
	want := migrationRelations(t)
	var extra []string
	for name := range got {
		if !want[name] {
			extra = append(extra, name)
		}
	}
	if len(extra) != 1 || extra[0] != "authority_probe_extra" {
		t.Fatalf("the comparison did not isolate the extra relation: %v", extra)
	}
}

// TestAuthorityDetectsAMissingRelation is the red proof for the missing
// direction. Dropping a baseline table inside the transaction must be named by
// the difference; PostgreSQL's transactional DDL means the drop is undone by
// the rollback and nothing is left behind.
func TestAuthorityDetectsAMissingRelation(t *testing.T) {
	applyAllMigrations(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := ownerDB(t)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DROP TABLE public.budget_alerts CASCADE`); err != nil {
		t.Fatalf("drop probe target: %v", err)
	}

	got, err := appliedRelations(ctx, tx)
	if err != nil {
		t.Fatalf("list relations under mutation: %v", err)
	}
	want := migrationRelations(t)
	var missing []string
	for name := range want {
		if !got[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) != 1 || missing[0] != "budget_alerts" {
		t.Fatalf("the comparison did not isolate the missing relation: %v", missing)
	}
}

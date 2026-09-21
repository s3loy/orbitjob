package postgrestest

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"orbitjob/internal/platform/config"
	"orbitjob/internal/platform/migrate"
)

const (
	sharedSchemaLockClassID  = 32117
	sharedSchemaLockObjectID = 260326
	advisoryLockWaitTimeout  = 5 * time.Minute
	testSchemaPrefix         = "ojtest_"
	maxIdentifierLength      = 63
)

// Run prepares the integration database before package tests execute.
func Run(m *testing.M) int {
	if err := config.LoadDotenv(); err != nil {
		fmt.Fprintf(os.Stderr, "load .env: %v\n", err)
		return 1
	}

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		return m.Run()
	}

	packageDSN, _, err := packageDSN(dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scope test dsn: %v\n", redactDSN(err.Error()))
		return 1
	}
	if err := os.Setenv("TEST_DATABASE_DSN", packageDSN); err != nil {
		fmt.Fprintf(os.Stderr, "set TEST_DATABASE_DSN: %v\n", redactDSN(err.Error()))
		return 1
	}

	if err := withAdvisoryLock(
		packageDSN,
		sharedSchemaLockClassID,
		sharedSchemaLockObjectID,
		func(db *sql.DB) error {
			return applySchemaWithDB(packageDSN, db)
		},
	); err != nil {
		fmt.Fprintf(os.Stderr, "apply test schema: %v\n", redactDSN(err.Error()))
		return 1
	}

	return m.Run()
}

// DSN returns the test database DSN or skips the integration test package.
func DSN(t *testing.T) string {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	return dsn
}

// Open returns a PostgreSQL handle scoped to the current test name.
func Open(t *testing.T) *sql.DB {
	t.Helper()

	dsn, schemaName, err := testDSN(DSN(t), t.Name())
	if err != nil {
		t.Fatalf("scope test dsn: %v", redactDSN(err.Error()))
	}

	db, err := open(dsn)
	if err != nil {
		t.Fatalf("open test db: %v", redactDSN(err.Error()))
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping test db: %v", err)
	}
	if err := withAdvisoryLock(
		dsn,
		sharedSchemaLockClassID,
		lockObjectID(dsn),
		func(_ *sql.DB) error {
			return applySchemaWithDB(dsn, db)
		},
	); err != nil {
		t.Fatalf("prepare test schema: %v", redactDSN(err.Error()))
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()

		if err := dropSchema(cleanupCtx, db, schemaName); err != nil {
			t.Errorf("drop test schema %q: %v", schemaName, err)
		}
	})

	return db
}

func open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(50)
	db.SetConnMaxLifetime(5 * time.Minute)

	return db, nil
}

// resetTestData empties every base table in the connection's schema and
// restarts its sequences.
//
// The table list comes from the catalog rather than from a hand-written list:
// the previous list named job_instance_attempts, job_instances, workers and
// jobs, four tables the run-ledger baseline does not create, so it failed with
// "relation does not exist" instead of resetting anything. A derived list
// cannot drift from the schema it runs against.
//
// Partitions are skipped: TRUNCATE cascades from the parent, and naming a
// child that the parent already covers is redundant.
func resetTestData(ctx context.Context, db *sql.DB) error {
	tables, err := currentSchemaTables(ctx, db)
	if err != nil {
		return err
	}
	if len(tables) == 0 {
		return fmt.Errorf("no tables found in the connection's schema; the schema was not built")
	}

	_, err = db.ExecContext(ctx, "TRUNCATE TABLE "+strings.Join(tables, ", ")+" RESTART IDENTITY CASCADE")
	return err
}

// currentSchemaTables lists the quotable names of the base tables in the
// schema the connection resolves to.
func currentSchemaTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT quote_ident(c.relname)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema()
		  AND c.relkind IN ('r', 'p')
		  AND NOT c.relispartition
		  AND c.relname <> 'schema_migrations'
		ORDER BY c.relname
	`)
	if err != nil {
		return nil, fmt.Errorf("list tables in the connection's schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan table name: %w", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tables in the connection's schema: %w", err)
	}
	return tables, nil
}

func applySchemaWithDB(dsn string, db *sql.DB) error {
	if err := validateDSN(dsn); err != nil {
		return err
	}
	schemaName, err := schemaNameFromDSN(dsn)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), schemaApplyTimeout)
	defer cancel()

	// Extensions belong to the database, not to this schema, and must be in
	// place before anything else. Placing them first also keeps
	// migrate.EnsureRoles' own CREATE EXTENSION a no-op, so it cannot land
	// pgcrypto inside the schema this function is about to drop.
	if err := ensureExtensions(ctx, dsn); err != nil {
		return err
	}

	// The baseline grants to, and creates policies for, the orbitjob_* roles.
	// They are cluster-level and normally already exist, but a fresh cluster
	// needs them before the migration can run. This is process-wide work, not
	// per-test work, so it happens once.
	if err := ensureClusterRoles(ctx, db); err != nil {
		return err
	}

	// Drop schema first to ensure clean state across test runs
	if schemaName != "public" {
		if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+quoteIdentifier(schemaName)+` CASCADE`); err != nil {
			return err
		}
	}
	if err := ensureSchema(ctx, db, schemaName); err != nil {
		return err
	}
	if err := applyMigrations(ctx, db, schemaName); err != nil {
		return err
	}

	return resetTestData(ctx, db)
}

// schemaApplyTimeout bounds one schema build. The baseline is a single file of
// DDL, ownership changes, policies and grants; it is the slowest step of every
// integration test, and it must not be cut off halfway by a short deadline.
const schemaApplyTimeout = 2 * time.Minute

var (
	ensureRolesMu   sync.Mutex
	ensureRolesDone bool
)

// ensureClusterRoles creates and hardens the orbitjob_* cluster roles once per
// process. The baseline refuses to run without them, so a fresh cluster needs
// this before the first schema build; after that it is a no-op.
//
// The flag is only set on success, so a transient failure is retried by the
// next caller instead of being remembered as done.
func ensureClusterRoles(ctx context.Context, db *sql.DB) error {
	ensureRolesMu.Lock()
	defer ensureRolesMu.Unlock()

	if ensureRolesDone {
		return nil
	}
	if err := migrate.EnsureRoles(ctx, db); err != nil {
		return err
	}
	ensureRolesDone = true
	return nil
}

// applyMigrations builds the schema by running every up migration in order.
// db/migrations is the only schema authority; the generated chart copy is not
// read here.
func applyMigrations(ctx context.Context, db *sql.DB, schemaName string) error {
	paths, err := migrationScripts()
	if err != nil {
		return err
	}

	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", filepath.Base(path), err)
		}
		script, err := qualifyForSchema(string(raw), schemaName)
		if err != nil {
			return fmt.Errorf("qualify migration %s: %w", filepath.Base(path), err)
		}
		if _, err := db.ExecContext(ctx, script); err != nil {
			return fmt.Errorf("apply migration %s: %w", filepath.Base(path), err)
		}
	}
	return nil
}

// migrationScripts lists the up migrations in application order. The answer is
// the same for every test, so it is computed once per process: re-walking the
// tree on each schema build would be per-test work for a per-process fact.
var migrationScripts = sync.OnceValues(func() ([]string, error) {
	goModPath, err := findMigrationFile("go.mod")
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(filepath.Dir(goModPath), "db", "migrations")
	paths, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no up migrations found in %s", dir)
	}

	sort.Strings(paths)
	return paths, nil
})

// baselineSchema is the schema the migrations are written against.
const baselineSchema = "public"

// safeSchemaName accepts the identifiers schemaNameForPath produces. The
// substitution below inlines the name into SQL text, including into single
// quoted string literals, so anything outside this set must be refused rather
// than quoted.
var safeSchemaName = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// qualifyForSchema rewrites a migration so that "public" means the schema the
// caller is scoped to.
//
// The harness isolates each test in its own schema by setting search_path, and
// the migration creates its objects unqualified -- those land in the test
// schema correctly. But the migration also *names* public in the places it has
// to be explicit: the SECURITY DEFINER function bodies, the function DDL and
// grants, the loop that transfers ownership, the grants on the schema, and the
// two catalog assertions. Under a scoped search_path those references still
// point at the shared public schema, so the ownership loop would find nothing,
// the assertions would validate an empty schema, and the lookup functions would
// read the wrong tables.
//
// Rewriting the text keeps one schema authority: the migration file stays the
// single description of the schema, and the harness only changes which schema
// it describes. The alternative -- building the database in public and giving
// each test a search_path that prefers it -- would put every test's tables in
// one shared schema and lose isolation entirely.
//
// Uppercase PUBLIC is the SQL keyword in REVOKE ... FROM PUBLIC and is left
// alone; every replacement below is case-sensitive.
func qualifyForSchema(script, schemaName string) (string, error) {
	if schemaName == baselineSchema {
		return script, nil
	}
	if !safeSchemaName.MatchString(schemaName) {
		return "", fmt.Errorf("schema name %q cannot be inlined into SQL", schemaName)
	}

	replacements := [][2]string{
		// SECURITY DEFINER bodies pin search_path to pg_catalog first; the
		// application schema has to follow it or the functions see nothing.
		{"pg_catalog, public", "pg_catalog, " + schemaName},
		{"public.", schemaName + "."},
		{"'public'", "'" + schemaName + "'"},
		{"SCHEMA public", "SCHEMA " + schemaName},
	}
	for _, replacement := range replacements {
		script = strings.ReplaceAll(script, replacement[0], replacement[1])
	}
	return script, nil
}

func validateDSN(dsn string) error {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return err
	}

	dbName := strings.TrimPrefix(parsed.Path, "/")
	if dbName == "" {
		return fmt.Errorf("test database name is required")
	}
	if strings.Contains(strings.ToLower(dbName), "test") {
		return nil
	}

	return fmt.Errorf("TEST_DATABASE_DSN must point to a dedicated test database, got %q", dbName)
}

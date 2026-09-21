//go:build integration

package postgrestest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// TestConcurrentSchemaPreparation reproduces the cross-package race that made
// `make integration` fail intermittently with
// "duplicate key value violates unique constraint pg_extension_name_index".
//
// Schema preparation is scoped per test, but CREATE EXTENSION is scoped per
// database. Two preparations running at once therefore both reach the extension
// statement. IF NOT EXISTS makes that statement idempotent against an extension
// that already exists -- it does not make it atomic against one being created
// concurrently, and PostgreSQL raises a unique violation for the loser.
func TestConcurrentSchemaPreparation(t *testing.T) {
	if os.Getenv("TEST_DATABASE_DSN") == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	baseDSN := os.Getenv("TEST_DATABASE_DSN")
	schemas := make([]string, 0, schemaRaceWorkers)
	for i := 0; i < schemaRaceWorkers; i++ {
		schemas = append(schemas, fmt.Sprintf("ojtest_race%02d", i))
	}
	defer dropSchemas(t, baseDSN, schemas)

	// Remove the extension first so every worker has to create it. Left in
	// place, the workers would find it already present and take the idempotent
	// path, which is exactly the situation the race hides in.
	dropExtension(t, baseDSN, "pgcrypto")

	start := make(chan struct{})
	errs := make([]error, len(schemas))
	var wg sync.WaitGroup

	for i, schema := range schemas {
		dsn, err := withSearchPath(baseDSN, schema)
		if err != nil {
			t.Fatalf("scope dsn for %s: %v", schema, err)
		}
		db, err := open(dsn)
		if err != nil {
			t.Fatalf("open %s: %v", schema, err)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { _ = db.Close() }()
			<-start
			errs[i] = applySchemaWithDB(dsn, db)
		}()
	}

	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("schema %s: %v", schemas[i], err)
		}
	}
}

const schemaRaceWorkers = 8

// TestExtensionLivesOutsideTestSchemas pins the invariant the race fix rests
// on: pgcrypto must belong to the database, not to a test schema. An extension
// created inside a test schema is dropped when that schema is dropped, so the
// next preparation has to create it again -- which is what puts two
// preparations in a race in the first place.
func TestExtensionLivesOutsideTestSchemas(t *testing.T) {
	if os.Getenv("TEST_DATABASE_DSN") == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	baseDSN := os.Getenv("TEST_DATABASE_DSN")
	// Start from a database that does not have the extension, so the assertion
	// is about where preparation puts it rather than about what a previous run
	// happened to leave behind.
	dropExtension(t, baseDSN, "pgcrypto")

	schema := "ojtest_extschema"
	dsn, err := withSearchPath(baseDSN, schema)
	if err != nil {
		t.Fatalf("scope dsn: %v", err)
	}
	db, err := open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()
	defer dropSchemas(t, baseDSN, []string{schema})

	if err := applySchemaWithDB(dsn, db); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	var namespace string
	err = db.QueryRowContext(context.Background(), `
		SELECT n.nspname
		FROM pg_extension e
		JOIN pg_namespace n ON n.oid = e.extnamespace
		WHERE e.extname = 'pgcrypto'
	`).Scan(&namespace)
	if err != nil {
		t.Fatalf("read pgcrypto namespace: %v", err)
	}
	if namespace != "public" {
		t.Fatalf("pgcrypto lives in %q; a test schema is dropped after every test, "+
			"taking the extension with it", namespace)
	}
}

// TestDropSchemaKeepsDatabaseExtensions is the consequence stated directly:
// preparing a second schema must not disturb the extension a first one is
// relying on.
func TestDropSchemaKeepsDatabaseExtensions(t *testing.T) {
	if os.Getenv("TEST_DATABASE_DSN") == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	baseDSN := os.Getenv("TEST_DATABASE_DSN")
	first, second := "ojtest_keepa", "ojtest_keepb"
	defer dropSchemas(t, baseDSN, []string{first, second})

	for _, name := range []string{first, second} {
		dsn, err := withSearchPath(baseDSN, name)
		if err != nil {
			t.Fatalf("scope dsn for %s: %v", name, err)
		}
		db, err := open(dsn)
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		if err := applySchemaWithDB(dsn, db); err != nil {
			_ = db.Close()
			t.Fatalf("apply schema %s: %v", name, err)
		}
		_ = db.Close()
	}

	// Dropping the first schema happens after the second was prepared. The
	// function the second schema's defaults rely on must still be callable.
	fresh, err := open(baseDSN)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = fresh.Close() }()

	if _, err := fresh.ExecContext(context.Background(),
		`DROP SCHEMA `+quoteIdentifier(first)+` CASCADE`); err != nil {
		t.Fatalf("drop %s: %v", first, err)
	}

	var id string
	if err := fresh.QueryRowContext(context.Background(),
		`SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		t.Fatalf("gen_random_uuid() unavailable after dropping a test schema: %v", err)
	}
	if len(strings.TrimSpace(id)) != 36 {
		t.Fatalf("gen_random_uuid() returned %q", id)
	}
}

// dropExtension removes a database-scoped extension so a test can start from a
// state where it must be created. The schema that held it goes with it.
func dropExtension(t *testing.T, baseDSN, name string) {
	t.Helper()
	// The drop takes the extension-placement lock like every creator does. An
	// unlocked DROP EXTENSION CASCADE can wait on a concurrent placement's
	// catalog lock and then commit after that placement's own check — landing
	// the removal between another package's "extension is present" decision
	// and this test's assertion, which is exactly the no-rows failure this
	// test used to flake on.
	if err := withAdvisoryLock(baseDSN, sharedSchemaLockClassID, extensionLockObjectID, func(db *sql.DB) error {
		_, err := db.ExecContext(context.Background(),
			`DROP EXTENSION IF EXISTS `+quoteIdentifier(name)+` CASCADE`)
		return err
	}); err != nil {
		t.Fatalf("drop extension %s: %v", name, err)
	}
}

func dropSchemas(t *testing.T, baseDSN string, schemas []string) {
	t.Helper()
	db, err := open(baseDSN)
	if err != nil {
		t.Fatalf("open for cleanup: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, schema := range schemas {
		if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+quoteIdentifier(schema)+` CASCADE`); err != nil {
			t.Errorf("drop %s: %v", schema, err)
		}
	}
}

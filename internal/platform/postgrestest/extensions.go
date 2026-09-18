package postgrestest

import (
	"context"
	"database/sql"
	"fmt"
)

// extensionSchema is where the test database keeps its extensions.
//
// It has to be a schema that outlives a single test. Extensions are scoped to
// the database, but a test's schema is dropped when that test finishes, taking
// anything created inside it. An extension created in a test schema therefore
// disappears constantly, and every preparation has to create it again -- which
// is what puts two concurrent preparations into a race on the extension name.
const extensionSchema = "public"

// extensionLockObjectID serialises extension changes. It must differ from
// sharedSchemaLockObjectID: applySchemaWithDB runs inside that lock already, and
// an advisory lock is held per session, so re-acquiring it from a second
// connection blocks until the first one releases -- which it cannot do while it
// is waiting. The two lock ids are also always taken in the same order
// (schema, then extension), so a cycle cannot form.
const extensionLockObjectID = 260327

// testExtensions are the extensions the test schema depends on:
// pgcrypto provides gen_random_uuid(), which check_runs.run_id defaults to.
var testExtensions = []string{"pgcrypto"}

// ensureExtensions makes the database-scoped extensions available in
// extensionSchema.
//
// It reads before it locks, because the common case is that they are already
// there and taking a database-wide lock on every preparation would serialize
// the whole integration suite for nothing. The lock is only taken when
// something actually has to change, and the condition is re-checked inside it:
// another process may have created the extension while this one was waiting.
func ensureExtensions(ctx context.Context, dsn string) error {
	db, err := open(dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	pending, err := extensionsNeedingWork(ctx, db)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}

	return withAdvisoryLock(dsn, sharedSchemaLockClassID, extensionLockObjectID, func(db *sql.DB) error {
		pending, err := extensionsNeedingWork(ctx, db)
		if err != nil {
			return err
		}
		for _, name := range pending {
			if err := placeExtension(ctx, db, name); err != nil {
				return err
			}
		}
		return nil
	})
}

// extensionsNeedingWork reports which extensions are absent or living outside
// extensionSchema.
//
// An extension stranded in a dropped test schema breaks every other test:
// gen_random_uuid() stops resolving, because the schema holding it is no longer
// on anyone's search path. Treating "present but in the wrong place" as work to
// do makes such a database heal instead of failing obscurely.
func extensionsNeedingWork(ctx context.Context, db *sql.DB) ([]string, error) {
	var pending []string
	for _, name := range testExtensions {
		var namespace sql.NullString
		err := db.QueryRowContext(ctx, `
			SELECT n.nspname
			FROM pg_extension e
			JOIN pg_namespace n ON n.oid = e.extnamespace
			WHERE e.extname = $1
		`, name).Scan(&namespace)
		switch {
		case err == sql.ErrNoRows:
			pending = append(pending, name)
		case err != nil:
			return nil, fmt.Errorf("read extension %s: %w", name, err)
		case namespace.String != extensionSchema:
			pending = append(pending, name)
		}
	}
	return pending, nil
}

// placeExtension creates an extension in extensionSchema, moving it there when
// it already exists somewhere else.
func placeExtension(ctx context.Context, db *sql.DB, name string) error {
	quotedName := quoteIdentifier(name)
	quotedSchema := quoteIdentifier(extensionSchema)

	var exists bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = $1)`, name,
	).Scan(&exists); err != nil {
		return fmt.Errorf("check extension %s: %w", name, err)
	}
	if exists {
		if _, err := db.ExecContext(ctx,
			`ALTER EXTENSION `+quotedName+` SET SCHEMA `+quotedSchema); err != nil {
			return fmt.Errorf("move extension %s to %s: %w", name, extensionSchema, err)
		}
		return nil
	}

	if _, err := db.ExecContext(ctx,
		`CREATE EXTENSION IF NOT EXISTS `+quotedName+` SCHEMA `+quotedSchema); err != nil {
		return fmt.Errorf("create extension %s: %w", name, err)
	}
	return nil
}

// extensionPlacementLockKeys are the advisory lock coordinates placeExtension
// serializes on. migrate.EnsureRoles creates the same extension from its own
// package, which cannot import this one, so it carries mirrored copies; the
// drift test in this package fails if the two copies ever diverge, because a
// divergence puts two CREATE EXTENSION calls back into the race this lock
// exists to prevent.

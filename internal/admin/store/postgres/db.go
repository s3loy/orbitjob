package postgres

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/lib/pq"
)

// Open creates a PostgreSQL DB handle from DSN.
func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	return db, nil
}

// WithTenant begins a transaction and sets the PostgreSQL application variable
// app.tenant_id so that row-level security policies scoped to the current tenant
// are enforced. If setting the variable fails, the transaction is rolled back and
// the error is returned. The caller is responsible for committing the transaction.
func WithTenant(ctx context.Context, db *sql.DB, tenantID string) (*sql.Tx, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "SET LOCAL app.tenant_id = $1", tenantID); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

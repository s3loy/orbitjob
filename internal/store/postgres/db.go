package postgres

import (
	"database/sql"

	_ "github.com/lib/pq"
)

// Open creates a PostgreSQL DB handle from DSN.
func Open(dsn string) (*sql.DB, error) {
	return sql.Open("postgres", dsn)
}

package postgres

import (
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

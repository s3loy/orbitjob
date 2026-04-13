package postgres

import (
	"database/sql"
	"time"
)

type rowScanner interface {
	Scan(dest ...any) error
}

func nullTimePtr(in sql.NullTime) *time.Time {
	if !in.Valid {
		return nil
	}

	t := in.Time
	return &t
}

func nullStringPtr(in sql.NullString) *string {
	if !in.Valid {
		return nil
	}

	s := in.String
	return &s
}

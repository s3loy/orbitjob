package scan

import (
	"database/sql"
	"time"
)

// NullTimePtr converts a sql.NullTime to *time.Time.
// Returns nil when the value is not valid (SQL NULL).
func NullTimePtr(in sql.NullTime) *time.Time {
	if !in.Valid {
		return nil
	}

	t := in.Time
	return &t
}

// NullStringPtr converts a sql.NullString to *string.
// Returns nil when the value is not valid (SQL NULL).
func NullStringPtr(in sql.NullString) *string {
	if !in.Valid {
		return nil
	}

	s := in.String
	return &s
}

package handler

import "database/sql"

import internal "orbitjob/internal/core/app/execute/handler"

// NewPGNotify creates a new PostgreSQL NOTIFY handler.
func NewPGNotify(db *sql.DB) *PGNotify {
	return internal.NewPGNotify(db)
}

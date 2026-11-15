//go:build integration

package postgres

import (
	"database/sql"
	"net/url"
	"orbitjob/internal/platform/postgrestest"
	"testing"
)

func queryAdminConnection(t *testing.T, db *sql.DB) *sql.DB {
	t.Helper()
	var schema string
	if err := db.QueryRow("SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(postgrestest.DSN(t))
	if err != nil {
		t.Fatal("invalid DSN")
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	q.Set("options", "-c role=orbitjob_admin")
	u.RawQuery = q.Encode()
	admin, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal("open scoped admin")
	}
	t.Cleanup(func() { _ = admin.Close() })
	return admin
}

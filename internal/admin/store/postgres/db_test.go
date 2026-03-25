//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"orbitjob/internal/platform/postgrestest"
)

// TestDBOpenAndPing verifies that Open() returns a usable DB handle and the DSN
// can establish a real connection to PostgreSQl.
func TestDBOpenAndPing(t *testing.T) {
	dsn := postgrestest.DSN(t)

	db, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("PingContext() err = %v", err)
	}
}

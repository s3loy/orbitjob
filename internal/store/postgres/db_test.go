package postgres

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestDBOpenAndPing verifies that Open() returns a usable DB handle and the DSN
// can establish a real connection to PostgreSQl.
func TestDBOpenAndPing(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set yet")
	}

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

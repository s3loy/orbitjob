//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/domain/resource"
	"orbitjob/internal/platform/postgrestest"
)

// OpenStoreDB returns the per-test schema handle the harness prepared.
func OpenStoreDB(t *testing.T) *sql.DB {
	t.Helper()
	return postgrestest.Open(t)
}

// ctxStore bounds one test's statements; the harness deadline must never be
// the reason a leak assertion fails, only the assertion itself.
func ctxStore(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// The not-found path of Delete used to return without rolling back, pinning
// one pooled connection per call. The unit tests above pin the rollback on
// the transaction lifecycle; this test pins the effect that made the leak
// worth fixing: after repeated not-found deletes every connection must be
// back in the pool, none held open by an abandoned transaction.
//
// The pre-fix behavior is captured in sli_repository_leak_test.go, which
// failed with an unmet Rollback expectation before the fix; this test was
// written against the fixed tree, so only its green side exists.
func TestSLIRepository_DeleteNotFound_ReleasesConnections(t *testing.T) {
	db := OpenStoreDB(t)
	// One connection more than the number of calls, so every call can run
	// concurrently-free on its own connection and a leaked transaction is
	// visible as InUse rather than as a blocked BeginTx.
	const calls = 5
	db.SetMaxOpenConns(calls + 1)
	db.SetMaxIdleConns(calls + 1)

	repo := NewSLIRepository(db)
	ctx := ctxStore(t)

	// A tenant id that owns no SLIs: every call takes the zero-rows path that
	// used to leak.
	const tenantID = "01F8ZCC0NQ5WX9HHQ3P0R7D3LE"
	for i := 0; i < calls; i++ {
		err := repo.Delete(ctx, tenantID, "", int64(1000+i), 1)
		var notFound *resource.NotFoundError
		if !errors.As(err, &notFound) {
			t.Fatalf("call %d: expected *resource.NotFoundError, got %T: %v", i, err, err)
		}
	}

	if inUse := db.Stats().InUse; inUse != 0 {
		t.Errorf("%d connection(s) still in use after %d not-found deletes; "+
			"an abandoned transaction is holding pooled connections open", inUse, calls)
	}
}

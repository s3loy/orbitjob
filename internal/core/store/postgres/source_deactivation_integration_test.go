//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"orbitjob/internal/core/domain/revision"
	store "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/scan"
)

// Pause activation after ApplyRevision has acquired its source lock. In the
// first-projection case the new row is still invisible to other connections.
func TestSourceDeactivationWaitsForUncommittedProjection(t *testing.T) {
	for _, method := range []string{"uid-first-projection", "name-inactive-history", "inventory-inactive-history"} {
		t.Run(method, func(t *testing.T) {
			f := seedAdmissionWorkflow(t)
			source := scan.GenerateID()
			rev := revision.Revision{
				Identity:   revision.Identity{SourceMode: "kubernetes", SourceUID: source, Namespace: "admission-ns", Name: "deactivation-task"},
				Generation: 1, SpecHash: "hash-1", NormalizedSpec: `{"concurrencyPolicy":"Allow"}`, Actor: "review",
			}
			if method != "uid-first-projection" {
				repo := store.NewControlPlaneRepository(f.application)
				if _, err := repo.ApplyRevisionForTenant(t.Context(), f.tenant, rev); err != nil {
					t.Fatal(err)
				}
				// Model pre-control inactive history. A real deletion now writes an
				// irreversible tombstone and correctly refuses this UID's revival.
				if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_definition_revisions SET is_active=false WHERE tenant_id=$1 AND source_uid=$2`, f.tenant, source); err != nil {
					t.Fatal(err)
				}
				rev.Generation = 2
				rev.SpecHash = "hash-2"
			}
			projectionDB, projectionPID := admissionConnection(t, f)
			deactivationDB, deactivationPID := admissionConnection(t, f)
			if projectionPID == deactivationPID {
				t.Fatal("projection and deactivation reused one session")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			barrierKey := "revision-activation-test/" + source
			barrier := pauseRevisionActivation(t, ctx, f.owner, source, barrierKey)
			defer barrier.Close()
			defer func() {
				_, _ = barrier.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(hashtextextended($1,0))`, barrierKey)
			}()
			projected := make(chan error, 1)
			go func() {
				_, err := store.NewControlPlaneRepository(projectionDB).ApplyRevisionForTenant(ctx, f.tenant, rev)
				projected <- err
			}()
			waitForAdvisoryWaiter(t, ctx, f.owner, projectionPID, projected)
			var active int
			if err := f.owner.QueryRowContext(ctx, `SELECT count(*) FROM job_definition_revisions WHERE tenant_id=$1 AND source_uid=$2 AND is_active`, f.tenant, source).Scan(&active); err != nil || active != 0 {
				t.Fatalf("projection became visible before barrier release: active=%d err=%v", active, err)
			}
			deactivated := make(chan error, 1)
			go func() {
				repo := store.NewControlPlaneRepository(deactivationDB)
				var err error
				switch method {
				case "uid-first-projection":
					err = repo.DeactivateSource(ctx, f.tenant, "kubernetes", source)
				case "name-inactive-history":
					err = repo.DeactivateSourceByName(ctx, f.tenant, "kubernetes", rev.Identity.Namespace, rev.Identity.Name)
				default:
					err = repo.DeactivateSourcesMissing(ctx, f.tenant, "kubernetes", []string{f.source})
				}
				deactivated <- err
			}()
			// A query that returns an empty active set before acquiring the source
			// lock finishes here and fails instead of relying on a timing guess.
			waitForAdvisoryWaiter(t, ctx, f.owner, deactivationPID, deactivated)
			if _, err := barrier.ExecContext(ctx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, barrierKey); err != nil {
				t.Fatal(err)
			}
			for name, result := range map[string]<-chan error{"projection": projected, "deactivation": deactivated} {
				select {
				case err := <-result:
					if err != nil {
						t.Fatalf("%s: %v", name, err)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if err := f.owner.QueryRowContext(ctx, `SELECT count(*) FROM job_definition_revisions WHERE tenant_id=$1 AND source_uid=$2 AND is_active`, f.tenant, source).Scan(&active); err != nil || active != 0 {
				t.Fatalf("deactivation missed committed activation: active=%d err=%v", active, err)
			}
		})
	}
}

func pauseRevisionActivation(t *testing.T, ctx context.Context, db *sql.DB, source, key string) *sql.Conn {
	t.Helper()
	// Both trigger arguments are generated locally, never untrusted SQL input.
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE FUNCTION pause_test_revision_activation() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
  IF NEW.source_uid=TG_ARGV[0] AND NEW.is_active THEN
   PERFORM pg_advisory_xact_lock(hashtextextended(TG_ARGV[1],0));
  END IF;
  RETURN NEW;
 END $$;
 CREATE TRIGGER pause_test_revision_activation BEFORE UPDATE OF is_active ON job_definition_revisions
 FOR EACH ROW EXECUTE FUNCTION pause_test_revision_activation('%s','%s')`, source, key)); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, key); err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	return conn
}

func waitForAdvisoryWaiter(t *testing.T, ctx context.Context, db *sql.DB, pid int, result <-chan error) {
	t.Helper()
	for {
		select {
		case err := <-result:
			t.Fatalf("session %d completed before required source/barrier lock wait: %v", pid, err)
		default:
		}
		var waiting bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND locktype='advisory' AND NOT granted)`, pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

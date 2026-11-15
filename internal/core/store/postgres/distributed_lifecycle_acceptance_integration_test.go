//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/lib/pq"
	"orbitjob/internal/core/domain/workflow"
	store "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/scan"
)

func waitLifecycleSourceGraph(t *testing.T, ctx context.Context, db *sql.DB, lifecyclePID, holderPID int, first, last string, done <-chan error) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			t.Fatalf("lifecycle completed before acquiring the required lock graph: %v", err)
		default:
		}
		var graph bool
		if err := db.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND locktype='advisory' AND granted
  AND classid=((hashtextextended($3,0)>>32)&4294967295)::oid
  AND objid=(hashtextextended($3,0)&4294967295)::oid AND objsubid=1)
 AND EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND locktype='advisory' AND NOT granted
  AND classid=((hashtextextended($4,0)>>32)&4294967295)::oid
  AND objid=(hashtextextended($4,0)&4294967295)::oid AND objsubid=1)
 AND $2=ANY(pg_blocking_pids($1))`, lifecyclePID, holderPID, first, last).Scan(&graph); err != nil {
			t.Fatal(err)
		}
		if graph {
			t.Logf("lifecycle PID %d holds sorted-first source and waits for PID %d on sorted-last source", lifecyclePID, holderPID)
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestAdmissionAcceptanceLifecycleSortedSourcesDoNotReverseParentLocks(t *testing.T) {
	for _, method := range []string{"by-name", "inventory"} {
		t.Run(method, func(t *testing.T) {
			f := seedAdmissionWorkflow(t)
			second := scan.GenerateID()
			if _, err := f.owner.ExecContext(t.Context(), `INSERT INTO job_definition_revisions
 (tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,actor,is_active)
 VALUES($1,'kubernetes',$2,'admission-ns','task',1,repeat('a',64),'{"concurrencyPolicy":"Allow"}','acceptance',false)`, f.tenant, second); err != nil {
				t.Fatal(err)
			}
			uids := []string{f.source, second}
			sort.Strings(uids)
			// The sorted-last UID is historical/inactive but must still be locked.
			if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_definition_revisions SET is_active=(source_uid=$2) WHERE tenant_id=$1 AND source_mode='kubernetes'`, f.tenant, uids[0]); err != nil {
				t.Fatal(err)
			}
			f.source = uids[0]
			if err := f.owner.QueryRowContext(t.Context(), `SELECT id FROM job_definition_revisions WHERE tenant_id=$1 AND source_uid=$2`, f.tenant, f.source).Scan(&f.revision); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			holderDB, holderPID := admissionConnection(t, f)
			lifecycleDB, lifecyclePID := admissionConnection(t, f)
			cancelDB, cancelPID := admissionConnection(t, f)
			stepDB, stepPID := admissionConnection(t, f)
			pids := []int{holderPID, lifecyclePID, cancelPID, stepPID}
			seen := map[int]bool{}
			for _, pid := range pids {
				if seen[pid] {
					t.Fatal("lifecycle race reused one physical session")
				}
				seen[pid] = true
			}
			holder, err := holderDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = holder.Rollback() }()
			first, last := f.tenant+"/kubernetes/"+uids[0], f.tenant+"/kubernetes/"+uids[1]
			if _, err := holder.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, last); err != nil {
				t.Fatal(err)
			}
			deactivated := make(chan error, 1)
			go func() {
				repo := store.NewControlPlaneRepository(lifecycleDB)
				if method == "by-name" {
					deactivated <- repo.DeactivateSourceByName(ctx, f.tenant, "kubernetes", "admission-ns", "task")
				} else {
					deactivated <- repo.DeactivateSourcesMissing(ctx, f.tenant, "kubernetes", nil)
				}
			}()
			waitLifecycleSourceGraph(t, ctx, f.owner, lifecyclePID, holderPID, first, last, deactivated)
			var active int
			if err := f.owner.QueryRowContext(ctx, `SELECT count(*) FROM job_definition_revisions WHERE tenant_id=$1 AND source_mode='kubernetes' AND is_active`, f.tenant).Scan(&active); err != nil || active != 1 {
				t.Fatalf("lifecycle wrote before holding all source locks: active=%d err=%v", active, err)
			}
			// This completes while lifecycle still holds first and waits for last.
			// A source -> parent lock acquisition would make the later step cycle.
			if changed, err := store.NewWorkflowRunRepository(cancelDB).UpdatePhase(ctx, f.tenant, f.parent.ID, workflow.PhaseCancelRequested, nil); err != nil || !changed {
				t.Fatalf("parent cancel could not commit independently: changed=%t err=%v", changed, err)
			}
			stepDone := make(chan admissionResult, 1)
			go func() { stepDone <- acceptanceStep(f, f.parent.ID, 1)(ctx, stepDB) }()
			acceptanceWaiters(t, ctx, f.owner, []int{stepPID}, stepDone)
			var blockedByLifecycle bool
			if err := f.owner.QueryRowContext(ctx, `SELECT $1=ANY(pg_blocking_pids($2))`, lifecyclePID, stepPID).Scan(&blockedByLifecycle); err != nil || !blockedByLifecycle {
				t.Fatalf("step did not wait behind lifecycle's source lock: blocked=%t err=%v", blockedByLifecycle, err)
			}
			if err := holder.Commit(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-deactivated:
				if err != nil {
					t.Fatalf("lifecycle failed/deadlocked with parent-owning step: %v", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case result := <-stepDone:
				if result.created || result.id != 0 || !errors.Is(result.err, store.ErrRunChanged) {
					t.Fatalf("canceled parent admitted a new step after the source wait: %+v", result)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var revisions, audits, leakedLocks int
			if err := f.owner.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE is_active) FROM job_definition_revisions WHERE tenant_id=$1 AND source_uid=ANY($2)`, f.tenant, pq.Array(uids)).Scan(&revisions, &active); err != nil || revisions != 2 || active != 0 {
				t.Fatalf("lifecycle lost history or missed a target: revisions=%d active=%d err=%v", revisions, active, err)
			}
			if err := f.owner.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND event_type='revision.deactivated'`, f.tenant).Scan(&audits); err != nil || audits != 1 {
				t.Fatalf("active revision deactivation audit=%d err=%v", audits, err)
			}
			if err := f.owner.QueryRowContext(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND pid=ANY($1)`, pq.Array(pids)).Scan(&leakedLocks); err != nil || leakedLocks != 0 {
				t.Fatalf("lifecycle race leaked advisory locks: count=%d err=%v", leakedLocks, err)
			}
			acceptanceCounts(t, f, 0)
			parent, found, err := store.NewWorkflowRunRepository(cancelDB).RunByID(ctx, f.tenant, f.parent.ID)
			if err != nil || !found || parent.Phase != workflow.PhaseCancelRequested {
				t.Fatalf("lifecycle changed committed parent cancel: parent=%+v found=%t err=%v", parent, found, err)
			}
			t.Logf("completed %s lifecycle/parent/step graph with physical sessions %s", method, fmt.Sprint(pids))
		})
	}
}

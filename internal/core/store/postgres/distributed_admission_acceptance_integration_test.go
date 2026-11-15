//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/workflow"
	store "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/scan"
)

type acceptanceAdmission func(context.Context, *sql.DB) admissionResult

func acceptanceOccurrence(f workflowFenceFixture, key int, trigger jobrun.Trigger) acceptanceAdmission {
	return func(ctx context.Context, db *sql.DB) admissionResult {
		row, created, err := store.NewControlPlaneRepository(db).CreateOccurrenceForTenant(ctx, f.tenant,
			jobrun.JobRun{SourceUID: f.source, RevisionID: fmt.Sprint(f.revision), OccurrenceKey: fmt.Sprintf("%064x", key), Trigger: trigger}, 1, "acceptance")
		return admissionResult{row.ID, created, err}
	}
}

func acceptanceStep(f workflowFenceFixture, parent int64, key int) acceptanceAdmission {
	return func(ctx context.Context, db *sql.DB) admissionResult {
		row, created, err := store.NewWorkflowRunRepository(db).CreateStepForTenant(ctx, f.tenant, parent,
			workflow.StepRun{SourceUID: f.source, RevisionID: f.revision, OccurrenceKey: fmt.Sprintf("%064x", key), Trigger: jobrun.Workflow}, 1, "acceptance")
		return admissionResult{row.ID, created, err}
	}
}

func acceptanceSourceLock(t *testing.T, ctx context.Context, f workflowFenceFixture) *sql.Tx {
	t.Helper()
	tx, err := f.owner.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, f.tenant+"/kubernetes/"+f.source); err != nil {
		t.Fatal(err)
	}
	return tx
}

// Poll observed PostgreSQL waiters; elapsed time never decides that a contender
// reached the barrier. Each connection has already verified its runtime role and
// hostile Repeatable Read default in admissionConnection.
func acceptanceWaiters(t *testing.T, ctx context.Context, db *sql.DB, pids []int, results <-chan admissionResult) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case result := <-results:
			t.Fatalf("admission completed before the source lock barrier: %+v", result)
		default:
		}
		var waiting int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted AND pid=ANY($1)`, pq.Array(pids)).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting == len(pids) {
			t.Logf("source admission barrier reached by physical sessions %v", pids)
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}

func acceptanceRace(t *testing.T, f workflowFenceFixture, calls ...acceptanceAdmission) []admissionResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	holder := acceptanceSourceLock(t, ctx, f)
	defer func() { _ = holder.Rollback() }()
	results := make(chan admissionResult, len(calls))
	pids := make([]int, 0, len(calls))
	seen := map[int]bool{}
	for _, call := range calls {
		db, pid := admissionConnection(t, f)
		if seen[pid] {
			t.Fatal("contenders reused a physical session")
		}
		seen[pid] = true
		pids = append(pids, pid)
		go func() { results <- call(ctx, db) }()
	}
	acceptanceWaiters(t, ctx, f.owner, pids, results)
	if err := holder.Commit(); err != nil {
		t.Fatal(err)
	}
	out := make([]admissionResult, 0, len(calls))
	for range calls {
		select {
		case result := <-results:
			out = append(out, result)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	return out
}

func acceptanceCounts(t *testing.T, f workflowFenceFixture, want int) {
	t.Helper()
	var runs, audits int
	if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM job_run_control_plane WHERE tenant_id=$1 AND source_uid=$2`, f.tenant, f.source).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND event_type='job_run.created' AND diff->>'source_uid'=$2`, f.tenant, f.source).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if runs != want || audits != want {
		t.Fatalf("source rows=%d creation audits=%d want=%d", runs, audits, want)
	}
}

func acceptancePolicy(t *testing.T, f workflowFenceFixture, policy string) {
	t.Helper()
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_definition_revisions SET normalized_spec=jsonb_set(normalized_spec,'{concurrencyPolicy}',to_jsonb($1::text)) WHERE id=$2`, policy, f.revision); err != nil {
		t.Fatal(err)
	}
}

func TestAdmissionAcceptanceCronManualAndTwoWorkflowsShareForbidSlot(t *testing.T) {
	f := seedAdmissionWorkflow(t)
	second := f.parent
	second.ID, second.OccurrenceKey = 0, strings.Repeat("f", 64)
	parent, created, err := store.NewWorkflowRunRepository(f.application).CreateRunForTenant(t.Context(), f.tenant, second)
	if err != nil || !created {
		t.Fatalf("second workflow parent: created=%t err=%v", created, err)
	}
	acceptancePolicy(t, f, "Forbid")
	results := acceptanceRace(t, f,
		acceptanceOccurrence(f, 1, jobrun.Schedule), acceptanceOccurrence(f, 2, jobrun.Manual),
		acceptanceStep(f, f.parent.ID, 3), acceptanceStep(f, parent.ID, 4))
	admitted, blocked := 0, 0
	for _, result := range results {
		switch {
		case result.err == nil && result.created && result.id > 0:
			admitted++
		case errors.Is(result.err, jobrun.ErrAdmissionBlocked) && !result.created && result.id == 0:
			blocked++
		default:
			t.Fatalf("unexpected contender result: %+v", result)
		}
	}
	if admitted != 1 || blocked != 3 {
		t.Fatalf("admitted=%d blocked=%d", admitted, blocked)
	}
	acceptanceCounts(t, f, 1)
}

func TestAdmissionAcceptanceWorkflowRootAndTaskGatesRemainSeparate(t *testing.T) {
	f := seedAdmissionWorkflow(t)
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_definition_revisions SET normalized_spec='{"concurrencyPolicy":"Forbid"}' WHERE id=$1`, f.parent.RevisionID); err != nil {
		t.Fatal(err)
	}
	acceptancePolicy(t, f, "Forbid")
	root := f.parent
	root.ID, root.OccurrenceKey = 0, strings.Repeat("f", 64)
	if _, created, err := store.NewWorkflowRunRepository(f.application).CreateRunForTenant(t.Context(), f.tenant, root); created || !errors.Is(err, jobrun.ErrAdmissionBlocked) {
		t.Fatalf("root gate: created=%t err=%v", created, err)
	}
	manual := acceptanceOccurrence(f, 1, jobrun.Manual)(t.Context(), f.application)
	if manual.err != nil || !manual.created {
		t.Fatalf("workflow root incorrectly occupied task slot: %+v", manual)
	}
	if step := acceptanceStep(f, f.parent.ID, 2)(t.Context(), f.application); step.created || !errors.Is(step.err, jobrun.ErrAdmissionBlocked) {
		t.Fatalf("step bypassed busy task gate: %+v", step)
	}
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET phase='Canceled' WHERE id=$1`, manual.id); err != nil {
		t.Fatal(err)
	}
	if step := acceptanceStep(f, f.parent.ID, 2)(t.Context(), f.application); !step.created || step.err != nil {
		t.Fatalf("root slot incorrectly blocked admitted parent's task: %+v", step)
	}
	acceptanceCounts(t, f, 2)
}

func TestAdmissionAcceptanceAllowOverlapsAfterContendedLock(t *testing.T) {
	f := seedAdmissionWorkflow(t)
	for _, result := range acceptanceRace(t, f, acceptanceOccurrence(f, 1, jobrun.Schedule),
		acceptanceOccurrence(f, 2, jobrun.Manual), acceptanceStep(f, f.parent.ID, 3)) {
		if result.err != nil || !result.created || result.id < 1 {
			t.Fatalf("Allow serialized work into a false block: %+v", result)
		}
	}
	acceptanceCounts(t, f, 3)
}

func TestAdmissionAcceptanceOtherSourcesProgressWhileOneSourceWaits(t *testing.T) {
	for _, otherTenant := range []bool{false, true} {
		t.Run(fmt.Sprintf("other-tenant=%t", otherTenant), func(t *testing.T) {
			f := seedAdmissionWorkflow(t)
			acceptancePolicy(t, f, "Forbid")
			other := f
			other.source = scan.GenerateID()
			if otherTenant {
				other.tenant = scan.GenerateID()
				if _, err := f.owner.ExecContext(t.Context(), `INSERT INTO tenants(id,slug,name) VALUES($1,$2,'independent source')`, other.tenant, "independent-"+other.tenant); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.owner.QueryRowContext(t.Context(), `INSERT INTO job_definition_revisions
 (tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,actor,is_active)
 VALUES($1,'kubernetes',$2,'admission-ns','independent',1,repeat('a',64),'{"concurrencyPolicy":"Forbid"}','acceptance',true) RETURNING id`, other.tenant, other.source).Scan(&other.revision); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			holder := acceptanceSourceLock(t, ctx, f)
			defer func() { _ = holder.Rollback() }()
			blockedDB, blockedPID := admissionConnection(t, f)
			independentDB, independentPID := admissionConnection(t, f)
			if blockedPID == independentPID {
				t.Fatal("independent sources reused one physical session")
			}
			waiting := make(chan admissionResult, 1)
			go func() { waiting <- acceptanceOccurrence(f, 1, jobrun.Manual)(ctx, blockedDB) }()
			acceptanceWaiters(t, ctx, f.owner, []int{blockedPID}, waiting)
			result := acceptanceOccurrence(other, 1, jobrun.Manual)(ctx, independentDB)
			if result.err != nil || !result.created {
				t.Fatalf("unrelated source failed before releasing first source: %+v", result)
			}
			acceptanceWaiters(t, ctx, f.owner, []int{blockedPID}, waiting)
			acceptanceCounts(t, other, 1)
			if err := holder.Commit(); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-waiting:
				if result.err != nil || !result.created {
					t.Fatalf("first source did not recover: %+v", result)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			acceptanceCounts(t, f, 1)
			if otherTenant {
				if _, found, err := store.NewControlPlaneRepository(independentDB).RunByOccurrence(ctx, other.tenant, f.source, fmt.Sprintf("%064x", 1)); err != nil || found {
					t.Fatalf("cross-tenant run became visible: found=%t err=%v", found, err)
				}
			}
		})
	}
}

type acceptanceReplacePublisher struct {
	db          *sql.DB
	key         string
	canceled    []v1alpha1.RunIdentity
	lockFailure error
}

func (*acceptanceReplacePublisher) Publish(context.Context, v1alpha1.JobRun) error { return nil }

func (p *acceptanceReplacePublisher) RequestCancel(ctx context.Context, _, _ string, identities ...v1alpha1.RunIdentity) error {
	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		p.lockFailure = err
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var free bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))`, p.key).Scan(&free); err == nil && !free {
		err = errors.New("cancel callback ran while admission source lock was held")
	}
	if err != nil {
		p.lockFailure = err
		return err
	}
	p.canceled = append(p.canceled, identities...)
	return nil
}

func TestAdmissionAcceptanceReplaceWaitsForTerminalAndDoesNotExpandManualOrStep(t *testing.T) {
	f := seedAdmissionWorkflow(t)
	for i, phase := range []jobrun.Phase{jobrun.Running, jobrun.ExecutionUnknown, jobrun.CancelUnknown} {
		result := acceptanceOccurrence(f, i+1, jobrun.Manual)(t.Context(), f.application)
		if result.err != nil || !result.created {
			t.Fatalf("seed open run: %+v", result)
		}
		if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET phase=$1 WHERE id=$2`, phase, result.id); err != nil {
			t.Fatal(err)
		}
	}
	acceptancePolicy(t, f, "Replace")
	for _, result := range []admissionResult{
		acceptanceOccurrence(f, 4, jobrun.Manual)(t.Context(), f.application),
		acceptanceStep(f, f.parent.ID, 5)(t.Context(), f.application),
	} {
		if result.err != nil || !result.created {
			t.Fatalf("Replace added a manual/step concurrency restriction: %+v", result)
		}
	}
	var changed int
	if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM job_run_control_plane WHERE tenant_id=$1 AND phase='CancelRequested'`, f.tenant).Scan(&changed); err != nil || changed != 0 {
		t.Fatalf("manual/step introduced cancellation: rows=%d err=%v", changed, err)
	}
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_definition_revisions SET normalized_spec=normalized_spec || '{"schedule":"* * * * *"}'::jsonb WHERE id=$1`, f.revision); err != nil {
		t.Fatal(err)
	}
	repo := store.NewControlPlaneRepository(f.application)
	publisher := &acceptanceReplacePublisher{db: f.owner, key: f.tenant + "/kubernetes/" + f.source}
	var activatedAt time.Time
	if err := f.owner.QueryRowContext(t.Context(), `SELECT COALESCE(activated_at,created_at) FROM job_definition_revisions WHERE id=$1`, f.revision).Scan(&activatedAt); err != nil {
		t.Fatal(err)
	}
	// The due minute must follow revision activation; the current wall-clock
	// minute usually predates this fixture's newly inserted revision.
	now := activatedAt.UTC().Truncate(time.Minute).Add(90 * time.Second)
	scheduler := controlplane.Scheduler{Revisions: repo, Runs: repo, Publisher: publisher, Tenants: []string{f.tenant}, Now: func() time.Time { return now }}
	if created, err := scheduler.Tick(t.Context()); err != nil || created != 0 || publisher.lockFailure != nil || len(publisher.canceled) != 5 {
		t.Fatalf("Replace before confirmed terminal: created=%d cancels=%d callback=%v err=%v", created, len(publisher.canceled), publisher.lockFailure, err)
	}
	identities := map[string]bool{}
	for _, identity := range publisher.canceled {
		if identity.SourceUID != f.source || identity.RevisionID != f.revision || identities[identity.OccurrenceKey] {
			t.Fatalf("Replace lost or duplicated cancellation identity: %+v", identity)
		}
		identities[identity.OccurrenceKey] = true
	}
	acceptanceCounts(t, f, 5)
	// A cancel request itself has supplied no execution fact. The uncertain and
	// running phases must remain open until a separately confirmed terminal fact.
	var preserved int
	if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM job_run_control_plane WHERE tenant_id=$1 AND source_uid=$2 AND phase IN ('Running','ExecutionUnknown','CancelUnknown')`, f.tenant, f.source).Scan(&preserved); err != nil || preserved != 3 {
		t.Fatalf("cancel request released an open/unknown slot: preserved=%d err=%v", preserved, err)
	}
	// Inject only the ledger fact here; this test does not claim Kubernetes stop
	// proof. The separate cancellation suites verify how that fact is established.
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET phase='Canceled' WHERE tenant_id=$1 AND source_uid=$2`, f.tenant, f.source); err != nil {
		t.Fatal(err)
	}
	if created, err := scheduler.Tick(t.Context()); err != nil || created != 1 || len(publisher.canceled) != 5 {
		t.Fatalf("confirmed terminal did not release replacement: created=%d cancels=%d err=%v", created, len(publisher.canceled), err)
	}
	acceptanceCounts(t, f, 6)
}

func TestAdmissionAcceptanceCanceledWaiterAndAuditRollbackLeaveNoState(t *testing.T) {
	for _, failure := range []string{"cancel_source_waiter", "audit_insert_failure"} {
		t.Run(failure, func(t *testing.T) {
			f := seedAdmissionWorkflow(t)
			acceptancePolicy(t, f, "Forbid")
			db, originalPID := admissionConnection(t, f)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			call := acceptanceOccurrence(f, 1, jobrun.Manual)
			if failure == "cancel_source_waiter" {
				holder := acceptanceSourceLock(t, ctx, f)
				defer func() { _ = holder.Rollback() }()
				waitCtx, stopWaiter := context.WithCancel(ctx)
				defer stopWaiter()
				done := make(chan admissionResult, 1)
				go func() { done <- call(waitCtx, db) }()
				acceptanceWaiters(t, ctx, f.owner, []int{originalPID}, done)
				stopWaiter()
				select {
				case result := <-done:
					var canceledSQL *pq.Error
					canceled := errors.Is(result.err, context.Canceled) || (errors.As(result.err, &canceledSQL) && canceledSQL.Code == "57014")
					if !canceled || result.created || result.id != 0 {
						t.Fatalf("canceled lock waiter left successful state: %+v", result)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if err := holder.Rollback(); err != nil {
					t.Fatal(err)
				}
			} else {
				// Abort after the run INSERT, at its transactional audit INSERT.
				// The trigger lives only in this fixture's disposable schema.
				if _, err := f.owner.ExecContext(ctx, fmt.Sprintf(`CREATE FUNCTION fail_acceptance_audit() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
  IF NEW.tenant_id=TG_ARGV[0] AND NEW.event_type='job_run.created' THEN
   RAISE EXCEPTION 'acceptance audit failure';
  END IF;
  RETURN NEW;
 END $$;
 CREATE TRIGGER fail_acceptance_audit BEFORE INSERT ON audit_events
 FOR EACH ROW EXECUTE FUNCTION fail_acceptance_audit('%s')`, f.tenant)); err != nil {
					t.Fatal(err)
				}
				result := call(ctx, db)
				if result.err == nil || !strings.Contains(result.err.Error(), "acceptance audit failure") || result.created || result.id != 0 {
					t.Fatalf("audit failure was not atomic: %+v", result)
				}
				if _, err := f.owner.ExecContext(ctx, `DROP TRIGGER fail_acceptance_audit ON audit_events`); err != nil {
					t.Fatal(err)
				}
			}
			acceptanceCounts(t, f, 0)
			var currentPID int
			var tenantSetting, isolation string
			if err := db.QueryRowContext(ctx, `SELECT pg_backend_pid(),COALESCE(current_setting('app.tenant_id',true),''),current_setting('default_transaction_isolation')`).Scan(&currentPID, &tenantSetting, &isolation); err != nil {
				t.Fatal(err)
			}
			if tenantSetting != "" || isolation != "repeatable read" || (failure == "audit_insert_failure" && currentPID != originalPID) {
				t.Fatalf("transaction contaminated pooled session: old/new PID=%d/%d tenant=%q isolation=%q", originalPID, currentPID, tenantSetting, isolation)
			}
			var leakedLocks int
			if err := f.owner.QueryRowContext(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND pid=ANY($1)`, pq.Array([]int{originalPID, currentPID})).Scan(&leakedLocks); err != nil || leakedLocks != 0 {
				t.Fatalf("aborted admission leaked a source lock: count=%d err=%v", leakedLocks, err)
			}
			result := call(ctx, db)
			if result.err != nil || !result.created || result.id < 1 {
				t.Fatalf("same occurrence could not be admitted after rollback: %+v", result)
			}
			acceptanceCounts(t, f, 1)
		})
	}
}

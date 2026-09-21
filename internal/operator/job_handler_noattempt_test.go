package operator

import (
	"context"
	"errors"
	"fmt"
	"testing"

	corepostgres "orbitjob/internal/core/store/postgres"
)

// A Job whose attempt row is gone — pruned by retention, wiped by a loadtest
// reset, or left behind by a previous install — can never gain one, so
// re-observing it must drop the key instead of requeueing an error forever
// (docs/known-issues.md, "A labeled Job without an owning attempt is
// reconciled forever").
func TestReconcileJobDropsJobWithoutOwningAttempt(t *testing.T) {
	runs := &fakeRuns{observedErr: fmt.Errorf("record job oj-x-1: %w", corepostgres.ErrNoAttempt)}
	rt := Runtime{
		Runs:    runs,
		Tenants: NamespaceTenantResolver{Tenants: map[string]string{"finance": "finance"}},
	}
	if err := rt.ReconcileJob(context.Background(), observedJob(t, "oj-x-1-1", "Succeeded")); err != nil {
		t.Fatalf("a Job no attempt owns must be dropped, not requeued: %v", err)
	}
	if len(runs.phases) != 0 {
		t.Fatalf("no run phase may be written for an ownerless Job, got %d", len(runs.phases))
	}
}

// The sentinel matches when the store returns it bare, too.
func TestReconcileJobDropsBareErrNoAttempt(t *testing.T) {
	rt := Runtime{
		Runs:    &fakeRuns{observedErr: corepostgres.ErrNoAttempt},
		Tenants: NamespaceTenantResolver{Tenants: map[string]string{"finance": "finance"}},
	}
	if err := rt.ReconcileJob(context.Background(), observedJob(t, "oj-x-1-1", "Succeeded")); err != nil {
		t.Fatalf("a bare ErrNoAttempt must still be dropped, not requeued: %v", err)
	}
}

// Only the ownerless-Job refusal is droppable. Every other store failure is a
// real reconcile failure and must keep surfacing.
func TestReconcileJobStillSurfacesOtherStoreErrors(t *testing.T) {
	rt := Runtime{
		Runs:    &fakeRuns{observedErr: errors.New("connection refused")},
		Tenants: NamespaceTenantResolver{Tenants: map[string]string{"finance": "finance"}},
	}
	if err := rt.ReconcileJob(context.Background(), observedJob(t, "oj-x-1-1", "Succeeded")); err == nil {
		t.Fatal("expected a store failure other than ErrNoAttempt to surface")
	}
}

func TestReconcileJobSurfacesAttemptOwnershipMismatch(t *testing.T) {
	runs := &fakeRuns{observedErr: corepostgres.ErrAttemptOwnership}
	rt := Runtime{
		Runs:    runs,
		Tenants: NamespaceTenantResolver{Tenants: map[string]string{"finance": "finance"}},
	}
	err := rt.ReconcileJob(context.Background(), observedJob(t, "oj-x-1-1", "Running"))
	if !errors.Is(err, corepostgres.ErrAttemptOwnership) {
		t.Fatalf("got %v, want ErrAttemptOwnership", err)
	}
	if len(runs.phases) != 0 {
		t.Fatalf("no run phase may be written for a replacement Job, got %d", len(runs.phases))
	}
}

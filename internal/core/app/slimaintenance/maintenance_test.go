package slimaintenance

import (
	"context"
	"errors"
	"testing"
	"time"
)

type pruneFunc func(context.Context, string, time.Time, Cursor, int) (Page, error)

func (f pruneFunc) PruneRetiredEventsPage(ctx context.Context, tenant string, before time.Time, cursor Cursor, limit int) (Page, error) {
	return f(ctx, tenant, before, cursor, limit)
}

func TestSweepContinuesAfterTenantTimeoutAndPreservesPartialCursor(t *testing.T) {
	calls := 0
	m := Maintainer{Tenants: []string{"suspended-tenant", "next-tenant"}, TenantTimeout: 10 * time.Millisecond, BatchSize: 1}
	m.Pruner = pruneFunc(func(ctx context.Context, tenant string, _ time.Time, cursor Cursor, limit int) (Page, error) {
		calls++
		if limit != 1 {
			t.Fatalf("limit=%d", limit)
		}
		if tenant == "suspended-tenant" {
			if calls > 2 && cursor.PlanRunID != 7 {
				t.Fatalf("lost partial cursor: %+v", cursor)
			}
			<-ctx.Done()
			return Page{Next: Cursor{PlanRunID: 7}, Counts: Counts{ScannedPlans: 1}}, ctx.Err()
		}
		return Page{Next: Cursor{PlanRunID: 9}, Counts: Counts{RemovedPlans: 1}}, nil
	})
	for range 2 {
		counts, err := m.Sweep(t.Context())
		if !errors.Is(err, context.DeadlineExceeded) || counts.RemovedPlans != 1 || counts.ScannedPlans != 1 {
			t.Fatalf("counts=%+v err=%v", counts, err)
		}
	}
	if calls != 4 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestSweepStopsBeforeNextTenantOnLeadershipCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	m := Maintainer{Tenants: []string{"first", "second"}, Pruner: pruneFunc(func(context.Context, string, time.Time, Cursor, int) (Page, error) {
		calls++
		cancel()
		return Page{}, nil
	})}
	_, err := m.Sweep(ctx)
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

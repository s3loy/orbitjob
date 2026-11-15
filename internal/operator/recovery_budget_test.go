package operator

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/domain/jobrun"
)

type budgetRecoveryRuns struct {
	*recoveryRuns
	blocked string
	visited []string
}

func (s *budgetRecoveryRuns) ExecutionRecoveriesAfter(ctx context.Context, tenant string, _ int64, _ int) ([]jobrun.Execution, error) {
	s.visited = append(s.visited, tenant)
	if tenant == s.blocked {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	e := s.e
	e.Run.Phase = jobrun.Succeeded
	return []jobrun.Execution{e}, nil
}

func TestRecoveryTenantBudgetLetsOtherTenantsAdvance(t *testing.T) {
	r, s, _ := recoveryFixture(t)
	store := &budgetRecoveryRuns{recoveryRuns: s, blocked: "slow-tenant"}
	r.Runs = store
	r.ExecutionRecoveryTenantBudget = 10 * time.Millisecond
	cursor := &ExecutionRecoveryCursor{}
	count, err := r.RecoverExecutions(t.Context(), []string{"slow-tenant", "ready-tenant"}, 10, cursor)
	if count != 1 || !errors.Is(err, context.DeadlineExceeded) || len(store.visited) != 2 || cursor.After["ready-tenant"] != s.e.Run.ID {
		t.Fatalf("count=%d err=%v visited=%v cursor=%v", count, err, store.visited, cursor.After)
	}
	if jobrun.Terminal(s.stored.Phase) {
		t.Fatal("slice timeout invented a workload outcome")
	}
}

func TestRecoveryRotatesAfterGlobalDeadline(t *testing.T) {
	r, s, _ := recoveryFixture(t)
	store := &budgetRecoveryRuns{recoveryRuns: s, blocked: "slow-tenant"}
	r.Runs = store
	r.ExecutionRecoveryTenantBudget = 10 * time.Millisecond
	cursor := &ExecutionRecoveryCursor{}
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	if _, err := r.RecoverExecutions(ctx, []string{"slow-tenant", "ready-tenant"}, 10, cursor); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	store.visited = nil
	if _, err := r.RecoverExecutions(t.Context(), []string{"slow-tenant", "ready-tenant"}, 10, cursor); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if len(store.visited) != 2 || store.visited[0] != "ready-tenant" {
		t.Fatalf("deadline starved later tenant: %v", store.visited)
	}
}

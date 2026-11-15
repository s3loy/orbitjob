package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/platform/scan"
)

type isolationReaderFunc func(context.Context, string) ([]jobrun.IsolationSummary, error)

func (f isolationReaderFunc) ExecutionIsolationSummary(ctx context.Context, tenant string) ([]jobrun.IsolationSummary, error) {
	return f(ctx, tenant)
}

func TestExecutionIsolationInventoryIncludesEveryConfiguredTenantOnce(t *testing.T) {
	now := time.Now().UTC()
	active, inactive := scan.GenerateID(), scan.GenerateID()
	calls := map[string]int{}
	reader := isolationReaderFunc(func(ctx context.Context, tenant string) ([]jobrun.IsolationSummary, error) {
		calls[tenant]++
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 2*time.Second {
			t.Fatal("tenant read has no bounded deadline")
		}
		if tenant == inactive {
			return []jobrun.IsolationSummary{{Phase: jobrun.ExecutionUnknown, Count: 2, OldestAt: now.Add(-time.Hour)}}, nil
		}
		return []jobrun.IsolationSummary{{Phase: jobrun.ExecutionUnknown, Count: 1, OldestAt: now}}, nil
	})
	snapshot, err := readExecutionIsolation(t.Context(), reader, []string{active, inactive, active})
	if err != nil {
		t.Fatal(err)
	}
	if calls[active] != 1 || calls[inactive] != 1 {
		t.Fatalf("scope calls = %v", calls)
	}
	unknown := snapshot[jobrun.ExecutionUnknown]
	if unknown.Count != 3 || !unknown.OldestAt.Equal(now.Add(-time.Hour)) {
		t.Fatalf("lost isolated obligations: %+v", unknown)
	}
	if canceled := snapshot[jobrun.CancelUnknown]; canceled.Count != 0 || !canceled.OldestAt.IsZero() {
		t.Fatalf("missing phase retained stale obligations: %+v", canceled)
	}
}

func TestExecutionIsolationRejectsPartialOrInvalidInventory(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows []jobrun.IsolationSummary
		err  error
	}{
		{name: "database unavailable", err: errors.New("database unavailable")},
		{name: "future phase", rows: []jobrun.IsolationSummary{{Phase: "Unrecognized", Count: 1, OldestAt: time.Now()}}},
		{name: "missing timestamp", rows: []jobrun.IsolationSummary{{Phase: jobrun.ExecutionUnknown, Count: 1}}},
		{name: "negative count", rows: []jobrun.IsolationSummary{{Phase: jobrun.ExecutionUnknown, Count: -1, OldestAt: time.Now()}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, later := scan.GenerateID(), scan.GenerateID()
			var laterRead bool
			reader := isolationReaderFunc(func(_ context.Context, tenant string) ([]jobrun.IsolationSummary, error) {
				if tenant == later {
					laterRead = true
					return nil, nil
				}
				return tc.rows, tc.err
			})
			snapshot, err := readExecutionIsolation(t.Context(), reader, []string{first, later})
			if err == nil || snapshot != nil || !laterRead {
				t.Fatalf("partial inventory was published or starved later scope: snapshot=%v err=%v later=%v", snapshot, err, laterRead)
			}
		})
	}
}

func TestExecutionIsolationCancellationStopsFurtherReads(t *testing.T) {
	first, later := scan.GenerateID(), scan.GenerateID()
	ctx, cancel := context.WithCancel(t.Context())
	reader := isolationReaderFunc(func(_ context.Context, tenant string) ([]jobrun.IsolationSummary, error) {
		if tenant != first {
			t.Fatal("read another tenant after cancellation")
		}
		cancel()
		return nil, nil
	})
	snapshot, err := readExecutionIsolation(ctx, reader, []string{first, later})
	if snapshot != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read published inventory: %v %v", snapshot, err)
	}
}

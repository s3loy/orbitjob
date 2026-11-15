package checkobserve

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/domain/checkrun"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/sli"
)

type onceReadModel struct {
	stubReadModel
	seen map[string]bool
}

func (s *onceReadModel) RecordCompletedOnce(ctx context.Context, tenant string, record checkrun.CompletedRecord) (bool, error) {
	if s.seen[record.RunID] {
		return false, nil
	}
	if err := s.RecordCompleted(ctx, tenant, record); err != nil {
		return false, err
	}
	s.seen[record.RunID] = true
	return true, nil
}

type onceSnapshots struct {
	*stubSnapshots
	seen    map[[2]int64]bool
	failSLI int64
}

func (s *onceSnapshots) IncrementSnapshotForRun(ctx context.Context, tenant string, runID, sliID int64, window time.Time, good bool) (bool, error) {
	if sliID == s.failSLI {
		return false, errors.New("snapshot interrupted")
	}
	key := [2]int64{runID, sliID}
	if s.seen[key] {
		return false, nil
	}
	if err := s.IncrementSnapshot(ctx, tenant, sliID, window, good); err != nil {
		return false, err
	}
	s.seen[key] = true
	return true, nil
}
func TestTerminalReplayRepairsPartialSLIFailureWithoutRecounting(t *testing.T) {
	f := newFixture()
	f.outcomes.outcome = checkOutcome(jobrun.Succeeded)
	model := &onceReadModel{seen: map[string]bool{}}
	snapshots := &onceSnapshots{stubSnapshots: newStubSnapshots(), seen: map[[2]int64]bool{}, failSLI: 2}
	f.recorder.ReadModel = model
	f.recorder.Snapshots = snapshots
	f.slis.slis = []sli.Snapshot{{ID: 1, SLIType: sli.TypeAvailability}, {ID: 2, SLIType: sli.TypeAvailability}}
	if err := f.recorder.RecordTerminalPhaseOnce(context.Background(), testTenant, 9, jobrun.Succeeded); err == nil {
		t.Fatal("failed SLI derivative must remain pending")
	}
	snapshots.failSLI = 0
	f.now = f.now.Add(24 * time.Hour)
	if err := f.recorder.RecordTerminalPhaseOnce(context.Background(), testTenant, 9, jobrun.Succeeded); err != nil {
		t.Fatal(err)
	}
	if snapshots.good[1] != 1 || snapshots.good[2] != 1 || len(model.records) != 1 {
		t.Fatalf("replay recounted successful derivatives: good=%v records=%d", snapshots.good, len(model.records))
	}
	wantWindow := f.outcomes.outcome.CompletedAt.Truncate(snapshotWindow)
	if !snapshots.windows[2].Equal(wantWindow) {
		t.Fatalf("replay put historical fact in recovery window: %v", snapshots.windows[2])
	}
}

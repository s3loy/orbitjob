package controlplane

import (
	"context"
	"errors"
	"testing"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
)

type fakePruner struct {
	prunable     []jobrun.PrunableRun
	scanErr      error
	deleteErr    error
	requested    []string
	deletedIDs   []int64
	deletesFound map[int64]bool
	order        *[]string
}

func (f *fakePruner) PrunableRuns(_ context.Context, _, sourceUID string, keepSuccessful, keepFailed int) ([]jobrun.PrunableRun, error) {
	f.requested = append(f.requested, sourceUID)
	if f.scanErr != nil {
		return nil, f.scanErr
	}
	return f.prunable, nil
}

func (f *fakePruner) DeleteRun(_ context.Context, _ string, runID int64) (bool, error) {
	if f.deleteErr != nil {
		return false, f.deleteErr
	}
	if f.order != nil {
		*f.order = append(*f.order, "delete")
	}
	f.deletedIDs = append(f.deletedIDs, runID)
	if f.deletesFound == nil {
		return true, nil
	}
	return f.deletesFound[runID], nil
}

type fakeRemover struct {
	removed []string
	err     error
	order   *[]string
}

func (f *fakeRemover) Remove(_ context.Context, _, scheduledJobName, occurrenceKey string) error {
	if f.err != nil {
		return f.err
	}
	if f.order != nil {
		*f.order = append(*f.order, "remove")
	}
	f.removed = append(f.removed, scheduledJobName+"/"+occurrenceKey)
	return nil
}

func retentionRevision(t *testing.T, history v1alpha1.HistoryPolicy) revision.Revision {
	t.Helper()
	return revisionWith(t, 9, v1alpha1.ScheduledJobSpec{
		Schedule: "0 * * * *", JobTemplate: v1alpha1.JobTemplateSpec{Image: "img"}, History: history,
	})
}

func TestSweepRemovesKubernetesObjectBeforeRow(t *testing.T) {
	// Record the interleaving so the test pins the ordering, not just the calls.
	order := []string{}
	rt := Retainer{
		Definitions: &fakeRevisions{revisions: []revision.Revision{retentionRevision(t, v1alpha1.HistoryPolicy{})}},
		History: &fakePruner{
			prunable: []jobrun.PrunableRun{{ID: 1, OccurrenceKey: "occ-1", Phase: "Succeeded"}},
			order:    &order,
		},
		Remover: &fakeRemover{order: &order},
		Tenants: []string{"tenant-a"},
	}

	removed, err := rt.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d", removed)
	}
	if len(order) != 2 || order[0] != "remove" {
		t.Fatalf("order = %v, want the kubernetes object removed first", order)
	}
}

func TestSweepUsesDefaultHistoryWhenUnset(t *testing.T) {
	pruner := &fakePruner{}
	rt := Retainer{
		Definitions: &fakeRevisions{revisions: []revision.Revision{retentionRevision(t, v1alpha1.HistoryPolicy{})}},
		History:     pruner,
		Remover:     &fakeRemover{},
		Tenants:     []string{"tenant-a"},
	}
	if _, err := rt.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(pruner.requested) != 1 {
		t.Fatalf("scan calls = %v", pruner.requested)
	}
}

func TestSweepStopsBeforeDeletingWhenObjectRemovalFails(t *testing.T) {
	pruner := &fakePruner{prunable: []jobrun.PrunableRun{{ID: 1, OccurrenceKey: "occ-1", Phase: "Succeeded"}}}
	rt := Retainer{
		Definitions: &fakeRevisions{revisions: []revision.Revision{retentionRevision(t, v1alpha1.HistoryPolicy{})}},
		History:     pruner,
		Remover:     &fakeRemover{err: errors.New("api down")},
		Tenants:     []string{"tenant-a"},
	}
	if _, err := rt.Sweep(context.Background()); err == nil {
		t.Fatal("expected removal failure to surface")
	}
	if len(pruner.deletedIDs) != 0 {
		t.Fatal("the row must survive a failed object removal")
	}
}

func TestSweepCountsOnlyRowsActuallyDeleted(t *testing.T) {
	pruner := &fakePruner{
		prunable:     []jobrun.PrunableRun{{ID: 1, OccurrenceKey: "a"}, {ID: 2, OccurrenceKey: "b"}},
		deletesFound: map[int64]bool{1: true, 2: false},
	}
	rt := Retainer{
		Definitions: &fakeRevisions{revisions: []revision.Revision{retentionRevision(t, v1alpha1.HistoryPolicy{})}},
		History:     pruner,
		Remover:     &fakeRemover{},
		Tenants:     []string{"tenant-a"},
	}
	removed, err := rt.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// A run that started executing between the scan and the delete is skipped
	// by the statement predicate and must not be reported as pruned.
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
}

func TestSweepPropagatesErrors(t *testing.T) {
	base := Retainer{
		Definitions: &fakeRevisions{revisions: []revision.Revision{retentionRevision(t, v1alpha1.HistoryPolicy{})}},
		History:     &fakePruner{},
		Remover:     &fakeRemover{},
		Tenants:     []string{"tenant-a"},
	}

	listFailure := base
	listFailure.Definitions = &fakeRevisions{listErr: errors.New("boom")}
	if _, err := listFailure.Sweep(context.Background()); err == nil {
		t.Error("expected list failure")
	}

	scanFailure := base
	scanFailure.History = &fakePruner{scanErr: errors.New("boom")}
	if _, err := scanFailure.Sweep(context.Background()); err == nil {
		t.Error("expected scan failure")
	}

	deleteFailure := base
	deleteFailure.History = &fakePruner{
		prunable:  []jobrun.PrunableRun{{ID: 1, OccurrenceKey: "a"}},
		deleteErr: errors.New("boom"),
	}
	if _, err := deleteFailure.Sweep(context.Background()); err == nil {
		t.Error("expected delete failure")
	}

	malformed := base
	malformed.Definitions = &fakeRevisions{revisions: []revision.Revision{{
		ID: 9, Identity: revision.Identity{SourceUID: "uid-1", Name: "broken"}, NormalizedSpec: "{not json",
	}}}
	if _, err := malformed.Sweep(context.Background()); err == nil {
		t.Error("expected malformed revision to surface")
	}
}

func TestRetainerRequiresDependencies(t *testing.T) {
	if _, err := (Retainer{}).Sweep(context.Background()); err == nil {
		t.Fatal("expected missing dependencies to be rejected")
	}
}

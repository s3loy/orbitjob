package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
)

// The operator's actor guard (jobRunSpecFromUnstructured, job_handler.go:107)
// refuses a JobRun whose spec names no actor, because the ledger's whole
// question is "who triggered this run" and the actor column is NOT NULL with a
// non-empty CHECK (db/migrations/0001_baseline.up.sql:275-283). These tests pin
// the refusal paths and prove the refusal happens before any store or cluster
// interaction.

// --- counting stubs: the refusal must precede every store call ---------------

type actorGuardRuns struct{ calls int }

func (s *actorGuardRuns) CreateOccurrenceForTenant(context.Context, string, jobrun.JobRun, int, string) (jobrun.StoredRun, bool, error) {
	s.calls++
	return jobrun.StoredRun{}, false, nil
}

func (s *actorGuardRuns) RunByOccurrence(context.Context, string, string, string) (jobrun.StoredRun, bool, error) {
	s.calls++
	return jobrun.StoredRun{}, false, nil
}

func (s *actorGuardRuns) CreateAttemptForTenant(context.Context, string, int64, jobrun.Attempt, jobrun.Phase) error {
	s.calls++
	return nil
}

func (s *actorGuardRuns) UpdateAttemptPhase(context.Context, string, string, string, string, string) (int64, int, error) {
	s.calls++
	return 0, 0, nil
}

func (s *actorGuardRuns) UpdateRunPhase(context.Context, string, int64, jobrun.Phase, int) (bool, error) {
	s.calls++
	return false, nil
}

type actorGuardRevisions struct{ calls int }

func (s *actorGuardRevisions) ApplyRevisionForTenant(context.Context, string, revision.Revision) (int64, error) {
	s.calls++
	return 0, nil
}

func (s *actorGuardRevisions) RevisionByID(context.Context, string, int64) (revision.Revision, error) {
	s.calls++
	return revision.Revision{}, nil
}

var (
	_ RunStore      = (*actorGuardRuns)(nil)
	_ RevisionStore = (*actorGuardRevisions)(nil)
)

// actorGuardRuntime is configured enough to pass the wiring check
// (runtime.go:113) and nothing more: any store touch would show up in the
// counters, and any cluster call would panic on the nil dynamic client.
func actorGuardRuntime() (Runtime, *actorGuardRuns, *actorGuardRevisions) {
	runs := &actorGuardRuns{}
	revisions := &actorGuardRevisions{}
	return Runtime{
		Runs:      runs,
		Revisions: revisions,
		Tenants:   NamespaceTenantResolver{Tenants: map[string]string{"finance": "tenant-a"}},
	}, runs, revisions
}

func jobRunObjectWithoutActor(t *testing.T) unstructured.Unstructured {
	t.Helper()
	obj := jobRunObject(t)
	spec := obj.Object["spec"].(map[string]any)
	delete(spec, "actor")
	return obj
}

func TestReconcileJobRunRefusesAJobRunWithNoActor(t *testing.T) {
	obj := jobRunObjectWithoutActor(t)
	rt, runs, revisions := actorGuardRuntime()

	err := rt.ReconcileJobRun(context.Background(), obj)
	if err == nil {
		t.Fatal("a JobRun with no actor must be refused")
	}
	if !strings.Contains(err.Error(), "has no actor") {
		t.Fatalf("error %q does not report the missing actor", err)
	}
	// The refusal names the object, so a silent stream of bad CRs is triagable.
	if !strings.Contains(err.Error(), "finance/nightly-report-1") {
		t.Fatalf("error %q does not name the refused object", err)
	}
	if runs.calls != 0 || revisions.calls != 0 {
		t.Fatalf("the guard must refuse before any store interaction, got runs=%d revisions=%d",
			runs.calls, revisions.calls)
	}
}

func TestReconcileJobRunRefusesAWhitespaceOnlyActor(t *testing.T) {
	// job_handler.go:128 trims before checking, so whitespace is not an actor.
	obj := jobRunObject(t)
	obj.Object["spec"].(map[string]any)["actor"] = " \t\n"
	rt, runs, revisions := actorGuardRuntime()

	err := rt.ReconcileJobRun(context.Background(), obj)
	if err == nil || !strings.Contains(err.Error(), "has no actor") {
		t.Fatalf("error = %v, want a has-no-actor refusal", err)
	}
	if runs.calls != 0 || revisions.calls != 0 {
		t.Fatalf("the guard must refuse before any store interaction, got runs=%d revisions=%d",
			runs.calls, revisions.calls)
	}
}

func TestReconcileJobRunRefusesAJobRunWithNoSpec(t *testing.T) {
	obj := jobRunObject(t)
	delete(obj.Object, "spec")
	rt, runs, revisions := actorGuardRuntime()

	err := rt.ReconcileJobRun(context.Background(), obj)
	if err == nil {
		t.Fatal("a JobRun with no spec must be refused")
	}
	if !strings.Contains(err.Error(), "has no spec") {
		t.Fatalf("error %q does not report the missing spec", err)
	}
	if !strings.Contains(err.Error(), "finance/nightly-report-1") {
		t.Fatalf("error %q does not name the refused object", err)
	}
	if runs.calls != 0 || revisions.calls != 0 {
		t.Fatalf("the guard must refuse before any store interaction, got runs=%d revisions=%d",
			runs.calls, revisions.calls)
	}
}

func TestReconcileJobRunWithAnActorProceedsPastTheGuard(t *testing.T) {
	// Control for the refusals above: the same runtime with a valid actor must
	// get past the actor guard and fail later, for a different reason. Without
	// this, the refusal tests would also pass on a runtime that refuses
	// everything.
	obj := jobRunObject(t)
	runs := &fakeRuns{found: false}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, &fakeJobs{})

	err := rt.ReconcileJobRun(context.Background(), obj)
	if err == nil {
		t.Fatal("expected the missing stored run to surface")
	}
	if strings.Contains(err.Error(), "has no actor") {
		t.Fatalf("a valid actor was rejected: %v", err)
	}
	if !strings.Contains(err.Error(), "no stored run for occurrence") {
		t.Fatalf("error %q shows the reconcile did not reach the run lookup", err)
	}
}

func TestJobRunSpecFromUnstructuredRoundTripsASpecWithAnActor(t *testing.T) {
	obj := jobRunObject(t)
	obj.Object["spec"].(map[string]any)["cancelRequested"] = true

	spec, err := jobRunSpecFromUnstructured(obj)
	if err != nil {
		t.Fatalf("a spec naming an actor must decode: %v", err)
	}
	if spec.Actor != "principal-key-1" {
		t.Fatalf("spec.Actor = %q", spec.Actor)
	}
	if spec.Trigger != v1alpha1.Schedule {
		t.Fatalf("spec.Trigger = %q", spec.Trigger)
	}
	if !spec.CancelRequested {
		t.Fatal("spec.CancelRequested = false, want true")
	}
}

// jobRunSpecFromUnstructuredWithoutActorCheck is a copy of
// jobRunSpecFromUnstructured (job_handler.go:107) with the actor refusal
// removed. It exists only so the red proof below can show the refusal
// assertions are load-bearing.
func jobRunSpecFromUnstructuredWithoutActorCheck(obj unstructured.Unstructured) (v1alpha1.JobRunSpec, error) {
	var spec v1alpha1.JobRunSpec
	raw, found, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil || !found {
		return spec, fmt.Errorf("job run %s/%s has no spec", obj.GetNamespace(), obj.GetName())
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return spec, err
	}
	if err := json.Unmarshal(encoded, &spec); err != nil {
		return spec, fmt.Errorf("decode job run spec: %w", err)
	}
	if spec.ScheduledJobRef.UID == "" || spec.OccurrenceKey == "" {
		return spec, fmt.Errorf("job run %s/%s is missing scheduledJobRef.uid or occurrenceKey",
			obj.GetNamespace(), obj.GetName())
	}
	return spec, nil
}

// TestJobRunActorGuardRedProof runs an actorless JobRun through the shipped
// guard and through a copy of the guard with the actor refusal removed. The
// two must diverge: if the copy behaves like the shipped guard, the refusal
// assertions above would pass either way and pin nothing.
func TestJobRunActorGuardRedProof(t *testing.T) {
	obj := jobRunObjectWithoutActor(t)

	if _, err := jobRunSpecFromUnstructured(obj); err == nil || !strings.Contains(err.Error(), "has no actor") {
		t.Fatalf("shipped guard returned %v, want a has-no-actor refusal", err)
	}
	if _, err := jobRunSpecFromUnstructuredWithoutActorCheck(obj); err != nil {
		t.Fatalf("removing the actor check did not change behavior (still refused: %v), so the guard assertions cannot fail", err)
	}
}

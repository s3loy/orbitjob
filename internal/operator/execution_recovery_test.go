package operator

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	corepostgres "orbitjob/internal/core/store/postgres"
)

// This fake models durable state across CR deletion/recreation, unlike the
// narrow legacy RunStore fake. SQL race/tenant guarantees have separate tests.
type recoveryRuns struct {
	*fakeRuns
	e                          jobrun.Execution
	unknownWrites, proofWrites int
	legacyUID                  string
}

func (s *recoveryRuns) CurrentExecution(context.Context, string, int64) (jobrun.Execution, bool, error) {
	s.e.Run = s.stored
	return s.e, true, nil
}
func (s *recoveryRuns) ExecutionRecoveriesAfter(_ context.Context, _ string, after int64, _ int) ([]jobrun.Execution, error) {
	if s.stored.ID <= after || jobrun.Terminal(s.stored.Phase) {
		return nil, nil
	}
	s.e.Run = s.stored
	return []jobrun.Execution{s.e}, nil
}
func (s *recoveryRuns) MarkExecutionUnknown(_ context.Context, _ string, _ jobrun.Execution, reason string) error {
	s.unknownWrites++
	s.stored.Phase = jobrun.ExecutionUnknown
	s.stored.AttemptPhase = jobrun.ExecutionUnknown
	s.stored.UnknownReason = reason
	if s.stored.UnknownSince == nil {
		at := time.Now()
		s.stored.UnknownSince = &at
	}
	return nil
}
func (s *recoveryRuns) ClearExecutionUnknown(_ context.Context, _ string, _ jobrun.Execution, owner string) error {
	s.stored.OwnerRunUID = owner
	s.stored.UnknownSince = nil
	s.stored.UnknownReason = ""
	if s.stored.Phase == jobrun.ExecutionUnknown {
		s.stored.Phase = jobrun.Running
	}
	return nil
}
func (s *recoveryRuns) RecordTerminationEvidence(_ context.Context, _ string, _ jobrun.Execution, proof []byte) error {
	s.proofWrites++
	s.stored.TerminationEvidence = append([]byte(nil), proof...)
	return nil
}
func (s *recoveryRuns) InvalidatePodTerminationEvidence(context.Context, string, jobrun.Execution) error {
	s.stored.TerminationEvidence = nil
	if s.stored.UnknownSince == nil {
		at := time.Now()
		s.stored.UnknownSince = &at
	}
	s.stored.Phase = jobrun.CancelUnknown
	return nil
}
func (s *recoveryRuns) UpdateRunPhase(ctx context.Context, tenant string, id int64, phase jobrun.Phase, attempt int) (bool, error) {
	_, err := s.fakeRuns.UpdateRunPhase(ctx, tenant, id, phase, attempt)
	if err != nil {
		return false, err
	}
	changed := s.stored.Phase != phase
	s.stored.Phase = phase
	return changed, nil
}
func (s *recoveryRuns) UpdateAttemptPhase(_ context.Context, _ string, _ string, phase, _, _ string) (int64, int, error) {
	s.stored.AttemptPhase = jobrun.Phase(phase)
	return s.stored.ID, s.stored.Attempt, nil
}
func (s *recoveryRuns) BindAttemptObservation(_ context.Context, _ string, _ int64, _ int, _ string, uid, _ string) error {
	s.stored.AttemptUID = uid
	return nil
}

func (s *recoveryRuns) MarkUnrecordedExecutionUnknown(_ context.Context, _ string, _ int64, uid string) error {
	if s.stored.Phase == jobrun.CancelUnknown && s.legacyUID != uid {
		return corepostgres.ErrAttemptOwnership
	}
	s.legacyUID = uid
	s.stored.Phase = jobrun.CancelUnknown
	return nil
}

func (s *recoveryRuns) MarkConflictingUnrecordedExecutionUnknown(_ context.Context, _ string, _ int64, _ string) error {
	s.stored.Phase = jobrun.CancelUnknown
	return nil
}

func TestLegacyIdentityConflictCannotReleaseOrAuthorizeDelete(t *testing.T) {
	r, s, j := recoveryFixture(t)
	s.stored.Attempt, s.stored.Phase = 0, jobrun.Pending
	obj, err := r.Dynamic.Resource(jobRunGVR).Namespace("finance").Get(t.Context(), v1alpha1.RunObjectName("nightly-report", "occ-1"), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	originalUID := obj.GetUID()
	obj.SetUID("replacement-run-uid")
	spec, err := jobRunSpecFromUnstructured(*obj)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileCancellation(t.Context(), executionTenant, *obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.CancelUnknown || len(j.deleted) != 0 || s.legacyUID != "" {
		t.Fatalf("conflict released or adopted: phase=%s deletes=%v trusted=%s", s.stored.Phase, j.deleted, s.legacyUID)
	}
	// Even if mutable identity fields later match, the first conflict did not
	// persist authority to delete this UID. Restart must keep the isolation.
	obj.SetUID(originalUID)
	if err := r.reconcileCancellation(t.Context(), executionTenant, *obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	j.gone = true
	if err := r.reconcileCancellation(t.Context(), executionTenant, *obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.CancelUnknown || len(j.deleted) != 0 {
		t.Fatalf("restart released or deleted conflicting legacy job: phase=%s deletes=%v", s.stored.Phase, j.deleted)
	}
}

func TestLegacyCancelRetriesDeleteWithoutDeletingAReplacement(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "same UID retry", true: "foreign replacement"}[replacement], func(t *testing.T) {
			r, s, j := recoveryFixture(t)
			s.stored.Attempt = 0
			s.stored.Phase = jobrun.Pending
			obj, err := r.Dynamic.Resource(jobRunGVR).Namespace("finance").Get(t.Context(), v1alpha1.RunObjectName("nightly-report", "occ-1"), metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			spec, err := jobRunSpecFromUnstructured(*obj)
			if err != nil {
				t.Fatal(err)
			}
			j.deleteErr = errors.New("503 temporary failure")
			if err = r.reconcileCancellation(t.Context(), executionTenant, *obj, s.stored, spec); err == nil {
				t.Fatal("delete failure was swallowed")
			}
			if s.stored.Phase != jobrun.CancelUnknown {
				t.Fatal("delete failure released legacy uncertainty")
			}
			j.deleteErr = nil
			if replacement {
				j.job.UID = "foreign-replacement"
			}
			if err = r.reconcileCancellation(t.Context(), executionTenant, *obj, s.stored, spec); err != nil {
				t.Fatal(err)
			}
			want := 1
			if replacement {
				want = 0
			}
			if len(j.deleted) != want || s.stored.Phase != jobrun.CancelUnknown {
				t.Fatal("unsafe legacy cancellation retry")
			}
		})
	}
}

func TestLegacyUnrecordedExecutionCannotBecomeCanceledAfterRestart(t *testing.T) {
	r, s, j := recoveryFixture(t)
	s.stored.Attempt = 0
	s.stored.Phase = jobrun.Pending
	s.e.Run = s.stored
	name := v1alpha1.RunObjectName("nightly-report", "occ-1")
	obj, err := r.Dynamic.Resource(jobRunGVR).Namespace("finance").Get(t.Context(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	spec, err := jobRunSpecFromUnstructured(*obj)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.reconcileCancellation(t.Context(), executionTenant, *obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.CancelUnknown || len(j.deleted) != 1 {
		t.Fatal("legacy evidence gap was released")
	}
	j.gone = true
	if err = r.reconcileCancellation(t.Context(), executionTenant, *obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.CancelUnknown {
		t.Fatal("restart/empty inventory invented a stop")
	}
}

type recoveryJobs struct {
	*fakeJobs
	proof          []byte
	verified       bool
	observationErr error
}

func (j *recoveryJobs) InspectTermination(context.Context, string, string, string) ([]byte, bool, error) {
	return j.proof, j.verified, j.observationErr
}

func recoveryFixture(t *testing.T) (Runtime, *recoveryRuns, *recoveryJobs) {
	t.Helper()
	obj := jobRunObject(t)
	obj.SetName(v1alpha1.RunObjectName("nightly-report", "occ-1"))
	id := execution.Identity{RunName: obj.GetName(), RunUID: string(obj.GetUID()), RevisionID: "7", Attempt: 1}
	job := execution.BuildJob(id, "finance", execution.Template{Image: "test.invalid/task"})
	// These cases retain the pre-hold recovery contract. New protocol lifecycle
	// cases use retainedCancellationFixture with a full evidence store.
	job.Spec.Template.Finalizers = nil
	job.UID = types.UID("original-job")
	job.ResourceVersion = "100"
	stored := jobrun.StoredRun{ID: 42, RevisionID: 7, Phase: jobrun.Running, Attempt: 1, MaxAttempts: 3, AttemptUID: string(job.UID), AttemptJobName: job.Name, AttemptPhase: jobrun.Running, OwnerRunUID: id.RunUID}
	s := &recoveryRuns{fakeRuns: &fakeRuns{stored: stored, found: true}, e: jobrun.Execution{SourceUID: "uid-1", OccurrenceKey: "occ-1", DefinitionName: "nightly-report", Namespace: "finance", Actor: "test", Trigger: jobrun.Schedule}}
	j := &recoveryJobs{fakeJobs: &fakeJobs{job: job}}
	r := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{byID: map[int64]revision.Revision{7: revisionFor(t, 7, pinnedSpec)}}, s, j)
	s.e.Run = s.stored
	return r, s, j
}

func TestRecoveryMissingBoundJobQuarantinesWithoutExecution(t *testing.T) {
	r, s, j := recoveryFixture(t)
	j.gone = true
	if err := r.recoverExecution(t.Context(), executionTenant, s.e); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.ExecutionUnknown || s.stored.UnknownSince == nil || s.stored.UnknownReason != jobrun.ReasonJobNotFound {
		t.Fatalf("missing durable uncertainty: %+v", s.stored)
	}
	if len(j.created) != 0 || len(s.attempts) != 0 || len(j.deleted) != 0 {
		t.Fatal("absence executed or deleted work")
	}
	obj, err := r.Dynamic.Resource(jobRunGVR).Namespace("finance").Get(t.Context(), v1alpha1.RunObjectName("nightly-report", "occ-1"), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if obj.Object["status"].(map[string]any)["executionUnknownReason"] != jobrun.ReasonJobNotFound {
		t.Fatalf("missing CR diagnostic: %v", obj.Object["status"])
	}
}

func TestRecoveryRepairsPersistedTerminalBeforeAPIErrors(t *testing.T) {
	for _, phase := range []jobrun.Phase{jobrun.Succeeded, jobrun.Failed} {
		t.Run(string(phase), func(t *testing.T) {
			r, s, j := recoveryFixture(t)
			s.stored.AttemptPhase = phase
			s.e.Run = s.stored
			j.err = errors.New("API unavailable")
			if err := r.recoverExecution(t.Context(), executionTenant, s.e); err != nil {
				t.Fatal(err)
			}
			want := phase
			if phase == jobrun.Failed {
				want = jobrun.RetryWaiting
			}
			if s.stored.Phase != want || s.unknownWrites != 0 {
				t.Fatalf("fact lost: %+v", s.stored)
			}
		})
	}
}

func TestRecoveryObservationErrorsNeverBecomeAbsence(t *testing.T) {
	for _, message := range []string{"403 forbidden", "429 retry", "500 unavailable", "context deadline exceeded"} {
		t.Run(message, func(t *testing.T) {
			r, s, j := recoveryFixture(t)
			j.err = errors.New(message)
			if err := r.recoverExecution(t.Context(), executionTenant, s.e); err == nil {
				t.Fatal("observation error suppressed")
			}
			if s.unknownWrites != 0 || s.stored.Phase != jobrun.Running || len(s.phases) != 0 {
				t.Fatal("API error changed execution state")
			}
		})
	}
}

func TestRecoveryForeignJobNeverAdoptedOrDeleted(t *testing.T) {
	r, s, j := recoveryFixture(t)
	j.job.UID = "foreign-job"
	if err := r.recoverExecution(t.Context(), executionTenant, s.e); err != nil {
		t.Fatal(err)
	}
	if s.stored.UnknownReason != jobrun.ReasonJobUIDMismatch || s.stored.AttemptUID != "original-job" || len(j.deleted) != 0 || len(j.created) != 0 {
		t.Fatalf("foreign job adopted: %+v", s.stored)
	}
}

func TestRecoveryCRRepairPreservesOriginalOwnershipAndNeverEnsures(t *testing.T) {
	for _, owner := range []string{"run-uid-1", ""} {
		t.Run("owner="+owner, func(t *testing.T) {
			r, s, j := recoveryFixture(t)
			s.stored.Phase = jobrun.CreatingAttempt
			s.stored.AttemptUID = ""
			s.stored.OwnerRunUID = owner
			s.e.Run = s.stored
			j.gone = true
			name := v1alpha1.RunObjectName("nightly-report", "occ-1")
			if err := r.Dynamic.Resource(jobRunGVR).Namespace("finance").Delete(t.Context(), name, metav1.DeleteOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := r.recoverExecution(t.Context(), executionTenant, s.e); err != nil {
				t.Fatal(err)
			}
			obj, err := r.Dynamic.Resource(jobRunGVR).Namespace("finance").Get(t.Context(), name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(obj.GetOwnerReferences()) != 0 {
				t.Fatal("repaired CR carries dangling source ownership")
			}
			obj.SetUID("recreated-run-uid")
			if _, err = r.Dynamic.Resource(jobRunGVR).Namespace("finance").Update(t.Context(), obj, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if err = r.ReconcileJobRun(t.Context(), *obj); err != nil {
				t.Fatal(err)
			}
			if len(j.created) != 0 || len(s.attempts) != 0 || s.stored.OwnerRunUID != owner {
				t.Fatal("repaired CR authorized execution or re-owned original")
			}
		})
	}
}

func TestCancellationEmptyInventoryNeverConfirmsStop(t *testing.T) {
	r, s, j := recoveryFixture(t)
	s.stored.Phase = jobrun.CancelRequested
	s.e.Run = s.stored
	j.gone = true
	obj, err := r.Dynamic.Resource(jobRunGVR).Namespace("finance").Get(t.Context(), v1alpha1.RunObjectName("nightly-report", "occ-1"), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	spec, err := jobRunSpecFromUnstructured(*obj)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.reconcileCancellation(t.Context(), executionTenant, *obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.CancelUnknown || len(j.deleted) != 0 {
		t.Fatal("empty API inventory invented a confirmed stop")
	}
}

func TestCancellationLaterActivePodInvalidatesPersistedPodProof(t *testing.T) {
	r, s, j := recoveryFixture(t)
	s.stored.Phase = jobrun.CancelRequested
	s.stored.TerminationEvidence = []byte(`{"source":"pods","job_uid":"original-job"}`)
	s.e.Run = s.stored
	j.gone = true
	j.observationErr = execution.ErrPodWorkloadActive
	obj, err := r.Dynamic.Resource(jobRunGVR).Namespace("finance").Get(t.Context(), v1alpha1.RunObjectName("nightly-report", "occ-1"), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	spec, err := jobRunSpecFromUnstructured(*obj)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.reconcileCancellation(t.Context(), executionTenant, *obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.CancelUnknown || len(s.stored.TerminationEvidence) != 0 {
		t.Fatal("stale Pod sample released slot")
	}
	j.observationErr = nil
	if err = r.reconcileCancellation(t.Context(), executionTenant, *obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.CancelUnknown {
		t.Fatal("restart/GC reused invalidated evidence")
	}
}

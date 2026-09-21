package operator

import (
	"context"
	"errors"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
)

// --- fakes -----------------------------------------------------------------

type fakeRevisions struct {
	applied  []revision.Revision
	applyErr error
	byID     map[int64]revision.Revision
	loadErr  error
}

func (f *fakeRevisions) ApplyRevisionForTenant(_ context.Context, tenant string, rev revision.Revision) (int64, error) {
	if f.applyErr != nil {
		return 0, f.applyErr
	}
	f.applied = append(f.applied, rev)
	return int64(len(f.applied)), nil
}

func (f *fakeRevisions) RevisionByID(_ context.Context, _ string, id int64) (revision.Revision, error) {
	if f.loadErr != nil {
		return revision.Revision{}, f.loadErr
	}
	rev, ok := f.byID[id]
	if !ok {
		return revision.Revision{}, errors.New("revision not found")
	}
	return rev, nil
}

type attemptWrite struct {
	runID    int64
	attempt  jobrun.Attempt
	runPhase jobrun.Phase
}

type runPhaseWrite struct {
	runID   int64
	phase   jobrun.Phase
	attempt int
}

type fakeRuns struct {
	stored      jobrun.StoredRun
	found       bool
	lookupErr   error
	createdRuns []jobrun.JobRun
	attempts    []attemptWrite
	phases      []runPhaseWrite
	attemptErr  error
	phaseErr    error
	observedRun int64
	observedNum int
	observedErr error
	// reportPhaseChange is what UpdateRunPhase reports: true models a real
	// transition, false a resync replay of already-stored state.
	reportPhaseChange bool
}

func (f *fakeRuns) CreateOccurrenceForTenant(_ context.Context, _ string, run jobrun.JobRun, _ int, _ string) (jobrun.StoredRun, bool, error) {
	f.createdRuns = append(f.createdRuns, run)
	return f.stored, true, nil
}

func (f *fakeRuns) RunByOccurrence(_ context.Context, _, _, _ string) (jobrun.StoredRun, bool, error) {
	return f.stored, f.found, f.lookupErr
}

func (f *fakeRuns) CreateAttemptForTenant(_ context.Context, _ string, runID int64, attempt jobrun.Attempt, runPhase jobrun.Phase) error {
	if f.attemptErr != nil {
		return f.attemptErr
	}
	f.attempts = append(f.attempts, attemptWrite{runID: runID, attempt: attempt, runPhase: runPhase})
	return nil
}

func (f *fakeRuns) UpdateAttemptPhase(_ context.Context, _, jobName, phase, _, _ string) (int64, int, error) {
	if f.observedErr != nil {
		return 0, 0, f.observedErr
	}
	_ = jobName
	_ = phase
	return f.observedRun, f.observedNum, nil
}

func (f *fakeRuns) UpdateRunPhase(_ context.Context, _ string, runID int64, phase jobrun.Phase, attempt int) (bool, error) {
	if f.phaseErr != nil {
		return false, f.phaseErr
	}
	f.phases = append(f.phases, runPhaseWrite{runID: runID, phase: phase, attempt: attempt})
	return f.reportPhaseChange, nil
}

type fakeJobs struct {
	created   []*batchv1.Job
	job       *batchv1.Job
	deleted   []string
	gone      bool
	err       error
	deleteErr error
	// terminationGrace keeps the Job visible for this long after Delete, which
	// is how the fake reproduces graceful Kubernetes deletion.
	terminationGrace time.Duration
	// deleteKeepsJob models the API not having registered the deletion yet.
	deleteKeepsJob bool
	terminatingAt  *time.Time
	now            func() time.Time
}

func (f *fakeJobs) Get(_ context.Context, _, name string) (*batchv1.Job, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	if f.gone {
		return nil, false, nil
	}
	if f.terminatingAt != nil {
		return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "finance", DeletionTimestamp: &metav1.Time{Time: *f.terminatingAt},
		}}, true, nil
	}
	if f.job != nil {
		return f.job, true, nil
	}
	for _, created := range f.created {
		if created.Name == name {
			live := created.DeepCopy()
			live.UID = "k8s-uid-1"
			live.ResourceVersion = "100"
			return live, true, nil
		}
	}
	return nil, false, nil
}

func (f *fakeJobs) Delete(_ context.Context, _, name string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, name)
	if f.deleteKeepsJob {
		return nil
	}
	if f.terminationGrace > 0 {
		// The real API keeps the Job visible with a DeletionTimestamp until its
		// Pods are gone, so the fake must too.
		at := f.clock().Add(-f.terminationGrace)
		f.terminatingAt = &at
		return nil
	}
	f.gone = true
	return nil
}

func (f *fakeJobs) clock() time.Time {
	if f.now != nil {
		return f.now()
	}
	return time.Unix(1700000000, 0).UTC()
}

func (f *fakeJobs) Ensure(_ context.Context, desired *batchv1.Job) (*batchv1.Job, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.created = append(f.created, desired)
	if f.job != nil {
		return f.job, nil
	}
	created := desired.DeepCopy()
	created.UID = "k8s-uid-1"
	created.ResourceVersion = "100"
	return created, nil
}

// --- helpers ---------------------------------------------------------------

func scheduledJobObject(t *testing.T) unstructured.Unstructured {
	t.Helper()
	obj := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "workloads.orbitjob.io/v1alpha1",
		"kind":       "ScheduledJob",
		"metadata": map[string]any{
			"name": "nightly-report", "namespace": "finance", "uid": "uid-1",
			"generation": int64(3),
		},
		"spec": map[string]any{
			"schedule":    "0 2 * * *",
			"jobTemplate": map[string]any{"image": "registry.example.com/report:v1"},
		},
	}}
	obj.SetGroupVersionKind(scheduledJobGVR.GroupVersion().WithKind("ScheduledJob"))
	return obj
}

func jobRunObject(t *testing.T) unstructured.Unstructured {
	t.Helper()
	obj := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "workloads.orbitjob.io/v1alpha1",
		"kind":       "JobRun",
		"metadata": map[string]any{
			"name": "nightly-report-1", "namespace": "finance", "uid": "run-uid-1",
		},
		"spec": map[string]any{
			"scheduledJobRef":    map[string]any{"name": "nightly-report", "uid": "uid-1"},
			"definitionRevision": int64(7),
			"trigger":            "Schedule",
			"actor":              "principal-key-1",
			"occurrenceKey":      "occ-1",
		},
	}}
	obj.SetGroupVersionKind(jobRunGVR.GroupVersion().WithKind("JobRun"))
	return obj
}

func (f *fakeJobs) lastCreated() *batchv1.Job {
	if len(f.created) == 0 {
		return nil
	}
	return f.created[len(f.created)-1]
}

func newRuntime(t *testing.T, dyn dynamic.Interface, revisions RevisionStore, runs RunStore, jobs JobManager) Runtime {
	t.Helper()
	return Runtime{
		Dynamic:   dyn,
		Jobs:      jobs,
		Revisions: revisions,
		Runs:      runs,
		Tenants:   NamespaceTenantResolver{Tenants: map[string]string{"finance": "tenant-a"}},
		Now:       func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}
}

func fakeDynamic(t *testing.T, objs ...runtime.Object) dynamic.Interface {
	t.Helper()
	return dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), objs...)
}

func revisionFor(t *testing.T, id int64, spec string) revision.Revision {
	t.Helper()
	rev, err := revision.New(revision.Identity{
		SourceMode: "kubernetes", SourceUID: "uid-1", Namespace: "finance", Name: "nightly-report",
	}, 3, spec, "operator", "", time.Unix(1000, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	rev.ID = id
	return rev
}

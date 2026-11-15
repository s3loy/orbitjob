package operator

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
)

func TestNewExecutionJobPinsActualJobRunController(t *testing.T) {
	obj := jobRunObject(t)
	runs := &fakeRuns{found: true, stored: jobrun.StoredRun{ID: 11, RevisionID: 7, Phase: jobrun.Pending, MaxAttempts: 1, Epoch: 4}}
	jobs := &fakeJobs{}
	revisions := &fakeRevisions{byID: map[int64]revision.Revision{7: revisionFor(t, 7, pinnedSpec)}}
	rt := newRuntime(t, fakeDynamic(t, &obj), revisions, runs, jobs)
	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	created := jobs.lastCreated()
	if created == nil {
		t.Fatal("no Job created")
	}
	owner := metav1.GetControllerOf(created)
	if created.Namespace != obj.GetNamespace() || owner == nil || owner.APIVersion != "workloads.orbitjob.io/v1alpha1" || owner.Kind != "JobRun" || owner.Name != obj.GetName() || owner.UID != obj.GetUID() {
		t.Fatalf("unverifiable ownership: %+v", created.ObjectMeta)
	}
}
func TestExecutionWithoutJobRunUIDDoesNotCreateJob(t *testing.T) {
	obj := jobRunObject(t)
	obj.SetUID("")
	runs := &fakeRuns{found: true, stored: jobrun.StoredRun{ID: 11, RevisionID: 7, Phase: jobrun.Pending, MaxAttempts: 1, Epoch: 4}}
	jobs := &fakeJobs{}
	revisions := &fakeRevisions{byID: map[int64]revision.Revision{7: revisionFor(t, 7, pinnedSpec)}}
	rt := newRuntime(t, fakeDynamic(t, &obj), revisions, runs, jobs)
	if err := rt.ReconcileJobRun(context.Background(), obj); err == nil {
		t.Fatal("accepted empty immutable identity")
	}
	if len(jobs.created) != 0 || len(runs.attempts) != 0 {
		t.Fatal("created unverifiable workload")
	}
}

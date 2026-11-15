package operator

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
)

func TestCancelUsesStoredHistoricalWorkloadAddress(t *testing.T) {
	obj, stored := jobRunObjectWithCancel(t, string(jobrun.Running), 1)
	obj.SetName(strings.Repeat("a", 60))
	id := execution.Identity{RunName: obj.GetName(), RunUID: string(obj.GetUID()), RevisionID: "7", Attempt: 1}
	stored.AttemptJobName = execution.LegacyJobName(id)
	if stored.AttemptJobName == execution.JobName(id) {
		t.Fatal("fixture must exercise changed historical address")
	}
	owned := execution.BuildJob(id, "finance", execution.Template{Image: "task"})
	owned.Name = stored.AttemptJobName
	owned.UID = "k8s-uid-1"
	jobs := &fakeJobs{job: owned}
	runs := &fakeRuns{found: true, stored: stored, reportPhaseChange: true}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, jobs)
	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if len(jobs.deleted) != 1 || jobs.deleted[0] != stored.AttemptJobName {
		t.Fatalf("cancel targeted computed address instead of existing Job: %v", jobs.deleted)
	}
}
func TestUnrecordedLegacyJobIsAdoptedBeforeNewNameIsReserved(t *testing.T) {
	obj := jobRunObject(t)
	obj.SetName(strings.Repeat("a", 60))
	id := execution.Identity{RunName: obj.GetName(), RunUID: string(obj.GetUID()), RevisionID: "7", Attempt: 1}
	owned := execution.BuildJob(id, "finance", execution.Template{Image: "task"})
	owned.Name = execution.LegacyJobName(id)
	owned.UID = "k8s-uid-1"
	kube := fake.NewClientset(owned)
	manager := execution.Adapter{Client: execution.KubernetesJobClient{Client: kube.BatchV1(), Core: kube.CoreV1()}}
	runs := &fakeRuns{found: true, stored: jobrun.StoredRun{ID: 42, RevisionID: 7, Phase: jobrun.Pending, MaxAttempts: 1}}
	revs := &fakeRevisions{byID: map[int64]revision.Revision{7: revisionFor(t, 7, pinnedSpec)}}
	rt := newRuntime(t, fakeDynamic(t, &obj), revs, runs, manager)
	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	jobs, err := kube.BatchV1().Jobs("finance").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs.Items) != 1 || len(runs.attempts) != 1 || runs.attempts[0].attempt.KubernetesJobName != owned.Name {
		t.Fatalf("legacy Job duplicated: Jobs=%d attempts=%+v", len(jobs.Items), runs.attempts)
	}
}

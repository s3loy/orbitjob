package operator

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
)

type lifecycleRevisions struct {
	*fakeRevisions
	deactivated []string
	sweeps      int
}

func (s *lifecycleRevisions) DeactivateSource(_ context.Context, _, _, uid string) error {
	s.deactivated = append(s.deactivated, uid)
	return nil
}
func (s *lifecycleRevisions) DeactivateSourceByName(_ context.Context, _, _, _, name string) error {
	s.deactivated = append(s.deactivated, name)
	return nil
}
func (s *lifecycleRevisions) DeactivateSourcesMissing(context.Context, string, string, []string) error {
	s.sweeps++
	return nil
}
func TestDeletedSourceCannotStartNewAttempt(t *testing.T) {
	obj := jobRunObject(t)
	revs := &lifecycleRevisions{fakeRevisions: &fakeRevisions{byID: map[int64]revision.Revision{7: revisionFor(t, 7, `{"jobTemplate":{"image":"example:v1"}}`)}}}
	runs := &fakeRuns{found: true, stored: jobrun.StoredRun{ID: 42, RevisionID: 7, Phase: jobrun.Pending, MaxAttempts: 1}}
	jobs := &fakeJobs{}
	rt := newRuntime(t, fakeDynamic(t, &obj), revs, runs, jobs)
	if err := rt.Dynamic.Resource(scheduledJobGVR).Namespace("finance").Delete(context.Background(), "nightly-report", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := rt.ReconcileJobRun(context.Background(), obj); err == nil {
		t.Fatal("deleted source accepted for execution")
	}
	if len(jobs.created) != 0 || len(revs.deactivated) != 1 {
		t.Fatalf("created=%d inactive=%v", len(jobs.created), revs.deactivated)
	}
}
func TestIncompleteSourceInventoryDoesNotDeactivateAnything(t *testing.T) {
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{scheduledJobGVR: "ScheduledJobList", workflowJobGVR: "WorkflowJobList"})
	dyn.PrependReactor("list", "scheduledjobs", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("incomplete API inventory")
	})
	revs := &lifecycleRevisions{fakeRevisions: &fakeRevisions{}}
	rt := Runtime{Dynamic: dyn, Revisions: revs, Tenants: NamespaceTenantResolver{Tenants: map[string]string{"finance": executionTenant}}}
	if err := rt.SyncSourceLiveness(context.Background()); err == nil {
		t.Fatal("incomplete inventory reported success")
	}
	if revs.sweeps != 0 {
		t.Fatal("failed inventory deactivated sources")
	}
}

func (s *lifecycleRevisions) RetentionRevisions(context.Context, string) ([]revision.Revision, error) {
	var all []revision.Revision
	for _, rev := range s.byID {
		all = append(all, rev)
	}
	return all, nil
}
func (s *lifecycleRevisions) RetentionWorkflowRevisions(ctx context.Context, tenant string) ([]revision.Revision, error) {
	return s.RetentionRevisions(ctx, tenant)
}

func TestDeleteRecoveryNeverDeactivatesSameNameReplacement(t *testing.T) {
	old := revisionFor(t, 7, `{"jobTemplate":{"image":"example:v1"}}`)
	current := old
	current.ID = 8
	current.Identity.SourceUID = "replacement-uid"
	revs := &lifecycleRevisions{fakeRevisions: &fakeRevisions{byID: map[int64]revision.Revision{7: old, 8: current}}}
	replacement := scheduledJobObject(t)
	replacement.SetUID("replacement-uid")
	rt := Runtime{Dynamic: fakeDynamic(t, &replacement), Revisions: revs, Tenants: NamespaceTenantResolver{Tenants: map[string]string{"finance": executionTenant}}}
	if err := rt.ReconcileDeleted(context.Background(), scheduledJobGVR.Resource, "finance", "nightly-report"); err != nil {
		t.Fatal(err)
	}
	if len(revs.deactivated) != 1 || revs.deactivated[0] != "uid-1" {
		t.Fatalf("replacement lost scheduling authority: %v", revs.deactivated)
	}
}

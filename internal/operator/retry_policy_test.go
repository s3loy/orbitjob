package operator

import (
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/platform/scan"
)

func TestV1SuppressesOnlyTrustedTaskOOMRetry(t *testing.T) {
	for _, c := range []struct {
		version int
		reason  string
		want    bool
	}{
		{0, "OOMKilled", false},
		{1, "OOMKilled", true},
		{1, "Error", false},
	} {
		r, s, _ := recoveryFixture(t)
		s.stored.Phase = jobrun.RetryWaiting
		s.stored.AttemptPhase = jobrun.Failed
		s.stored.TerminationEvidence = []byte(fmt.Sprintf(`{"source":"pods","job_uid":%q,"pods":[{"uid":%q,"containers":[{"name":"task","state":{"terminated":{"finishedAt":"2026-11-12T00:00:00Z","reason":%q,"exitCode":137}}}]}]}`, s.stored.AttemptUID, scan.GenerateID(), c.reason))
		rev := revisionFor(t, s.stored.RevisionID, fmt.Sprintf(`{"executionPolicyVersion":%d,"retrySafe":true,"jobTemplate":{"image":"registry.example.com/finance/extract:reviewed"},"retryPolicy":{"maxAttempts":3}}`, c.version))
		r.Revisions.(*fakeRevisions).byID[s.stored.RevisionID] = rev
		got, err := r.suppressOOMRetry(t.Context(), executionTenant, s.stored)
		if err != nil || got != c.want {
			t.Fatalf("version=%d reason=%s suppressed=%t want=%t err=%v", c.version, c.reason, got, c.want, err)
		}
	}
}

func TestV1TaskOOMFinishesRunWithoutAnotherAttempt(t *testing.T) {
	r, s, jobs := recoveryFixture(t)
	s.stored.Phase, s.stored.AttemptPhase = jobrun.RetryWaiting, jobrun.Failed
	s.stored.TerminationEvidence = []byte(fmt.Sprintf(`{"source":"pods","job_uid":%q,"pods":[{"uid":%q,"containers":[{"name":"task","state":{"terminated":{"finishedAt":"2026-11-12T00:00:00Z","reason":"OOMKilled","exitCode":137}}}]}]}`, s.stored.AttemptUID, scan.GenerateID()))
	r.Revisions.(*fakeRevisions).byID[s.stored.RevisionID] = revisionFor(t, s.stored.RevisionID, `{"executionPolicyVersion":1,"retrySafe":true,"jobTemplate":{"image":"registry.example.com/finance/extract:reviewed"},"retryPolicy":{"maxAttempts":3}}`)
	obj, err := r.Dynamic.Resource(jobRunGVR).Namespace("finance").Get(t.Context(), v1alpha1.RunObjectName("nightly-report", "occ-1"), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ReconcileJobRun(t.Context(), *obj); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.Failed || len(s.attempts) != 0 || len(jobs.created) != 0 {
		t.Fatalf("OOM consumed another attempt: run=%+v attempts=%+v jobs=%d", s.stored, s.attempts, len(jobs.created))
	}
}

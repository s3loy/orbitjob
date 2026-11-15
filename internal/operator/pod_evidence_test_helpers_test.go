package operator

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/core/domain/jobrun"
)

type retainedRuns struct {
	*recoveryRuns
	recordErr, releasedErr error
	isolations, releases   int
	targetNumber           int
}

func (s *retainedRuns) recorded() jobrun.RecordedAttempt {
	s.e.Run = s.stored
	number := s.stored.Attempt
	if s.targetNumber != 0 {
		number = s.targetNumber
	}
	return jobrun.RecordedAttempt{Execution: s.e, ID: 101, Number: number, Phase: s.stored.AttemptPhase,
		JobName: s.stored.AttemptJobName, JobUID: s.stored.AttemptUID, OwnerRunUID: s.stored.OwnerRunUID,
		UnknownSince: s.stored.UnknownSince, UnknownReason: s.stored.UnknownReason, TerminationEvidence: s.stored.TerminationEvidence}
}

func (s *retainedRuns) RecordedExecution(_ context.Context, _, namespace, name, uid string) (jobrun.RecordedAttempt, bool, error) {
	a := s.recorded()
	return a, namespace == a.Execution.Namespace && name == a.JobName && uid == a.JobUID, nil
}
func (s *retainedRuns) PendingEvidenceAfter(_ context.Context, _ string, after int64, _ int) ([]jobrun.RecordedAttempt, error) {
	a := s.recorded()
	if a.ID <= after || !jobrun.EvidenceReleasePending(a.TerminationEvidence) {
		return nil, nil
	}
	return []jobrun.RecordedAttempt{a}, nil
}
func (s *retainedRuns) RecordAttemptTerminationEvidence(_ context.Context, _ string, _ jobrun.RecordedAttempt, proof []byte) error {
	if s.recordErr != nil {
		return s.recordErr
	}
	if s.stored.UnknownSince != nil {
		return errors.New("evidence isolated")
	}
	s.proofWrites++
	s.stored.TerminationEvidence = append([]byte(nil), proof...)
	return nil
}
func (s *retainedRuns) MarkEvidenceReleased(_ context.Context, _ string, _ jobrun.RecordedAttempt) error {
	if s.releasedErr != nil {
		return s.releasedErr
	}
	s.releases++
	s.setEvidence("hold_release", "done")
	return nil
}
func (s *retainedRuns) IsolateAttemptEvidence(_ context.Context, _ string, _ jobrun.RecordedAttempt, reason string) error {
	s.isolations++
	at := time.Now()
	s.stored.UnknownSince, s.stored.UnknownReason = &at, reason
	if s.stored.Phase == jobrun.CancelRequested || s.stored.Phase == jobrun.CancelUnknown {
		s.stored.Phase = jobrun.CancelUnknown
	} else if !jobrun.Terminal(s.stored.Phase) {
		s.stored.Phase = jobrun.ExecutionUnknown
	}
	s.setEvidence("protocol", jobrun.PodHoldProtocol)
	s.setEvidence("hold_release", "pending")
	s.setEvidence("revoked", true)
	return nil
}
func (s *retainedRuns) setEvidence(key string, value any) {
	body := map[string]any{}
	_ = json.Unmarshal(s.stored.TerminationEvidence, &body)
	body[key] = value
	s.stored.TerminationEvidence, _ = json.Marshal(body)
}

type retainedJobs struct {
	*recoveryJobs
	client        *fake.Clientset
	adapter       execution.Adapter
	foreignHold   bool
	releaseErr    error
	beforeRelease func()
	stopRequests  int
}

func (j *retainedJobs) RequestStop(_ context.Context, job *batchv1.Job) (bool, error) {
	if job.UID != j.job.UID || job.ResourceVersion != j.job.ResourceVersion {
		return false, errors.New("stop request identity differs")
	}
	changed := j.job.Spec.ActiveDeadlineSeconds == nil || *j.job.Spec.ActiveDeadlineSeconds > 1
	if changed {
		deadline := int64(1)
		j.job.Spec.ActiveDeadlineSeconds = &deadline
		j.stopRequests++
	}
	// This fixture's controller has no outstanding create. The dedicated
	// inflightCreateJobs fixture controls acknowledgement separately.
	j.job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue, Reason: "DeadlineExceeded"}}
	return changed, nil
}

func (j *retainedJobs) Delete(_ context.Context, _, name string, uid ...string) error {
	if j.deleteErr != nil {
		return j.deleteErr
	}
	if len(uid) != 1 || uid[0] != string(j.job.UID) {
		return errors.New("delete missing original UID")
	}
	j.deleted = append(j.deleted, name)
	at := metav1.Now()
	j.job.DeletionTimestamp = &at
	if !j.foreignHold {
		pods, err := j.client.CoreV1().Pods(j.job.Namespace).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		blocked := false
		for _, pod := range pods.Items {
			blocked = blocked || len(pod.Finalizers) > 0
		}
		j.gone = !blocked
	}
	return nil
}
func (j *retainedJobs) InspectRetainedTermination(ctx context.Context, job *batchv1.Job, proof []byte) ([]byte, bool, error) {
	return j.adapter.InspectRetainedTermination(ctx, job, proof)
}
func (j *retainedJobs) ReleaseTerminationEvidence(ctx context.Context, namespace, name, uid string, proof []byte) error {
	if j.beforeRelease != nil {
		j.beforeRelease()
	}
	if j.releaseErr != nil {
		return j.releaseErr
	}
	if err := j.adapter.ReleaseTerminationEvidence(ctx, namespace, name, uid, proof); err != nil {
		return err
	}
	if j.job.DeletionTimestamp != nil && !j.foreignHold {
		j.gone = true
	}
	return nil
}
func (j *retainedJobs) pod(t *testing.T) *corev1.Pod {
	t.Helper()
	pod, err := j.client.CoreV1().Pods("finance").Get(t.Context(), "original-pod", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return pod
}
func (j *retainedJobs) terminate(t *testing.T) {
	t.Helper()
	pod := j.pod(t)
	pod.Status.Phase = corev1.PodSucceeded
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "task", State: corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{FinishedAt: metav1.Now(), Reason: "Completed"},
	}}}
	if _, err := j.client.CoreV1().Pods("finance").UpdateStatus(t.Context(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}
func (j *retainedJobs) held(t *testing.T) bool {
	t.Helper()
	return slices.Contains(j.pod(t).Finalizers, execution.TerminationEvidenceFinalizer)
}
func retainedCancellationFixture(t *testing.T) (Runtime, *retainedRuns, *retainedJobs, unstructured.Unstructured, v1alpha1.JobRunSpec) {
	t.Helper()
	r, legacy, original, obj, spec := cancellationFixture(t)
	s := &retainedRuns{recoveryRuns: legacy}
	original.job.Spec.Template.Finalizers = []string{execution.TerminationEvidenceFinalizer}
	controller, block := true, true
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "original-pod", Namespace: "finance", UID: "original-pod-uid", ResourceVersion: "7",
		Finalizers:      []string{execution.TerminationEvidenceFinalizer},
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: original.job.Name, UID: original.job.UID, Controller: &controller, BlockOwnerDeletion: &block}}},
		Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "task", Image: "test.invalid/task"}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	client := fake.NewClientset(pod)
	j := &retainedJobs{recoveryJobs: original.recoveryJobs, client: client,
		adapter: execution.Adapter{Client: execution.KubernetesJobClient{Client: client.BatchV1(), Core: client.CoreV1()}}}
	r.Runs, r.Jobs = s, j
	return r, s, j, obj, spec
}

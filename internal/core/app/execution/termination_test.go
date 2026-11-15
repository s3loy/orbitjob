package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"orbitjob/internal/platform/scan"
)

type terminationInventory struct {
	fakeClient
	pods []corev1.Pod
	err  error
}

func (c *terminationInventory) PodInventory(context.Context, string) ([]corev1.Pod, error) {
	return c.pods, c.err
}

func terminatedPod(uid string) corev1.Pod {
	controller := true
	finished := metav1.NewTime(time.Now().UTC())
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "owned-pod", Namespace: "tasks", UID: types.UID(scan.GenerateID()), ResourceVersion: "17",
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: "attempt", UID: types.UID(uid), Controller: &controller}}},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "task", Image: "task"}}},
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded, ContainerStatuses: []corev1.ContainerStatus{{Name: "task", ContainerID: "containerd://completed",
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, Reason: "Completed", FinishedAt: finished, StartedAt: metav1.NewTime(finished.Add(-time.Second))}}}}},
	}
}
func TestInspectTerminationRequiresOriginalUIDAndRuntimeEvidence(t *testing.T) {
	uid := scan.GenerateID()
	cases := []struct {
		name   string
		mutate func(*corev1.Pod)
		empty  bool
		want   bool
	}{
		{name: "owned-without-mutable-labels", want: true},
		{name: "empty-inventory", empty: true},
		{name: "running", mutate: func(p *corev1.Pod) { p.Status.Phase = corev1.PodRunning }},
		{name: "NodeLost", mutate: func(p *corev1.Pod) { p.Status.Phase = corev1.PodFailed; p.Status.Reason = "NodeLost" }},
		{name: "unknown-container-status", mutate: func(p *corev1.Pod) {
			p.Status.Phase = corev1.PodFailed
			p.Status.ContainerStatuses[0].State.Terminated.Reason = "ContainerStatusUnknown"
		}},
		{name: "missing-runtime-finish", mutate: func(p *corev1.Pod) { p.Status.ContainerStatuses[0].State.Terminated.FinishedAt = metav1.Time{} }},
		{name: "missing-pod-uid", mutate: func(p *corev1.Pod) { p.UID = "" }},
		{name: "missing-regular-status", mutate: func(p *corev1.Pod) { p.Status.ContainerStatuses = nil }},
		{name: "running-init", mutate: func(p *corev1.Pod) {
			p.Spec.InitContainers = []corev1.Container{{Name: "init"}}
			p.Status.InitContainerStatuses = []corev1.ContainerStatus{{Name: "init", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}
		}},
		{name: "unobserved-ephemeral", mutate: func(p *corev1.Pod) {
			p.Spec.EphemeralContainers = []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "debug"}}}
		}},
		{name: "label-only", mutate: func(p *corev1.Pod) {
			p.OwnerReferences = nil
			p.Labels = map[string]string{batchv1.JobNameLabel: "attempt"}
		}},
		{name: "foreign-uid-same-label", mutate: func(p *corev1.Pod) {
			p.OwnerReferences[0].UID = types.UID(scan.GenerateID())
			p.Labels = map[string]string{batchv1.JobNameLabel: "attempt"}
		}},
		{name: "non-controller-owner", mutate: func(p *corev1.Pod) {
			p.OwnerReferences[0].Controller = nil
			p.Labels = map[string]string{batchv1.JobNameLabel: "attempt"}
		}},
		{name: "conflicting-owner-name", mutate: func(p *corev1.Pod) { p.OwnerReferences[0].Name = "foreign-attempt" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pod := terminatedPod(uid)
			if tc.mutate != nil {
				tc.mutate(&pod)
			}
			var pods []corev1.Pod
			if !tc.empty {
				pods = []corev1.Pod{pod}
			}
			proof, verified, err := (Adapter{Client: &terminationInventory{pods: pods}}).InspectTermination(t.Context(), "tasks", "attempt", uid)
			if verified != tc.want {
				t.Fatalf("verified=%v want=%v error=%v proof=%s", verified, tc.want, err, proof)
			}
			if !tc.want {
				if len(proof) != 0 {
					t.Fatal("uncertain sample returned stop proof")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var evidence struct {
				Source, JobUID string
				Pods           []struct{ UID string }
			}
			// Use tagged fields to independently verify the persisted wire identity.
			var raw map[string]json.RawMessage
			if err = json.Unmarshal(proof, &raw); err != nil {
				t.Fatal(err)
			}
			_ = json.Unmarshal(raw["job_uid"], &evidence.JobUID)
			_ = json.Unmarshal(raw["source"], &evidence.Source)
			_ = json.Unmarshal(raw["pods"], &evidence.Pods)
			if evidence.Source != "pods" || evidence.JobUID != uid || len(evidence.Pods) != 1 || evidence.Pods[0].UID != string(pod.UID) {
				t.Fatalf("wrong proof identity: %s", proof)
			}
		})
	}
}
func TestInspectTerminationDoesNotTreatIncompleteInventoryAsStop(t *testing.T) {
	uid := scan.GenerateID()
	for _, err := range []error{apierrors.NewResourceExpired("continuation expired"), context.DeadlineExceeded, errors.New("transport unavailable")} {
		proof, verified, got := (Adapter{Client: &terminationInventory{pods: []corev1.Pod{terminatedPod(uid)}, err: err}}).InspectTermination(t.Context(), "tasks", "attempt", uid)
		if got == nil || verified || len(proof) != 0 {
			t.Fatalf("incomplete inventory used as stop evidence: %v %v %s", got, verified, proof)
		}
	}
	if _, verified, err := (Adapter{Client: &terminationInventory{pods: []corev1.Pod{terminatedPod(uid)}}}).InspectTermination(t.Context(), "tasks", "attempt", ""); err == nil || verified {
		t.Fatal("missing original UID accepted")
	}
}
func paginatedClient(t *testing.T, reply func(int, metav1.ListOptions) (*corev1.PodList, error)) (KubernetesJobClient, *int) {
	t.Helper()
	client := fake.NewClientset()
	calls := 0
	client.PrependReactor("list", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		opts := a.(interface{ GetListOptions() metav1.ListOptions }).GetListOptions()
		if opts.LabelSelector != "" || opts.FieldSelector != "" {
			t.Errorf("inventory was label/name filtered: %+v", opts)
		}
		if opts.Limit != 100 {
			t.Errorf("unbounded inventory page: %+v", opts)
		}
		page, err := reply(calls, opts)
		calls++
		if err != nil {
			return true, nil, err
		}
		return true, page, err
	})
	return KubernetesJobClient{Client: client.BatchV1(), Core: client.CoreV1()}, &calls
}
func TestPodInventoryReachesUnlabeledOwnedPodOnLaterPage(t *testing.T) {
	uid := scan.GenerateID()
	client, calls := paginatedClient(t, func(page int, opts metav1.ListOptions) (*corev1.PodList, error) {
		if page == 0 {
			if opts.Continue != "" {
				t.Error("initial continuation must be empty")
			}
			return &corev1.PodList{ListMeta: metav1.ListMeta{Continue: "next"}, Items: []corev1.Pod{terminatedPod(scan.GenerateID())}}, nil
		}
		if opts.Continue != "next" {
			t.Errorf("continuation=%q", opts.Continue)
		}
		pod := terminatedPod(uid)
		pod.Status.Phase = corev1.PodRunning
		return &corev1.PodList{Items: []corev1.Pod{pod}}, nil
	})
	proof, verified, err := (Adapter{Client: client}).InspectTermination(t.Context(), "tasks", "attempt", uid)
	if *calls != 2 || verified || len(proof) != 0 || err == nil {
		t.Fatalf("later live Pod missed: calls=%d verified=%v err=%v", *calls, verified, err)
	}
}
func TestPodInventoryDiscardsPartialPagesAndBoundsPagination(t *testing.T) {
	uid := scan.GenerateID()
	for _, which := range []string{"expired", "transport", "budget"} {
		t.Run(which, func(t *testing.T) {
			client, calls := paginatedClient(t, func(page int, _ metav1.ListOptions) (*corev1.PodList, error) {
				if page > 0 && which == "expired" {
					return nil, apierrors.NewResourceExpired("retry list from scratch")
				}
				if page > 0 && which == "transport" {
					return nil, fmt.Errorf("read timeout")
				}
				return &corev1.PodList{ListMeta: metav1.ListMeta{Continue: "more"}, Items: []corev1.Pod{terminatedPod(uid)}}, nil
			})
			pods, err := client.PodInventory(t.Context(), "tasks")
			if err == nil || len(pods) != 0 {
				t.Fatalf("partial inventory escaped: count=%d err=%v", len(pods), err)
			}
			expected := 2
			if which == "budget" {
				expected = 100
			}
			if *calls != expected {
				t.Fatalf("calls=%d want=%d", *calls, expected)
			}
		})
	}
}

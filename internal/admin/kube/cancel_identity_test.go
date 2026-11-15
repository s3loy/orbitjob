package kube

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/domain/resource"
)

func TestCancelRequiresClientAndAddress(t *testing.T) {
	id := v1alpha1.RunIdentity{}
	if _, err := (JobRunPublisher{}).RequestCancel(t.Context(), "ns", "def", id); err == nil {
		t.Fatal("missing client accepted")
	}
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	for _, address := range [][2]string{{"", "def"}, {"ns", ""}} {
		if _, err := (JobRunPublisher{Client: client}).RequestCancel(t.Context(), address[0], address[1], id); err == nil {
			t.Fatal("missing address accepted")
		}
	}
}

func TestCancelResolvesAndChecksCompleteIdentity(t *testing.T) {
	id := v1alpha1.RunIdentity{SourceUID: "def-1", RevisionID: 7, OccurrenceKey: "occ-1", Trigger: v1alpha1.Manual}
	for _, scenario := range []string{"modern", "legacy", "modern_conflict", "legacy_conflict", "missing", "transport"} {
		t.Run(scenario, func(t *testing.T) {
			name := v1alpha1.RunObjectName("nightly", id.OccurrenceKey)
			if strings.HasPrefix(scenario, "legacy") {
				name = v1alpha1.LegacyRunObjectName("nightly", id.OccurrenceKey)
			}
			object := seededJobRun("ns", name)
			if strings.HasSuffix(scenario, "conflict") {
				_ = unstructured.SetNestedField(object.Object, "another-occurrence", "spec", "occurrenceKey")
			}
			objects := []runtime.Object{object}
			if scenario == "missing" {
				objects = nil
			}
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), objects...)
			patches := 0
			client.PrependReactor("patch", "jobruns", func(action k8stesting.Action) (bool, runtime.Object, error) {
				patches++
				var operations []struct {
					Op, Path string
					Value    any
				}
				if err := json.Unmarshal(action.(k8stesting.PatchAction).GetPatch(), &operations); err != nil {
					t.Fatal(err)
				}
				if len(operations) != 3 || operations[0].Op != "test" || operations[0].Path != "/metadata/uid" || operations[1].Op != "test" || operations[1].Path != "/metadata/resourceVersion" || operations[2].Op != "add" || operations[2].Path != "/spec/cancelRequested" || operations[2].Value != true {
					t.Fatalf("patch changed fields beyond identity tests and cancellation: %+v", operations)
				}
				if scenario == "transport" {
					return true, nil, errors.New("api server unavailable")
				}
				return false, nil, nil
			})
			actual, err := (JobRunPublisher{Client: client}).RequestCancel(t.Context(), "ns", "nightly", id)
			switch scenario {
			case "modern", "legacy":
				if err != nil || actual != name || patches != 1 {
					t.Fatalf("resolved=%s patches=%d err=%v", actual, patches, err)
				}
				stored, getErr := client.Resource(jobRunGVR).Namespace("ns").Get(t.Context(), name, metav1.GetOptions{})
				if getErr != nil {
					t.Fatal(getErr)
				}
				actor, _, _ := unstructured.NestedString(stored.Object, "spec", "actor")
				if actor != "original" {
					t.Fatal("cancel changed actor")
				}
			case "transport":
				if err == nil || !strings.Contains(err.Error(), "api server unavailable") {
					t.Fatalf("lost transport error: %v", err)
				}
			default:
				var conflict *resource.ConflictError
				if !errors.As(err, &conflict) || patches != 0 {
					t.Fatalf("unsafe cancellation patches=%d err=%v", patches, err)
				}
			}
		})
	}
}

package kube

import (
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/domain/resource"
)

func publishIdentityFixture(workflow bool, name string) *unstructured.Unstructured {
	object := seededJobRun("ns", name)
	if workflow {
		object.SetKind("WorkflowRun")
		ref, _, _ := unstructured.NestedMap(object.Object, "spec", "scheduledJobRef")
		unstructured.RemoveNestedField(object.Object, "spec", "scheduledJobRef")
		_ = unstructured.SetNestedMap(object.Object, ref, "spec", "workflowRef")
	}
	return object
}

func publishIdentityCall(t *testing.T, client dynamic.Interface, workflow bool, name, actor string) (string, string, bool, error) {
	t.Helper()
	job := jobRunFor("ns", name)
	job.Spec.Actor = actor
	if workflow {
		run := v1alpha1.WorkflowRun{ObjectMeta: job.ObjectMeta, Spec: v1alpha1.WorkflowRunSpec{
			WorkflowRef: job.Spec.ScheduledJobRef, DefinitionRevision: job.Spec.DefinitionRevision,
			Trigger: job.Spec.Trigger, Actor: job.Spec.Actor, OccurrenceKey: job.Spec.OccurrenceKey,
		}}
		stored, created, err := (WorkflowRunPublisher{Client: client}).Create(t.Context(), run)
		return stored.Name, stored.Spec.Actor, created, err
	}
	stored, created, err := (JobRunPublisher{Client: client}).Publish(t.Context(), job)
	return stored.Name, stored.Spec.Actor, created, err
}

func TestPublishChecksImmutableIdentityAndPreservesLegacyReplay(t *testing.T) {
	for _, workflow := range []bool{false, true} {
		kind := "job"
		if workflow {
			kind = "workflow"
		}
		for _, scenario := range []string{"new", "modern_replay", "legacy_replay", "legacy_collision", "source_conflict", "revision_conflict", "trigger_conflict", "key_conflict", "create_race", "create_race_conflict"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				modern := v1alpha1.RunObjectName("nightly", "occ-1")
				legacy := v1alpha1.LegacyRunObjectName("nightly", "occ-1")
				name := modern
				if scenario == "legacy_replay" || scenario == "legacy_collision" {
					name = legacy
				}
				object := publishIdentityFixture(workflow, name)
				switch scenario {
				case "source_conflict":
					ref := "scheduledJobRef"
					if workflow {
						ref = "workflowRef"
					}
					_ = unstructured.SetNestedField(object.Object, "different-source", "spec", ref, "uid")
				case "revision_conflict":
					_ = unstructured.SetNestedField(object.Object, int64(8), "spec", "definitionRevision")
				case "trigger_conflict":
					_ = unstructured.SetNestedField(object.Object, "Scheduled", "spec", "trigger")
				case "key_conflict", "legacy_collision", "create_race_conflict":
					_ = unstructured.SetNestedField(object.Object, "different-key", "spec", "occurrenceKey")
				}
				objects := []runtime.Object{object}
				if scenario == "new" || scenario == "create_race" || scenario == "create_race_conflict" {
					objects = nil
				}
				client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), objects...)
				gvr := jobRunGVR
				if workflow {
					gvr = workflowRunGVR
				}
				creates := 0
				client.PrependReactor("create", gvr.Resource, func(action k8stesting.Action) (bool, runtime.Object, error) {
					creates++
					if scenario != "create_race" && scenario != "create_race_conflict" {
						return false, nil, nil
					}
					if err := client.Tracker().Create(gvr, object, "ns"); err != nil {
						t.Fatal(err)
					}
					return true, nil, apierrors.NewAlreadyExists(gvr.GroupResource(), name)
				})
				requestedActor := "original"
				if scenario == "new" || scenario == "legacy_collision" {
					requestedActor = "alice"
				}
				actual, actor, created, err := publishIdentityCall(t, client, workflow, modern, requestedActor)
				conflict := scenario == "source_conflict" || scenario == "revision_conflict" || scenario == "trigger_conflict" || scenario == "key_conflict" || scenario == "create_race_conflict"
				if conflict {
					var mismatch *resource.ConflictError
					if !errors.As(err, &mismatch) || created {
						t.Fatalf("identity mismatch was adopted: created=%t err=%v", created, err)
					}
					if scenario != "create_race_conflict" && creates != 0 {
						t.Fatal("conflicting modern object reached create")
					}
					return
				}
				wantCreated := scenario == "new" || scenario == "legacy_collision"
				wantName, wantActor := name, "original"
				if wantCreated {
					wantName, wantActor = modern, "alice"
				}
				if err != nil || actual != wantName || actor != wantActor || created != wantCreated {
					t.Fatalf("name=%s actor=%s created=%t err=%v", actual, actor, created, err)
				}
				if _, err := client.Resource(gvr).Namespace("ns").Get(t.Context(), wantName, metav1.GetOptions{}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

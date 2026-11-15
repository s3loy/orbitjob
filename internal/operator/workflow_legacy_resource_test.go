package operator

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/revision"
)

func TestWorkflowObserverResolvesLegacyWithOriginalBinding(t *testing.T) {
	for _, kind := range []string{"legacy", "foreign_legacy", "legacy_actor", "legacy_uid", "modern_conflict"} {
		t.Run(kind, func(t *testing.T) {
			definition, row, obj := retainedWorkflowFixture()
			if kind != "modern_conflict" {
				obj.Name = v1alpha1.LegacyRunObjectName(definition.Name, row.OccurrenceKey)
			}
			obj.Spec.CancelRequested = true
			switch kind {
			case "foreign_legacy", "modern_conflict":
				obj.Spec.OccurrenceKey = "other-full-occurrence"
			case "legacy_actor":
				obj.Spec.Actor = "other-actor"
			case "legacy_uid":
				obj.UID = "replacement"
			}
			rev := workflowRevision(t, row.RevisionID, workflowSpecJSON(t, twoTaskSpec()))
			rev.Identity = definition
			rt := Runtime{Dynamic: fakeDynamic(t, &obj), Revisions: &fakeWorkflowRevisions{byID: map[int64]revision.Revision{row.RevisionID: rev}}}
			cancel, err := rt.ObserveWorkflowResource(t.Context(), "tenant-a", row)
			if kind == "legacy" {
				if err != nil || !cancel {
					t.Fatalf("original legacy cancel intent not observed: cancel=%t err=%v", cancel, err)
				}
			} else if err == nil || cancel {
				t.Fatalf("foreign or altered resource accepted: cancel=%t err=%v", cancel, err)
			}
		})
	}
}

func TestWorkflowRetentionResolvesLegacyWithStrictDeleteIdentity(t *testing.T) {
	for _, kind := range []string{"legacy", "foreign_legacy", "legacy_actor", "legacy_uid", "legacy_active", "modern_conflict"} {
		t.Run(kind, func(t *testing.T) {
			definition, row, obj := retainedWorkflowFixture()
			if kind != "modern_conflict" {
				obj.Name = v1alpha1.LegacyRunObjectName(definition.Name, row.OccurrenceKey)
			}
			switch kind {
			case "foreign_legacy", "modern_conflict":
				obj.Spec.OccurrenceKey = "other-full-occurrence"
			case "legacy_actor":
				obj.Spec.Actor = "other-actor"
			case "legacy_uid":
				obj.UID = "replacement"
			case "legacy_active":
				obj.Status.Phase = "Running"
			}
			client := fakeDynamic(t, &obj).(*dynamicfake.FakeDynamicClient)
			deleted := expectWorkflowLegacyDelete(t, client, "workflowruns", obj.Name, obj.UID, obj.ResourceVersion)
			err := (RunRemover{Client: client}).RemoveWorkflow(t.Context(), definition, row)
			if kind == "legacy" {
				if err != nil || !*deleted {
					t.Fatalf("legacy terminal resource not removed: deleted=%t err=%v", *deleted, err)
				}
			} else {
				if *deleted || (kind == "foreign_legacy" && err != nil) || (kind != "foreign_legacy" && err == nil) {
					t.Fatalf("unsafe/foreign legacy handling: deleted=%t err=%v", *deleted, err)
				}
				if _, getErr := client.Resource(workflowRunGVR).Namespace(obj.Namespace).Get(t.Context(), obj.Name, metav1.GetOptions{}); getErr != nil {
					t.Fatalf("foreign resource disappeared: %v", getErr)
				}
			}
		})
	}
}

func TestWorkflowStepRetentionResolvesLegacyWithStrictDeleteIdentity(t *testing.T) {
	for _, kind := range []string{"legacy", "foreign_legacy", "legacy_refname", "legacy_active", "legacy_no_version", "modern_conflict"} {
		t.Run(kind, func(t *testing.T) {
			definition, parent, row, obj := retainedStepFixture()
			if kind != "modern_conflict" {
				obj.Name = v1alpha1.LegacyRunObjectName(definition.Name, row.OccurrenceKey)
			}
			switch kind {
			case "foreign_legacy", "modern_conflict":
				obj.Spec.OccurrenceKey = "12345678-other-full-occurrence"
			case "legacy_refname":
				obj.Spec.ScheduledJobRef.Name = "foreign-definition"
			case "legacy_active":
				obj.Status.Phase = "Running"
			case "legacy_no_version":
				obj.ResourceVersion = ""
			}
			client := fakeDynamic(t, &obj).(*dynamicfake.FakeDynamicClient)
			deleted := expectWorkflowLegacyDelete(t, client, "jobruns", obj.Name, obj.UID, obj.ResourceVersion)
			err := (RunRemover{Client: client}).RemoveWorkflowStep(t.Context(), definition, parent, row)
			if kind == "legacy" {
				if err != nil || !*deleted {
					t.Fatalf("legacy terminal step not removed: deleted=%t err=%v", *deleted, err)
				}
			} else {
				if *deleted || (kind == "foreign_legacy" && err != nil) || (kind != "foreign_legacy" && err == nil) {
					t.Fatalf("unsafe/foreign step handling: deleted=%t err=%v", *deleted, err)
				}
				if _, getErr := client.Resource(jobRunGVR).Namespace(obj.Namespace).Get(t.Context(), obj.Name, metav1.GetOptions{}); getErr != nil {
					t.Fatalf("foreign step disappeared: %v", getErr)
				}
			}
		})
	}
}

func expectWorkflowLegacyDelete(t *testing.T, client *dynamicfake.FakeDynamicClient, resource, name string, uid types.UID, version string) *bool {
	t.Helper()
	deleted := new(bool)
	client.PrependReactor("delete", resource, func(action clienttesting.Action) (bool, runtime.Object, error) {
		deleteAction := action.(clienttesting.DeleteAction)
		opts := deleteAction.GetDeleteOptions()
		if deleteAction.GetName() != name || opts.Preconditions == nil || opts.Preconditions.UID == nil || *opts.Preconditions.UID != uid || opts.Preconditions.ResourceVersion == nil || *opts.Preconditions.ResourceVersion != version {
			t.Fatalf("legacy deletion lost actual address/instance guard: name=%s options=%+v", deleteAction.GetName(), opts)
		}
		*deleted = true
		return false, nil, nil
	})
	return deleted
}

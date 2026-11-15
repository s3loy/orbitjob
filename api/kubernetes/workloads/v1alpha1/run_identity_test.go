package v1alpha1

import (
	"context"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"strings"
	"testing"
)

func TestRunNamesKeepFullOccurrenceIdentity(t *testing.T) {
	a := strings.Repeat("a", 64)
	b := a[:63] + "b"
	if RunObjectName("report", a) == RunObjectName("report", b) {
		t.Fatal("distinct full occurrence keys collide")
	}
	if n := RunObjectName(strings.Repeat("a", 253), a); len(n) > 253 {
		t.Fatalf("name too long: %d", len(n))
	}
}
func TestResolveRunAdoptsCheckedLegacyIdentity(t *testing.T) {
	key := strings.Repeat("a", 64)
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": GroupVersion.String(), "kind": "JobRun", "metadata": map[string]any{"name": "report-aaaaaaaa", "namespace": "tasks"}, "spec": map[string]any{"scheduledJobRef": map[string]any{"uid": "source"}, "occurrenceKey": key, "definitionRevision": int64(7), "trigger": "Manual"}}}
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), obj)
	resource := client.Resource(schema.GroupVersionResource{Group: GroupVersion.Group, Version: GroupVersion.Version, Resource: "jobruns"}).Namespace("tasks")
	live, found, err := ResolveRun(context.Background(), resource, RunNameCandidates("report", key), RunIdentity{SourceUID: "source", OccurrenceKey: key, RevisionID: 7, Trigger: Manual})
	if err != nil || !found || live.GetName() != "report-aaaaaaaa" {
		t.Fatalf("found=%v object=%v err=%v", found, live, err)
	}
	if _, adopted, err := ResolveRun(context.Background(), resource, RunNameCandidates("report", key), RunIdentity{SourceUID: "other", OccurrenceKey: key, RevisionID: 7, Trigger: Manual}); err != nil || adopted {
		t.Fatalf("foreign legacy collision should be skipped: adopted=%v err=%v", adopted, err)
	}
}

func TestLongDefinitionSkipsImpossibleLegacyAddress(t *testing.T) {
	candidates := RunNameCandidates(strings.Repeat("a", 253), strings.Repeat("b", 64))
	if len(candidates) != 1 || len(candidates[0]) > 253 {
		t.Fatalf("impossible historical address would fail lookup before modern create: %v", candidates)
	}
}

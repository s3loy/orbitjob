package kube

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
	"orbitjob/internal/admin/definition"
	"orbitjob/internal/domain/resource"
)

const definitionTestSpec = `{"schedule":"* * * * *","jobTemplate":{"image":"example/task:v1"}}`
const definitionTestRequest = "a2098db5-f383-4fd7-b46d-61286a7f3869"

func TestDefinitionDryRunAdmissionCreateAndReconcile(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependReactor("create", "scheduledjobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		a := action.(ktesting.CreateAction)
		obj := a.GetObject().(*unstructured.Unstructured).DeepCopy()
		obj.SetUID(types.UID("immutable"))
		obj.SetResourceVersion("7")
		// Fake client trackers do not implement DryRun; explicitly model admission.
		if len(action.(interface{ GetCreateOptions() metav1.CreateOptions }).GetCreateOptions().DryRun) > 0 {
			return true, obj, nil
		}
		return false, nil, nil
	})
	d := DefinitionClient{Client: client, NamespaceTenants: map[string]string{"own": "tenant"}}
	in := definition.ApplyRequest{RequestID: definitionTestRequest, SpecJSON: definitionTestSpec, DryRun: true}
	out, err := d.Apply(context.Background(), "jobs", "own", "task", in)
	if err != nil {
		t.Fatal(err)
	}
	if out.State != "validated" || out.UID != "" || out.ResourceVersion != "" || out.ManifestDigest == "" {
		t.Fatalf("bad dry run: %+v", out)
	}
	if _, err = d.Get(context.Background(), "jobs", "own", "task"); err == nil {
		t.Fatal("dry run persisted")
	}
	in.DryRun = false
	in.ManifestDigest = out.ManifestDigest
	in.SpecJSON = out.SpecJSON
	accepted, err := d.Apply(context.Background(), "jobs", "own", "task", in)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.State != "accepted" {
		t.Fatalf("bad accept: %+v", accepted)
	}
	replay, err := d.Apply(context.Background(), "jobs", "own", "task", in)
	if err != nil || replay.State != "reconciled" {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	creates := 0
	for _, a := range client.Actions() {
		if a.GetVerb() == "create" {
			creates++
		}
	}
	if creates != 2 {
		t.Fatalf("replay performed write: %d creates", creates)
	}
	in.SpecJSON = strings.Replace(definitionTestSpec, "v1", "v2", 1)
	_, in.ManifestDigest, _ = definition.CanonicalSpec("jobs", in.SpecJSON)
	if _, err = d.Apply(context.Background(), "jobs", "own", "task", in); err == nil {
		t.Fatal("request ID reused for different spec")
	}
}
func TestDefinitionUpdateCASAndMetadataPreserved(t *testing.T) {
	canonical, digest, err := definition.CanonicalSpec("jobs", definitionTestSpec)
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	_ = json.Unmarshal([]byte(canonical), &spec)
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "workloads.orbitjob.io/v1alpha1", "kind": "ScheduledJob", "metadata": map[string]any{"name": "task", "namespace": "own", "uid": "uid-1", "resourceVersion": "42", "annotations": map[string]any{"gitops": "keep"}}, "spec": spec}}
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), obj)
	sawPatch := false
	client.PrependReactor("patch", "scheduledjobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		a := action.(ktesting.PatchAction)
		if a.GetPatchType() != types.JSONPatchType {
			t.Fatal("not atomic JSON patch")
		}
		var p []map[string]any
		if err := json.Unmarshal(a.GetPatch(), &p); err != nil {
			t.Fatal(err)
		}
		if p[0]["op"] != "test" || p[0]["path"] != "/metadata/uid" || p[0]["value"] != "uid-1" || p[1]["path"] != "/metadata/resourceVersion" || p[1]["value"] != "42" {
			t.Fatalf("missing atomic identity tests: %s", a.GetPatch())
		}
		if len(p) != 4 || p[2]["path"] != "/spec" || p[3]["path"] != "/metadata/annotations" {
			t.Fatal("unexpected mutation surface")
		}
		if p[3]["value"].(map[string]any)["gitops"] != "keep" {
			t.Fatal("discarded existing annotations")
		}
		sawPatch = true
		return false, nil, nil
	})
	d := DefinitionClient{Client: client, NamespaceTenants: map[string]string{"own": "tenant"}}
	in := definition.ApplyRequest{RequestID: definitionTestRequest, ExpectedUID: "uid-1", ExpectedResourceVersion: "41", SpecJSON: canonical, ManifestDigest: digest}
	_, err = d.Apply(context.Background(), "jobs", "own", "task", in)
	var conflict *resource.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("stale version: %v", err)
	}
	if sawPatch {
		t.Fatal("stale version reached mutation")
	}
	in.ExpectedResourceVersion = "42"
	if _, err = d.Apply(context.Background(), "jobs", "own", "task", in); err != nil {
		t.Fatal(err)
	}
	if !sawPatch {
		t.Fatal("no patch")
	}
	if _, err = d.Get(context.Background(), "jobs", "foreign", "task"); err == nil {
		t.Fatal("unmapped namespace accepted")
	}
	// Replacement UID must still conflict even when the resourceVersion matches.
	in.RequestID = "c824c867-8937-418b-b141-b482ea930c57"
	in.ExpectedUID = "old-uid"
	if _, err = d.Apply(context.Background(), "jobs", "own", "task", in); !errors.As(err, &conflict) {
		t.Fatalf("UID replacement: %v", err)
	}
}

func TestDefinitionLostCreateResponseReconcilesWithoutAnotherWrite(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	creates := 0
	client.PrependReactor("create", "scheduledjobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		creates++
		obj := action.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured).DeepCopy()
		obj.SetUID("accepted-uid")
		obj.SetResourceVersion("1")
		if err := client.Tracker().Add(obj); err != nil {
			t.Fatal(err)
		}
		return true, nil, io.ErrUnexpectedEOF
	})
	d := DefinitionClient{Client: client, NamespaceTenants: map[string]string{"own": "tenant"}}
	spec, digest, err := definition.CanonicalSpec("jobs", definitionTestSpec)
	if err != nil {
		t.Fatal(err)
	}
	in := definition.ApplyRequest{RequestID: definitionTestRequest, SpecJSON: spec, ManifestDigest: digest}
	if _, err = d.Apply(context.Background(), "jobs", "own", "task", in); err == nil {
		t.Fatal("lost response must remain unknown to first caller")
	}
	out, err := d.Apply(context.Background(), "jobs", "own", "task", in)
	if err != nil || out.State != "reconciled" || out.UID != "accepted-uid" || creates != 1 {
		t.Fatalf("bad reconciliation: %+v err=%v writes=%d", out, err, creates)
	}
	// A replacement copied from the old object may preserve server annotations.
	// An update request that knew the original UID must still refuse that clone.
	obj, err := client.Resource(schema.GroupVersionResource{Group: "workloads.orbitjob.io", Version: "v1alpha1", Resource: "scheduledjobs"}).Namespace("own").Get(context.Background(), "task", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	obj.SetUID("replacement-uid")
	if err = client.Tracker().Update(schema.GroupVersionResource{Group: "workloads.orbitjob.io", Version: "v1alpha1", Resource: "scheduledjobs"}, obj, "own"); err != nil {
		t.Fatal(err)
	}
	in.ExpectedUID = "accepted-uid"
	in.ExpectedResourceVersion = "1"
	_, err = d.Apply(context.Background(), "jobs", "own", "task", in)
	var conflict *resource.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("clone accepted as original update: %v", err)
	}
}

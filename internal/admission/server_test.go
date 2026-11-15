package admission

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"orbitjob/internal/admin/definition"
	"orbitjob/internal/platform/scan"
)

type authorizer struct {
	called int
	intent definition.AdmissionIntent
	err    error
}

func (a *authorizer) AdmitDefinition(_ context.Context, in definition.AdmissionIntent) error {
	a.called++
	a.intent = in
	return a.err
}

func definitionReview(t *testing.T) (Server, *authorizer, *admissionv1.AdmissionRequest) {
	t.Helper()
	store := &authorizer{}
	s := Server{Namespaces: map[string]string{"finance-prod": scan.GenerateID()}, DefinitionWriter: "system:serviceaccount:orbitjob:orbitjob-admin-api", Definitions: store}
	canonical, digest, err := definition.CanonicalSpec("jobs", `{"jobTemplate":{"image":"busybox:1.37.0"}}`)
	if err != nil {
		t.Fatal(err)
	}
	var spec any
	if err = json.Unmarshal([]byte(canonical), &spec); err != nil {
		t.Fatal(err)
	}
	request := uuid.NewString()
	uid := uuid.NewString()
	object := map[string]any{"apiVersion": "workloads.orbitjob.io/v1alpha1", "kind": "ScheduledJob", "metadata": map[string]any{"namespace": "finance-prod", "name": "billing-reconciliation", "uid": uid, "annotations": map[string]string{definition.RequestAnnotation: request, definition.DigestAnnotation: digest, definition.ProofAnnotation: "opaque-dispatch-proof"}}, "spec": spec}
	raw, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	return s, store, &admissionv1.AdmissionRequest{UID: types.UID(uuid.NewString()), Name: "billing-reconciliation", Namespace: "finance-prod", Resource: metav1.GroupVersionResource{Group: "workloads.orbitjob.io", Version: "v1alpha1", Resource: "scheduledjobs"}, Operation: admissionv1.Create, UserInfo: authenticationv1.UserInfo{Username: s.DefinitionWriter}, Object: runtime.RawExtension{Raw: raw}}
}

func TestFinalAdmissionRejectsMutatedExecutionAndUntrustedWriter(t *testing.T) {
	s, store, in := definitionReview(t)
	in.Object.Raw = []byte(string(in.Object.Raw[:]))
	var object map[string]any
	_ = json.Unmarshal(in.Object.Raw, &object)
	object["spec"].(map[string]any)["jobTemplate"].(map[string]any)["image"] = "changed:latest"
	in.Object.Raw, _ = json.Marshal(object)
	if s.validate(context.Background(), in).Allowed || store.called != 0 {
		t.Fatal("mutating admission changed reviewed execution")
	}
	s, store, in = definitionReview(t)
	in.UserInfo.Username = "untrusted-writer"
	if s.validate(context.Background(), in).Allowed || store.called != 0 {
		t.Fatal("untrusted writer bypassed checked API")
	}
}

func TestDryRunHasNoAdmissionSideEffectAndActualSaveRequiresStore(t *testing.T) {
	s, store, in := definitionReview(t)
	dryRun := true
	in.DryRun = &dryRun
	if !s.validate(context.Background(), in).Allowed || store.called != 0 {
		t.Fatal("dry-run reserved a real UID")
	}
	dryRun = false
	if !s.validate(context.Background(), in).Allowed || store.called != 1 || store.intent.UID == "" || store.intent.Proof == "" {
		t.Fatal("real save did not validate its durable dispatch")
	}
	store.err = errors.New("database unavailable")
	if s.validate(context.Background(), in).Allowed {
		t.Fatal("unavailable check store allowed persistence")
	}
}

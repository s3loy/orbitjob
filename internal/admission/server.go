// Package admission verifies final workload definitions before persistence.
package admission

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"orbitjob/internal/admin/definition"
)

type DefinitionAuthorizer interface {
	AdmitDefinition(context.Context, definition.AdmissionIntent) error
}

type Server struct {
	Namespaces       map[string]string
	DefinitionWriter string
	NativeWriters    map[string]bool
	Definitions      DefinitionAuthorizer
}

func (s Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 512*1024))
	var review admissionv1.AdmissionReview
	if err != nil || json.Unmarshal(body, &review) != nil || review.Request == nil || review.Request.UID == "" {
		http.Error(w, "invalid admission review", http.StatusBadRequest)
		return
	}
	response := s.validate(ctx, review.Request)
	response.UID = review.Request.UID
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(admissionv1.AdmissionReview{TypeMeta: metav1.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"}, Response: response})
}

func denied(message string) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{Allowed: false, Result: &metav1.Status{Code: http.StatusForbidden, Reason: metav1.StatusReasonForbidden, Message: message}}
}

func (s Server) validate(ctx context.Context, in *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	allowed := &admissionv1.AdmissionResponse{Allowed: true}
	tenant, mapped := s.Namespaces[in.Namespace]
	if !mapped || in.SubResource == "status" || in.Operation == admissionv1.Delete {
		return allowed
	}
	kind := ""
	if in.Resource.Group == "workloads.orbitjob.io" && in.Resource.Resource == "scheduledjobs" {
		kind = "jobs"
	}
	if in.Resource.Group == "workloads.orbitjob.io" && in.Resource.Resource == "workflowjobs" {
		kind = "workflows"
	}
	if kind == "" {
		return denied("this workload kind is not supported by the definition admission endpoint")
	}
	var object unstructured.Unstructured
	if json.Unmarshal(in.Object.Raw, &object) != nil {
		return denied("the final definition cannot be decoded")
	}
	if object.GetNamespace() != in.Namespace || object.GetName() != in.Name {
		return denied("definition target identity changed")
	}
	spec, ok := object.Object["spec"]
	if !ok {
		return denied("definition spec is required")
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return denied("the final spec cannot be decoded")
	}
	_, digest, err := definition.CanonicalSpec(kind, string(raw))
	if err != nil {
		return denied("the final spec is not a supported execution configuration")
	}
	if s.NativeWriters[in.UserInfo.Username] {
		return allowed
	}
	if in.UserInfo.Username != s.DefinitionWriter {
		return denied("use the checked definition API or an explicitly configured native writer")
	}
	if in.DryRun != nil && *in.DryRun {
		return allowed
	}
	annotations := object.GetAnnotations()
	if annotations[definition.DigestAnnotation] != digest {
		return denied("admission changed the reviewed execution configuration")
	}
	var old unstructured.Unstructured
	if in.Operation == admissionv1.Update {
		if json.Unmarshal(in.OldObject.Raw, &old) != nil {
			return denied("original definition identity is unavailable")
		}
		oldRaw, err := json.Marshal(old.Object["spec"])
		if err != nil {
			return denied("original definition cannot be decoded")
		}
		_, oldDigest, err := definition.CanonicalSpec(kind, string(oldRaw))
		// Unchanged spec metadata maintenance requires the same immutable object.
		if err == nil && oldDigest == digest && old.GetUID() == object.GetUID() && annotations[definition.RequestAnnotation] == old.GetAnnotations()[definition.RequestAnnotation] {
			return allowed
		}
	}
	if s.Definitions == nil {
		return denied("definition check storage is unavailable")
	}
	intent := definition.AdmissionIntent{TenantID: tenant, RequestID: annotations[definition.RequestAnnotation], Kind: kind, Namespace: in.Namespace, Name: in.Name, UID: string(object.GetUID()), OldUID: string(old.GetUID()), OldResourceVersion: old.GetResourceVersion(), Digest: digest, Proof: annotations[definition.ProofAnnotation]}
	if err := s.Definitions.AdmitDefinition(ctx, intent); err != nil {
		return denied("the final object does not match a live checked save operation")
	}
	return allowed
}

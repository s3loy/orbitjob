package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
	v1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/admin/definition"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

const requestAnnotation = "orbitjob.io/admin-request-id"
const digestAnnotation = "orbitjob.io/admin-manifest-digest"
const proofAnnotation = "orbitjob.io/admin-admission-proof"

type DefinitionClient struct {
	Client           dynamic.Interface
	NamespaceTenants map[string]string
}

func (d DefinitionClient) Tenant(namespace string) (string, bool) {
	tenant, ok := d.NamespaceTenants[namespace]
	return tenant, ok && tenant != ""
}
func definitionResource(kind string) (schema.GroupVersionResource, string, error) {
	name := "ScheduledJob"
	plural := "scheduledjobs"
	if kind == "workflows" {
		name = "WorkflowJob"
		plural = "workflowjobs"
	} else if kind != "jobs" {
		return schema.GroupVersionResource{}, "", validation.New("kind", "unsupported definition kind")
	}
	return schema.GroupVersionResource{Group: v1.GroupVersion.Group, Version: v1.GroupVersion.Version, Resource: plural}, name, nil
}
func (d DefinitionClient) target(kind, namespace, name string) (dynamic.ResourceInterface, string, error) {
	if _, ok := d.Tenant(namespace); !ok {
		return nil, "", &resource.ScopeError{Resource: "definition", Scope: "unmapped namespace"}
	}
	if len(kvalidation.IsDNS1123Subdomain(name)) != 0 || len(kvalidation.IsDNS1123Label(namespace)) != 0 {
		return nil, "", validation.New("name", "invalid Kubernetes name or namespace")
	}
	gvr, k, err := definitionResource(kind)
	if err != nil {
		return nil, "", err
	}
	if d.Client == nil {
		return nil, "", fmt.Errorf("definition client unavailable")
	}
	return d.Client.Resource(gvr).Namespace(namespace), k, nil
}
func definitionResult(kind string, obj *unstructured.Unstructured) (definition.Result, error) {
	raw, ok := obj.Object["spec"]
	if !ok {
		return definition.Result{}, fmt.Errorf("definition spec missing")
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return definition.Result{}, err
	}
	canonical, digest, err := definition.CanonicalSpec(kind, string(b))
	if err != nil {
		return definition.Result{}, err
	}
	a := obj.GetAnnotations()
	return definition.Result{Kind: kind, Name: obj.GetName(), Namespace: obj.GetNamespace(), UID: string(obj.GetUID()), ResourceVersion: obj.GetResourceVersion(), Generation: obj.GetGeneration(), SpecJSON: canonical, ManifestDigest: digest, LastRequestID: a[requestAnnotation], LastManifestDigest: a[digestAnnotation]}, nil
}
func definitionError(err error) error {
	if apierrors.IsNotFound(err) {
		return &resource.NotFoundError{Resource: "definition"}
	}
	if apierrors.IsInvalid(err) {
		return validation.New("admission", "Kubernetes admission rejected this configuration; review the supplied fields")
	}
	if apierrors.IsAlreadyExists(err) {
		return &resource.ConflictError{Resource: "definition", Field: "name", Message: "this name is already in use; load the existing resource before editing"}
	}
	if apierrors.IsConflict(err) {
		return &resource.ConflictError{Resource: "definition", Field: "resource_version", Message: "the resource changed; compare and merge against its current version"}
	}
	if apierrors.IsForbidden(err) {
		return &resource.ScopeError{Resource: "definition", Scope: "Kubernetes access"}
	}
	return err
}
func (d DefinitionClient) Get(ctx context.Context, kind, namespace, name string) (definition.Result, error) {
	client, _, err := d.target(kind, namespace, name)
	if err != nil {
		return definition.Result{}, err
	}
	obj, err := client.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return definition.Result{}, definitionError(err)
	}
	return definitionResult(kind, obj)
}
func (d DefinitionClient) Apply(ctx context.Context, kind, namespace, name string, in definition.ApplyRequest) (definition.Result, error) {
	client, k, err := d.target(kind, namespace, name)
	if err != nil {
		return definition.Result{}, err
	}
	if _, err = uuid.Parse(in.RequestID); err != nil {
		return definition.Result{}, validation.New("request_id", "UUID required")
	}
	if (in.ExpectedUID == "") != (in.ExpectedResourceVersion == "") {
		return definition.Result{}, validation.New("expected_uid", "UID and resourceVersion must be supplied together")
	}
	canonical, digest, err := definition.CanonicalSpec(kind, in.SpecJSON)
	if err != nil {
		return definition.Result{}, err
	}
	if !in.DryRun && in.ManifestDigest != digest {
		return definition.Result{}, &resource.ConflictError{Resource: "definition", Message: "validate this exact spec and acknowledge its digest before applying"}
	}
	current, err := client.Get(ctx, name, metav1.GetOptions{})
	absent := apierrors.IsNotFound(err)
	if err != nil && !absent {
		return definition.Result{}, definitionError(err)
	}
	conflict := func() (definition.Result, error) {
		return definition.Result{}, &resource.ConflictError{Resource: "definition", Message: "UID, resourceVersion or request digest changed; reload and reconcile before applying"}
	}
	if !absent {
		if in.ExpectedUID != "" && string(current.GetUID()) != in.ExpectedUID {
			return conflict()
		}
		a := current.GetAnnotations()
		if a[requestAnnotation] == in.RequestID {
			if a[digestAnnotation] != digest {
				return conflict()
			}
			out, e := definitionResult(kind, current)
			if e != nil {
				return out, e
			}
			if out.ManifestDigest != digest {
				return conflict()
			}
			out.RequestID = in.RequestID
			out.State = "reconciled"
			return out, nil
		}
		if string(current.GetUID()) != in.ExpectedUID || current.GetResourceVersion() != in.ExpectedResourceVersion {
			return conflict()
		}
	} else if in.ExpectedUID != "" {
		return conflict()
	}
	if kind == "workflows" {
		if err := d.validateWorkflowReferences(ctx, namespace, canonical); err != nil {
			return definition.Result{}, err
		}
	}
	// Preserve integral JSON values: Unstructured represents integers as int64,
	// unlike an ordinary interface{} unmarshal which would round through float64.
	dec := json.NewDecoder(strings.NewReader(canonical))
	dec.UseNumber()
	var spec map[string]any
	if err = dec.Decode(&spec); err != nil {
		return definition.Result{}, err
	}
	spec, err = integerObject(spec)
	if err != nil {
		return definition.Result{}, err
	}
	options := metav1.CreateOptions{}
	patchOptions := metav1.PatchOptions{}
	if in.DryRun {
		options.DryRun = []string{metav1.DryRunAll}
		patchOptions.DryRun = []string{metav1.DryRunAll}
	}
	var obj *unstructured.Unstructured
	if absent {
		obj = &unstructured.Unstructured{Object: map[string]any{"apiVersion": v1.GroupVersion.String(), "kind": k, "metadata": map[string]any{"name": name, "namespace": namespace, "annotations": map[string]any{requestAnnotation: in.RequestID, digestAnnotation: digest}}, "spec": spec}}
		if in.AdmissionProof != "" {
			annotations := obj.GetAnnotations()
			annotations[proofAnnotation] = in.AdmissionProof
			obj.SetAnnotations(annotations)
		}
		obj, err = client.Create(ctx, obj, options)
	} else {
		annotations := current.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[requestAnnotation] = in.RequestID
		annotations[digestAnnotation] = digest
		if in.AdmissionProof != "" {
			annotations[proofAnnotation] = in.AdmissionProof
		}
		patch := []map[string]any{{"op": "test", "path": "/metadata/uid", "value": in.ExpectedUID}, {"op": "test", "path": "/metadata/resourceVersion", "value": in.ExpectedResourceVersion}, {"op": "replace", "path": "/spec", "value": spec}, {"op": "add", "path": "/metadata/annotations", "value": annotations}}
		payload, _ := json.Marshal(patch)
		obj, err = client.Patch(ctx, name, types.JSONPatchType, payload, patchOptions)
	}
	if err != nil {
		return definition.Result{}, definitionError(err)
	}
	out, err := definitionResult(kind, obj)
	if err != nil {
		return out, err
	}
	out.RequestID = in.RequestID
	out.State = "accepted"
	if in.DryRun {
		out.State = "validated"
		if absent {
			out.UID = ""
			out.ResourceVersion = ""
			out.Generation = 0
			out.LastRequestID = ""
			out.LastManifestDigest = ""
		} else {
			out.UID = string(current.GetUID())
			out.ResourceVersion = current.GetResourceVersion()
			out.Generation = current.GetGeneration()
			out.LastRequestID = current.GetAnnotations()[requestAnnotation]
			out.LastManifestDigest = current.GetAnnotations()[digestAnnotation]
		}
	}
	slog.InfoContext(ctx, "definition request completed", "kind", kind, "namespace", namespace, "name", name, "request_id", in.RequestID, "digest", digest, "state", out.State)
	return out, nil
}
func integerObject(in map[string]any) (map[string]any, error) {
	var convert func(any) (any, error)
	convert = func(v any) (any, error) {
		switch x := v.(type) {
		case json.Number:
			return x.Int64()
		case map[string]any:
			for k, item := range x {
				n, e := convert(item)
				if e != nil {
					return nil, e
				}
				x[k] = n
			}
		case []any:
			for i, item := range x {
				n, e := convert(item)
				if e != nil {
					return nil, e
				}
				x[i] = n
			}
		}
		return v, nil
	}
	out, err := convert(in)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, io.ErrUnexpectedEOF
	}
	return out.(map[string]any), nil
}

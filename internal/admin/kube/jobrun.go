// Package kube holds the small Kubernetes client the admin API needs to ask the
// operator to do ledger work the API is not allowed to do itself.
//
// The actor of a manual run travels in spec.actor, the field the CRD makes
// required and validates. It used to ride an orbitjob.io/actor annotation, which
// admission cannot check and which the CRD does not require; the operator now
// reads the actor from the spec, so the API must set it there or the run is
// rejected.
package kube

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/domain/resource"
)

var jobRunGVR = schema.GroupVersionResource{
	Group:    v1alpha1.GroupVersion.Group,
	Version:  v1alpha1.GroupVersion.Version,
	Resource: "jobruns",
}

// JobRunPublisher creates JobRun custom resources for manual triggers.
type JobRunPublisher struct {
	Client dynamic.Interface
}

// Publish creates the JobRun, or returns the existing one when a run already
// exists under the same name. An AlreadyExists result is success, not conflict:
// the object name is derived from the occurrence key, so the same trigger
// replaying is deduplication, and a run whose create was retried after a
// partial failure adopts the object rather than doubling it.
//
// The returned bool reports whether this call created the object, so the caller
// can answer 201 for a new run and 200 for a replay.
func (p JobRunPublisher) Publish(ctx context.Context, run v1alpha1.JobRun) (v1alpha1.JobRun, bool, error) {
	if p.Client == nil {
		return v1alpha1.JobRun{}, false, fmt.Errorf("dynamic client is required")
	}
	if run.Namespace == "" || run.Name == "" {
		return v1alpha1.JobRun{}, false, fmt.Errorf("job run namespace and name are required")
	}

	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&run)
	if err != nil {
		return v1alpha1.JobRun{}, false, fmt.Errorf("encode job run %s: %w", run.Name, err)
	}

	created, err := p.Client.Resource(jobRunGVR).Namespace(run.Namespace).
		Create(ctx, &unstructured.Unstructured{Object: object}, metav1.CreateOptions{})
	if err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return v1alpha1.JobRun{}, false, fmt.Errorf("create job run %s/%s: %w", run.Namespace, run.Name, err)
		}
		existing, getErr := p.Client.Resource(jobRunGVR).Namespace(run.Namespace).
			Get(ctx, run.Name, metav1.GetOptions{})
		if getErr != nil {
			return v1alpha1.JobRun{}, false, fmt.Errorf("read existing job run %s/%s: %w", run.Namespace, run.Name, getErr)
		}
		out, decodeErr := decodeJobRun(existing)
		if decodeErr != nil {
			return v1alpha1.JobRun{}, false, decodeErr
		}
		return out, false, nil
	}

	out, err := decodeJobRun(created)
	if err != nil {
		return v1alpha1.JobRun{}, false, err
	}
	return out, true, nil
}

// Get reads one JobRun by namespace and name. The invoke route's synchronous
// variant polls it -- the CR the API just created is itself the read model,
// because the operator patches status from stored state. A missing object is a
// NotFoundError, not an internal error: the caller addresses the run by the
// name this package derives, so "not there" is an answer about the run.
func (p JobRunPublisher) Get(ctx context.Context, namespace, name string) (v1alpha1.JobRun, error) {
	if p.Client == nil {
		return v1alpha1.JobRun{}, fmt.Errorf("dynamic client is required")
	}
	if namespace == "" || name == "" {
		return v1alpha1.JobRun{}, fmt.Errorf("job run namespace and name are required")
	}

	object, err := p.Client.Resource(jobRunGVR).Namespace(namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return v1alpha1.JobRun{}, &resource.NotFoundError{
				Resource: "job run resource",
				ID:       namespace + "/" + name,
			}
		}
		return v1alpha1.JobRun{}, fmt.Errorf("get job run %s/%s: %w", namespace, name, err)
	}
	return decodeJobRun(object)
}

func decodeJobRun(object *unstructured.Unstructured) (v1alpha1.JobRun, error) {
	var out v1alpha1.JobRun
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &out); err != nil {
		return v1alpha1.JobRun{}, fmt.Errorf("decode job run %s/%s: %w",
			object.GetNamespace(), object.GetName(), err)
	}
	return out, nil
}

// cancelPatch is the merge patch a cancel request sends. It touches only
// spec.cancelRequested, so a patch can never clobber the rest of the spec the
// scheduler or the operator last wrote.
const cancelPatch = `{"spec":{"cancelRequested":true}}`

// RequestCancel patches spec.cancelRequested to true on one JobRun. The patch
// is the whole effect of the request: the operator observes it, deletes the
// Kubernetes Job, and writes the ledger transitions. Repeated patches are
// no-ops, so a retried request converges on the same state.
//
// A JobRun that is not found is returned as a ConflictError, not a NotFound:
// the caller reads the ledger first, so the run row exists. Ledger-open but
// cluster-absent means the object was lost or already pruned while the row is
// not yet terminal; the scheduler repairs such runs by re-publishing, and a
// caller that reported 404 here would tell the user the run does not exist
// when the ledger says it does.
func (p JobRunPublisher) RequestCancel(ctx context.Context, namespace, name string) error {
	if p.Client == nil {
		return fmt.Errorf("dynamic client is required")
	}
	if namespace == "" || name == "" {
		return fmt.Errorf("job run namespace and name are required")
	}

	_, err := p.Client.Resource(jobRunGVR).Namespace(namespace).
		Patch(ctx, name, types.MergePatchType, []byte(cancelPatch), metav1.PatchOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return &resource.ConflictError{
				Resource: "job run resource",
				ID:       namespace + "/" + name,
				Message:  "the JobRun custom resource is missing; the scheduler republishes open runs",
			}
		}
		return fmt.Errorf("patch job run %s/%s: %w", namespace, name, err)
	}
	return nil
}

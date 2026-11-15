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
// exists under the same name and has the complete requested identity. Names
// use a shortened digest, so a name alone never proves an idempotent replay.
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
		return v1alpha1.JobRun{}, false, fmt.Errorf("encode job run: %w", err)
	}
	identity := v1alpha1.RunIdentity{SourceUID: run.Spec.ScheduledJobRef.UID, OccurrenceKey: run.Spec.OccurrenceKey, RevisionID: run.Spec.DefinitionRevision, Trigger: run.Spec.Trigger}
	names := []string{run.Name, v1alpha1.LegacyRunObjectName(run.Spec.ScheduledJobRef.Name, run.Spec.OccurrenceKey)}
	stored, created, err := publishRun(ctx, p.Client.Resource(jobRunGVR).Namespace(run.Namespace), &unstructured.Unstructured{Object: object}, names, identity)
	if err != nil {
		return v1alpha1.JobRun{}, false, fmt.Errorf("publish job run %s/%s: %w", run.Namespace, run.Name, err)
	}
	out, err := decodeJobRun(stored)
	if err == nil {
		err = validatePublishedJobRun(run, out)
	}
	return out, created, err
}

func validatePublishedJobRun(wanted, stored v1alpha1.JobRun) error {
	wantedSpec, storedSpec := wanted.Spec, stored.Spec
	// Cancellation is independent mutable intent and must survive replay.
	wantedSpec.CancelRequested, storedSpec.CancelRequested = false, false
	// publishRun has already resolved the modern or retained legacy address.
	if wanted.Namespace != stored.Namespace || stored.DeletionTimestamp != nil || wantedSpec != storedSpec {
		return &resource.ConflictError{Resource: "job run resource", ID: wanted.Namespace + "/" + wanted.Name,
			Field: "occurrence_key", Message: "the JobRun name belongs to a different or deleting execution; retrying cannot adopt it"}
	}
	return nil
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
func (p JobRunPublisher) RequestCancel(ctx context.Context, namespace, definitionName string, identity v1alpha1.RunIdentity) (string, error) {
	if p.Client == nil {
		return "", fmt.Errorf("dynamic client is required")
	}
	if namespace == "" || definitionName == "" {
		return "", fmt.Errorf("job run namespace and name are required")
	}
	name, err := cancelRun(ctx, p.Client.Resource(jobRunGVR).Namespace(namespace), definitionName, identity)
	if err != nil {
		return "", fmt.Errorf("cancel job run %s: %w", namespace, err)
	}
	return name, nil
}

package operator

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/controlplane"
	domainresource "orbitjob/internal/domain/resource"
)

// RunPublisher exposes a stored run as a JobRun custom resource.
type RunPublisher struct {
	Client dynamic.Interface
}

// Publish repairs a missing JobRun CR. Existing modern or historical addresses
// are adopted only after their complete immutable run identity is verified.
func (p RunPublisher) Publish(ctx context.Context, run v1alpha1.JobRun) error {
	if p.Client == nil {
		return fmt.Errorf("dynamic client is required")
	}
	if run.Namespace == "" || run.Name == "" {
		return fmt.Errorf("job run namespace and name are required")
	}
	expected := v1alpha1.RunIdentity{SourceUID: run.Spec.ScheduledJobRef.UID, OccurrenceKey: run.Spec.OccurrenceKey, RevisionID: run.Spec.DefinitionRevision, Trigger: run.Spec.Trigger}
	resource := p.Client.Resource(jobRunGVR).Namespace(run.Namespace)
	names := []string{run.Name, v1alpha1.LegacyRunObjectName(run.Spec.ScheduledJobRef.Name, run.Spec.OccurrenceKey)}
	if existing, found, err := v1alpha1.ResolveRun(ctx, resource, names, expected); err != nil || found {
		if err != nil {
			return err
		}
		return validatePublishedJobRun(existing, run)
	}
	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&run)
	if err != nil {
		return fmt.Errorf("encode job run %s: %w", run.Name, err)
	}
	_, err = p.Client.Resource(jobRunGVR).Namespace(run.Namespace).
		Create(ctx, &unstructured.Unstructured{Object: object}, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create job run %s/%s: %w", run.Namespace, run.Name, err)
	}
	existing, err := p.Client.Resource(jobRunGVR).Namespace(run.Namespace).Get(ctx, run.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	return validatePublishedJobRun(existing, run)
}

func validatePublishedJobRun(existing *unstructured.Unstructured, run v1alpha1.JobRun) error {
	var recorded v1alpha1.JobRun
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(existing.Object, &recorded); err != nil {
		return err
	}
	wanted, actual := run.Spec, recorded.Spec
	wanted.CancelRequested, actual.CancelRequested = false, false
	if recorded.DeletionTimestamp != nil || wanted != actual {
		return &domainresource.ConflictError{Resource: "job run resource", ID: run.Namespace + "/" + run.Name, Message: "existing JobRun belongs to another execution or differs from its pinned spec"}
	}
	return nil
}

// RequestCancel patches a run CR's spec.cancelRequested. It is the walker's
// cancel fan-out primitive: each in-flight step of a stopping workflow run is
// asked to stop here, and the ordinary run reconciliation turns the request
// into an observed Canceled. A missing identified step remains an error so a
// lost CR is repaired before cancellation is considered delivered.
func (p RunPublisher) RequestCancel(ctx context.Context, namespace, name string, identity ...v1alpha1.RunIdentity) error {
	if p.Client == nil {
		return fmt.Errorf("dynamic client is required")
	}
	if namespace == "" || name == "" {
		return fmt.Errorf("job run namespace and name are required")
	}
	if len(identity) != 1 {
		return fmt.Errorf("complete run identity is required for cancellation")
	}
	resource := p.Client.Resource(jobRunGVR).Namespace(namespace)
	_, err := v1alpha1.CancelRun(ctx, resource, v1alpha1.RunNameCandidates(name, identity[0].OccurrenceKey), identity[0])
	if err != nil {
		return fmt.Errorf("patch cancel request on job run %s/%s: %w", namespace, name, err)
	}
	return nil
}

// RunRemover deletes JobRun custom resources that retention has pruned.
type RunRemover struct {
	Client dynamic.Interface
}

// Remove deletes the JobRun CR named after the occurrence key. The name is
// derived the same way the publisher derived it, so retention does not need an
// index of published objects.
func (r RunRemover) Remove(ctx context.Context, namespace, scheduledJobName, occurrenceKey string) error {
	if r.Client == nil {
		return fmt.Errorf("dynamic client is required")
	}
	if len(occurrenceKey) < 8 {
		return fmt.Errorf("occurrence key %q is too short to derive a run name", occurrenceKey)
	}
	for _, name := range v1alpha1.RunNameCandidates(scheduledJobName, occurrenceKey) {
		obj, err := r.Client.Resource(jobRunGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		key, _, _ := unstructured.NestedString(obj.Object, "spec", "occurrenceKey")
		if key != occurrenceKey {
			continue
		}
		uid := obj.GetUID()
		if err = r.Client.Resource(jobRunGVR).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// RemoveIdentity only prunes an object proved to represent the stored row.
func (r RunRemover) RemoveIdentity(ctx context.Context, namespace, definitionName string, id v1alpha1.RunIdentity) error {
	if r.Client == nil {
		return fmt.Errorf("dynamic client required")
	}
	gvr := jobRunGVR
	if id.Workflow {
		gvr = workflowRunGVR
	}
	resource := r.Client.Resource(gvr).Namespace(namespace)
	obj, found, err := v1alpha1.ResolveRun(ctx, resource, v1alpha1.RunNameCandidates(definitionName, id.OccurrenceKey), id)
	if err != nil || !found {
		return err
	}
	uid, resourceVersion := obj.GetUID(), obj.GetResourceVersion()
	if uid == "" || resourceVersion == "" {
		return fmt.Errorf("%w: run resource lacks server identity", v1alpha1.ErrRunIdentityConflict)
	}
	// Protect the full identity proof against both replacement objects and
	// edits to this UID. A conflict leaves the object and ledger for next sweep.
	err = resource.Delete(ctx, obj.GetName(), metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &resourceVersion}, PropagationPolicy: ptr(metav1.DeletePropagationForeground)})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	live, err := resource.Get(ctx, obj.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: %s/%s (uid %s)", controlplane.ErrObjectDeletionPending, namespace, obj.GetName(), live.GetUID())
}

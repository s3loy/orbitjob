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

var workflowRunGVR = schema.GroupVersionResource{
	Group:    v1alpha1.GroupVersion.Group,
	Version:  v1alpha1.GroupVersion.Version,
	Resource: "workflowruns",
}

// WorkflowRunPublisher is the workflow-side mirror of JobRunPublisher: it
// creates the WorkflowRun custom resource a manual workflow trigger asks for,
// reads one back, and patches a cancel request onto it. The operator owns every
// effect -- it materializes the workflow_run_control_plane row from the CR,
// fans a cancel out over the workflow's non-terminal step JobRuns, and writes
// every ledger transition -- so this client is the admin API's entire cluster
// surface for workflows, exactly as the jobrun client is for runs.
type WorkflowRunPublisher struct {
	Client dynamic.Interface
}

// Create publishes the WorkflowRun, or returns the existing one when a run
// already exists under the same name. An AlreadyExists result is success, not
// conflict: the object name is derived from the occurrence key, so the same
// trigger replaying is deduplication, and a run whose create was retried after
// a partial failure adopts the object rather than doubling it.
//
// The returned bool reports whether this call created the object, so the caller
// can answer 201 for a new run and 200 for a replay.
func (p WorkflowRunPublisher) Create(ctx context.Context, run v1alpha1.WorkflowRun) (v1alpha1.WorkflowRun, bool, error) {
	if p.Client == nil {
		return v1alpha1.WorkflowRun{}, false, fmt.Errorf("dynamic client is required")
	}
	if run.Namespace == "" || run.Name == "" {
		return v1alpha1.WorkflowRun{}, false, fmt.Errorf("workflow run namespace and name are required")
	}
	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&run)
	if err != nil {
		return v1alpha1.WorkflowRun{}, false, fmt.Errorf("encode workflow run: %w", err)
	}
	identity := v1alpha1.RunIdentity{SourceUID: run.Spec.WorkflowRef.UID, OccurrenceKey: run.Spec.OccurrenceKey, RevisionID: run.Spec.DefinitionRevision, Trigger: run.Spec.Trigger, Workflow: true}
	names := []string{run.Name, v1alpha1.LegacyRunObjectName(run.Spec.WorkflowRef.Name, run.Spec.OccurrenceKey)}
	stored, created, err := publishRun(ctx, p.Client.Resource(workflowRunGVR).Namespace(run.Namespace), &unstructured.Unstructured{Object: object}, names, identity)
	if err != nil {
		return v1alpha1.WorkflowRun{}, false, fmt.Errorf("publish workflow run %s/%s: %w", run.Namespace, run.Name, err)
	}
	out, err := decodeWorkflowRun(stored)
	if err == nil {
		wanted, actual := run.Spec, out.Spec
		wanted.CancelRequested, actual.CancelRequested = false, false
		if out.Namespace != run.Namespace || out.DeletionTimestamp != nil || wanted.WorkflowRef != actual.WorkflowRef || wanted.DefinitionRevision != actual.DefinitionRevision || wanted.Trigger != actual.Trigger || wanted.Actor != actual.Actor || wanted.OccurrenceKey != actual.OccurrenceKey {
			err = &resource.ConflictError{Resource: "workflow run resource", ID: out.Namespace + "/" + out.Name, Message: "existing WorkflowRun belongs to a different execution"}
		}
	}
	return out, created, err
}

// Get reads one WorkflowRun by namespace and name. A missing object is a
// NotFoundError, not an internal error: callers address a run by the object
// name this package derives, so "not there" is an answer about the run, and
// the handler maps it to 404.
func (p WorkflowRunPublisher) Get(ctx context.Context, namespace, name string) (v1alpha1.WorkflowRun, error) {
	if p.Client == nil {
		return v1alpha1.WorkflowRun{}, fmt.Errorf("dynamic client is required")
	}
	if namespace == "" || name == "" {
		return v1alpha1.WorkflowRun{}, fmt.Errorf("workflow run namespace and name are required")
	}

	object, err := p.Client.Resource(workflowRunGVR).Namespace(namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return v1alpha1.WorkflowRun{}, &resource.NotFoundError{
				Resource: "workflow run resource",
				ID:       namespace + "/" + name,
			}
		}
		return v1alpha1.WorkflowRun{}, fmt.Errorf("get workflow run %s/%s: %w", namespace, name, err)
	}
	return decodeWorkflowRun(object)
}

// RequestCancel patches spec.cancelRequested to true on one WorkflowRun. The
// patch is the whole effect of the request: the operator observes it, fans the
// stop out over the workflow's non-terminal step JobRuns, and writes the ledger
// transitions. Repeated patches are no-ops, so a retried request converges on
// the same state.
//
// A WorkflowRun that is not found is returned as a ConflictError, not a
// NotFound, mirroring the jobrun client: the caller reads the ledger first, so
// the run row exists. Ledger-open but cluster-absent means the object was lost
// or already pruned while the row is not yet terminal; reporting 404 here would
// tell the user the run does not exist when the ledger says it does.
func (p WorkflowRunPublisher) RequestCancel(ctx context.Context, namespace, definitionName string, identity v1alpha1.RunIdentity) (string, error) {
	if p.Client == nil {
		return "", fmt.Errorf("dynamic client is required")
	}
	if namespace == "" || definitionName == "" {
		return "", fmt.Errorf("workflow run namespace and name are required")
	}
	name, err := cancelRun(ctx, p.Client.Resource(workflowRunGVR).Namespace(namespace), definitionName, identity)
	if err != nil {
		return "", fmt.Errorf("cancel workflow run %s: %w", namespace, err)
	}
	return name, nil
}

func decodeWorkflowRun(object *unstructured.Unstructured) (v1alpha1.WorkflowRun, error) {
	var out v1alpha1.WorkflowRun
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &out); err != nil {
		return v1alpha1.WorkflowRun{}, fmt.Errorf("decode workflow run %s/%s: %w",
			object.GetNamespace(), object.GetName(), err)
	}
	return out, nil
}

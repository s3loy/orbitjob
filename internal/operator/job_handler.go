package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/core/domain/jobrun"
	corepostgres "orbitjob/internal/core/store/postgres"
)

// ReconcileJob turns an observed Kubernetes Job phase into platform state. It
// only observes: deciding whether another attempt is allowed belongs to the run
// reconciliation, which owns the retry policy.
func (r Runtime) ReconcileJob(ctx context.Context, obj unstructured.Unstructured) error {
	if r.Runs == nil || r.Tenants == nil {
		return fmt.Errorf("job reconciliation is not configured")
	}
	var job batchv1.Job
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &job); err != nil {
		return fmt.Errorf("decode kubernetes job: %w", err)
	}
	identity, identityErr := execution.IdentityFromJob(&job)
	if identityErr != nil {
		// An unmanaged Job in a watched namespace is not an error; the operator
		// shares the cluster with everything else.
		return nil
	}
	tenant, err := r.Tenants.Resolve(obj)
	if err != nil {
		return err
	}
	settled, err := r.settleObservedJob(ctx, tenant, &job, identity)
	if err != nil || !settled {
		return err
	}

	observed := execution.Phase(job)
	if execution.StopBoundary(&job) != "" && observed != execution.PhaseSucceeded && observed != execution.PhaseFailed {
		// Releasing evidence can unblock Kubernetes's final condition. Its
		// preterminal stop signal must not move the ledger back to Pending.
		return nil
	}
	phase := executionMapToRunPhase(observed)
	attemptPhase := phase
	if observed == execution.PhaseFailed {
		// This execution finished even when the logical run can retry.
		attemptPhase = jobrun.Failed
	}
	runID, attemptNumber, err := r.Runs.UpdateAttemptPhase(
		ctx, tenant, job.Name, string(attemptPhase), string(job.UID), job.ResourceVersion,
	)
	if errors.Is(err, corepostgres.ErrAttemptOwnership) && r.Dynamic != nil {
		if bindErr := r.bindObservedAttempt(ctx, tenant, &job, identity); bindErr != nil {
			return bindErr
		}
		runID, attemptNumber, err = r.Runs.UpdateAttemptPhase(ctx, tenant, job.Name, string(attemptPhase), string(job.UID), job.ResourceVersion)
		if r.Enqueue != nil {
			r.Enqueue("jobruns", job.Namespace, identity.RunName)
		}
	}
	if errors.Is(err, corepostgres.ErrStaleObservation) {
		return nil
	}
	if errors.Is(err, corepostgres.ErrNoAttempt) {
		// No attempt row owns this Job name, and no future event will create
		// one: the row was pruned by retention, wiped by a loadtest reset, or
		// the Job predates this install. The store's refusal is what keeps a
		// foreign Job from being adopted into a run, so it must stay; but
		// requeueing the observation would burn a PostgreSQL transaction, an
		// ERROR line and a reconcile-error increment on every retry and
		// resync forever, on an answer that cannot change (see the known
		// issues doc). Drop the key instead. The one transient way to land
		// here — the informer delivering the Job in the sliver between Ensure
		// and the attempt's commit — heals itself: the attempt row lands
		// moments later, and the Job's next status change re-observes it.
		return nil
	}
	if err != nil {
		return fmt.Errorf("record job %s phase %s: %w", job.Name, phase, err)
	}
	phaseChanged, err := r.Runs.UpdateRunPhase(ctx, tenant, runID, phase, attemptNumber)
	if errors.Is(err, jobrun.ErrRunTerminal) || errors.Is(err, corepostgres.ErrRunChanged) {
		// The run finished before this observation landed. Kubernetes Jobs are not
		// deleted with their run and the informer re-delivers them, so a late
		// observation is normal: the store refused the write to keep the finished
		// run final, and there is nothing left to record.
		return nil
	}
	if err != nil {
		return err
	}
	if phaseChanged && r.Enqueue != nil {
		r.Enqueue("jobruns", job.Namespace, identity.RunName)
	}
	if phaseChanged {
		return r.recordTerminal(ctx, tenant, runID, phase)
	}
	return nil
}

// executionMapToRunPhase maps an observed Job phase onto the run lifecycle.
// A failed Job moves the run to RetryWaiting, not to Failed: only the run
// reconciler knows whether attempts remain, and it makes that call with the
// stored policy in hand.
func executionMapToRunPhase(jobPhase string) jobrun.Phase {
	switch jobPhase {
	case execution.PhaseSucceeded:
		return jobrun.Succeeded
	case execution.PhaseFailed:
		return jobrun.RetryWaiting
	case execution.PhaseRunning:
		return jobrun.Running
	default:
		return jobrun.CreatingAttempt
	}
}

// patchStatus merges a status fragment onto a custom resource's status
// subresource. Merge patch keeps fields owned by other controllers intact.
func (r Runtime) patchStatus(ctx context.Context, gvr schema.GroupVersionResource, obj unstructured.Unstructured, status map[string]any) error {
	if r.Dynamic == nil {
		return fmt.Errorf("dynamic client is required")
	}
	payload, err := json.Marshal(map[string]any{"status": status})
	if err != nil {
		return err
	}
	_, err = r.Dynamic.Resource(gvr).Namespace(obj.GetNamespace()).
		Patch(ctx, obj.GetName(), types.MergePatchType, payload, metav1.PatchOptions{}, "status")
	if err != nil {
		return fmt.Errorf("patch %s status: %w", obj.GetName(), err)
	}
	return nil
}

// patchRunStatus projects stored run progress onto the JobRun CR. The CR is a
// read model: it is derived from stored state, never the input to it.
func (r Runtime) patchRunStatus(ctx context.Context, obj unstructured.Unstructured, stored jobrun.StoredRun) error {
	status := map[string]any{
		"conditions":             r.admissionConditions(obj, false, ""),
		"phase":                  string(stored.Phase),
		"attempt":                int32(stored.Attempt),
		"executionUnknownSince":  nil,
		"executionUnknownReason": nil,
	}
	if stored.UnknownSince != nil {
		status["executionUnknownSince"] = stored.UnknownSince.UTC().Format(time.RFC3339Nano)
		status["executionUnknownReason"] = stored.UnknownReason
	}
	return r.patchStatus(ctx, jobRunGVR, obj, status)
}

// jobRunSpecFromUnstructured decodes only the spec, so a status write by another
// writer cannot influence how this operator interprets the run.
func jobRunSpecFromUnstructured(obj unstructured.Unstructured) (v1alpha1.JobRunSpec, error) {
	var spec v1alpha1.JobRunSpec
	raw, found, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil || !found {
		return spec, fmt.Errorf("job run %s/%s has no spec", obj.GetNamespace(), obj.GetName())
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return spec, err
	}
	if err := json.Unmarshal(encoded, &spec); err != nil {
		return spec, fmt.Errorf("decode job run spec: %w", err)
	}
	if spec.ScheduledJobRef.UID == "" || spec.OccurrenceKey == "" {
		return spec, fmt.Errorf("job run %s/%s is missing scheduledJobRef.uid or occurrenceKey",
			obj.GetNamespace(), obj.GetName())
	}
	// The CRD makes actor required, so an empty one here means the object
	// reached the API server without schema validation, or predates the field.
	// Refusing it keeps the ledger from being asked to record a run whose
	// triggerer cannot be named, which is the one question it exists to answer.
	if strings.TrimSpace(spec.Actor) == "" {
		return spec, fmt.Errorf("job run %s/%s has no actor", obj.GetNamespace(), obj.GetName())
	}
	return spec, nil
}

// bindObservedAttempt recovers an Ensure response lost after API acceptance.
// Full live CR ownership and the reserved ledger attempt must agree first.
func (r Runtime) bindObservedAttempt(ctx context.Context, tenant string, job *batchv1.Job, id execution.Identity) error {
	obj, err := r.Dynamic.Resource(jobRunGVR).Namespace(job.Namespace).Get(ctx, id.RunName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if obj.GetUID() == "" || string(obj.GetUID()) != id.RunUID {
		return corepostgres.ErrAttemptOwnership
	}
	spec, err := jobRunSpecFromUnstructured(*obj)
	if err != nil {
		return err
	}
	stored, found, err := r.Runs.RunByOccurrence(ctx, tenant, spec.ScheduledJobRef.UID, spec.OccurrenceKey)
	if err != nil {
		return err
	}
	if !found {
		return corepostgres.ErrNoAttempt
	}
	if err := execution.ValidateOwnership(job, execution.Identity{RunName: obj.GetName(), RunUID: string(obj.GetUID()), RevisionID: fmt.Sprint(stored.RevisionID), Attempt: stored.Attempt}); err != nil {
		return err
	}
	return r.Runs.BindAttemptObservation(ctx, tenant, stored.ID, stored.Attempt, job.Name, string(job.UID), job.ResourceVersion)
}

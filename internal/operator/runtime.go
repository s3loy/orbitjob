package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/core/app/projection"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/metrics"
)

// RevisionStore projects and loads immutable definition revisions.
type RevisionStore interface {
	ApplyRevisionForTenant(ctx context.Context, tenantID string, rev revision.Revision) (int64, error)
	RevisionByID(ctx context.Context, tenantID string, id int64) (revision.Revision, error)
}

// RunStore owns run and attempt progress. Reads go through RunByOccurrence
// rather than the CR status subresource, so a forged status cannot change what
// actually executes.
type RunStore interface {
	CreateOccurrenceForTenant(ctx context.Context, tenantID string, run jobrun.JobRun, maxAttempts int, actor string) (jobrun.StoredRun, bool, error)
	RunByOccurrence(ctx context.Context, tenantID, sourceUID, occurrenceKey string) (jobrun.StoredRun, bool, error)
	// CreateAttemptWithEnsure commits Prepared and possibly-sent stages before
	// its one external call, then serializes binding against cancellation.
	CreateAttemptWithEnsure(ctx context.Context, tenantID string, runID int64, attempt jobrun.Attempt, ensure func(context.Context) (jobrun.Attempt, error)) error
	BindAttemptObservation(context.Context, string, int64, int, string, string, string) error
	UpdateAttemptPhase(ctx context.Context, tenantID, jobName, phase, jobUID, resourceVersion string) (int64, int, error)
	// UpdateRunPhase advances a run and reports whether the run's phase
	// changed. Transition-only bookkeeping uses this flag; derived Function
	// history is independently idempotent and can be repaired on a replay.
	UpdateRunPhase(ctx context.Context, tenantID string, runID int64, phase jobrun.Phase, attempt int) (bool, error)
}

// TerminalOutcomeRecorder receives the runs whose phase genuinely changed to a
// terminal value. The check read model and SLI increments deduplicate durable
// facts; an error keeps the ledger acknowledgement pending for replay.
type TerminalOutcomeRecorder interface {
	RecordTerminalPhaseOnce(ctx context.Context, tenantID string, runID int64, phase jobrun.Phase) error
}

// JobManager drives Kubernetes Jobs for the control plane. Ensure is authorized
// once by a committed dispatch marker; recovery only Get-observes that identity.
type JobManager interface {
	Ensure(ctx context.Context, desired *batchv1.Job) (*batchv1.Job, error)
	Get(ctx context.Context, namespace, name string) (*batchv1.Job, bool, error)
	Delete(ctx context.Context, namespace, name string, uid ...string) error
	Stopped(ctx context.Context, namespace, name string) (bool, error)
}

// Runtime is the composition root that turns watched Kubernetes objects into
// platform state and back. Every handler is idempotent: it reads current stored
// state, computes the desired action, and writes only what is missing.
type Runtime struct {
	Dynamic      dynamic.Interface
	Jobs         JobManager
	Revisions    RevisionStore
	Runs         RunStore
	ArchivedJobs ArchivedJobSource
	Tenants      TenantResolver
	// Workflows, when set, is the workflow ledger the WorkflowRun reconcile
	// materializes into and the workflow walker advances from. A runtime
	// without one refuses workflow run reconciliation loudly rather than
	// silently ignoring the CR.
	Workflows WorkflowRunStore
	// OutcomeReader loads a run's terminal facts for the bookkeeping hooks
	// that need more than the run id — the function_runs read model records
	// the occurrence key's run id and the attempt timing.
	OutcomeReader controlplane.OutcomeReader
	// Outcomes, when set, receives every run whose phase genuinely changed to
	// Succeeded or Failed, and maintains the check read model, SLI snapshots
	// and check metrics from that one fact.
	Outcomes TerminalOutcomeRecorder
	// Functions, when set, maintains the function_runs read model from the
	// same terminal transitions, routed by the function source-uid prefix.
	Functions    FunctionRunRecorder
	Enqueue      func(resource, namespace, name string)
	EnqueueAfter func(resource, namespace, name string, after time.Duration)
	Now          func() time.Time
	// ExecutionRecoveryTenantBudget bounds one tenant's recovery slice. It is
	// independent of workload startup or termination deadlines.
	ExecutionRecoveryTenantBudget time.Duration
}

// Handlers renders the runtime as the callbacks the informer dispatcher expects.
func (r Runtime) Handlers() DynamicHandler {
	return DynamicHandler{
		Client:                r.Dynamic,
		ReconcileDeleted:      r.ReconcileDeleted,
		ReconcileScheduledJob: r.ReconcileScheduledJob,
		ReconcileJobRun:       r.ReconcileJobRun,
		ReconcileJob:          r.ReconcileJob,
		ReconcileWorkflowRun:  r.ReconcileWorkflowRun,
		ReconcileWorkflowJob:  r.ReconcileWorkflowJob,
	}
}

func (r Runtime) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

// ReconcileScheduledJob projects the declared spec as an immutable revision and
// reports the outcome on the CR status. It never creates runs: scheduling is the
// scheduler's job, and keeping the two apart is what stops an operator restart
// from firing a backlog of occurrences.
func (r Runtime) ReconcileScheduledJob(ctx context.Context, obj unstructured.Unstructured) error {
	if r.Revisions == nil || r.Tenants == nil {
		return fmt.Errorf("scheduled job reconciliation is not configured")
	}
	live, exists, err := r.current(ctx, scheduledJobGVR, obj)
	if err != nil {
		return err
	}
	if !exists {
		return r.ReconcileDeleted(ctx, scheduledJobGVR.Resource, obj.GetNamespace(), obj.GetName())
	}
	obj = live
	typed, err := scheduledJobFromUnstructured(obj)
	if err != nil {
		return err
	}
	tenant, err := r.Tenants.Resolve(obj)
	if err != nil {
		return err
	}
	revisionID, err := (projection.Service{Revisions: r.Revisions, Now: r.Now}).
		ApplyScheduledJob(ctx, typed, tenant, "orbitjob-operator")
	if err != nil {
		return fmt.Errorf("project scheduled job %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
	}
	return r.patchStatus(ctx, scheduledJobGVR, obj, map[string]any{
		"observedGeneration": typed.Generation,
		"activeRevision":     revisionID,
	})
}

// ReconcileWorkflowJob projects the declared workflow spec as an immutable
// revision and reports the outcome on the CR status, exactly as a ScheduledJob
// reconcile does one resource kind up. It never creates runs: firing the DAG
// is the workflow walker's job, and keeping the two apart is what stops an
// operator restart from fanning out a backlog of workflow executions.
func (r Runtime) ReconcileWorkflowJob(ctx context.Context, obj unstructured.Unstructured) error {
	if r.Revisions == nil || r.Tenants == nil {
		return fmt.Errorf("workflow job reconciliation is not configured")
	}
	live, exists, err := r.current(ctx, workflowJobGVR, obj)
	if err != nil {
		return err
	}
	if !exists {
		return r.ReconcileDeleted(ctx, workflowJobGVR.Resource, obj.GetNamespace(), obj.GetName())
	}
	obj = live
	typed, err := workflowJobFromUnstructured(obj)
	if err != nil {
		return err
	}
	tenant, err := r.Tenants.Resolve(obj)
	if err != nil {
		return err
	}
	revisionID, err := (projection.Service{Revisions: r.Revisions, Now: r.Now}).
		ApplyWorkflowJob(ctx, typed, tenant, "orbitjob-operator")
	if err != nil {
		return fmt.Errorf("project workflow job %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
	}
	return r.patchStatus(ctx, workflowJobGVR, obj, map[string]any{
		"observedGeneration": typed.Generation,
		"activeRevision":     revisionID,
	})
}

// ReconcileJobRun drives one logical run towards its next attempt. The run row is
// the source of truth for how many attempts exist and how many are permitted, so
// a re-reconcile after a restart lands on the same attempt number and therefore
// the same deterministic Job name.
func (r Runtime) ReconcileJobRun(ctx context.Context, obj unstructured.Unstructured) error {
	if r.Runs == nil || r.Revisions == nil || r.Tenants == nil {
		return fmt.Errorf("job run reconciliation is not configured")
	}
	spec, err := jobRunSpecFromUnstructured(obj)
	if err != nil {
		return err
	}
	live, exists, liveErr := r.current(ctx, jobRunGVR, obj)
	if liveErr != nil {
		return liveErr
	}
	if !exists {
		return nil
	}
	obj = live
	spec, err = jobRunSpecFromUnstructured(obj)
	if err != nil {
		return err
	}
	tenant, err := r.Tenants.Resolve(obj)
	if err != nil {
		return err
	}

	stored, found, err := r.Runs.RunByOccurrence(ctx, tenant, spec.ScheduledJobRef.UID, spec.OccurrenceKey)
	if err != nil {
		return err
	}
	if !found {
		stored, found, err = r.materializeManualRun(ctx, tenant, spec)
		if errors.Is(err, jobrun.ErrAdmissionBlocked) {
			r.requeueRun(obj)
			return r.patchAdmissionBlocked(ctx, jobRunGVR, obj, err)
		}
		if err != nil {
			return err
		}
		if !found {
			// A CR with no stored run and no manual trigger is not one this
			// operator creates state for: the scheduler owns the creation of
			// scheduled runs, and doing its job here would let a JobRun written
			// by anyone with CR access bypass occurrence deduplication and the
			// concurrency policy. Scheduled and check rows are always committed
			// before their CR can be observed, so not-found here means the row
			// was deleted afterwards -- retention, or a tenant-blind loadtest
			// reset. The answer cannot change, and requeueing would burn a
			// transaction, an ERROR line and a reconcile-error increment on
			// every resync forever. Drop the key.
			return nil
		}
	}

	// Terminal first: a run that already finished must not be dragged back into
	// cancellation by a stale request flag, which would otherwise fail forever
	// and pin the reconcile loop on an outcome that can never change.
	if jobrun.Terminal(stored.Phase) {
		var historyErr error
		if _, isFunction := function.IDFromSourceUID(spec.ScheduledJobRef.UID); isFunction {
			historyErr = r.recordFunctionOutcome(ctx, tenant, stored.ID, stored.Phase)
		}
		return errors.Join(historyErr, r.patchRunStatus(ctx, obj, stored))
	}
	// A cancel request is intent, not fact. Drive it to completion before any
	// other branch: a run the user asked to stop must not start another attempt.
	if stored.Phase == jobrun.CancelRequested || stored.Phase == jobrun.CancelUnknown || spec.CancelRequested {
		return r.reconcileCancellation(ctx, tenant, obj, stored, spec)
	}
	// One attempt at a time. The attempt's Job is live, so starting another would
	// run the same occurrence concurrently and recording an outcome would report a
	// result that has not happened yet. Wait for the Job observation to move the
	// run; the status is still derived from stored state, so a drifted CR status
	// is repaired here.
	resuming := stored.Phase == jobrun.CreatingAttempt && stored.Attempt > 0 && stored.AttemptUID == ""
	if resuming {
		if store, ok := r.Runs.(executionRecoveryStore); ok {
			e, found, err := store.CurrentExecution(ctx, tenant, stored.ID)
			if err != nil {
				return err
			}
			if found && (e.DispatchStage != jobrun.DispatchPrepared || e.Run.OwnerRunUID == "" || e.Run.OwnerRunUID != string(obj.GetUID())) {
				return r.recoverExecution(ctx, tenant, e)
			}
		}
	}
	if stored.AttemptInFlight() && !resuming {
		if store, ok := r.Runs.(executionRecoveryStore); ok {
			e, found, err := store.CurrentExecution(ctx, tenant, stored.ID)
			if err != nil {
				return err
			}
			if found {
				return r.recoverExecution(ctx, tenant, e)
			}
		}
		return r.patchRunStatus(ctx, obj, stored)
	}
	if stored.Phase == jobrun.ExecutionUnknown {
		if store, ok := r.Runs.(executionRecoveryStore); ok {
			e, found, err := store.CurrentExecution(ctx, tenant, stored.ID)
			if err != nil {
				return err
			}
			if found {
				return r.recoverExecution(ctx, tenant, e)
			}
		}
		return r.patchRunStatus(ctx, obj, stored)
	}
	suppressRetry, err := r.suppressOOMRetry(ctx, tenant, stored)
	if err != nil {
		return err
	}
	if suppressRetry || (!stored.CanStartAttempt() && !resuming) {
		// No attempt is in flight and none may start: the attempt budget is spent.
		// Record the outcome once and stop.
		failed, err := jobrun.Transition(jobrun.JobRun{Phase: stored.Phase}, jobrun.Failed)
		if err != nil {
			return nil // already in a phase that cannot fail again
		}
		phaseChanged, err := r.Runs.UpdateRunPhase(ctx, tenant, stored.ID, failed.Phase, stored.Attempt)
		if err != nil {
			return err
		}
		if !phaseChanged {
			return r.refreshRunStatus(ctx, tenant, obj, spec)
		}
		metrics.OperatorRunsFailedTotal.Inc()
		if suppressRetry {
			executionObservations.WithLabelValues("oom_retry_suppressed").Inc()
		}
		if phaseChanged {
			if err := r.recordTerminal(ctx, tenant, stored.ID, failed.Phase); err != nil {
				return err
			}
		}
		stored.Phase = failed.Phase
		return r.patchRunStatus(ctx, obj, stored)
	}

	attemptNumber := stored.NextAttempt()
	if resuming {
		attemptNumber = stored.Attempt
	}
	rev, err := r.Revisions.RevisionByID(ctx, tenant, stored.RevisionID)
	if err != nil {
		return err
	}
	var declared v1alpha1.ScheduledJobSpec
	if err := json.Unmarshal([]byte(rev.NormalizedSpec), &declared); err != nil {
		return fmt.Errorf("decode pinned revision %d: %w", rev.ID, err)
	}

	identity := execution.Identity{
		RunName:    obj.GetName(),
		RunUID:     string(obj.GetUID()),
		RevisionID: fmt.Sprint(rev.ID),
		Attempt:    attemptNumber,
	}
	acceptedSnapshot := false
	if spec.Trigger == v1alpha1.Workflow {
		if snapshots, ok := r.Runs.(interface {
			SnapshotWorkflowStep(context.Context, string, int64, int64) (bool, error)
		}); ok {
			acceptedSnapshot, err = snapshots.SnapshotWorkflowStep(ctx, tenant, stored.ID, stored.RevisionID)
			if err != nil {
				return err
			}
		}
	}
	alive := acceptedSnapshot
	if !acceptedSnapshot {
		alive, err = r.SourceExists(ctx, rev)
	}
	if err != nil {
		return err
	}
	if !alive {
		return fmt.Errorf("source definition no longer exists")
	}
	if obj.GetUID() == "" {
		return fmt.Errorf("job run UID required before creating a Kubernetes Job")
	}
	resources, err := execution.ResourceRequirements(declared.JobTemplate.Resources)
	if err != nil {
		return fmt.Errorf("pinned execution resources: %w", err)
	}
	desired := execution.BuildJob(identity, obj.GetNamespace(), execution.Template{
		ExecutionPolicyVersion: declared.ExecutionPolicyVersion,
		Resources:              resources,
		Image:                  declared.JobTemplate.Image,
		Command:                declared.JobTemplate.Command,
		Args:                   declared.JobTemplate.Args,
		BackoffLimit:           declared.JobTemplate.BackoffLimit,
		ActiveDeadlineSeconds:  int64(spec.TimeoutSeconds),
	})

	if resuming && stored.AttemptJobName != "" {
		desired.Name = stored.AttemptJobName
	} else {
		// Check the bounded historical candidate before reserving a new address.
		for _, name := range execution.JobNameCandidates(identity) {
			existing, found, err := r.Jobs.Get(ctx, desired.Namespace, name)
			if err != nil {
				return err
			}
			if !found {
				continue
			}
			if err := execution.ValidateOwnership(existing, identity); err != nil {
				if name == execution.JobName(identity) {
					return err
				}
				continue
			}
			desired.Name = name
			break
		}
	}

	reservation := jobrun.Attempt{Number: attemptNumber, KubernetesJobName: desired.Name, OwnerRunUID: identity.RunUID, StartedAt: r.now(), Phase: jobrun.CreatingAttempt}
	err = r.Runs.CreateAttemptWithEnsure(ctx, tenant, stored.ID, reservation, func(ensureCtx context.Context) (jobrun.Attempt, error) {
		job, err := r.Jobs.Ensure(ensureCtx, desired)
		if err != nil {
			metrics.OperatorJobCreationFailuresTotal.Inc()
			return jobrun.Attempt{}, fmt.Errorf("ensure kubernetes job for run %s attempt %d: %w", identity.RunName, attemptNumber, err)
		}
		if err := execution.ValidateOwnership(job, identity); err != nil {
			return jobrun.Attempt{}, fmt.Errorf("kubernetes job %s/%s: %w", job.Namespace, job.Name, err)
		}
		if job.UID == "" {
			return jobrun.Attempt{}, fmt.Errorf("kubernetes job %s has no uid", job.Name)
		}
		return jobrun.Attempt{Number: attemptNumber, KubernetesJobName: job.Name, KubernetesJobUID: string(job.UID), ObservedResourceVersion: job.ResourceVersion, Phase: jobrun.CreatingAttempt, StartedAt: reservation.StartedAt}, nil
	})
	if errors.Is(err, jobrun.ErrDispatchMayHaveStarted) {
		r.requeueRun(obj)
		if store, ok := r.Runs.(executionRecoveryStore); ok {
			e, found, readErr := store.CurrentExecution(ctx, tenant, stored.ID)
			if readErr != nil || !found {
				return readErr
			}
			return r.recoverExecution(ctx, tenant, e)
		}
		return err
	}
	if errors.Is(err, corepostgres.ErrWorkflowBlocked) {
		if r.EnqueueAfter != nil {
			r.EnqueueAfter("jobruns", obj.GetNamespace(), obj.GetName(), 5*time.Second)
		}
		return r.patchRunStatus(ctx, obj, stored)
	}
	if errors.Is(err, jobrun.ErrAdmissionBlocked) {
		r.requeueRun(obj)
		return r.patchAdmissionBlocked(ctx, jobRunGVR, obj, err)
	}
	if errors.Is(err, corepostgres.ErrWorkflowStopped) {
		// A delayed step CR must honor its parent's committed stop even before
		// the walker has patched cancelRequested. Reload after reservation: an
		// uncertain intent may have committed, and attempt zero must not be
		// mistaken for proof that no external request could have happened.
		latest, found, readErr := r.Runs.RunByOccurrence(ctx, tenant, spec.ScheduledJobRef.UID, spec.OccurrenceKey)
		if readErr != nil || !found {
			return readErr
		}
		if jobrun.Terminal(latest.Phase) {
			return r.patchRunStatus(ctx, obj, latest)
		}
		return r.reconcileCancellation(ctx, tenant, obj, latest, spec)
	}
	if errors.Is(err, jobrun.ErrRunTerminal) || errors.Is(err, corepostgres.ErrRunChanged) {
		if r.Enqueue != nil {
			r.Enqueue("jobruns", obj.GetNamespace(), obj.GetName())
		}
		return nil
	}
	if err != nil {
		return err
	}

	stored.Phase = jobrun.Running
	stored.Attempt = attemptNumber
	if r.Enqueue != nil {
		r.Enqueue("jobs", desired.Namespace, desired.Name)
	}
	return r.patchRunStatus(ctx, obj, stored)
}

// materializeManualRun creates the ledger row for a JobRun that arrived as a
// bare Custom Resource, and reports whether the run is now this operator's to
// drive.
//
// The admin API cannot write the ledger itself -- it holds SELECT only on the
// control-plane tables -- so a manual trigger travels as a JobRun and the
// operator, which owns the tables, writes the row from the CR's declared spec.
// The row, its actor and its audit entry commit in one transaction inside the
// store, and the caller then drives the first attempt in the same reconcile, so
// one pass takes a manual trigger from declaration to execution. A reconcile
// that repeats finds the row and skips this path, so it is idempotent.
//
// Only the CR-first triggers are materialized here: Manual, and Function --
// an HTTP function invocation is CR-first exactly like a manual trigger, and
// distinct only so ledger readers can count invocations without parsing
// occurrence keys. A scheduled occurrence always has its row committed before
// its Custom Resource is published, so a Schedule-trigger CR with no row was
// not written by the scheduler and is left alone rather than allowed to bypass
// occurrence deduplication and concurrency. The same holds for a Workflow step
// CR: the walker commits the step row before publishing, so a Workflow-trigger
// CR with no row is refused.
//
// The pinned revision must exist for the tenant. That is what stops a forged CR
// from inventing work: the run is bound to a definition the tenant already
// declared, and the revision's retry policy is the run's.
func (r Runtime) materializeManualRun(ctx context.Context, tenant string, spec v1alpha1.JobRunSpec) (jobrun.StoredRun, bool, error) {
	if spec.Trigger != v1alpha1.Manual && spec.Trigger != v1alpha1.Function {
		return jobrun.StoredRun{}, false, nil
	}
	rev, err := r.Revisions.RevisionByID(ctx, tenant, spec.DefinitionRevision)
	if err != nil {
		return jobrun.StoredRun{}, false, fmt.Errorf("load pinned revision %d for manual run: %w", spec.DefinitionRevision, err)
	}
	// The run is deduplicated under the definition identity and pinned to a
	// revision; if the two disagree, the CR is tying one definition's occurrence
	// key to another definition's spec, and the ledger would report a run the
	// definition never declared. Refuse it.
	if rev.Identity.SourceUID != spec.ScheduledJobRef.UID {
		return jobrun.StoredRun{}, false, fmt.Errorf(
			"manual run %s pins revision %d of definition %s, not %s",
			spec.OccurrenceKey, spec.DefinitionRevision, rev.Identity.SourceUID, spec.ScheduledJobRef.UID)
	}
	var declared v1alpha1.ScheduledJobSpec
	if err := json.Unmarshal([]byte(rev.NormalizedSpec), &declared); err != nil {
		return jobrun.StoredRun{}, false, fmt.Errorf("decode pinned revision %d: %w", rev.ID, err)
	}
	create := r.Runs.CreateOccurrenceForTenant
	if spec.CancelRequested {
		if canceler, ok := r.Runs.(interface {
			CancelUnadmittedOccurrenceForTenant(context.Context, string, jobrun.JobRun, int, string) (jobrun.StoredRun, bool, error)
		}); ok {
			create = canceler.CancelUnadmittedOccurrenceForTenant
		}
	}
	stored, created, err := create(ctx, tenant, jobrun.JobRun{
		SourceUID:     spec.ScheduledJobRef.UID,
		RevisionID:    fmt.Sprint(spec.DefinitionRevision),
		OccurrenceKey: spec.OccurrenceKey,
		Trigger:       jobrun.Trigger(spec.Trigger),
	}, declared.RetryPolicy.EffectiveMaxAttempts(), spec.Actor)
	if err != nil {
		return jobrun.StoredRun{}, false, fmt.Errorf("create manual run for occurrence %s: %w", spec.OccurrenceKey, err)
	}
	// Only the CR-first triggers count here. A scheduled occurrence is created
	// by the scheduler, and counting it would make this series say "triggered
	// by a person" when nothing was.
	if created {
		metrics.OperatorRunsCreatedTotal.Inc()
	}
	return stored, true, nil
}

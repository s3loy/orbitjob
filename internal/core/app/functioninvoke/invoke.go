// Package functioninvoke turns an HTTP function invocation into one JobRun
// Custom Resource. It is CR-first exactly like the manual trigger: the use
// case validates the definition, pins the invocation to the revision the
// operator's sync loop materialized, and publishes a Function-trigger JobRun
// whose reconciliation materializes the ledger row. The use case writes no
// ledger rows and no read-model rows — the API role holds SELECT only on the
// control-plane tables, and the operator owns those writes.
//
// A retried invocation is idempotent by construction: with an idempotency key
// the occurrence key is stable, so the derived CR name addresses one object,
// and a publisher that reports AlreadyExists as adoption makes the retry
// converge on the first invocation's run.
package functioninvoke

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/platform/metrics"
)

// maxInvokeWaitSeconds caps the synchronous variant of an invocation. The
// platform's honest latency promise for a function is seconds -- every
// invocation is a cold pod and image pull dominates -- so a longer wait would
// hold an HTTP request open against work the caller can already poll.
const maxInvokeWaitSeconds = 60

// invokePollInterval is how often the synchronous variant re-reads the JobRun
// custom resource while waiting for a terminal phase.
const invokePollInterval = time.Second

// FunctionReader loads one live function definition. It is the FunctionStore
// read the invoke path needs; a deleted or other-tenant definition arrives as
// not found.
type FunctionReader interface {
	GetForTenant(ctx context.Context, tenantID string, id int64) (function.Definition, bool, error)
}

// Revision is the active revision an invocation pins: the row the operator's
// revision-sync loop materialized from the definition's current version, and
// the namespace its ScheduledJob-shaped identity lives in.
type Revision struct {
	ID        int64
	Namespace string
}

// RevisionSource resolves a definition's active revision by its source_uid.
// The CRD requires a revision id before anything can be published, so the
// resolver is the one dependency the invoke path cannot satisfy from the
// functions row itself. Resolution rides the sync loop's one-tick lag by
// design: until a new version's revision is materialized the previous one
// stays active, and an invocation in that window pins it — invoke-what-you-
// read, the same generation lag every CR-first path exposes.
type RevisionSource interface {
	ActiveRevision(ctx context.Context, tenantID, sourceUID string) (Revision, bool, error)
}

// RunPublisher hands a JobRun Custom Resource to Kubernetes and reads one back
// while a synchronous invocation waits. Publish must be idempotent: the object
// name derives from the occurrence key, so a retry after a failed publish
// adopts the object instead of doubling it. The bool reports whether this call
// created the object.
type RunPublisher interface {
	Publish(ctx context.Context, run v1alpha1.JobRun) (v1alpha1.JobRun, bool, error)
	Get(ctx context.Context, namespace, name string) (v1alpha1.JobRun, error)
}

// UseCase invokes a function by publishing one Function-trigger JobRun.
type UseCase struct {
	functions    FunctionReader
	revisions    RevisionSource
	publisher    RunPublisher
	pollInterval time.Duration
}

// New assembles the invoke use case.
func New(functions FunctionReader, revisions RevisionSource, publisher RunPublisher) *UseCase {
	return &UseCase{functions: functions, revisions: revisions, publisher: publisher, pollInterval: invokePollInterval}
}

// WithPollInterval overrides how often the synchronous variant re-reads the
// JobRun custom resource. Production wiring keeps the default; tests shrink it
// so a wait expires without real time passing.
func (uc *UseCase) WithPollInterval(interval time.Duration) *UseCase {
	uc.pollInterval = interval
	return uc
}

// Input is one invocation request.
type Input struct {
	TenantID string
	// FunctionID is the definition being invoked.
	FunctionID int64
	// ActorID is the authenticated principal that asked for the run. It rides
	// spec.actor, the field the CRD makes required, so the value the operator
	// copies into the ledger's actor column is the caller's credential, not a
	// client-supplied label.
	ActorID string
	// IdempotencyKey, when set, makes a repeated invocation resolve to the
	// same run instead of creating another.
	IdempotencyKey string
	// ResourceGroupID is the caller's key scope; a function outside it is as
	// invisible as a missing one.
	ResourceGroupID string
	// WaitSeconds is the synchronous variant's budget: when positive, the
	// call polls the JobRun custom resource until a terminal phase or this
	// many seconds elapse, then answers with the phase observed. Zero means
	// async-with-reference.
	WaitSeconds int
}

// Result is what a caller gets back: a reference to the JobRun Custom
// Resource, not a ledger row. The row is written later by the operator, so
// Phase is whatever the CR's status says, empty until the operator has
// observed it once -- and an expired wait reports the last observed phase
// rather than pretending the run finished. Created is false for a replay that
// adopted an existing run -- the same occurrence, not a new one.
type Result struct {
	Namespace     string
	Name          string
	OccurrenceKey string
	Trigger       string
	Phase         string
	Created       bool
	FunctionID    int64
	RevisionID    int64
}

// Invoke validates the request, pins the active revision, and publishes one
// JobRun. It never writes the ledger.
func (uc *UseCase) Invoke(ctx context.Context, in Input) (Result, error) {
	normalized, err := normalizeInput(in)
	if err != nil {
		return Result{}, err
	}
	if uc.functions == nil || uc.revisions == nil {
		return Result{}, fmt.Errorf("function reader and revision source are required")
	}
	if uc.publisher == nil {
		return Result{}, fmt.Errorf("job run publisher is required")
	}

	def, found, err := uc.functions.GetForTenant(ctx, normalized.TenantID, normalized.FunctionID)
	if err != nil {
		return Result{}, fmt.Errorf("read function for invoke: %w", err)
	}
	if !found || !visibleToGroup(def.ResourceGroupID, normalized.ResourceGroupID) {
		return Result{}, &resource.NotFoundError{Resource: "function", ID: normalized.FunctionID}
	}
	// Invoking a paused function is a conflict, not an error: the definition
	// exists and the caller reached it, but the platform has been told not to
	// run it.
	if def.Status == function.StatusPaused {
		return Result{}, &resource.ConflictError{
			Resource: "function",
			ID:       def.ID,
			Field:    "status",
			Message:  "cannot invoke a paused function",
		}
	}

	sourceUID := function.SourceUID(def.ID)
	rev, found, err := uc.revisions.ActiveRevision(ctx, normalized.TenantID, sourceUID)
	if err != nil {
		return Result{}, fmt.Errorf("resolve active revision: %w", err)
	}
	// The CRD requires definitionRevision before publish, so a function whose
	// revision has not been materialized yet cannot be invoked -- a conflict
	// the caller can retry, not a 500.
	if !found {
		return Result{}, &resource.ConflictError{
			Resource: "function",
			ID:       def.ID,
			Field:    "revision",
			Message:  "the function has no active revision yet; the operator's revision sync has not caught up",
		}
	}

	occurrenceKey, err := deriveOccurrenceKey(normalized, sourceUID)
	if err != nil {
		return Result{}, err
	}

	start := time.Now()
	stored, created, err := uc.publisher.Publish(ctx, jobRun(def, rev, occurrenceKey, normalized.ActorID))
	if err != nil {
		return Result{}, fmt.Errorf("publish function invocation: %w", err)
	}
	elapsed := time.Since(start).Seconds()

	metrics.FunctionInvocationsTotal.WithLabelValues(normalized.TenantID, outcomeLabel(created)).Inc()
	metrics.FunctionDurationSeconds.WithLabelValues(normalized.TenantID).Observe(elapsed)

	result := Result{
		Namespace:     stored.Namespace,
		Name:          stored.Name,
		OccurrenceKey: occurrenceKey,
		Trigger:       string(v1alpha1.Function),
		Phase:         stored.Status.Phase,
		Created:       created,
		FunctionID:    def.ID,
		RevisionID:    rev.ID,
	}

	if normalized.WaitSeconds > 0 {
		result.Phase = uc.awaitTerminalPhase(ctx, stored.Namespace, stored.Name,
			start.Add(time.Duration(normalized.WaitSeconds)*time.Second))
	}
	return result, nil
}

// awaitTerminalPhase polls the custom resource until its phase is terminal or
// the deadline passes, whichever comes first. The resource is the read model:
// the operator patches status from stored state, so no new read surface is
// needed. An expired wait is not an error -- the caller always gets the
// reference, and the phase observed last.
func (uc *UseCase) awaitTerminalPhase(ctx context.Context, namespace, name string, deadline time.Time) string {
	interval := uc.pollInterval
	if interval <= 0 {
		interval = invokePollInterval
	}
	phase := ""
	for {
		if err := ctx.Err(); err != nil {
			return phase
		}
		run, err := uc.publisher.Get(ctx, namespace, name)
		// Only a real observation updates the phase: a failed or empty read
		// must not blank out the last phase the resource reported.
		if err == nil && run.Status.Phase != "" {
			phase = run.Status.Phase
			if jobrun.Terminal(jobrun.Phase(phase)) {
				return phase
			}
		}
		if !time.Now().Add(interval).Before(deadline) {
			return phase
		}
		select {
		case <-ctx.Done():
			return phase
		case <-time.After(interval):
		}
	}
}

// visibleToGroup decides whether one definition is in a caller's scope. An
// unscoped caller (empty scope) sees everything; a scoped caller sees only
// rows stamped with its group, mirroring the checks read model's
// `resource_group_id = $caller_group` filter, where an ungrouped row is as
// invisible to a scoped key as another group's row.
func visibleToGroup(rowGroup, callerScope string) bool {
	return callerScope == "" || rowGroup == callerScope
}

func normalizeInput(in Input) (Input, error) {
	if in.FunctionID < 1 {
		return Input{}, validation.New("id", "must be >= 1")
	}

	// Tenant ids are tenants.id values, 26-character ULIDs; the CHAR(26)
	// column and its foreign keys reject anything else at write time, and a
	// 400 here is that rejection with its type intact.
	tenantID := strings.TrimSpace(in.TenantID)
	if len(tenantID) != 26 {
		return Input{}, validation.New("tenant_id", "must be a 26-character tenant id")
	}

	actorID := strings.TrimSpace(in.ActorID)
	if actorID == "" {
		return Input{}, validation.New("actor_id", "is required")
	}
	if len(actorID) > 255 {
		return Input{}, validation.New("actor_id", "must be <= 255 characters")
	}
	if in.WaitSeconds < 0 || in.WaitSeconds > maxInvokeWaitSeconds {
		return Input{}, validation.New("wait_seconds",
			fmt.Sprintf("must be between 0 and %d", maxInvokeWaitSeconds))
	}

	return Input{
		TenantID:        tenantID,
		FunctionID:      in.FunctionID,
		ActorID:         actorID,
		IdempotencyKey:  strings.TrimSpace(in.IdempotencyKey),
		ResourceGroupID: in.ResourceGroupID,
		WaitSeconds:     in.WaitSeconds,
	}, nil
}

// deriveOccurrenceKey derives the 64-hex occurrence key the ledger
// deduplicates on — the manual trigger's derivation with the function
// identity. With an idempotency key the value is stable, so a retried
// invocation resolves to the same run; without one, fresh entropy makes each
// call its own run, since two invocations are two runs unless the caller says
// otherwise.
func deriveOccurrenceKey(in Input, sourceUID string) (string, error) {
	base := fmt.Sprintf("function|%s|%s|%d", in.TenantID, sourceUID, in.FunctionID)
	if in.IdempotencyKey != "" {
		return sha256Hex(base + "|idem|" + in.IdempotencyKey), nil
	}

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("generate invocation nonce: %w", err)
	}
	return sha256Hex(base + "|nonce|" + hex.EncodeToString(nonce[:])), nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// jobRun renders the JobRun Custom Resource for one invocation. The scheduled
// job ref names the function's stable revision identity -- the same string the
// ledger stores as source_uid -- and the object name derives from the
// occurrence key, so a replayed invocation addresses one object. There are
// deliberately no owner references: the ref does not point at a ScheduledJob
// custom resource, and an owner pointer to an object that does not exist would
// have Kubernetes garbage-collect the run immediately. The timeout rides the
// CR so enforcement survives an operator outage, pinned to what the invoked
// version declared.
func jobRun(def function.Definition, rev Revision, occurrenceKey, actorID string) v1alpha1.JobRun {
	name := function.SourceUID(def.ID)
	return v1alpha1.JobRun{
		TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "JobRun"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: rev.Namespace,
			Name:      v1alpha1.RunObjectName(name, occurrenceKey),
		},
		Spec: v1alpha1.JobRunSpec{
			ScheduledJobRef:    v1alpha1.ObjectReference{Name: name, UID: name},
			DefinitionRevision: rev.ID,
			Trigger:            v1alpha1.Function,
			Actor:              actorID,
			OccurrenceKey:      occurrenceKey,
			TimeoutSeconds:     int32(def.TimeoutSeconds),
		},
	}
}

// outcomeLabel names the counter's outcome dimension: created for an
// invocation whose JobRun was accepted as a new object, deduplicated for a
// replay that adopted the run an earlier attempt published.
func outcomeLabel(created bool) string {
	if created {
		return "created"
	}
	return "deduplicated"
}

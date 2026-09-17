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
	"errors"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/platform/metrics"
)

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

// RunPublisher hands a JobRun Custom Resource to Kubernetes. It must be
// idempotent: the object name derives from the occurrence key, so a retry
// after a failed publish adopts the object instead of doubling it. The bool
// reports whether this call created the object.
type RunPublisher interface {
	Publish(ctx context.Context, run v1alpha1.JobRun) (v1alpha1.JobRun, bool, error)
}

// ErrRevisionPending reports that the function exists but the operator has
// not yet materialized any revision for it — a definition invoked between its
// save and the sync loop's first pass. It is a retryable condition, not a
// not-found one: the definition is there, its executable form is not.
var ErrRevisionPending = errors.New("function revision is not materialized yet")

// UseCase invokes a function by publishing one Function-trigger JobRun.
type UseCase struct {
	functions FunctionReader
	revisions RevisionSource
	publisher RunPublisher
}

// New assembles the invoke use case.
func New(functions FunctionReader, revisions RevisionSource, publisher RunPublisher) *UseCase {
	return &UseCase{functions: functions, revisions: revisions, publisher: publisher}
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
}

// Result is what a caller gets back: a reference to the JobRun Custom
// Resource, not a ledger row. The row is written later by the operator, so
// Phase is whatever the CR's status says, empty until the operator has
// observed it once. Created is false for a replay that adopted an existing
// run — the same occurrence, not a new one.
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
	if uc.publisher == nil {
		return Result{}, fmt.Errorf("job run publisher is required")
	}
	if uc.functions == nil || uc.revisions == nil {
		return Result{}, fmt.Errorf("function reader and revision source are required")
	}

	def, found, err := uc.functions.GetForTenant(ctx, normalized.TenantID, normalized.FunctionID)
	if err != nil {
		return Result{}, fmt.Errorf("read function for invoke: %w", err)
	}
	if !found {
		return Result{}, &resource.NotFoundError{Resource: "function", ID: normalized.FunctionID}
	}
	if def.Status != function.StatusActive {
		// Paused is the suspended-definition conflict, not an error: the
		// definition exists and the caller reached it, but invocation is
		// switched off.
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
		return Result{}, fmt.Errorf("read active revision for invoke: %w", err)
	}
	if !found {
		return Result{}, fmt.Errorf("invoke function %d: %w", def.ID, ErrRevisionPending)
	}

	occurrenceKey, err := deriveOccurrenceKey(normalized, sourceUID)
	if err != nil {
		return Result{}, err
	}

	start := time.Now()
	stored, created, err := uc.publisher.Publish(ctx, jobRun(def, rev, occurrenceKey, normalized.ActorID))
	if err != nil {
		return Result{}, fmt.Errorf("publish function run: %w", err)
	}
	elapsed := time.Since(start).Seconds()

	metrics.FunctionInvocationsTotal.WithLabelValues(normalized.TenantID, outcomeLabel(created)).Inc()
	metrics.FunctionDurationSeconds.WithLabelValues(normalized.TenantID).Observe(elapsed)

	return Result{
		Namespace:     stored.Namespace,
		Name:          stored.Name,
		OccurrenceKey: occurrenceKey,
		Trigger:       string(v1alpha1.Function),
		Phase:         stored.Status.Phase,
		Created:       created,
		FunctionID:    def.ID,
		RevisionID:    rev.ID,
	}, nil
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

	return Input{
		TenantID:       tenantID,
		FunctionID:     in.FunctionID,
		ActorID:        actorID,
		IdempotencyKey: strings.TrimSpace(in.IdempotencyKey),
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
		return "", fmt.Errorf("generate invoke nonce: %w", err)
	}
	return sha256Hex(base + "|nonce|" + hex.EncodeToString(nonce[:])), nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// jobRun renders the JobRun Custom Resource for one invocation. The object
// name derives from the source uid and the occurrence key through the shared
// naming rule, so a retried invocation, a cancel request and the operator all
// address one object. The timeout rides the CR so enforcement survives an
// operator outage, pinned to what the invoked version declared.
func jobRun(def function.Definition, rev Revision, occurrenceKey, actorID string) v1alpha1.JobRun {
	name := function.SourceUID(def.ID)
	return v1alpha1.JobRun{
		TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "JobRun"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: rev.Namespace,
			Name:      v1alpha1.RunObjectName(name, occurrenceKey),
			// Deleting the definition garbage-collects its runs rather than
			// leaving orphans no reconciler owns, mirroring the manual
			// trigger's owner reference shape.
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         v1alpha1.GroupVersion.String(),
				Kind:               "ScheduledJob",
				Name:               name,
				UID:                types.UID(name),
				Controller:         ptr(true),
				BlockOwnerDeletion: ptr(false),
			}},
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

func ptr[T any](v T) *T { return &v }

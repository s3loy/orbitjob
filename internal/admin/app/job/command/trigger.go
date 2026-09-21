package command

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	query "orbitjob/internal/admin/app/job/query"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/platform/metrics"
)

// jobReader reads the active definition a manual trigger refers to.
type jobReader interface {
	Get(ctx context.Context, in query.GetInput) (query.GetItem, error)
}

// jobRunPublisher hands a JobRun Custom Resource to Kubernetes. The operator
// reconciles it and owns the ledger write: orbitjob_admin holds SELECT only on
// job_run_control_plane, so the API cannot create the run row and must not be
// given the grant to try.
type jobRunPublisher interface {
	Publish(ctx context.Context, run v1alpha1.JobRun) (v1alpha1.JobRun, bool, error)
}

// TriggerJobUseCase turns a manual trigger into a JobRun Custom Resource.
type TriggerJobUseCase struct {
	jobReader jobReader
	publisher jobRunPublisher
}

// NewTriggerJobUseCase builds a TriggerJobUseCase.
func NewTriggerJobUseCase(jobReader jobReader, publisher jobRunPublisher) *TriggerJobUseCase {
	return &TriggerJobUseCase{jobReader: jobReader, publisher: publisher}
}

// TriggerInput is the admin command input for a manual trigger.
type TriggerInput struct {
	// JobID is the active revision id of the definition being triggered.
	JobID    int64
	TenantID string
	// ActorID is the authenticated principal that asked for the run. It is
	// required: the operator records it in the ledger's actor column, which the
	// schema keeps NOT NULL and non-empty, so a run with no attributable actor
	// could not be written and would not answer the ledger's own question.
	ActorID string
	// IdempotencyKey, when set, makes a repeated trigger resolve to the same run
	// instead of creating another.
	IdempotencyKey string
	// ResourceGroupID is the scope the caller's key is limited to; empty for an
	// unscoped caller. A definition has no group, so a scoped caller is refused
	// rather than allowed to trigger across the whole tenant.
	ResourceGroupID string
}

// TriggerResult is what a caller gets back: a reference to the JobRun Custom
// Resource, not a ledger row. The API cannot return a row id because it does not
// write the row — the operator does, asynchronously, after the CR is reconciled.
// Phase is whatever the CR's status currently says, empty until the operator has
// observed it once; reporting a phase the operator has not set would be a guess.
type TriggerResult struct {
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	OccurrenceKey string `json:"occurrence_key"`
	Trigger       string `json:"trigger"`
	Phase         string `json:"phase"`
	Created       bool   `json:"created"`
}

// Trigger validates the request, reads the definition it names, and publishes a
// JobRun. It never writes the ledger.
func (uc *TriggerJobUseCase) Trigger(ctx context.Context, in TriggerInput) (TriggerResult, error) {
	normalized, err := normalizeTriggerInput(in)
	if err != nil {
		return TriggerResult{}, err
	}
	if uc.publisher == nil {
		return TriggerResult{}, fmt.Errorf("job run publisher is required")
	}

	def, err := uc.jobReader.Get(ctx, query.GetInput{ID: normalized.JobID, TenantID: normalized.TenantID})
	if err != nil {
		return TriggerResult{}, fmt.Errorf("read definition for trigger: %w", err)
	}
	if def.Suspend {
		return TriggerResult{}, &resource.ConflictError{
			Resource: "job",
			ID:       def.ID,
			Field:    "suspend",
			Message:  "cannot trigger a suspended definition",
		}
	}

	occurrenceKey, err := manualOccurrenceKey(normalized, def.SourceUID)
	if err != nil {
		return TriggerResult{}, err
	}

	run := manualJobRun(def, occurrenceKey, normalized.ActorID)

	start := time.Now()
	stored, created, err := uc.publisher.Publish(ctx, run)
	if err != nil {
		return TriggerResult{}, fmt.Errorf("publish job run: %w", err)
	}
	metrics.TriggerLatency.WithLabelValues(normalized.TenantID).Observe(time.Since(start).Seconds())

	return TriggerResult{
		Namespace:     stored.Namespace,
		Name:          stored.Name,
		OccurrenceKey: occurrenceKey,
		Trigger:       string(v1alpha1.Manual),
		Phase:         stored.Status.Phase,
		Created:       created,
	}, nil
}

func normalizeTriggerInput(in TriggerInput) (TriggerInput, error) {
	if in.JobID < 1 {
		return TriggerInput{}, validation.New("id", "must be >= 1")
	}

	// Scope is decided first: a scoped caller is refused even when the rest of
	// the input is invalid, so an authorization denial never depends on the
	// request being otherwise well-formed. The read gates use the same order.
	if err := resource.RequireUnscoped(in.ResourceGroupID, "job definition"); err != nil {
		return TriggerInput{}, err
	}

	// Tenant ids are tenants.id values, 26-character ULIDs; the CHAR(26)
	// columns and their foreign keys reject anything else at write time, and a
	// 400 here is that rejection with its type intact. There is no default
	// tenant to fall back to.
	tenantID := strings.TrimSpace(in.TenantID)
	if len(tenantID) != 26 {
		return TriggerInput{}, validation.New("tenant_id", "must be a 26-character tenant id")
	}

	actorID := strings.TrimSpace(in.ActorID)
	if actorID == "" {
		return TriggerInput{}, validation.New("actor_id", "is required")
	}
	if len(actorID) > 255 {
		return TriggerInput{}, validation.New("actor_id", "must be <= 255 characters")
	}

	return TriggerInput{
		JobID:           in.JobID,
		TenantID:        tenantID,
		ActorID:         actorID,
		IdempotencyKey:  strings.TrimSpace(in.IdempotencyKey),
		ResourceGroupID: in.ResourceGroupID,
	}, nil
}

// manualJobRun renders the JobRun Custom Resource for a manual trigger. The
// object name is derived from the occurrence key, so a replay that reaches the
// API twice addresses one object rather than minting a second run. The actor is
// spec.actor, the field the CRD makes required and validates: the caller's
// authenticated principal, so the value the operator copies into the ledger's
// actor column cannot be a header the caller invented.
func manualJobRun(def query.GetItem, occurrenceKey, actorID string) v1alpha1.JobRun {
	return v1alpha1.JobRun{
		TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "JobRun"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: def.Namespace,
			Name:      manualRunName(def.Name, occurrenceKey),
			// Deleting the definition garbage-collects its runs rather than
			// leaving orphans no reconciler owns.
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         v1alpha1.GroupVersion.String(),
				Kind:               "ScheduledJob",
				Name:               def.Name,
				UID:                types.UID(def.SourceUID),
				Controller:         ptr(true),
				BlockOwnerDeletion: ptr(false),
			}},
		},
		Spec: v1alpha1.JobRunSpec{
			ScheduledJobRef:    v1alpha1.ObjectReference{Name: def.Name, UID: def.SourceUID},
			DefinitionRevision: def.ID,
			Trigger:            v1alpha1.Manual,
			Actor:              actorID,
			OccurrenceKey:      occurrenceKey,
			TimeoutSeconds:     def.TimeoutSeconds,
		},
	}
}

// manualOccurrenceKey derives the 64-hex occurrence key the ledger deduplicates
// on. With an idempotency key the value is stable, so a retried trigger lands on
// the same run; without one, fresh entropy makes each trigger its own run, since
// two manual triggers are two runs unless the caller says otherwise.
func manualOccurrenceKey(in TriggerInput, sourceUID string) (string, error) {
	base := fmt.Sprintf("manual|%s|%s|%d", in.TenantID, sourceUID, in.JobID)
	if in.IdempotencyKey != "" {
		return sha256Hex(base + "|idem|" + in.IdempotencyKey), nil
	}

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("generate trigger nonce: %w", err)
	}
	return sha256Hex(base + "|nonce|" + hex.EncodeToString(nonce[:])), nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// manualRunName delegates to the shared JobRun naming rule, so a manually
// triggered run is addressable exactly like a scheduled one.
func manualRunName(scheduledJobName, occurrenceKey string) string {
	return v1alpha1.RunObjectName(scheduledJobName, occurrenceKey)
}

func ptr[T any](v T) *T { return &v }

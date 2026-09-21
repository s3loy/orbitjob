package command

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	query "orbitjob/internal/admin/app/job/query"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

// The manual trigger gate refuses a scoped caller because a definition has no
// group to be limited by: serving one would let a resource-group key trigger
// across the whole tenant. trigger_test.go pins the refusal at the normalizer;
// these tests pin it at the use case, where the refusal must land before any
// effect exists -- no definition read, no JobRun Custom Resource published --
// and where a spy can prove it. The 403 mapping of the refusal is verified by
// inspection of internal/admin/http/errors.go, not by a test here.

// Fixtures inserted by this file. Tenant ids are tenants.id values, CHAR(26)
// ULIDs; slugs like "default" are never tenant identifiers. t3ScopedGroup is a
// resource group id, a different kind of identifier and never used as a tenant.
const (
	t3Tenant      = "01J9Z7V2M4QK7P3XWQ5R8TNBCE"
	t3ScopedGroup = "01JBB0W9YRXG4SZV2QKM78N3PD"
)

// t3SpyDefinitionReader records every definition read that crosses the use
// case, so a test can prove none happened.
type t3SpyDefinitionReader struct {
	calls int
	in    query.GetInput

	item query.GetItem
	err  error
}

func (s *t3SpyDefinitionReader) Get(_ context.Context, in query.GetInput) (query.GetItem, error) {
	s.calls++
	s.in = in
	return s.item, s.err
}

// t3SpyRunPublisher records every JobRun handed to the cluster.
type t3SpyRunPublisher struct {
	calls int
	run   v1alpha1.JobRun

	stored  v1alpha1.JobRun
	created bool
	err     error
}

func (s *t3SpyRunPublisher) Publish(_ context.Context, run v1alpha1.JobRun) (v1alpha1.JobRun, bool, error) {
	s.calls++
	s.run = run
	return s.stored, s.created, s.err
}

func t3SuspendableDefinition() query.GetItem {
	return query.GetItem{
		ID:        7,
		TenantID:  t3Tenant,
		Name:      "nightly-index",
		Namespace: "orbitjob-jobs",
		SourceUID: "src-nightly-index",
		Suspend:   false,
	}
}

func t3PublishedRun() v1alpha1.JobRun {
	return v1alpha1.JobRun{
		ObjectMeta: metav1.ObjectMeta{Namespace: "orbitjob-jobs", Name: "nightly-index-run"},
		Status:     v1alpha1.JobRunStatus{Phase: string(jobrun.Pending)},
	}
}

func TestTriggerScopedCallerIsRefusedBeforeAnyEffect(t *testing.T) {
	reader := &t3SpyDefinitionReader{item: t3SuspendableDefinition()}
	publisher := &t3SpyRunPublisher{stored: t3PublishedRun(), created: true}
	uc := NewTriggerJobUseCase(reader, publisher)

	result, err := uc.Trigger(context.Background(), TriggerInput{
		JobID:           7,
		TenantID:        t3Tenant,
		ActorID:         "ci-actor",
		ResourceGroupID: t3ScopedGroup,
	})

	var serr *resource.ScopeError
	if !errors.As(err, &serr) {
		t.Fatalf("scoped trigger got %v (%T), want *resource.ScopeError", err, err)
	}
	if serr.Resource != "job definition" || serr.Scope != t3ScopedGroup {
		t.Fatalf("refusal = %+v, want resource %q scope %q", serr, "job definition", t3ScopedGroup)
	}
	// The refusal precedes every effect: the definition was never read and no
	// JobRun reached the cluster. A refusal that arrives after the publish
	// would already have minted a run the scoped key was never allowed to ask
	// for.
	if reader.calls != 0 {
		t.Fatalf("scoped trigger read the definition %d times before being refused", reader.calls)
	}
	if publisher.calls != 0 {
		t.Fatalf("scoped trigger published a JobRun before being refused")
	}
	if result != (TriggerResult{}) {
		t.Fatalf("refused trigger returned a result %+v, want the zero value", result)
	}
}

func TestTriggerUnscopedCallerPublishesExactlyOneRun(t *testing.T) {
	// Control for the refusal above: the same use case with the spies armed.
	// An unscoped trigger must flow through to exactly one publish, so the
	// scoped test's zero calls mean the guard refused, not that the spies were
	// inert.
	reader := &t3SpyDefinitionReader{item: t3SuspendableDefinition()}
	publisher := &t3SpyRunPublisher{stored: t3PublishedRun(), created: true}
	uc := NewTriggerJobUseCase(reader, publisher)

	result, err := uc.Trigger(context.Background(), TriggerInput{
		JobID:    7,
		TenantID: t3Tenant,
		ActorID:  "ci-actor",
	})
	if err != nil {
		t.Fatalf("unscoped trigger refused: %v", err)
	}
	if reader.calls != 1 || publisher.calls != 1 {
		t.Fatalf("read %d times, published %d times, want exactly one of each", reader.calls, publisher.calls)
	}
	if reader.in.ID != 7 || reader.in.TenantID != t3Tenant {
		t.Fatalf("definition read with %+v, want the requested id and tenant", reader.in)
	}
	// The run the API hands to the cluster carries the authenticated actor and
	// the revision it triggers, the fields the operator copies into the ledger.
	if publisher.run.Spec.Actor != "ci-actor" {
		t.Fatalf("published run actor %q, want the authenticated caller", publisher.run.Spec.Actor)
	}
	if publisher.run.Spec.DefinitionRevision != 7 {
		t.Fatalf("published run pins revision %d, want 7", publisher.run.Spec.DefinitionRevision)
	}
	if publisher.run.Namespace != "orbitjob-jobs" {
		t.Fatalf("published run namespace %q, want the definition's namespace", publisher.run.Namespace)
	}
	if result.Trigger != string(v1alpha1.Manual) {
		t.Fatalf("result trigger %q, want %q", result.Trigger, string(v1alpha1.Manual))
	}
	if !result.Created || result.Phase != string(jobrun.Pending) {
		t.Fatalf("result = created=%v phase=%q, want the publisher's answer echoed", result.Created, result.Phase)
	}
	// Without an idempotency key every trigger is its own run: the occurrence
	// key carries fresh entropy, 64 hex characters.
	if _, hexErr := hex.DecodeString(result.OccurrenceKey); hexErr != nil || len(result.OccurrenceKey) != 64 {
		t.Fatalf("occurrence key %q is not a 64-character hex value", result.OccurrenceKey)
	}
}

func TestTriggerTenantSlugIsNeverATenantId(t *testing.T) {
	// "default" is the slug a deployment is most likely to confuse for a
	// tenant. The gate must refuse it as a typed validation error -- the 400
	// VALIDATION_ERROR shape -- before any effect, and never fall back to a
	// default tenant.
	reader := &t3SpyDefinitionReader{item: t3SuspendableDefinition()}
	publisher := &t3SpyRunPublisher{stored: t3PublishedRun(), created: true}
	uc := NewTriggerJobUseCase(reader, publisher)

	_, err := uc.Trigger(context.Background(), TriggerInput{
		JobID:    7,
		TenantID: "default",
		ActorID:  "ci-actor",
	})
	var verr *validation.Error
	if !errors.As(err, &verr) {
		t.Fatalf("slug tenant got %v (%T), want *validation.Error", err, err)
	}
	if verr.Field != "tenant_id" {
		t.Fatalf("refusal names field %q, want %q", verr.Field, "tenant_id")
	}
	if reader.calls != 0 || publisher.calls != 0 {
		t.Fatalf("slug tenant reached the reader %d times / publisher %d times before refusal", reader.calls, publisher.calls)
	}

	// At the gate itself: this file's own 26-character ULID is the accepted
	// shape; the slug is not.
	out, err := normalizeTriggerInput(TriggerInput{JobID: 7, TenantID: t3Tenant, ActorID: "ci-actor"})
	if err != nil {
		t.Fatalf("26-character tenant refused: %v", err)
	}
	if out.TenantID != t3Tenant {
		t.Fatalf("tenant normalized to %q, want %q", out.TenantID, t3Tenant)
	}
	_, err = normalizeTriggerInput(TriggerInput{JobID: 7, TenantID: "default", ActorID: "ci-actor"})
	var verr2 *validation.Error
	if !errors.As(err, &verr2) || verr2.Field != "tenant_id" {
		t.Fatalf("slug tenant at the gate got %v, want a *validation.Error on tenant_id", err)
	}
}

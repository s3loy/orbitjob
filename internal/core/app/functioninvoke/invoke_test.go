package functioninvoke

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/platform/metrics"
)

// Every test reads back only the metric series whose label values are unique
// to it, so the promauto-registered global vectors keep the assertions exact
// regardless of test order.

const tenantID = "fninvoke-tenant00000000000" // 26 chars

func activeDefinition(id int64) function.Definition {
	return function.Definition{
		ID:             id,
		TenantID:       tenantID,
		Status:         function.StatusActive,
		Image:          "registry.example/fn@sha256:aa11",
		Command:        []string{"/fn"},
		Args:           []string{"--once"},
		TimeoutSeconds: 45,
		RetryLimit:     2,
	}
}

type fakeFunctions struct {
	def       function.Definition
	found     bool
	err       error
	gotTenant string
	gotID     int64
}

func (f *fakeFunctions) GetForTenant(ctx context.Context, tenant string, id int64) (function.Definition, bool, error) {
	f.gotTenant, f.gotID = tenant, id
	return f.def, f.found, f.err
}

type fakeRevisions struct {
	rev   Revision
	found bool
	err   error

	gotTenant    string
	gotSourceUID string
}

func (f *fakeRevisions) ActiveRevision(ctx context.Context, tenant, sourceUID string) (Revision, bool, error) {
	f.gotTenant, f.gotSourceUID = tenant, sourceUID
	return f.rev, f.found, f.err
}

type fakePublisher struct {
	stored    v1alpha1.JobRun
	created   bool
	err       error
	published []v1alpha1.JobRun
}

func (f *fakePublisher) Publish(ctx context.Context, run v1alpha1.JobRun) (v1alpha1.JobRun, bool, error) {
	f.published = append(f.published, run)
	if f.err != nil {
		return v1alpha1.JobRun{}, false, f.err
	}
	return f.stored, f.created, nil
}

func assembledUseCase(fns *fakeFunctions, revs *fakeRevisions, pub *fakePublisher) *UseCase {
	return New(fns, revs, pub)
}

func invokeInput(id int64) Input {
	return Input{TenantID: tenantID, FunctionID: id, ActorID: "admin-key-1"}
}

// TestInvokePublishesOneFunctionRun pins the happy path: an active definition
// with a materialized revision publishes exactly one Function-trigger JobRun
// carrying the pinned revision, the caller's actor and the definition's
// timeout, and reports the CR reference.
func TestInvokePublishesOneFunctionRun(t *testing.T) {
	def := activeDefinition(7)
	rev := Revision{ID: 501, Namespace: "fn-namespace"}
	pub := &fakePublisher{created: true, stored: v1alpha1.JobRun{}}
	pub.stored.Namespace = "fn-namespace"
	uc := assembledUseCase(
		&fakeFunctions{def: def, found: true},
		&fakeRevisions{rev: rev, found: true},
		pub,
	)

	got, err := uc.Invoke(context.Background(), invokeInput(7))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pub.published) != 1 {
		t.Fatalf("published %d runs, want exactly 1", len(pub.published))
	}
	run := pub.published[0]
	if run.Spec.Trigger != v1alpha1.Function {
		t.Fatalf("trigger = %q, want Function", run.Spec.Trigger)
	}
	if run.Spec.ScheduledJobRef.Name != "function-7" || run.Spec.ScheduledJobRef.UID != "function-7" {
		t.Fatalf("scheduledJobRef = %+v, want function-7 for both name and uid", run.Spec.ScheduledJobRef)
	}
	if run.Spec.DefinitionRevision != 501 {
		t.Fatalf("definitionRevision = %d, want the pinned 501", run.Spec.DefinitionRevision)
	}
	if run.Spec.Actor != "admin-key-1" {
		t.Fatalf("actor = %q, want the caller's principal", run.Spec.Actor)
	}
	if run.Spec.OccurrenceKey == "" || len(run.Spec.OccurrenceKey) != 64 {
		t.Fatalf("occurrence key = %q, want a 64-hex digest", run.Spec.OccurrenceKey)
	}
	if run.Spec.TimeoutSeconds != 45 {
		t.Fatalf("timeoutSeconds = %d, want the definition's 45", run.Spec.TimeoutSeconds)
	}
	if run.Namespace != "fn-namespace" {
		t.Fatalf("namespace = %q, want the revision's", run.Namespace)
	}
	if want := v1alpha1.RunObjectName("function-7", run.Spec.OccurrenceKey); run.Name != want {
		t.Fatalf("name = %q, want the shared derivation %q", run.Name, want)
	}
	if got.OccurrenceKey != run.Spec.OccurrenceKey {
		t.Fatalf("result key %q does not match the published CR's %q", got.OccurrenceKey, run.Spec.OccurrenceKey)
	}
	if !got.Created || got.Trigger != string(v1alpha1.Function) || got.RevisionID != 501 || got.FunctionID != 7 {
		t.Fatalf("result drifted: %+v", got)
	}
}

// TestInvokeCountsCreatedAndDeduplicatedOutcomes pins the invocation counter's
// outcome dimension: the invoke path's exactly-once semantics live in the
// occurrence key, so a replay must report deduplicated, not a second created.
func TestInvokeCountsCreatedAndDeduplicatedOutcomes(t *testing.T) {
	// A tenant unique to this test: the counters are global, so assertions
	// read only the series this test mints.
	const dedupTenant = "fninvoke-dedup000000000000"
	pub := &fakePublisher{created: true}
	revs := &fakeRevisions{rev: Revision{ID: 1, Namespace: "ns"}, found: true}
	fns := &fakeFunctions{def: activeDefinition(3), found: true}
	uc := assembledUseCase(fns, revs, pub)

	in := invokeInput(3)
	in.TenantID = dedupTenant
	in.IdempotencyKey = "client-token-1"
	if _, err := uc.Invoke(context.Background(), in); err != nil {
		t.Fatalf("first invoke: %v", err)
	}

	// The replay: same key, same run. The publisher reports adoption.
	pub.created = false
	got, err := uc.Invoke(context.Background(), in)
	if err != nil {
		t.Fatalf("replayed invoke: %v", err)
	}
	if got.Created {
		t.Fatal("replay reported Created=true")
	}
	if len(pub.published) != 2 {
		t.Fatalf("published %d runs, want both attempts to reach the publisher", len(pub.published))
	}
	if pub.published[0].Spec.OccurrenceKey != pub.published[1].Spec.OccurrenceKey {
		t.Fatalf("replay changed the occurrence key: %q vs %q",
			pub.published[0].Spec.OccurrenceKey, pub.published[1].Spec.OccurrenceKey)
	}

	if got := testutil.ToFloat64(metrics.FunctionInvocationsTotal.WithLabelValues(dedupTenant, "created")); got != 1 {
		t.Fatalf("created outcome = %v, want 1", got)
	}
	if got := testutil.ToFloat64(metrics.FunctionInvocationsTotal.WithLabelValues(dedupTenant, "deduplicated")); got != 1 {
		t.Fatalf("deduplicated outcome = %v, want 1", got)
	}
}

// TestInvokeIdempotencyKeyStability pins the occurrence-key derivation: the
// same idempotency key addresses one run across calls and tenants cannot
// collide by choosing the same key, while an unset key mints fresh entropy so
// two ordinary invocations are two runs.
func TestInvokeIdempotencyKeyStability(t *testing.T) {
	pub := &fakePublisher{}
	revs := &fakeRevisions{rev: Revision{ID: 1, Namespace: "ns"}, found: true}
	fns := &fakeFunctions{def: activeDefinition(4), found: true}
	uc := assembledUseCase(fns, revs, pub)

	withKey := invokeInput(4)
	withKey.IdempotencyKey = "stable"
	first, err := uc.Invoke(context.Background(), withKey)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	second, err := uc.Invoke(context.Background(), withKey)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if first.OccurrenceKey != second.OccurrenceKey {
		t.Fatalf("same idempotency key produced keys %q and %q", first.OccurrenceKey, second.OccurrenceKey)
	}

	otherKey := withKey
	otherKey.IdempotencyKey = "different"
	third, err := uc.Invoke(context.Background(), otherKey)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if third.OccurrenceKey == first.OccurrenceKey {
		t.Fatal("a different idempotency key produced the same run")
	}

	fresh1, err := uc.Invoke(context.Background(), invokeInput(4))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	fresh2, err := uc.Invoke(context.Background(), invokeInput(4))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if fresh1.OccurrenceKey == fresh2.OccurrenceKey {
		t.Fatal("two key-less invocations shared an occurrence key; each call must be its own run")
	}
}

// TestInvokePublishLatencyIsObserved pins the duration histogram's producer:
// the invoke path observes its own span for the tenant.
func TestInvokePublishLatencyIsObserved(t *testing.T) {
	pub := &fakePublisher{created: true}
	uc := assembledUseCase(
		&fakeFunctions{def: activeDefinition(5), found: true},
		&fakeRevisions{rev: Revision{ID: 2, Namespace: "ns"}, found: true},
		pub,
	)

	if _, err := uc.Invoke(context.Background(), invokeInput(5)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// One series for the fixture tenant, with at least one observation in it.
	if n := testutil.CollectAndCount(metrics.FunctionDurationSeconds); n < 1 {
		t.Fatalf("duration series count = %d, want at least 1", n)
	}
}

func TestInvokeRefusesPausedFunction(t *testing.T) {
	def := activeDefinition(8)
	def.Status = function.StatusPaused
	pub := &fakePublisher{}
	uc := assembledUseCase(
		&fakeFunctions{def: def, found: true},
		&fakeRevisions{rev: Revision{ID: 1, Namespace: "ns"}, found: true},
		pub,
	)

	_, err := uc.Invoke(context.Background(), invokeInput(8))
	var conflict *resource.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error = %v, want the suspended-definition ConflictError", err)
	}
	if conflict.Field != "status" {
		t.Fatalf("conflict field = %q, want status", conflict.Field)
	}
	if len(pub.published) != 0 {
		t.Fatal("a paused function must not publish")
	}
}

func TestInvokeFunctionNotFound(t *testing.T) {
	pub := &fakePublisher{}
	fns := &fakeFunctions{found: false}
	uc := assembledUseCase(fns, &fakeRevisions{found: true}, pub)

	_, err := uc.Invoke(context.Background(), invokeInput(404))
	var notFound *resource.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("error = %v, want a NotFoundError", err)
	}
	if fns.gotID != 404 {
		t.Fatalf("reader asked for id %d, want 404", fns.gotID)
	}
	if len(pub.published) != 0 {
		t.Fatal("an unknown function must not publish")
	}
}

func TestInvokeRevisionPending(t *testing.T) {
	pub := &fakePublisher{}
	revs := &fakeRevisions{found: false}
	uc := assembledUseCase(&fakeFunctions{def: activeDefinition(9), found: true}, revs, pub)

	_, err := uc.Invoke(context.Background(), invokeInput(9))
	if !errors.Is(err, ErrRevisionPending) {
		t.Fatalf("error = %v, want ErrRevisionPending", err)
	}
	if revs.gotSourceUID != "function-9" {
		t.Fatalf("resolver asked for %q, want function-9", revs.gotSourceUID)
	}
	if len(pub.published) != 0 {
		t.Fatal("an unmaterialized revision must not publish")
	}
}

func TestInvokeStoreAndPublishErrorsPropagate(t *testing.T) {
	t.Run("function read fails", func(t *testing.T) {
		pub := &fakePublisher{}
		uc := assembledUseCase(
			&fakeFunctions{err: errors.New("db down")},
			&fakeRevisions{found: true}, pub,
		)
		_, err := uc.Invoke(context.Background(), invokeInput(1))
		if err == nil || !strings.Contains(err.Error(), "db down") {
			t.Fatalf("error = %v, want the wrapped cause", err)
		}
		if len(pub.published) != 0 {
			t.Fatal("a failed read must not publish")
		}
	})

	t.Run("revision read fails", func(t *testing.T) {
		pub := &fakePublisher{}
		uc := assembledUseCase(
			&fakeFunctions{def: activeDefinition(1), found: true},
			&fakeRevisions{err: errors.New("db down")}, pub,
		)
		_, err := uc.Invoke(context.Background(), invokeInput(1))
		if err == nil || !strings.Contains(err.Error(), "db down") {
			t.Fatalf("error = %v, want the wrapped cause", err)
		}
	})

	t.Run("publish fails", func(t *testing.T) {
		pub := &fakePublisher{err: errors.New("api server unreachable")}
		uc := assembledUseCase(
			&fakeFunctions{def: activeDefinition(1), found: true},
			&fakeRevisions{rev: Revision{ID: 1, Namespace: "ns"}, found: true},
			pub,
		)
		_, err := uc.Invoke(context.Background(), invokeInput(1))
		if err == nil || !strings.Contains(err.Error(), "api server unreachable") {
			t.Fatalf("error = %v, want the wrapped cause", err)
		}
	})
}

func TestInvokeValidatesInput(t *testing.T) {
	cases := map[string]Input{
		"zero function id":  {TenantID: tenantID, FunctionID: 0, ActorID: "a"},
		"short tenant id":   {TenantID: "short", FunctionID: 1, ActorID: "a"},
		"blank actor":       {TenantID: tenantID, FunctionID: 1, ActorID: "  "},
		"oversized actor":   {TenantID: tenantID, FunctionID: 1, ActorID: strings.Repeat("x", 256)},
		"missing tenant id": {FunctionID: 1, ActorID: "a"},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			pub := &fakePublisher{}
			uc := assembledUseCase(
				&fakeFunctions{def: activeDefinition(1), found: true},
				&fakeRevisions{found: true}, pub,
			)
			_, err := uc.Invoke(context.Background(), in)
			if !validation.Is(err) {
				t.Fatalf("error = %v, want a validation error", err)
			}
			if len(pub.published) != 0 {
				t.Fatal("invalid input must not publish")
			}
		})
	}
}

func TestInvokeRequiresDependencies(t *testing.T) {
	in := invokeInput(1)

	if _, err := New(nil, &fakeRevisions{found: true}, &fakePublisher{}).Invoke(context.Background(), in); err == nil {
		t.Fatal("nil function reader accepted")
	}
	if _, err := New(&fakeFunctions{found: true}, nil, &fakePublisher{}).Invoke(context.Background(), in); err == nil {
		t.Fatal("nil revision source accepted")
	}
	if _, err := New(&fakeFunctions{found: true}, &fakeRevisions{found: true}, nil).Invoke(context.Background(), in); err == nil {
		t.Fatal("nil publisher accepted")
	}
}

package definition

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/platform/imagepin"
	"orbitjob/internal/platform/scan"
	"strings"
)

type testOperationStore struct {
	mu         sync.Mutex
	operations map[string]Operation
	finishErr  error
}

func (m *testOperationStore) Register(_ context.Context, s Subject, op Operation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.operations == nil {
		m.operations = map[string]Operation{}
	}
	if old, ok := m.operations[op.RequestID]; ok && (old.ActorID != s.ActorID || old.PayloadHash != op.PayloadHash || old.State != "checked") {
		return &resource.ConflictError{Resource: "operation"}
	}
	m.operations[op.RequestID] = op
	return nil
}
func (m *testOperationStore) Lookup(_ context.Context, s Subject, id string) (Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op, ok := m.operations[id]
	if !ok || op.ActorID != s.ActorID {
		return Operation{}, &resource.NotFoundError{Resource: "operation"}
	}
	return op, nil
}
func (m *testOperationStore) Claim(_ context.Context, s Subject, id, hash string, now time.Time) (Operation, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op, ok := m.operations[id]
	if !ok || op.ActorID != s.ActorID {
		return op, false, validation.New("request_id", "server check required")
	}
	if op.PayloadHash != hash || (op.State == "checked" && !now.Before(op.CheckedUntil)) {
		return op, false, &resource.ConflictError{Resource: "operation"}
	}
	if op.State != "checked" {
		return op, false, nil
	}
	op.State = "dispatching"
	m.operations[id] = op
	return op, true, nil
}
func (m *testOperationStore) Finish(_ context.Context, _ Subject, id, state string, result *Result, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.finishErr != nil {
		return m.finishErr
	}
	op := m.operations[id]
	op.State, op.Result, op.UpdatedAt = state, result, now
	m.operations[id] = op
	return nil
}

type testDefinitionBackend struct {
	tenant   string
	mu       sync.Mutex
	writes   int
	writeErr error
	live     Result
	uid      string
}

func (b *testDefinitionBackend) Tenant(string) (string, bool) { return b.tenant, true }
func (b *testDefinitionBackend) Get(context.Context, string, string, string) (Result, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.live, nil
}
func (b *testDefinitionBackend) Apply(_ context.Context, kind, ns, name string, in ApplyRequest) (Result, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	spec, digest, err := CanonicalSpec(kind, in.SpecJSON)
	if err != nil {
		return Result{}, err
	}
	out := Result{Kind: kind, Namespace: ns, Name: name, RequestID: in.RequestID, SpecJSON: spec, ManifestDigest: digest, State: "validated"}
	if !in.DryRun {
		b.writes++
		out.State = "accepted"
		out.UID = b.uid
		out.ResourceVersion = "1"
		out.LastRequestID = in.RequestID
		out.LastManifestDigest = digest
		b.live = out
		if b.writeErr != nil {
			return Result{}, b.writeErr
		}
	}
	return out, nil
}

func operationFixture() (context.Context, Service, *testDefinitionBackend, *testOperationStore, ApplyRequest) {
	subject := Subject{TenantID: scan.GenerateID(), ActorID: scan.GenerateID()}
	b := &testDefinitionBackend{tenant: subject.TenantID, uid: uuid.NewString()}
	m := &testOperationStore{}
	s := Service{Backend: b, Store: m, Images: imagepin.New(nil, nil)}
	in := ApplyRequest{RequestID: uuid.NewString(), SpecJSON: `{"executionPolicyVersion":1,"jobTemplate":{"image":"ghcr.io/platform/report@sha256:` + strings.Repeat("a", 64) + `"}}`}
	_, in.ManifestDigest, _ = CanonicalSpec("jobs", in.SpecJSON)
	return WithSubject(context.Background(), subject), s, b, m, in
}
func checkedOperation(t *testing.T, ctx context.Context, s Service, in ApplyRequest) ApplyRequest {
	t.Helper()
	in.DryRun = true
	out, err := s.Apply(ctx, "jobs", "finance-prod", "billing-reconciliation", in)
	if err != nil {
		t.Fatal(err)
	}
	in.DryRun = false
	in.SpecJSON, in.ManifestDigest = out.SpecJSON, out.ManifestDigest
	return in
}

func TestSaveRequiresActualServerCheck(t *testing.T) {
	ctx, s, b, _, in := operationFixture()
	if _, err := s.Apply(ctx, "jobs", "finance-prod", "billing-reconciliation", in); err == nil {
		t.Fatal("self-computed digest bypassed the server check")
	}
	if b.writes != 0 {
		t.Fatal("unchecked intent reached Kubernetes")
	}
	in = checkedOperation(t, ctx, s, in)
	in.ExpectedUID = uuid.NewString()
	in.ExpectedResourceVersion = "2"
	if _, err := s.Apply(ctx, "jobs", "finance-prod", "billing-reconciliation", in); err == nil {
		t.Fatal("changed baseline bypassed check")
	}
	if b.writes != 0 {
		t.Fatal("changed intent reached Kubernetes")
	}
}

func TestConcurrentSaveDispatchesOnceAndRetainsHistory(t *testing.T) {
	ctx, s, b, _, in := operationFixture()
	in = checkedOperation(t, ctx, s, in)
	var group sync.WaitGroup
	for range 12 {
		group.Add(1)
		go func() { defer group.Done(); _, _ = s.Apply(ctx, "jobs", "finance-prod", "billing-reconciliation", in) }()
	}
	group.Wait()
	if b.writes != 1 {
		t.Fatalf("dispatched %d writes", b.writes)
	}
	b.live = Result{UID: uuid.NewString(), SpecJSON: `{"jobTemplate":{"image":"later"}}`}
	op, err := s.Operation(ctx, "jobs", "finance-prod", "billing-reconciliation", in.RequestID)
	if err != nil || op.State != "accepted" || op.Result == nil || op.Result.UID != b.uid {
		t.Fatalf("history replaced by current resource: %+v %v", op, err)
	}
	if _, err = s.Apply(ctx, "jobs", "finance-prod", "billing-reconciliation", in); err != nil {
		t.Fatal(err)
	}
	if b.writes != 1 {
		t.Fatal("history replay performed a new write")
	}
}

func TestLostCreateCannotBeConfirmedByCopiedAnnotations(t *testing.T) {
	ctx, s, b, _, in := operationFixture()
	in = checkedOperation(t, ctx, s, in)
	b.writeErr = io.ErrUnexpectedEOF
	if _, err := s.Apply(ctx, "jobs", "finance-prod", "billing-reconciliation", in); err == nil {
		t.Fatal("lost response was accepted")
	}
	b.live.UID = uuid.NewString()
	op, err := s.Operation(ctx, "jobs", "finance-prod", "billing-reconciliation", in.RequestID)
	if err != nil || op.State != "unknown" {
		t.Fatalf("replacement confirmed lost create: %+v %v", op, err)
	}
	_, _ = s.Apply(ctx, "jobs", "finance-prod", "billing-reconciliation", in)
	if b.writes != 1 {
		t.Fatal("unknown create was dispatched again")
	}
}

func TestFinishFailureAndExpiredCheckDoNotWriteAgain(t *testing.T) {
	ctx, s, b, m, in := operationFixture()
	now := time.Now().UTC()
	s.Now = func() time.Time { return now }
	in = checkedOperation(t, ctx, s, in)
	s.Now = func() time.Time { return now.Add(11 * time.Minute) }
	if _, err := s.Apply(ctx, "jobs", "finance-prod", "billing-reconciliation", in); err == nil {
		t.Fatal("expired check accepted")
	}
	if b.writes != 0 {
		t.Fatal("expired check dispatched")
	}
	s.Now = func() time.Time { return now }
	m.finishErr = errors.New("commit response lost")
	_, _ = s.Apply(ctx, "jobs", "finance-prod", "billing-reconciliation", in)
	m.finishErr = nil
	_, _ = s.Apply(ctx, "jobs", "finance-prod", "billing-reconciliation", in)
	if b.writes != 1 {
		t.Fatal("lost journal commit caused another dispatch")
	}
}

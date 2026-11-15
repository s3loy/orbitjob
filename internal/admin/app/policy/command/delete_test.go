package command

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/core/domain/policy"

	"orbitjob/internal/platform/scan"
)

// stubPolicyDeleter captures the event it was handed: the repository writes the
// audit row inside the same transaction as the delete.
type stubPolicyDeleter struct {
	gotTenantID string
	gotID       string
	gotEvent    audit.Event
	err         error
	calls       int
}

func (s *stubPolicyDeleter) Delete(_ context.Context, tenantID, id string, ev audit.Event) error {
	s.calls++
	s.gotTenantID = tenantID
	s.gotID = id
	s.gotEvent = ev
	return s.err
}

func TestDeletePassesTheTenantAndID(t *testing.T) {
	repo := &stubPolicyDeleter{}

	if err := NewDeleter(repo).Delete(context.Background(), DeleteInput{
		TenantID: policyDeleteFixtureTenantID,
		ID:       "policy-1",
		ActorID:  policyDeleteFixtureActorID,
	}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if repo.gotTenantID != policyDeleteFixtureTenantID || repo.gotID != "policy-1" {
		t.Fatalf("unexpected arguments: %s / %s", repo.gotTenantID, repo.gotID)
	}
	e := repo.gotEvent
	if e.EventType != audit.EventDelete || e.ResourceID != "policy-1" || e.ActorID != policyDeleteFixtureActorID {
		t.Fatalf("unexpected audit event: %+v", e)
	}
	if e.TenantID != policyDeleteFixtureTenantID {
		t.Fatalf("audit event is not scoped to the tenant: %+v", e)
	}
}

// A refused delete must not produce an audit row claiming it happened. The
// repository returns before recording, and the use case must not swallow that.
func TestDeletePropagatesThePlatformPresetRefusal(t *testing.T) {
	repo := &stubPolicyDeleter{err: policy.ErrPlatformPolicyImmutable}

	err := NewDeleter(repo).Delete(context.Background(), DeleteInput{
		TenantID: policyDeleteFixtureTenantID,
		ID:       "preset-1",
	})
	if !errors.Is(err, policy.ErrPlatformPolicyImmutable) {
		t.Fatalf("expected the platform-preset refusal to pass through, got %v", err)
	}
}

func TestDeletePropagatesAWriteFailure(t *testing.T) {
	repo := &stubPolicyDeleter{err: errors.New("transaction aborted")}
	if err := NewDeleter(repo).Delete(context.Background(), DeleteInput{
		TenantID: policyDeleteFixtureTenantID,
		ID:       "policy-1",
	}); err == nil {
		t.Fatal("expected the write failure to propagate")
	}
}

var policyDeleteFixtureActorID = scan.GenerateID()

var policyDeleteFixtureTenantID = scan.GenerateID()

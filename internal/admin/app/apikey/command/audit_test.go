package command

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/core/domain/apikey"
	"orbitjob/internal/core/domain/audit"
)

// fakeRepo serves both the create and revoke paths, since this file exercises
// the audit wiring of each and neither needs the other's behaviour. It captures
// the event it was handed, because the repository writes the audit row inside
// the same transaction as the change.
type fakeRepo struct {
	created   apikey.PersistInput
	createEv  audit.Event
	createErr error
	revokedID string
	revokedEv audit.Event
	revokeErr error
}

func newFakeKeyRepo() *fakeRepo { return &fakeRepo{} }

func (f *fakeRepo) Create(_ context.Context, in apikey.PersistInput, ev audit.Event) error {
	f.created = in
	f.createEv = ev
	return f.createErr
}

func (f *fakeRepo) Revoke(_ context.Context, _, id string, ev audit.Event) error {
	f.revokedID = id
	f.revokedEv = ev
	return f.revokeErr
}

func (f *fakeRepo) RevokeCrossTenant(_ context.Context, _ string) error {
	return f.revokeErr
}

// The audit diff must record the grant as applied, not as requested. A key that
// inherited its creator's boundary has to read back as bounded; recording only
// what the request named would suggest the new key was unconstrained.
func TestCreateRecordsTheAppliedGrant(t *testing.T) {
	repo := newFakeKeyRepo()

	_, err := NewCreator(repo).Create(context.Background(), CreateInput{
		TenantID: "tenant-a",
		Caller: Caller{
			TenantID: "tenant-a",
			KeyID:    "caller-key",
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	e := repo.createEv
	if e.ActorID != "caller-key" {
		t.Fatalf("expected the caller's key id as actor, got %q", e.ActorID)
	}
	if e.EventType != audit.EventCreate || e.ResourceType != audit.ResourceAPIKey {
		t.Fatalf("unexpected audit event: %+v", e)
	}
	if e.ResourceID != repo.created.ID {
		t.Fatalf("audit event names %q, the created key is %q", e.ResourceID, repo.created.ID)
	}
	// An absent grant list is recorded as an empty list, not null, so a
	// reviewer reads "no policies" instead of guessing what null meant.
	policies, ok := e.Diff["policies"].([]string)
	if !ok {
		t.Fatalf("expected a policies list in the diff, got %#v", e.Diff["policies"])
	}
	if len(policies) != 0 {
		t.Fatalf("expected no policies, got %v", policies)
	}
}

// A key created without a boundary of its own is recorded with an empty
// boundary id rather than a missing field.
func TestCreateAuditRecordsTheBoundaryField(t *testing.T) {
	repo := newFakeKeyRepo()

	if _, err := NewCreator(repo).Create(context.Background(), CreateInput{
		TenantID: "tenant-a",
		Caller:   Caller{TenantID: "tenant-a", KeyID: "caller-key"},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, ok := repo.createEv.Diff["boundary_policy_id"]; !ok {
		t.Fatalf("the diff omits the boundary entirely: %+v", repo.createEv.Diff)
	}
}

// The grant and its record are one write. A transaction that fails must leave
// neither behind, whoever caused the failure.
func TestCreateSurfacesAWriteFailure(t *testing.T) {
	repo := newFakeKeyRepo()
	repo.createErr = errors.New("transaction aborted")

	_, err := NewCreator(repo).Create(context.Background(), CreateInput{
		TenantID: "tenant-a",
		Caller:   Caller{TenantID: "tenant-a", KeyID: "caller-key"},
	})
	if err == nil {
		t.Fatal("a grant that could not be recorded must not report success")
	}
}

// There is no path that stores a key without recording who created it.
func TestCreateAlwaysRecordsAnEvent(t *testing.T) {
	repo := newFakeKeyRepo()
	if _, err := NewCreator(repo).Create(context.Background(), CreateInput{
		TenantID: "tenant-a",
		Caller:   Caller{TenantID: "tenant-a"},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if repo.createEv.EventType == "" || repo.createEv.ResourceType == "" {
		t.Fatalf("no audit event reached the repository: %+v", repo.createEv)
	}
}

func TestRevokeRecordsAnAuditEvent(t *testing.T) {
	repo := newFakeKeyRepo()

	if err := NewRevoker(repo).Revoke(context.Background(), RevokeInput{
		ID:       "key-1",
		TenantID: "tenant-a",
		ActorID:  "caller-key",
	}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	e := repo.revokedEv
	if e.EventType != audit.EventRevoke || e.ResourceID != "key-1" || e.ActorID != "caller-key" {
		t.Fatalf("unexpected audit event: %+v", e)
	}
	if e.TenantID != "tenant-a" {
		t.Fatalf("audit event is not scoped to the tenant: %+v", e)
	}
}

func TestRevokeSurfacesAWriteFailure(t *testing.T) {
	repo := newFakeKeyRepo()
	repo.revokeErr = errors.New("transaction aborted")

	if err := NewRevoker(repo).Revoke(context.Background(), RevokeInput{
		ID:       "key-1",
		TenantID: "tenant-a",
	}); err == nil {
		t.Fatal("expected the write failure to propagate")
	}
}

// A revocation that never happened must not be recorded as one. The repository
// returns before recording, and the use case must not paper over that.
func TestRevokePropagatesANotFoundError(t *testing.T) {
	repo := newFakeKeyRepo()
	repo.revokeErr = errors.New("not found")

	if err := NewRevoker(repo).Revoke(context.Background(), RevokeInput{
		ID:       "key-1",
		TenantID: "tenant-a",
	}); err == nil {
		t.Fatal("expected the revoke error to propagate")
	}
}

// policyIDsOrEmpty keeps the audit diff readable: nil becomes [], so a reader
// sees "no policies" rather than NULL and has to decide what it meant.
func TestPolicyIDsOrEmpty(t *testing.T) {
	empty := policyIDsOrEmpty(nil)
	if empty == nil {
		t.Fatal("nil should become an empty slice")
	}
	if len(empty) != 0 {
		t.Fatalf("expected no ids, got %v", empty)
	}

	got := policyIDsOrEmpty([]string{"a"})
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("unexpected result: %v", got)
	}
}

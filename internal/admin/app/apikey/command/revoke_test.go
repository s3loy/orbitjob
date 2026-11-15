package command

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/core/domain/apikey"
	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/platform/scan"
)

type stubAPIKeyRevoker struct {
	called      bool
	tenantID    string
	id          string
	err         error
	scopeCalled bool
	scopeID     string
	scopeErr    error
}

func (s *stubAPIKeyRevoker) Revoke(ctx context.Context, tenantID, id string, _ audit.Event) error {
	s.called = true
	s.tenantID = tenantID
	s.id = id
	return s.err
}

func (s *stubAPIKeyRevoker) Scope(ctx context.Context, id string) (apikey.APIKey, error) {
	s.scopeCalled = true
	s.scopeID = id
	return apikey.APIKey{ID: id, TenantID: s.tenantID}, s.scopeErr
}

func TestRevoker_Revoke_Success(t *testing.T) {
	repo := &stubAPIKeyRevoker{}
	uc := NewRevoker(repo)

	if err := uc.Revoke(context.Background(), RevokeInput{ID: revokeFixtureKeyID, TenantID: revokeFixtureTenantID}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.called {
		t.Fatal("expected repo.Revoke to be called")
	}
	if repo.id != revokeFixtureKeyID {
		t.Fatalf("expected id=%q, got %q", revokeFixtureKeyID, repo.id)
	}
	if repo.tenantID != revokeFixtureTenantID {
		t.Fatalf("expected tenantID=%q, got %q", revokeFixtureTenantID, repo.tenantID)
	}
}

func TestRevoker_Revoke_NotFound(t *testing.T) {
	repo := &stubAPIKeyRevoker{err: &resource.NotFoundError{Resource: "api_key", ID: revokeFixtureKeyID}}
	uc := NewRevoker(repo)

	err := uc.Revoke(context.Background(), RevokeInput{ID: revokeFixtureKeyID, TenantID: revokeFixtureTenantID})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsNotFound(err) {
		t.Fatalf("expected not-found error, got %T", err)
	}
}

func TestRevoker_Revoke_RepoError(t *testing.T) {
	repo := &stubAPIKeyRevoker{err: errors.New("db down")}
	uc := NewRevoker(repo)

	if err := uc.Revoke(context.Background(), RevokeInput{ID: revokeFixtureKeyID, TenantID: revokeFixtureTenantID}); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRevoker_Target_Success(t *testing.T) {
	repo := &stubAPIKeyRevoker{}
	uc := NewRevoker(repo)

	if _, err := uc.Target(context.Background(), revokeFixtureKeyID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.scopeCalled {
		t.Fatal("expected repo.Scope to be called")
	}
	if repo.scopeID != revokeFixtureKeyID {
		t.Fatalf("expected id=%q, got %q", revokeFixtureKeyID, repo.scopeID)
	}
}

func TestRevoker_Target_Error(t *testing.T) {
	repo := &stubAPIKeyRevoker{scopeErr: &resource.NotFoundError{Resource: "api_key", ID: revokeFixtureKeyID}}
	uc := NewRevoker(repo)

	_, err := uc.Target(context.Background(), revokeFixtureKeyID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsNotFound(err) {
		t.Fatalf("expected not-found error, got %T", err)
	}
}

func TestIsNotFound_OtherResource(t *testing.T) {
	err := &resource.NotFoundError{Resource: "tenant", ID: revokeFixtureKeyID}
	if IsNotFound(err) {
		t.Fatal("expected IsNotFound=false for tenant")
	}
}

var revokeFixtureKeyID = scan.GenerateID()

var revokeFixtureTenantID = scan.GenerateID()

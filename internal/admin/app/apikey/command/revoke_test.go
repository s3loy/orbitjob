package command

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/domain/resource"
)

type stubAPIKeyRevoker struct {
	called            bool
	tenantID          string
	id                string
	err               error
	crossTenantCalled bool
	crossTenantID     string
	crossTenantErr    error
}

func (s *stubAPIKeyRevoker) Revoke(ctx context.Context, tenantID, id string) error {
	s.called = true
	s.tenantID = tenantID
	s.id = id
	return s.err
}

func (s *stubAPIKeyRevoker) RevokeCrossTenant(ctx context.Context, id string) error {
	s.crossTenantCalled = true
	s.crossTenantID = id
	return s.crossTenantErr
}

func TestRevoker_Revoke_Success(t *testing.T) {
	repo := &stubAPIKeyRevoker{}
	uc := NewRevoker(repo)

	if err := uc.Revoke(context.Background(), RevokeInput{ID: "01HZX", TenantID: "tenant1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.called {
		t.Fatal("expected repo.Revoke to be called")
	}
	if repo.id != "01HZX" {
		t.Fatalf("expected id=%q, got %q", "01HZX", repo.id)
	}
	if repo.tenantID != "tenant1" {
		t.Fatalf("expected tenantID=%q, got %q", "tenant1", repo.tenantID)
	}
}

func TestRevoker_Revoke_NotFound(t *testing.T) {
	repo := &stubAPIKeyRevoker{err: &resource.NotFoundError{Resource: "api_key", ID: "01HZX"}}
	uc := NewRevoker(repo)

	err := uc.Revoke(context.Background(), RevokeInput{ID: "01HZX", TenantID: "tenant1"})
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

	if err := uc.Revoke(context.Background(), RevokeInput{ID: "01HZX", TenantID: "tenant1"}); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRevoker_RevokeAsAdmin_Success(t *testing.T) {
	repo := &stubAPIKeyRevoker{}
	uc := NewRevoker(repo)

	if err := uc.RevokeAsAdmin(context.Background(), "01HZX"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.crossTenantCalled {
		t.Fatal("expected repo.RevokeCrossTenant to be called")
	}
	if repo.crossTenantID != "01HZX" {
		t.Fatalf("expected id=%q, got %q", "01HZX", repo.crossTenantID)
	}
}

func TestRevoker_RevokeAsAdmin_Error(t *testing.T) {
	repo := &stubAPIKeyRevoker{crossTenantErr: &resource.NotFoundError{Resource: "api_key", ID: "01HZX"}}
	uc := NewRevoker(repo)

	err := uc.RevokeAsAdmin(context.Background(), "01HZX")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsNotFound(err) {
		t.Fatalf("expected not-found error, got %T", err)
	}
}

func TestIsNotFound_OtherResource(t *testing.T) {
	err := &resource.NotFoundError{Resource: "tenant", ID: "01HZX"}
	if IsNotFound(err) {
		t.Fatal("expected IsNotFound=false for tenant")
	}
}

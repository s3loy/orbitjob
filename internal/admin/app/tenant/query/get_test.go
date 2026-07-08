package query

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/core/domain/tenant"
)

type stubTenantGetReader struct {
	called   bool
	tenantID string
	id       string
	result   tenant.Tenant
	err      error
}

func (s *stubTenantGetReader) Get(ctx context.Context, tenantID, id string) (tenant.Tenant, error) {
	s.called = true
	s.tenantID = tenantID
	s.id = id
	return s.result, s.err
}

func TestGetter_Get_Success(t *testing.T) {
	repo := &stubTenantGetReader{result: tenant.Tenant{ID: "t1", Slug: "acme"}}
	uc := NewGetter(repo)

	out, err := uc.Get(context.Background(), GetInput{TenantID: "tenant1", ID: "t1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.called {
		t.Fatal("expected repo.Get to be called")
	}
	if repo.tenantID != "tenant1" {
		t.Fatalf("expected tenantID=%q, got %q", "tenant1", repo.tenantID)
	}
	if repo.id != "t1" {
		t.Fatalf("expected id=%q, got %q", "t1", repo.id)
	}
	if out.ID != "t1" {
		t.Fatalf("expected result id=%q, got %q", "t1", out.ID)
	}
}

func TestGetter_Get_RepoError(t *testing.T) {
	repo := &stubTenantGetReader{err: errors.New("db down")}
	uc := NewGetter(repo)

	_, err := uc.Get(context.Background(), GetInput{TenantID: "tenant1", ID: "t1"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

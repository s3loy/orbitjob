package query

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/core/domain/tenant"

	"orbitjob/internal/platform/scan"
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
	repo := &stubTenantGetReader{result: tenant.Tenant{ID: tenantGetFixtureSubjectID, Slug: "acme"}}
	uc := NewGetter(repo)

	out, err := uc.Get(context.Background(), GetInput{TenantID: tenantGetFixtureCallerID, ID: tenantGetFixtureSubjectID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.called {
		t.Fatal("expected repo.Get to be called")
	}
	if repo.tenantID != tenantGetFixtureCallerID {
		t.Fatalf("expected tenantID=%q, got %q", tenantGetFixtureCallerID, repo.tenantID)
	}
	if repo.id != tenantGetFixtureSubjectID {
		t.Fatalf("expected id=%q, got %q", tenantGetFixtureSubjectID, repo.id)
	}
	if out.ID != tenantGetFixtureSubjectID {
		t.Fatalf("expected result id=%q, got %q", tenantGetFixtureSubjectID, out.ID)
	}
}

func TestGetter_Get_RepoError(t *testing.T) {
	repo := &stubTenantGetReader{err: errors.New("db down")}
	uc := NewGetter(repo)

	_, err := uc.Get(context.Background(), GetInput{TenantID: tenantGetFixtureCallerID, ID: tenantGetFixtureSubjectID})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

var tenantGetFixtureSubjectID = scan.GenerateID()

var tenantGetFixtureCallerID = scan.GenerateID()

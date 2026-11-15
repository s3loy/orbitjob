package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/domain/apikey"
	"orbitjob/internal/platform/scan"
)

type stubAPIKeyLister struct {
	called   bool
	tenantID string
	out      []apikey.APIKey
	err      error
}

func (s *stubAPIKeyLister) ListByTenant(ctx context.Context, tenantID string) ([]apikey.APIKey, error) {
	s.called = true
	s.tenantID = tenantID
	return s.out, s.err
}

func TestLister_List_Success(t *testing.T) {
	createdAt := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	repo := &stubAPIKeyLister{
		out: []apikey.APIKey{
			{ID: listFixtureKeyID, Name: "Daily operator", KeyPrefix: "otj_abc123", CreatedAt: createdAt},
		},
	}
	uc := NewLister(repo)

	out, err := uc.List(context.Background(), ListInput{TenantID: listFixtureTenantID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.called {
		t.Fatal("expected repo.ListByTenant to be called")
	}
	if repo.tenantID != listFixtureTenantID {
		t.Fatalf("expected tenantID=%q, got %q", listFixtureTenantID, repo.tenantID)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out))
	}
	if out[0].ID != listFixtureKeyID {
		t.Fatalf("expected id=%q, got %q", listFixtureKeyID, out[0].ID)
	}
	if out[0].KeyPrefix != "otj_abc123" {
		t.Fatalf("expected prefix=%q, got %q", "otj_abc123", out[0].KeyPrefix)
	}
	if out[0].Name != "Daily operator" {
		t.Fatalf("name lost: %+v", out[0])
	}
	if !out[0].CreatedAt.Equal(createdAt) {
		t.Fatalf("expected createdAt=%v, got %v", createdAt, out[0].CreatedAt)
	}
}

func TestLister_List_Empty(t *testing.T) {
	repo := &stubAPIKeyLister{out: []apikey.APIKey{}}
	uc := NewLister(repo)

	out, err := uc.List(context.Background(), ListInput{TenantID: listFixtureTenantID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected 0 items, got %d", len(out))
	}
}

func TestLister_List_RepoError(t *testing.T) {
	repo := &stubAPIKeyLister{err: errors.New("db down")}
	uc := NewLister(repo)

	_, err := uc.List(context.Background(), ListInput{TenantID: listFixtureTenantID})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

var listFixtureKeyID = scan.GenerateID()

var listFixtureTenantID = scan.GenerateID()

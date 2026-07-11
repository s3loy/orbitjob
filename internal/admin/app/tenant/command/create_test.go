package command

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/core/domain/tenant"
)

type stubTenantCreator struct {
	called bool
	in     *tenant.Tenant
	err    error
}

func (s *stubTenantCreator) Create(ctx context.Context, t *tenant.Tenant) error {
	s.called = true
	s.in = t
	return s.err
}

func TestCreator_Create_Success(t *testing.T) {
	repo := &stubTenantCreator{}
	uc := NewCreator(repo)

	out, err := uc.Create(context.Background(), CreateInput{
		Slug: "acme",
		Name: "Acme Corp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.called {
		t.Fatal("expected repo.Create to be called")
	}
	if repo.in.Slug != "acme" {
		t.Fatalf("expected slug=%q, got %q", "acme", repo.in.Slug)
	}
	if repo.in.Name != "Acme Corp" {
		t.Fatalf("expected name=%q, got %q", "Acme Corp", repo.in.Name)
	}
	if repo.in.Status != tenant.StatusActive {
		t.Fatalf("expected status=%q, got %q", tenant.StatusActive, repo.in.Status)
	}
	if repo.in.ID == "" {
		t.Fatal("expected generated id")
	}
	if out.ID != repo.in.ID {
		t.Fatalf("expected result id=%q, got %q", repo.in.ID, out.ID)
	}
	if out.Status != tenant.StatusActive {
		t.Fatalf("expected result status=%q, got %q", tenant.StatusActive, out.Status)
	}
}

func TestCreator_Create_WithSuspendedStatus(t *testing.T) {
	repo := &stubTenantCreator{}
	uc := NewCreator(repo)

	_, err := uc.Create(context.Background(), CreateInput{
		Slug:   "acme",
		Name:   "Acme Corp",
		Status: tenant.StatusSuspended,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.in.Status != tenant.StatusSuspended {
		t.Fatalf("expected status=%q, got %q", tenant.StatusSuspended, repo.in.Status)
	}
}

func TestCreator_Create_ValidationError(t *testing.T) {
	uc := NewCreator(&stubTenantCreator{})

	cases := []struct {
		name string
		in   CreateInput
	}{
		{"empty slug", CreateInput{Slug: "", Name: "Acme"}},
		{"empty name", CreateInput{Slug: "acme", Name: ""}},
		{"invalid status", CreateInput{Slug: "acme", Name: "Acme", Status: "deleted"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := uc.Create(context.Background(), tc.in)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestCreator_Create_RepoError(t *testing.T) {
	repo := &stubTenantCreator{err: errors.New("db down")}
	uc := NewCreator(repo)

	_, err := uc.Create(context.Background(), CreateInput{
		Slug: "acme",
		Name: "Acme Corp",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/domain/sli"
)

type stubSLIWriter struct {
	in  sli.CreateSpec
	out sli.Snapshot
	err error
}

func (s *stubSLIWriter) Create(_ context.Context, _ string, spec sli.CreateSpec) (sli.Snapshot, error) {
	s.in = spec
	return s.out, s.err
}

type stubSLIDeleter struct {
	lastTenantID string
	lastID       int64
	lastVersion  int
	err          error
}

func (s *stubSLIDeleter) Delete(_ context.Context, tenantID string, id int64, version int) error {
	s.lastTenantID = tenantID
	s.lastID = id
	s.lastVersion = version
	return s.err
}

func validSLIInput() CreateInput {
	return CreateInput{
		Name:     "sli-1",
		TenantID: "t1",
		SLIType:  sli.TypeAvailability, SourceType: sli.SourceTypeCheckRun,
		SourceConfig:      map[string]any{"check_id": 1},
		Aggregation:       sli.AggregationRatio,
		GoodEventCriteria: map[string]any{"field": "status", "op": "eq", "value": "success"},
	}
}

func TestCreateSLI_Success(t *testing.T) {
	repo := &stubSLIWriter{out: sli.Snapshot{
		ID: 1, Name: "sli-1", SLIType: sli.TypeAvailability, Version: 1, CreatedAt: time.Now(),
	}}
	uc := NewCreateSLIUseCase(repo)

	result, err := uc.Create(context.Background(), validSLIInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != 1 || result.Name != "sli-1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if repo.in.Name != "sli-1" {
		t.Fatalf("expected repo input name=sli-1, got %+v", repo.in)
	}
}

func TestCreateSLI_NormalizeError(t *testing.T) {
	repo := &stubSLIWriter{}
	uc := NewCreateSLIUseCase(repo)

	in := validSLIInput()
	in.Name = ""
	_, err := uc.Create(context.Background(), in)
	if err == nil {
		t.Fatal("expected validation error for empty name")
	}
	if repo.in.Name != "" {
		t.Fatal("expected repo.Create not called on normalize error")
	}
}

func TestCreateSLI_RepoError(t *testing.T) {
	repo := &stubSLIWriter{err: errors.New("write failed")}
	uc := NewCreateSLIUseCase(repo)

	_, err := uc.Create(context.Background(), validSLIInput())
	if err == nil {
		t.Fatal("expected repo error")
	}
}

func TestDeleteSLI_Success(t *testing.T) {
	repo := &stubSLIDeleter{}
	uc := NewDeleteSLIUseCase(repo)

	if err := uc.Delete(context.Background(), DeleteInput{TenantID: "t1", ID: 5, Version: 2}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastTenantID != "t1" || repo.lastID != 5 || repo.lastVersion != 2 {
		t.Fatalf("expected delete t1/5/2, got %s/%d/%d", repo.lastTenantID, repo.lastID, repo.lastVersion)
	}
}

func TestDeleteSLI_RepoError(t *testing.T) {
	repo := &stubSLIDeleter{err: errors.New("not found")}
	uc := NewDeleteSLIUseCase(repo)

	if err := uc.Delete(context.Background(), DeleteInput{TenantID: "t1", ID: 5, Version: 2}); err == nil {
		t.Fatal("expected error")
	}
}

func TestDeleteSLI_InvalidID(t *testing.T) {
	repo := &stubSLIDeleter{}
	uc := NewDeleteSLIUseCase(repo)

	if err := uc.Delete(context.Background(), DeleteInput{TenantID: "t1", ID: 0, Version: 2}); err == nil {
		t.Fatal("expected validation error for id<=0")
	}
	if repo.lastID != 0 {
		t.Fatal("expected repo.Delete not called for invalid id")
	}
}

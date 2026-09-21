package query

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/core/domain/policy"
)

type stubPolicyGetter struct {
	rec         policy.Record
	err         error
	gotTenantID string
	gotID       string
}

func (s *stubPolicyGetter) Get(_ context.Context, tenantID, id string) (policy.Record, error) {
	s.gotTenantID = tenantID
	s.gotID = id
	return s.rec, s.err
}

func TestGetReturnsTheRequestedPolicy(t *testing.T) {
	repo := &stubPolicyGetter{rec: policy.Record{ID: "p1", Name: "JobOperator", TenantID: "t1"}}

	out, err := NewGetter(repo).Get(context.Background(), GetInput{TenantID: "t1", ID: "p1"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if repo.gotTenantID != "t1" || repo.gotID != "p1" {
		t.Fatalf("unexpected arguments: %s / %s", repo.gotTenantID, repo.gotID)
	}
	if out.Name != "JobOperator" || out.Platform {
		t.Fatalf("unexpected result: %+v", out)
	}
}

func TestGetMarksAPlatformPreset(t *testing.T) {
	repo := &stubPolicyGetter{rec: policy.Record{ID: "preset", Name: "ReadOnlyAccess"}}

	out, err := NewGetter(repo).Get(context.Background(), GetInput{TenantID: "t1", ID: "preset"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !out.Platform {
		t.Fatal("a preset was not marked as platform-owned")
	}
}

// A policy belonging to another tenant is reported as missing rather than as
// forbidden: distinguishing the two would confirm that the id exists.
func TestGetPropagatesNotFound(t *testing.T) {
	repo := &stubPolicyGetter{err: errors.New("not found")}
	if _, err := NewGetter(repo).Get(context.Background(), GetInput{TenantID: "t1", ID: "other"}); err == nil {
		t.Fatal("expected the not-found error to propagate")
	}
}

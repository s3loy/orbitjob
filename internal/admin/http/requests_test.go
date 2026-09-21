package http

import (
	"testing"
)

func TestListJobsRequest_ToListInput(t *testing.T) {
	req := ListJobsRequest{
		TenantID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Limit:    20,
		Offset:   40,
	}

	got := req.ToListInput()

	if got.TenantID != req.TenantID {
		t.Fatalf("expected tenant_id=%q, got %q", req.TenantID, got.TenantID)
	}
	if got.Limit != req.Limit {
		t.Fatalf("expected limit=%d, got %d", req.Limit, got.Limit)
	}
	if got.Offset != req.Offset {
		t.Fatalf("expected offset=%d, got %d", req.Offset, got.Offset)
	}
}

func TestGetJobRequest_ToGetInput(t *testing.T) {
	req := GetJobRequest{
		ID:       42,
		TenantID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
	}

	got := req.ToGetInput()

	if got.ID != req.ID {
		t.Fatalf("expected id=%d, got %d", req.ID, got.ID)
	}
	if got.TenantID != req.TenantID {
		t.Fatalf("expected tenant_id=%q, got %q", req.TenantID, got.TenantID)
	}
}

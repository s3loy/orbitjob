package http

import (
	"testing"

	command "orbitjob/internal/admin/app/job/command"
)

func TestChangeStatusRequest_ToChangeStatusInput(t *testing.T) {
	req := ChangeStatusRequest{
		ID:       42,
		TenantID: "tenant-a",
		Version:  7,
	}

	got := req.ToChangeStatusInput("control-plane-user")

	want := command.ChangeStatusInput{
		ID:        42,
		TenantID:  "tenant-a",
		Version:   7,
		ChangedBy: "control-plane-user",
	}
	if got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
}

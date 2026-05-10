package job

import "testing"

func TestNewQuotaExceededError(t *testing.T) {
	err := NewQuotaExceededError("jobs_per_tenant", 10)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if err.Quota != "jobs_per_tenant" {
		t.Fatalf("expected Quota=%q, got %q", "jobs_per_tenant", err.Quota)
	}
	if err.Limit != 10 {
		t.Fatalf("expected Limit=10, got %d", err.Limit)
	}
}

func TestQuotaExceededError_Error(t *testing.T) {
	err := NewQuotaExceededError("workers_per_tenant", 5)
	want := "quota exceeded: workers_per_tenant (limit=5)"
	if got := err.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

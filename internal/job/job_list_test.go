package job

import (
	"strings"
	"testing"
)

func TestNormalizeListJobsQuery_Defaults(t *testing.T) {
	out, err := NormalizeListJobsQuery(ListJobsQuery{})
	if err != nil {
		t.Fatalf("NormalizeListJobsQuery() error = %v", err)
	}

	if out.TenantID != DefaultTenantID {
		t.Fatalf("expected tenant_id=%q, got %q", DefaultTenantID, out.TenantID)
	}
	if out.Status != "" {
		t.Fatalf("expected empty status, got %q", out.Status)
	}
	if out.Limit != DefaultListJobsLimit {
		t.Fatalf("expected limit=%d, got %d", DefaultListJobsLimit, out.Limit)
	}
	if out.Offset != 0 {
		t.Fatalf("expected offset=0, got %d", out.Offset)
	}
}

func TestNormalizeListJobsQuery_TrimsTenantID(t *testing.T) {
	out, err := NormalizeListJobsQuery(ListJobsQuery{
		TenantID: " tenant-a ",
		Status:   JobStatusActive,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("NormalizeListJobsQuery() error = %v", err)
	}

	if out.TenantID != "tenant-a" {
		t.Fatalf("expected tenant_id=%q, got %q", "tenant-a", out.TenantID)
	}
	if out.Status != JobStatusActive {
		t.Fatalf("expected status=%q, got %q", JobStatusActive, out.Status)
	}
	if out.Limit != 10 {
		t.Fatalf("expected limit=10, got %d", out.Limit)
	}
}

func TestNormalizeListJobsQuery_InvalidInput(t *testing.T) {
	tests := []struct {
		name        string
		input       ListJobsQuery
		wantField   string
		wantMessage string
	}{
		{
			name: "tenant too long",
			input: ListJobsQuery{
				TenantID: strings.Repeat("t", 65),
			},
			wantField:   "tenant_id",
			wantMessage: "must be <= 64 characters",
		},
		{
			name: "invalid status",
			input: ListJobsQuery{
				Status: "running",
			},
			wantField:   "status",
			wantMessage: "must be one of: active, paused",
		},
		{
			name: "limit less than one",
			input: ListJobsQuery{
				Limit: -1,
			},
			wantField:   "limit",
			wantMessage: "must be >= 1",
		},
		{
			name: "limit too large",
			input: ListJobsQuery{
				Limit: MaxListJobsLimit + 1,
			},
			wantField:   "limit",
			wantMessage: "must be <= 100",
		},
		{
			name: "offset less than zero",
			input: ListJobsQuery{
				Offset: -1,
			},
			wantField:   "offset",
			wantMessage: "must be >= 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeListJobsQuery(tt.input)
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}

			var validationErr *ValidationError
			if !IsValidationError(err) {
				t.Fatalf("expected validation error, got %T", err)
			}
			if !AsValidationError(err, &validationErr) {
				t.Fatalf("expected error to unwrap as ValidationError")
			}
			if validationErr.Field != tt.wantField {
				t.Fatalf("expected field=%q, got %q", tt.wantField, validationErr.Field)
			}
			if validationErr.Message != tt.wantMessage {
				t.Fatalf("expected message=%q, got %q", tt.wantMessage, validationErr.Message)
			}
		})
	}
}

func TestBuildJobScheduleSummary(t *testing.T) {
	cronExpr := "*/5 * * * *"

	if got := BuildJobScheduleSummary(TriggerTypeManual, nil, ""); got != "manual" {
		t.Fatalf("expected manual summary, got %q", got)
	}

	if got := BuildJobScheduleSummary(TriggerTypeCron, &cronExpr, "Asia/Shanghai"); got != "cron: */5 * * * * (Asia/Shanghai)" {
		t.Fatalf("unexpected cron summary: %q", got)
	}

	if got := BuildJobScheduleSummary(TriggerTypeCron, nil, ""); got != "cron (UTC)" {
		t.Fatalf("unexpected empty cron summary: %q", got)
	}
}

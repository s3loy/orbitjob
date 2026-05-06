package query

import (
	"strings"
	"testing"

	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/domain/validation"
)

func TestNormalizeListInput_Defaults(t *testing.T) {
	out, err := NormalizeListInput(ListInput{})
	if err != nil {
		t.Fatalf("NormalizeListInput() error = %v", err)
	}

	if out.TenantID != defaultTenantID {
		t.Fatalf("expected tenant_id=%q, got %q", defaultTenantID, out.TenantID)
	}
	if out.Status != "" {
		t.Fatalf("expected empty status, got %q", out.Status)
	}
	if out.Limit != DefaultListLimit {
		t.Fatalf("expected limit=%d, got %d", DefaultListLimit, out.Limit)
	}
	if out.Offset != 0 {
		t.Fatalf("expected offset=0, got %d", out.Offset)
	}
}

func TestNormalizeListInput_TrimsTenantID(t *testing.T) {
	out, err := NormalizeListInput(ListInput{
		TenantID: " tenant-a ",
		Status:   StatusActive,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("NormalizeListInput() error = %v", err)
	}

	if out.TenantID != "tenant-a" {
		t.Fatalf("expected tenant_id=%q, got %q", "tenant-a", out.TenantID)
	}
	if out.Status != StatusActive {
		t.Fatalf("expected status=%q, got %q", StatusActive, out.Status)
	}
	if out.Limit != 10 {
		t.Fatalf("expected limit=10, got %d", out.Limit)
	}
}

func TestNormalizeListInput_InvalidInput(t *testing.T) {
	tests := []struct {
		name        string
		input       ListInput
		wantField   string
		wantMessage string
	}{
		{
			name: "tenant too long",
			input: ListInput{
				TenantID: strings.Repeat("t", 65),
			},
			wantField:   "tenant_id",
			wantMessage: "must be <= 64 characters",
		},
		{
			name: "invalid status",
			input: ListInput{
				Status: "running",
			},
			wantField:   "status",
			wantMessage: "must be one of: active, paused",
		},
		{
			name: "limit less than one",
			input: ListInput{
				Limit: -1,
			},
			wantField:   "limit",
			wantMessage: "must be >= 1",
		},
		{
			name: "limit too large",
			input: ListInput{
				Limit: MaxListLimit + 1,
			},
			wantField:   "limit",
			wantMessage: "must be <= 100",
		},
		{
			name: "offset less than zero",
			input: ListInput{
				Offset: -1,
			},
			wantField:   "offset",
			wantMessage: "must be >= 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeListInput(tt.input)
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}

			if !validation.Is(err) {
				t.Fatalf("expected validation error, got %T", err)
			}

			var validationErr *validation.Error
			if !validation.As(err, &validationErr) {
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

func TestBuildScheduleSummary(t *testing.T) {
	cronExpr := "*/5 * * * *"

	if got := BuildScheduleSummary(domainjob.TriggerTypeManual, nil, ""); got != "manual" {
		t.Fatalf("expected manual summary, got %q", got)
	}

	if got := BuildScheduleSummary(domainjob.TriggerTypeCron, &cronExpr, "Asia/Shanghai"); got != "cron: */5 * * * * (Asia/Shanghai)" {
		t.Fatalf("unexpected cron summary: %q", got)
	}

	if got := BuildScheduleSummary(domainjob.TriggerTypeCron, nil, ""); got != "cron (UTC)" {
		t.Fatalf("unexpected empty cron summary: %q", got)
	}
}

package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	slocommand "orbitjob/internal/admin/app/slo/command"
	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/platform/scan"
)

type durationValidationSLOWriter struct {
	calls    int
	duration time.Duration
}

func (s *durationValidationSLOWriter) Create(_ context.Context, _ string, spec slo.CreateSpec) (slo.Snapshot, error) {
	s.calls++
	s.duration = spec.WindowDuration
	return slo.Snapshot{
		ID: 1, Name: spec.Name, SLIID: spec.SLIID, Target: spec.Target,
		WindowType: spec.WindowType, WindowDuration: spec.WindowDuration,
		Status: slo.StatusActive, Version: 1,
	}, nil
}

func TestCreateSLO_HTTPWindowDurationPrecision(t *testing.T) {
	tenantID := scan.GenerateID()
	for _, tc := range []struct {
		input string
		valid bool
	}{
		{input: "0s"},
		{input: "-1s"},
		{input: "1ns"},
		{input: "1ms"},
		{input: "999ms"},
		{input: "1.5s"},
		{input: "8760h1s"},
		{input: "1s", valid: true},
		{input: "1.0s", valid: true},
		{input: "1000ms", valid: true},
		{input: "2s", valid: true},
		{input: "24h", valid: true},
		{input: "8760h", valid: true},
	} {
		t.Run(tc.input, func(t *testing.T) {
			repo := &durationValidationSLOWriter{}
			h := NewHandler(nil, nil, nil)
			h.SetCreateSLOUseCase(slocommand.NewCreateSLOUseCase(repo))
			router := gin.New()
			router.Use(func(c *gin.Context) {
				ctx := middleware.WithTenantID(c.Request.Context(), tenantID, middleware.TenantSourceAPIKey)
				c.Request = c.Request.WithContext(ctx)
				middleware.SetPrincipal(c, middleware.Principal{TenantID: tenantID, Kind: middleware.KindTenant})
				c.Next()
			})
			router.POST("/api/v1/slos", h.CreateSLO)
			body := fmt.Sprintf(`{"name":"availability","sli_id":1,"target":0.99,"window_duration":%q}`, tc.input)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/slos", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if tc.valid {
				if response.Code != http.StatusCreated {
					t.Fatalf("valid duration rejected: %d %s", response.Code, response.Body.String())
				}
				duration, err := time.ParseDuration(tc.input)
				if err != nil {
					t.Fatal(err)
				}
				var result slocommand.CreateResult
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if repo.calls != 1 || repo.duration != duration || result.WindowDuration != duration.String() {
					t.Fatalf("duration changed: calls=%d stored=%v response=%q, want %v", repo.calls, repo.duration, result.WindowDuration, duration)
				}
				return
			}
			if response.Code != http.StatusBadRequest {
				t.Fatalf("invalid duration status=%d, want 400: %s", response.Code, response.Body.String())
			}
			var result apperror.ErrorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Error.Code != apperror.CodeValidation || result.Error.Field != "window_duration" {
				t.Fatalf("expected window_duration validation error, got %+v", result.Error)
			}
			if repo.calls != 0 {
				t.Fatalf("invalid duration reached repository %d times", repo.calls)
			}
		})
	}
}

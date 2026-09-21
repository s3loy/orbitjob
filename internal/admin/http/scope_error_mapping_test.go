package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	jobcommand "orbitjob/internal/admin/app/job/command"
	jobquery "orbitjob/internal/admin/app/job/query"
	runquery "orbitjob/internal/admin/app/run/query"
	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

// The five guarded gates refuse a scoped caller with a *resource.ScopeError,
// and internal/admin/http/errors.go maps that refusal to 403 FORBIDDEN. The
// contract tests in the app packages left that mapping to inspection; this
// file pins it. It also pins the precedence the gates follow: the scope is
// decided before the tenant id is judged, so a scoped caller sees the 403
// refusal even when the rest of its input is invalid, never the 400 a
// validation error would map to.

// notATenantID is the slug a deployment is most likely to confuse for a
// tenant. Tenant ids are tenants.id values, CHAR(26) ULIDs; slugs are never
// tenant identifiers, so the gates refuse it as a validation error -- but only
// after the scope has been decided.
const notATenantID = "default"

// scopedGroup is a resource group id, a different kind of identifier and never
// used as a tenant. Carrying it means the caller's key is scoped, and a run or
// a job definition has no group column to be limited by.
const scopedGroup = "rg-7"

func TestToAPIError_ScopeErrorMapsToForbidden(t *testing.T) {
	scopeErr := &resource.ScopeError{Resource: "run", Scope: scopedGroup}
	got := toAPIError(scopeErr)

	if got.Code != apperror.CodeForbidden {
		t.Fatalf("expected code=%q, got %q", apperror.CodeForbidden, got.Code)
	}
	if got.Message != scopeErr.Error() {
		t.Fatalf("expected message=%q, got %q", scopeErr.Error(), got.Message)
	}
	if got.Field != "run" {
		t.Fatalf("expected field=%q, got %q", "run", got.Field)
	}
	if status := apperror.StatusForCode(got.Code); status != http.StatusForbidden {
		t.Fatalf("expected status=%d, got %d", http.StatusForbidden, status)
	}
}

func TestToAPIError_ValidationMapsToBadRequest(t *testing.T) {
	// The contrast case: an input that merely fails validation maps to 400,
	// which is exactly the response a scoped caller must NOT receive when its
	// scope is already a refusal.
	got := toAPIError(validation.New("tenant_id", "must be a 26-character tenant id"))

	if got.Code != apperror.CodeValidation {
		t.Fatalf("expected code=%q, got %q", apperror.CodeValidation, got.Code)
	}
	if got.Field != "tenant_id" {
		t.Fatalf("expected field=%q, got %q", "tenant_id", got.Field)
	}
	if status := apperror.StatusForCode(got.Code); status != http.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", http.StatusBadRequest, status)
	}
}

func TestWriteAPIError_ScopeErrorWrites403(t *testing.T) {
	// Through the handler's own error path, the refusal reaches the wire as a
	// 403 response, not just as the intermediate APIError structure.
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	writeAPIError(c, &resource.ScopeError{Resource: "run", Scope: scopedGroup})

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status=%d, got %d", http.StatusForbidden, w.Code)
	}
}

func TestScopeRefusalPrecedesTenantValidation(t *testing.T) {
	// Every guarded gate refuses the scope before it judges the tenant id: a
	// scoped caller with an invalid tenant gets the 403-mapped refusal, not
	// the 400 a validation error maps to. An authorization denial must not
	// depend on the request being otherwise well-formed. The trigger enters
	// through the use case because its gate sits behind the exported method;
	// the refusal lands in normalizeTriggerInput before the use case touches
	// any collaborator, so the nil reader and publisher are never reached.
	tests := []struct {
		name         string
		gate         func() error
		wantResource string
	}{
		{
			name: "job definition list",
			gate: func() error {
				_, err := jobquery.NormalizeListInput(jobquery.ListInput{TenantID: notATenantID, ResourceGroupID: scopedGroup})
				return err
			},
			wantResource: "job definition",
		},
		{
			name: "run list",
			gate: func() error {
				_, err := runquery.NormalizeListInput(runquery.ListInput{TenantID: notATenantID, ResourceGroupID: scopedGroup})
				return err
			},
			wantResource: "run",
		},
		{
			name: "manual trigger",
			gate: func() error {
				_, err := jobcommand.NewTriggerJobUseCase(nil, nil).Trigger(context.Background(),
					jobcommand.TriggerInput{JobID: 7, TenantID: notATenantID, ActorID: "key-9", ResourceGroupID: scopedGroup})
				return err
			},
			wantResource: "job definition",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.gate()

			var verr *validation.Error
			if errors.As(err, &verr) {
				t.Fatalf("scoped caller refused on validation (field %q), want the scope refusal first", verr.Field)
			}

			var serr *resource.ScopeError
			if !errors.As(err, &serr) {
				t.Fatalf("got %v (%T), want *resource.ScopeError", err, err)
			}
			if serr.Resource != tc.wantResource || serr.Scope != scopedGroup {
				t.Fatalf("scope refusal = %+v, want resource %q scope %q", serr, tc.wantResource, scopedGroup)
			}

			apiErr := toAPIError(err)
			if apiErr.Code != apperror.CodeForbidden {
				t.Fatalf("mapped code %q, want %q", apiErr.Code, apperror.CodeForbidden)
			}
			if status := apperror.StatusForCode(apiErr.Code); status != http.StatusForbidden {
				t.Fatalf("mapped status %d, want %d", status, http.StatusForbidden)
			}
		})
	}
}

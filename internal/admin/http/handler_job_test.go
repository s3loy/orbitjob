package http

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	jobcommand "orbitjob/internal/admin/app/job/command"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/resource"
)

// This file restores the dedicated unit coverage the trigger handler lost when
// the deleted handler_coverage_test.go went with the write API. TriggerJob
// reads no JSON body -- its input is the path id, the idempotency header and
// the authenticated principal -- so the decoding cases here are URI and header
// decoding; the 400s a malformed JSON body produces are owned by the body-limit
// middleware and the binding layer, not by this handler.

type stubTriggerJobUseCase struct {
	called bool
	in     jobcommand.TriggerInput
	out    jobcommand.TriggerResult
	err    error
}

func (s *stubTriggerJobUseCase) Trigger(ctx context.Context, in jobcommand.TriggerInput) (jobcommand.TriggerResult, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

// testPrincipalMiddleware injects the principal a real authenticated key
// carries, including its id: the trigger handler records the key's id as the
// run's actor, so a stub principal without a key id could not prove who the
// actor was. The wildcard grant mirrors grantAllPolicies; the extra fields are
// the difference.
func testPrincipalMiddleware(tenantID, keyID, groupID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := middleware.WithTenantID(c.Request.Context(), tenantID, middleware.TenantSourceHeader)
		c.Request = c.Request.WithContext(ctx)
		middleware.SetPrincipal(c, middleware.Principal{
			TenantID:        tenantID,
			KeyID:           keyID,
			Kind:            middleware.KindTenant,
			ResourceGroupID: groupID,
		})
		middleware.SetDocuments(c, []policy.Document{{Statement: []policy.Statement{
			{
				Effect:   policy.EffectAllow,
				Action:   []string{policy.WildcardAction},
				Resource: []string{"orbitjob:*:*:*/*"},
			},
		}}})
		c.Next()
	}
}

// newTriggerHandler mounts the trigger route behind the stub principal, with
// the key scoped to groupID (empty for an unscoped key).
func newTriggerHandler(triggerUC *stubTriggerJobUseCase, groupID string) (*Handler, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(nil, nil, triggerUC)
	r := gin.New()
	r.Use(testPrincipalMiddleware("tenant-a", "key-123", groupID))
	h.Register(r)
	return h, r
}

func TestHandler_TriggerJob_DelegatesToAuthenticatedKey(t *testing.T) {
	triggerUC := &stubTriggerJobUseCase{
		out: jobcommand.TriggerResult{
			Namespace:     "orbitjob",
			Name:          "nightly-report-manual",
			OccurrenceKey: "abc123",
			Trigger:       "manual",
			Created:       true,
		},
	}
	_, router := newTriggerHandler(triggerUC, "")

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/7/trigger", nil)
	req.Header.Set("X-OrbitJob-Idempotency-Key", "idem-1")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusCreated {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusCreated, resp.Code, resp.Body.String())
	}
	if !triggerUC.called {
		t.Fatal("expected use case to be called")
	}
	if triggerUC.in.JobID != 7 {
		t.Errorf("job id = %d, want 7", triggerUC.in.JobID)
	}
	if triggerUC.in.TenantID != "tenant-a" {
		t.Errorf("tenant = %q, want the authenticated principal's tenant", triggerUC.in.TenantID)
	}
	if triggerUC.in.ActorID != "key-123" {
		t.Errorf("actor = %q, want the authenticated key's id", triggerUC.in.ActorID)
	}
	if triggerUC.in.IdempotencyKey != "idem-1" {
		t.Errorf("idempotency key = %q, want idem-1", triggerUC.in.IdempotencyKey)
	}
	if triggerUC.in.ResourceGroupID != "" {
		t.Errorf("resource group = %q, want the unscoped caller's empty scope", triggerUC.in.ResourceGroupID)
	}
}

func TestHandler_TriggerJob_IgnoresClientActorHeader(t *testing.T) {
	// X-Actor-ID used to be required by the mutating routes. The actor is now
	// always the authenticated key: a header the caller controls must never
	// decide who a run is attributed to.
	triggerUC := &stubTriggerJobUseCase{
		out: jobcommand.TriggerResult{Created: true},
	}
	_, router := newTriggerHandler(triggerUC, "")

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/7/trigger", nil)
	req.Header.Set("X-Actor-ID", "someone-else")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusCreated {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusCreated, resp.Code, resp.Body.String())
	}
	if triggerUC.in.ActorID != "key-123" {
		t.Fatalf("actor = %q, want the authenticated key id, not the client header", triggerUC.in.ActorID)
	}
}

func TestHandler_TriggerJob_ReplayedTriggerReturns200(t *testing.T) {
	// Created=false means the idempotency key resolved to an existing run, so
	// the replay answers 200 rather than 201.
	triggerUC := &stubTriggerJobUseCase{
		out: jobcommand.TriggerResult{Created: false},
	}
	_, router := newTriggerHandler(triggerUC, "")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/7/trigger", nil))

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
}

func TestHandler_TriggerJob_InvalidPathIDReturns400(t *testing.T) {
	triggerUC := &stubTriggerJobUseCase{}
	_, router := newTriggerHandler(triggerUC, "")

	for _, path := range []string{"/api/v1/jobs/notanint/trigger", "/api/v1/jobs/0/trigger"} {
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodPost, path, nil))

		if resp.Code != stdhttp.StatusBadRequest {
			t.Errorf("%s: expected status=%d, got %d, body=%s", path, stdhttp.StatusBadRequest, resp.Code, resp.Body.String())
		}
		if triggerUC.called {
			t.Errorf("%s: use case must not be called for an undecodable id", path)
		}
	}
}

func TestHandler_TriggerJob_IdempotencyKeyTooLongReturns400(t *testing.T) {
	triggerUC := &stubTriggerJobUseCase{}
	_, router := newTriggerHandler(triggerUC, "")

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/7/trigger", nil)
	req.Header.Set("X-OrbitJob-Idempotency-Key", strings.Repeat("a", 129))
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusBadRequest, resp.Code, resp.Body.String())
	}
	if triggerUC.called {
		t.Fatal("use case must not be called for an over-long idempotency key")
	}
}

func TestHandler_TriggerJob_StatusMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			// A scoped caller is refused: a definition has no group to be
			// narrowed by, so the refusal is 403, never an empty success.
			name: "scope error maps to 403",
			err:  &resource.ScopeError{Resource: "job definition", Scope: "rg-7"},
			want: stdhttp.StatusForbidden,
		},
		{
			name: "not found maps to 404",
			err:  &resource.NotFoundError{Resource: "job", ID: 99},
			want: stdhttp.StatusNotFound,
		},
		{
			// The use case refuses a suspended definition with a conflict,
			// which is what the 409 must carry.
			name: "conflict maps to 409",
			err:  &resource.ConflictError{Resource: "job", ID: 7, Field: "suspend", Message: "cannot trigger a suspended definition"},
			want: stdhttp.StatusConflict,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			triggerUC := &stubTriggerJobUseCase{err: tc.err}
			_, router := newTriggerHandler(triggerUC, "")

			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/7/trigger", nil))

			if resp.Code != tc.want {
				t.Fatalf("expected status=%d, got %d, body=%s", tc.want, resp.Code, resp.Body.String())
			}
		})
	}
}

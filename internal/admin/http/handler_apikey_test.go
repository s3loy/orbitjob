package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	apikeycommand "orbitjob/internal/admin/app/apikey/command"
	apikeyquery "orbitjob/internal/admin/app/apikey/query"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/domain/resource"
)

type stubCreateAPIKeyUseCase struct {
	called bool
	in     apikeycommand.CreateInput
	out    apikeycommand.APIKeyCreateResult
	err    error
}

func (s *stubCreateAPIKeyUseCase) Create(ctx context.Context, in apikeycommand.CreateInput) (apikeycommand.APIKeyCreateResult, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubListAPIKeysUseCase struct {
	called bool
	in     apikeyquery.ListInput
	out    []apikeyquery.APIKeyListItem
	err    error
}

func (s *stubListAPIKeysUseCase) List(ctx context.Context, in apikeyquery.ListInput) ([]apikeyquery.APIKeyListItem, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubRevokeAPIKeyUseCase struct {
	called      bool
	in          apikeycommand.RevokeInput
	err         error
	adminCalled bool
	adminID     string
	adminErr    error
}

func (s *stubRevokeAPIKeyUseCase) Revoke(ctx context.Context, in apikeycommand.RevokeInput) error {
	s.called = true
	s.in = in
	return s.err
}

func (s *stubRevokeAPIKeyUseCase) RevokeAsAdmin(ctx context.Context, id string) error {
	s.adminCalled = true
	s.adminID = id
	return s.adminErr
}

func newAPIKeyTestRouter(t *testing.T, h *Handler) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(middleware.WithTenantID(c.Request.Context(), "tenant1", middleware.TenantSourceAPIKey))
		c.Next()
	})
	h.Register(r)
	return r
}

func TestHandler_CreateAPIKey(t *testing.T) {
	uc := &stubCreateAPIKeyUseCase{
		out: apikeycommand.APIKeyCreateResult{
			ID:        "01HZX",
			Key:       "otj_abcdef1234567890123456",
			KeyPrefix: "otj_abcdef",
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		},
	}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetCreateAPIKeyUseCase(uc)

	router := newAPIKeyTestRouter(t, handler)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/tenants/tenant1/api_keys", bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusCreated {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusCreated, resp.Code, resp.Body.String())
	}
	if !uc.called {
		t.Fatal("expected use case to be called")
	}
	if uc.in.TenantID != "tenant1" {
		t.Fatalf("expected tenantID=%q, got %q", "tenant1", uc.in.TenantID)
	}

	var out apikeycommand.APIKeyCreateResult
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Key != "otj_abcdef1234567890123456" {
		t.Fatalf("expected key=%q, got %q", "otj_abcdef1234567890123456", out.Key)
	}
}

func TestHandler_CreateAPIKey_BindError(t *testing.T) {
	uc := &stubCreateAPIKeyUseCase{}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetCreateAPIKeyUseCase(uc)

	router := newAPIKeyTestRouter(t, handler)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/tenants//api_keys", bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
	if uc.called {
		t.Fatal("expected use case not to be called on bind error")
	}
}

func TestHandler_CreateAPIKey_BodyBindError(t *testing.T) {
	uc := &stubCreateAPIKeyUseCase{}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetCreateAPIKeyUseCase(uc)

	router := newAPIKeyTestRouter(t, handler)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/tenants/tenant1/api_keys", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
	if uc.called {
		t.Fatal("expected use case not to be called on bind error")
	}
}

func TestHandler_CreateAPIKey_UseCaseError(t *testing.T) {
	uc := &stubCreateAPIKeyUseCase{err: errors.New("db down")}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetCreateAPIKeyUseCase(uc)

	router := newAPIKeyTestRouter(t, handler)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/tenants/tenant1/api_keys", bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusInternalServerError, resp.Code)
	}
}

func TestHandler_ListAPIKeys(t *testing.T) {
	createdAt := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	uc := &stubListAPIKeysUseCase{
		out: []apikeyquery.APIKeyListItem{
			{ID: "01HZX", KeyPrefix: "otj_abc123", CreatedAt: createdAt},
		},
	}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetListAPIKeysUseCase(uc)

	router := newAPIKeyTestRouter(t, handler)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/tenants/tenant1/api_keys", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !uc.called {
		t.Fatal("expected use case to be called")
	}
	if uc.in.TenantID != "tenant1" {
		t.Fatalf("expected tenantID=%q, got %q", "tenant1", uc.in.TenantID)
	}

	var out apiKeyListResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out.Items))
	}
	if out.Items[0].ID != "01HZX" {
		t.Fatalf("expected id=%q, got %q", "01HZX", out.Items[0].ID)
	}
}

func TestHandler_ListAPIKeys_BindError(t *testing.T) {
	uc := &stubListAPIKeysUseCase{}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetListAPIKeysUseCase(uc)

	router := newAPIKeyTestRouter(t, handler)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/tenants//api_keys", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
	if uc.called {
		t.Fatal("expected use case not to be called on bind error")
	}
}

func TestHandler_ListAPIKeys_UseCaseError(t *testing.T) {
	uc := &stubListAPIKeysUseCase{err: errors.New("db down")}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetListAPIKeysUseCase(uc)

	router := newAPIKeyTestRouter(t, handler)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/tenants/tenant1/api_keys", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusInternalServerError, resp.Code)
	}
}

func TestHandler_RevokeAPIKey(t *testing.T) {
	uc := &stubRevokeAPIKeyUseCase{}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetRevokeAPIKeyUseCase(uc)

	router := newAPIKeyTestRouter(t, handler)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/api_keys/01HZX/revoke", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !uc.called {
		t.Fatal("expected use case to be called")
	}
	if uc.in.ID != "01HZX" {
		t.Fatalf("expected id=%q, got %q", "01HZX", uc.in.ID)
	}
	if uc.in.TenantID != "tenant1" {
		t.Fatalf("expected tenantID=%q, got %q", "tenant1", uc.in.TenantID)
	}
}

func TestHandler_RevokeAPIKey_NotFound(t *testing.T) {
	uc := &stubRevokeAPIKeyUseCase{err: &resource.NotFoundError{Resource: "api_key", ID: "01HZX"}}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetRevokeAPIKeyUseCase(uc)

	router := newAPIKeyTestRouter(t, handler)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/api_keys/01HZX/revoke", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusNotFound, resp.Code)
	}
}

func TestHandler_RevokeAPIKey_BindError(t *testing.T) {
	uc := &stubRevokeAPIKeyUseCase{}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetRevokeAPIKeyUseCase(uc)

	router := newAPIKeyTestRouter(t, handler)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/api_keys//revoke", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
	if uc.called {
		t.Fatal("expected use case not to be called on bind error")
	}
}

func TestHandler_RevokeAPIKey_AdminCrossTenant(t *testing.T) {
	uc := &stubRevokeAPIKeyUseCase{}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetRevokeAPIKeyUseCase(uc)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(middleware.WithTenantID(c.Request.Context(), "00000000000000000000000001", middleware.TenantSourceAPIKey))
		c.Next()
	})
	handler.Register(r)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/api_keys/01HZX/revoke", nil)
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !uc.adminCalled {
		t.Fatal("expected RevokeAsAdmin to be called for bootstrap tenant")
	}
	if uc.adminID != "01HZX" {
		t.Fatalf("expected id=%q, got %q", "01HZX", uc.adminID)
	}
	if uc.called {
		t.Fatal("expected Revoke not to be called for bootstrap tenant")
	}
}

func TestHandler_RevokeAPIKey_UseCaseError(t *testing.T) {
	uc := &stubRevokeAPIKeyUseCase{err: errors.New("db down")}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetRevokeAPIKeyUseCase(uc)

	router := newAPIKeyTestRouter(t, handler)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/api_keys/01HZX/revoke", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusInternalServerError, resp.Code)
	}
}

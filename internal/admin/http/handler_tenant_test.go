package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	tenantcommand "orbitjob/internal/admin/app/tenant/command"
	tenantquery "orbitjob/internal/admin/app/tenant/query"
	"orbitjob/internal/core/domain/tenant"
	"orbitjob/internal/domain/validation"
)

type stubCreateTenantUseCase struct {
	called bool
	in     tenantcommand.CreateInput
	out    tenantcommand.TenantCreateResult
	err    error
}

func (s *stubCreateTenantUseCase) Create(ctx context.Context, in tenantcommand.CreateInput) (tenantcommand.TenantCreateResult, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubListTenantsUseCase struct {
	called bool
	in     tenantquery.ListInput
	out    []tenantquery.TenantListItem
	err    error
}

func (s *stubListTenantsUseCase) List(ctx context.Context, in tenantquery.ListInput) ([]tenantquery.TenantListItem, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubGetTenantUseCase struct {
	called bool
	in     tenantquery.GetInput
	out    tenantquery.TenantGetResult
	err    error
}

func (s *stubGetTenantUseCase) Get(ctx context.Context, in tenantquery.GetInput) (tenantquery.TenantGetResult, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

func TestHandler_CreateTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubCreateTenantUseCase{
		out: tenantcommand.TenantCreateResult{
			ID:     "01HZX",
			Slug:   "acme",
			Name:   "Acme Corp",
			Status: tenant.StatusActive,
		},
	}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetCreateTenantUseCase(uc)

	router := testRouter(handler)

	body := `{"slug":"acme","name":"Acme Corp"}`
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/tenants", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusCreated {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusCreated, resp.Code, resp.Body.String())
	}
	if !uc.called {
		t.Fatal("expected use case to be called")
	}
	if uc.in.Slug != "acme" {
		t.Fatalf("expected slug=%q, got %q", "acme", uc.in.Slug)
	}
	if uc.in.Name != "Acme Corp" {
		t.Fatalf("expected name=%q, got %q", "Acme Corp", uc.in.Name)
	}

	var out tenantcommand.TenantCreateResult
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.ID != "01HZX" {
		t.Fatalf("expected id=%q, got %q", "01HZX", out.ID)
	}
}

func TestHandler_CreateTenant_BindError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubCreateTenantUseCase{}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetCreateTenantUseCase(uc)

	router := testRouter(handler)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/tenants", bytes.NewBufferString(`{}`))
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

func TestHandler_CreateTenant_UseCaseError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubCreateTenantUseCase{
		err: &validation.Error{Field: "slug", Message: "is required"},
	}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetCreateTenantUseCase(uc)

	router := testRouter(handler)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/tenants",
		bytes.NewBufferString(`{"slug":"","name":"Acme"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
}

func TestHandler_ListTenants(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubListTenantsUseCase{
		out: []tenantquery.TenantListItem{
			{ID: "01HZX", Slug: "acme", Name: "Acme Corp", Status: tenant.StatusActive},
		},
	}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetListTenantsUseCase(uc)

	router := testRouter(handler)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/tenants?limit=10&offset=5", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !uc.called {
		t.Fatal("expected use case to be called")
	}
	if uc.in.Limit != 10 {
		t.Fatalf("expected limit=%d, got %d", 10, uc.in.Limit)
	}
	if uc.in.Offset != 5 {
		t.Fatalf("expected offset=%d, got %d", 5, uc.in.Offset)
	}

	var out tenantListResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out.Items))
	}
	if out.Items[0].Slug != "acme" {
		t.Fatalf("expected slug=%q, got %q", "acme", out.Items[0].Slug)
	}
}

func TestHandler_ListTenants_UseCaseError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubListTenantsUseCase{err: errors.New("db down")}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetListTenantsUseCase(uc)

	router := testRouter(handler)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/tenants", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusInternalServerError, resp.Code)
	}
}

func TestHandler_GetTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubGetTenantUseCase{
		out: tenantquery.TenantGetResult{
			ID:     "01HZX",
			Slug:   "acme",
			Name:   "Acme Corp",
			Status: tenant.StatusActive,
		},
	}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetGetTenantUseCase(uc)

	router := testRouter(handler)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/tenants/01HZX", nil)
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
}

func TestHandler_GetTenant_BindError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubGetTenantUseCase{}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetGetTenantUseCase(uc)

	router := testRouter(handler)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/tenants/", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusNotFound, resp.Code)
	}
}

func TestHandler_GetTenant_UseCaseError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubGetTenantUseCase{err: errors.New("db down")}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetGetTenantUseCase(uc)

	router := testRouter(handler)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/tenants/01HZX", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusInternalServerError, resp.Code)
	}
}

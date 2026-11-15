package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunFiltersReachUseCase(t *testing.T) {
	uc := &stubListRunsUseCase{}
	_, router := newRunListHandler(uc, "")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet,
		"/api/v1/instances?source_uid=uid-1&occurrence_key=key-1&limit=2", nil))
	if resp.Code != http.StatusOK || uc.in.SourceUID != "uid-1" || uc.in.OccurrenceKey != "key-1" {
		t.Fatalf("filter delegation failed: %d %+v", resp.Code, uc.in)
	}
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet,
		"/api/v1/instances?occurrence_key="+strings.Repeat("x", 65), nil))
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("oversize key: %d", resp.Code)
	}
}

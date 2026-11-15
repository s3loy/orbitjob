package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDeferredExperimentRoutesAreUnavailable(t *testing.T) {
	router := gin.New()
	(&Handler{}).Register(router)
	for _, path := range []string{"/api/v1/experiments", "/api/v1/experiments/example", "/api/v1/experiments/example/credentials", "/api/v1/experiments/example/observations"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
			if response.Code != http.StatusNotFound {
				t.Fatalf("%s %s returned %d, want 404", method, path, response.Code)
			}
		}
	}
	for path := range ServiceOpenAPIDocument().Paths {
		if strings.HasPrefix(path, "/api/v1/experiments") {
			t.Fatalf("deferred route remains advertised: %s", path)
		}
	}
}

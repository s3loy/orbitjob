package console

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestConsoleBoundary(t *testing.T) {
	handler := NewHandler(fstest.MapFS{
		"index.html":             {Data: []byte("<html>OrbitJob</html>")},
		"assets/app-12345678.js": {Data: []byte("export {};")},
	})
	for _, tc := range []struct {
		method, url, accept string
		status              int
		html                bool
	}{
		{"GET", "/console/", "", 200, true},
		{"HEAD", "/console/", "", 200, false},
		{"GET", "/console/runs/123", "text/html", 200, true},
		{"GET", "/console/assets/app-12345678.js", "", 200, false},
		{"GET", "/console/assets/missing.js", "text/html", 404, false},
		{"GET", "/console/assets/missing", "text/html", 404, false},
		{"HEAD", "/console/assets/missing", "text/html", 404, false},
		{"GET", "/console/assets", "text/html", 404, false},
		{"GET", "/console/assets/", "text/html", 404, false},
		{"GET", "/api/v1/unknown", "text/html", 404, false},
		{"GET", "/console/../index.html", "text/html", 404, false},
		{"POST", "/console/", "", 405, false},
	} {
		t.Run(tc.method+tc.url, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.url, nil)
			req.Header.Set("Accept", tc.accept)
			resp := httptest.NewRecorder()
			handler.ServeHTTP(resp, req)
			if resp.Code != tc.status || strings.Contains(resp.Body.String(), "<html>") != tc.html {
				t.Fatalf("response %d: %s", resp.Code, resp.Body.String())
			}
			if resp.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("missing CSP")
			}
			if strings.Contains(tc.url, "app-") && !strings.Contains(resp.Header().Get("Cache-Control"), "immutable") {
				t.Fatal("hashed asset is not cached")
			}
		})
	}
}

func TestMissingBundleIsExplicit(t *testing.T) {
	resp := httptest.NewRecorder()
	NewHandler(fstest.MapFS{}).ServeHTTP(resp, httptest.NewRequest("GET", "/console/", nil))
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing bundle: %d", resp.Code)
	}
}

package http

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/logarchive"
	"orbitjob/internal/platform/scan"
)

type unicodeArchiveDirectory struct{ identity logarchive.Identity }

func (d unicodeArchiveDirectory) Resolve(_ context.Context, _ string, _ int64, _ int64, _ string) (logarchive.Identity, error) {
	return d.identity, nil
}

func (unicodeArchiveDirectory) RequestCleanup(context.Context, logarchive.Identity, string) error {
	return nil
}

type unicodeArchiveTransport struct{ handler http.Handler }

func (transport unicodeArchiveTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cert := &x509.Certificate{Subject: pkix.Name{CommonName: "orbitjob-log-reader"}}
	request.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
		VerifiedChains:   [][]*x509.Certificate{{cert}},
	}
	response := httptest.NewRecorder()
	transport.handler.ServeHTTP(response, request)
	return response.Result(), nil
}

func TestArchiveRoutesAcceptUnicodeSearchAtCharacterBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	functionRunID := uuid.NewString()
	for _, path := range []string{
		"/api/v1/instances/11/logs/archive",
		"/api/v1/instances/11/logs/archive/download",
		fmt.Sprintf("/api/v1/functions/9/runs/%s/logs/archive", functionRunID),
		fmt.Sprintf("/api/v1/functions/9/runs/%s/logs/archive/download", functionRunID),
	} {
		t.Run(path, func(t *testing.T) {
			store, err := logarchive.Open(t.TempDir(), logarchive.DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			identity := logarchive.Identity{
				TenantID: usecaseTestTenant, SourceUID: uuid.NewString(), OccurrenceKey: uuid.NewString(),
				Namespace: "test-execution", RunName: "unicode-search", RunUID: uuid.NewString(), RevisionID: 1, RunID: 11,
				FunctionID: 9, FunctionRunID: functionRunID,
			}
			if err := store.Register(identity); err != nil {
				t.Fatal(err)
			}
			observer := (&logarchive.Observer{Store: store}).Handler()
			client := &logarchive.Client{URL: "https://archive.invalid", HTTP: &http.Client{Transport: unicodeArchiveTransport{handler: observer}}}
			handler := NewHandler(nil, nil, nil)
			handler.SetArchiveBackend(unicodeArchiveDirectory{identity: identity}, client)
			functions := &fakeFunctionStore{}
			functions.seed(function.Definition{ID: 9, TenantID: usecaseTestTenant})
			handler.SetFunctionManagementUseCase(nil, functions, nil)
			router := gin.New()
			router.Use(testPrincipalMiddleware(usecaseTestTenant, scan.GenerateID(), ""))
			router.Use(func(c *gin.Context) {
				middleware.SetDocuments(c, []policy.Document{{Statement: []policy.Statement{{
					Effect: policy.EffectAllow, Action: []string{"instance:ReadLogs", "function:ReadLogs"}, Resource: []string{"orbitjob:*:*:*/*"},
				}}}})
			})
			handler.Register(router)
			for _, sample := range []struct {
				name, search string
				status       int
			}{
				{"ASCII boundary", strings.Repeat("a", 256), http.StatusOK},
				{"Chinese byte boundary", strings.Repeat("\u4e2d", 86), http.StatusOK},
				{"Chinese boundary", strings.Repeat("\u4e2d", 256), http.StatusOK},
				{"emoji boundary", strings.Repeat("\U0001F600", 256), http.StatusOK},
				{"ASCII oversized", strings.Repeat("a", 257), http.StatusBadRequest},
				{"Chinese oversized", strings.Repeat("\u4e2d", 257), http.StatusBadRequest},
				{"emoji oversized", strings.Repeat("\U0001F600", 257), http.StatusBadRequest},
			} {
				t.Run(sample.name, func(t *testing.T) {
					response := httptest.NewRecorder()
					router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path+"?search="+url.QueryEscape(sample.search), nil))
					if response.Code != sample.status {
						t.Fatalf("HTTP %d, want %d: %s", response.Code, sample.status, response.Body.String())
					}
				})
			}
		})
	}
}

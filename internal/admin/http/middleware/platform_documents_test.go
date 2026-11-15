package middleware

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"orbitjob/internal/core/domain/policy"
)

type platformLoaderProbe struct {
	tenantCalls, platformCalls int
	keyID                      string
}

func (p *platformLoaderProbe) EffectiveDocuments(context.Context, string, string) ([]policy.Document, error) {
	p.tenantCalls++
	return nil, nil
}
func (p *platformLoaderProbe) EffectivePlatformDocuments(_ context.Context, keyID string) ([]policy.Document, error) {
	p.platformCalls++
	p.keyID = keyID
	return nil, nil
}

type tenantOnlyLoader struct{ calls int }

func (p *tenantOnlyLoader) EffectiveDocuments(context.Context, string, string) ([]policy.Document, error) {
	p.calls++
	return nil, nil
}

func TestPlatformDocumentLoaderRequiresAuthenticatedPrincipalKind(t *testing.T) {
	for _, tc := range []struct {
		name, kind, tenant string
		platform           bool
	}{
		{"platform", KindPlatform, "", true}, {"tenant", KindTenant, "tenant", false},
		{"empty tenant is not platform", KindTenant, "", false}, {"malformed platform", KindPlatform, "tenant", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/?key_id=untrusted&tenant_id=selected", nil)
			loader := &platformLoaderProbe{}
			auth := Auth{Documents: loader}
			if err := auth.loadDocuments(c.Request.Context(), c, Principal{Kind: tc.kind, TenantID: tc.tenant, KeyID: "authenticated-id"}); err != nil {
				t.Fatal(err)
			}
			if tc.platform && loader.keyID != "authenticated-id" {
				t.Fatal("request selector replaced authenticated key ID")
			}
			if (loader.platformCalls == 1) != tc.platform || loader.platformCalls+loader.tenantCalls != 1 {
				t.Fatalf("platform=%d tenant=%d", loader.platformCalls, loader.tenantCalls)
			}
		})
	}
}
func TestPlatformDocumentLoaderAbsentFailsClosed(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/?key_id=untrusted&tenant_id=selected", nil)
	loader := &tenantOnlyLoader{}
	auth := Auth{Documents: loader}
	if err := auth.loadDocuments(c.Request.Context(), c, Principal{Kind: KindPlatform, KeyID: "authenticated"}); err != nil {
		t.Fatal(err)
	}
	if loader.calls != 0 {
		t.Fatal("platform fell back to tenant loader")
	}
}

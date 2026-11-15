package http

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	tenantquery "orbitjob/internal/admin/app/tenant/query"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/apikey"
	"orbitjob/internal/core/domain/policy"
)

func TestIdentityMetadataStaysInsideVerifiedWorkspace(t *testing.T) {
	for _, tc := range []struct {
		name, kind, target  string
		grant               bool
		status              int
		wantTenant, wantKey bool
	}{
		{name: "tenant without management permissions", kind: middleware.KindTenant, status: 200, wantTenant: true, wantKey: true},
		{name: "tenant explicit own", kind: middleware.KindTenant, target: "home", status: 200, wantTenant: true, wantKey: true},
		{name: "tenant foreign target denied", kind: middleware.KindTenant, target: "foreign", status: 403},
		{name: "platform no target", kind: middleware.KindPlatform, status: 200},
		{name: "platform target without grant", kind: middleware.KindPlatform, target: "home", status: 200},
		{name: "platform permitted target", kind: middleware.KindPlatform, target: "home", grant: true, status: 200, wantTenant: true},
		{name: "platform foreign target not granted", kind: middleware.KindPlatform, target: "foreign", grant: true, status: 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler(nil, nil, nil)
			tenant := &stubGetTenantUseCase{out: tenantquery.TenantGetResult{ID: "home", Name: "Operations", Slug: "operations"}}
			key := &stubKeyMetadata{out: apikey.Metadata{Name: "On-call operator"}}
			h.SetGetTenantUseCase(tenant)
			h.SetAPIKeyMetadataReader(key)
			mapping := map[string]string{"z-home": "home", "a-home": "home", "secret-foreign": "foreign"}
			h.SetWorkloadNamespaceTenants(mapping)
			mapping["injected"] = "home"
			r := gin.New()
			r.Use(func(c *gin.Context) {
				p := middleware.Principal{KeyID: "self-key", Kind: tc.kind}
				if tc.kind == middleware.KindTenant {
					p.TenantID = "home"
				}
				middleware.SetPrincipal(c, p)
				var docs []policy.Document
				if tc.grant {
					docs = []policy.Document{{Statement: []policy.Statement{{Effect: policy.EffectAllow, Action: []string{"tenant:Get"}, Resource: []string{"orbitjob:home:*:tenant/home"}}}}}
				}
				middleware.SetDocuments(c, docs)
				c.Next()
			})
			h.Register(r)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/identity?tenant_id="+tc.target, nil))
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if tenant.called != tc.wantTenant || key.called != tc.wantKey {
				t.Fatalf("unexpected reads tenant=%v key=%v", tenant.called, key.called)
			}
			if tc.status != 200 {
				return
			}
			var out IdentityResponse
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if out.WorkloadNamespaces == nil {
				t.Fatal("namespace array must be present")
			}
			if tc.wantTenant {
				if out.TenantName != "Operations" || out.TenantSlug != "operations" || !reflect.DeepEqual(out.WorkloadNamespaces, []string{"a-home", "z-home"}) {
					t.Fatalf("wrong scoped metadata %+v", out)
				}
				if tenant.in.ID != "home" || tenant.in.TenantID != "home" {
					t.Fatalf("unscoped tenant read %+v", tenant.in)
				}
			} else if out.TenantName != "" || out.TenantSlug != "" || len(out.WorkloadNamespaces) != 0 {
				t.Fatalf("foreign metadata leaked %+v", out)
			}
			if tc.wantKey {
				if out.KeyName != "On-call operator" || key.tenant != "home" || key.id != "self-key" {
					t.Fatalf("wrong own metadata %+v reader=%+v", out, key)
				}
			} else if out.KeyName != "" {
				t.Fatal("platform key label must not use a tenant lookup")
			}
		})
	}
}

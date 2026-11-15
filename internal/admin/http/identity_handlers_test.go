package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"orbitjob/internal/domain/resource"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/apikey"
	"orbitjob/internal/core/domain/policy"
)

type stubKeyOwner struct {
	tenant string
	err    error
}

func (s stubKeyOwner) FindKeyTenant(context.Context, string) (string, error) { return s.tenant, s.err }

func identityRouter(p *middleware.Principal, docs []policy.Document) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if p != nil {
			middleware.SetPrincipal(c, *p)
			middleware.SetDocuments(c, docs)
		}
		c.Next()
	})
	h := NewHandler(nil, nil, nil)
	h.Register(r)
	return r
}
func TestIdentitySafeDiscoveryAndExactCapabilities(t *testing.T) {
	p := middleware.Principal{KeyID: "key-1", Kind: middleware.KindPlatform}
	docs := []policy.Document{{Statement: []policy.Statement{{Effect: policy.EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:tenant-a:*:job/5"}}}}}
	r := identityRouter(&p, docs)
	for _, tc := range []struct {
		url     string
		status  int
		allowed bool
		count   int
	}{
		{"/api/v1/identity", 200, false, 0},
		{"/api/v1/identity?tenant_id=tenant-a&action=job:Get&resource=orbitjob:tenant-a:*:job/5", 200, true, 1},
		{"/api/v1/identity?tenant_id=tenant-a&action=job:Get&resource=orbitjob:tenant-a:*:job/6", 200, false, 1},
		{"/api/v1/identity?tenant_id=tenant-a&action=job:Get&resource=orbitjob:tenant-b:*:job/5", 400, false, 0},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", tc.url, nil))
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.url, w.Code, w.Body)
		}
		if tc.status == 200 {
			var out IdentityResponse
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if out.KeyID != "key-1" || len(out.Capabilities) != tc.count {
				t.Fatalf("unexpected identity %+v", out)
			}
			if tc.count > 0 && out.Capabilities[0].Allowed != tc.allowed {
				t.Fatalf("unexpected decision %+v", out)
			}
			if strings.Contains(w.Body.String(), "statements") {
				t.Fatal("policy leaked")
			}
		}
	}
	w := httptest.NewRecorder()
	identityRouter(nil, nil).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/identity", nil))
	if w.Code != 401 {
		t.Fatalf("unauthenticated %d", w.Code)
	}
}
func TestTenantTargetResolverGuards(t *testing.T) {
	for _, tc := range []struct {
		name, kind, tenant, query, body, want string
		denied                                bool
	}{
		{name: "platform explicit", kind: middleware.KindPlatform, query: "target", want: "target"},
		{name: "platform body", kind: middleware.KindPlatform, body: `{"tenant_id":"target"}`, want: "target"},
		{name: "platform missing", kind: middleware.KindPlatform, denied: true},
		{name: "tenant default", kind: middleware.KindTenant, tenant: "home", want: "home"},
		{name: "tenant foreign", kind: middleware.KindTenant, tenant: "home", query: "foreign", denied: true},
		{name: "conflicting selectors", kind: middleware.KindPlatform, query: "a", body: `{"tenant_id":"b"}`, denied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/?tenant_id="+tc.query, strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			middleware.SetPrincipal(c, middleware.Principal{Kind: tc.kind, TenantID: tc.tenant})
			arn, err := tenantScoped("job")(c)
			if (err != nil) != tc.denied {
				t.Fatalf("arn=%+v err=%v", arn, err)
			}
			if !tc.denied && arn.Tenant != tc.want {
				t.Fatalf("wrong tenant %+v", arn)
			}
			if !tc.denied {
				var raw map[string]any
				_ = json.NewDecoder(c.Request.Body).Decode(&raw)
				if tc.body != "" && raw["tenant_id"] != tc.want {
					t.Fatal("body consumed")
				}
			}
		})
	}
}
func TestRevokeOwnerResourceIsExactAndForeignMatchesMissing(t *testing.T) {
	h := NewHandler(nil, nil, nil)
	for _, tc := range []struct {
		kind, owner string
		lookupErr   error
		denied      bool
	}{
		{middleware.KindTenant, "home", nil, false}, {middleware.KindTenant, "foreign", nil, true}, {middleware.KindTenant, "", &resource.NotFoundError{Resource: "api_key", ID: "missing"}, true}, {middleware.KindPlatform, "foreign", nil, false},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/", nil)
		c.Params = gin.Params{{Key: "id", Value: "key-target"}}
		middleware.SetPrincipal(c, middleware.Principal{Kind: tc.kind, TenantID: "home"})
		h.SetAPIKeyOwnerReader(stubKeyOwner{tenant: tc.owner, err: tc.lookupErr})
		arn, err := h.revokeKeyResource(c)
		if (err != nil) != tc.denied {
			t.Fatalf("arn=%+v err=%v", arn, err)
		}
		if tc.denied {
			if err.Error() != "resource unavailable" {
				t.Fatal(err)
			}
			continue
		}
		if arn.ID != "key-target" || arn.Tenant != tc.owner || arn.Type != "apikey" {
			t.Fatalf("incorrect revoke resource %+v", arn)
		}
	}
}

func TestPlatformGuardAuthorizesActualTenantTarget(t *testing.T) {
	docs := []policy.Document{{Statement: []policy.Statement{{Effect: policy.EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:allowed:*:job/7"}}}}}
	for _, tc := range []struct {
		target string
		status int
	}{{"allowed", 200}, {"other", 403}, {"", 403}} {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			middleware.SetPrincipal(c, middleware.Principal{Kind: middleware.KindPlatform})
			middleware.SetDocuments(c, docs)
			c.Next()
		})
		r.GET("/jobs/:id", middleware.Require("job:Get", tenantScoped("job")), func(c *gin.Context) {
			tenant, ok := requireTenantID(c, "")
			if ok {
				c.String(200, tenant)
			}
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/jobs/7?tenant_id="+tc.target, nil))
		if w.Code != tc.status {
			t.Fatalf("target=%q status=%d body=%s", tc.target, w.Code, w.Body)
		}
		if w.Code == 200 && w.Body.String() != tc.target {
			t.Fatal("handler target differs")
		}
	}
}

func TestPolicyRawJSONOpenAPIIsAnObject(t *testing.T) {
	doc := ServiceOpenAPIDocument()
	schema := doc.Components.Schemas["CreatePolicyRequest"]
	field, ok := schema.Properties["document"]
	if !ok || field.Type != "object" || field.Items != nil {
		t.Fatalf("policy document must be a JSON object: %+v", field)
	}
}

func TestRevokeRouteUsesOwnerGrantAndPreservesDenials(t *testing.T) {
	for _, tc := range []struct {
		name, kind, owner, grant string
		missing                  bool
		status                   int
	}{
		{name: "own exact grant", kind: middleware.KindTenant, owner: "home", grant: "orbitjob:home:*:apikey/key-target", status: 200},
		{name: "wrong key grant", kind: middleware.KindTenant, owner: "home", grant: "orbitjob:home:*:apikey/another", status: 403},
		{name: "foreign explicit broad grant", kind: middleware.KindTenant, owner: "foreign", grant: "orbitjob:*:*:apikey/*", status: 200},
		{name: "missing key", kind: middleware.KindTenant, missing: true, grant: "orbitjob:*:*:apikey/*", status: 404},
		{name: "platform owner grant", kind: middleware.KindPlatform, owner: "foreign", grant: "orbitjob:foreign:*:apikey/key-target", status: 200},
		{name: "platform wrong owner grant", kind: middleware.KindPlatform, owner: "foreign", grant: "orbitjob:home:*:apikey/key-target", status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler(nil, nil, nil)
			uc := &stubRevokeAPIKeyUseCase{target: &apikey.APIKey{ID: "key-target", TenantID: tc.owner}}
			h.SetRevokeAPIKeyUseCase(uc)
			owner := stubKeyOwner{tenant: tc.owner}
			if tc.missing {
				owner.err = &resource.NotFoundError{Resource: "api_key", ID: "key-target"}
				uc.targetErr = owner.err
			}
			h.SetAPIKeyOwnerReader(owner)
			r := gin.New()
			r.Use(func(c *gin.Context) {
				middleware.SetPrincipal(c, middleware.Principal{Kind: tc.kind, TenantID: "home", KeyID: "actor"})
				middleware.SetDocuments(c, []policy.Document{{Statement: []policy.Statement{{Effect: policy.EffectAllow, Action: []string{"apikey:Revoke"}, Resource: []string{tc.grant}}}}})
				c.Next()
			})
			h.Register(r)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/api_keys/key-target/revoke", nil))
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if uc.called != (tc.status == 200) {
				t.Fatalf("use case called=%v", uc.called)
			}
			if uc.called && (uc.in.TenantID != tc.owner || uc.in.ActorID != "actor") {
				t.Fatalf("wrong revoke scope %+v", uc.in)
			}
		})
	}
}

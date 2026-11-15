package http

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"

	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/policy"
)

func TestIdentityManagementProbesDoNotSwitchTenantOrRevealMetadata(t *testing.T) {
	all := []policy.Document{{Statement: []policy.Statement{{Effect: policy.EffectAllow, Action: []string{"*"}, Resource: []string{"orbitjob:*:*:*/*"}}}}}
	for _, principal := range []middleware.Principal{{Kind: middleware.KindTenant, TenantID: usecaseTestTenant}, {Kind: middleware.KindPlatform}} {
		r := identityRouter(&principal, all)
		for _, probe := range []struct {
			action, arn string
			status      int
		}{
			{"tenant:Create", "orbitjob:*:*:tenant/*", 200},
			{"tenant:List", "orbitjob:*:*:tenant/*", 200},
			{"policy:Create", "orbitjob:" + usecaseTestTenantB + ":*:policy/*", 200},
			{"group:List", "orbitjob:" + usecaseTestTenantB + ":*:group/*", 200},
			{"apikey:Create", "orbitjob:" + usecaseTestTenantB + ":*:apikey/*", 200},
			{"job:Get", "orbitjob:" + usecaseTestTenantB + ":*:job/5", 400},
			{"policy:Get", "orbitjob:" + usecaseTestTenantB + ":*:policy/5", 400},
			{"policy:Create", "orbitjob:*:*:policy/*", 400},
			{"tenant:Create", "orbitjob:" + usecaseTestTenantB + ":*:tenant/*", 400},
		} {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/identity?action="+url.QueryEscape(probe.action)+"&resource="+url.QueryEscape(probe.arn), nil))
			if w.Code != probe.status {
				t.Fatalf("%s %s: %d %s", principal.Kind, probe.action, w.Code, w.Body)
			}
			if w.Code == 200 {
				var out IdentityResponse
				if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				if len(out.Capabilities) != 1 || !out.Capabilities[0].Allowed || out.TenantName != "" || out.TargetTenantID == usecaseTestTenantB {
					t.Fatalf("probe leaked/switched target: %+v", out)
				}
			}
		}
	}
	// Capability discovery evaluates Deny; it never manufactures permissions.
	p := middleware.Principal{Kind: middleware.KindTenant, TenantID: usecaseTestTenant}
	denied := append(all, policy.Document{Statement: []policy.Statement{{Effect: policy.EffectDeny, Action: []string{"policy:Create"}, Resource: []string{"orbitjob:*:*:policy/*"}}}})
	w := httptest.NewRecorder()
	identityRouter(&p, denied).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/identity?action=policy:Create&resource=orbitjob:"+usecaseTestTenantB+":*:policy/*", nil))
	var out IdentityResponse
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || len(out.Capabilities) != 1 || out.Capabilities[0].Allowed {
		t.Fatalf("deny ignored: %d %s", w.Code, w.Body)
	}
}

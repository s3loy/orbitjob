package http

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"

	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/policy"
)

func TestInstallationAccountManagementProbesMatchRouteAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, subject, action, resource string
		admin, deny, allowed            bool
	}{
		{"administrator creates workspace", "user", "tenant:Create", "orbitjob:*:*:tenant/*", true, false, true},
		{"administrator lists workspace metadata", "user", "tenant:List", "orbitjob:*:*:tenant/*", true, false, true},
		{"explicit deny remains authoritative", "user", "tenant:Create", "orbitjob:*:*:tenant/*", true, true, false},
		{"ordinary account", "user", "tenant:Create", "orbitjob:*:*:tenant/*", false, false, false},
		{"key does not become installation administrator", "api_key", "tenant:Create", "orbitjob:*:*:tenant/*", true, false, false},
		{"installation administration does not grant keys", "user", "apikey:Create", "orbitjob:00000000000000000000000001:*:apikey/*", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			principal := middleware.Principal{SubjectType: tc.subject, Kind: middleware.KindTenant, UserID: "user-id", InstallationAdmin: tc.admin}
			if tc.subject == "api_key" {
				principal.Kind = middleware.KindPlatform
			}
			var documents []policy.Document
			if tc.deny {
				documents = []policy.Document{{Statement: []policy.Statement{{Effect: policy.EffectDeny, Action: []string{tc.action}, Resource: []string{tc.resource}}}}}
			}
			query := url.Values{"action": {tc.action}, "resource": {tc.resource}}
			response := httptest.NewRecorder()
			identityRouter(&principal, documents).ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/identity?"+query.Encode(), nil))
			if response.Code != 200 {
				t.Fatalf("identity probe returned %d: %s", response.Code, response.Body)
			}
			var identity IdentityResponse
			if err := json.Unmarshal(response.Body.Bytes(), &identity); err != nil {
				t.Fatal(err)
			}
			if len(identity.Capabilities) != 1 || identity.Capabilities[0].Allowed != tc.allowed {
				t.Fatalf("capabilities = %+v", identity.Capabilities)
			}
		})
	}
}
